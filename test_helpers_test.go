package wasihost

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

type stubFSFile struct{}

func (stubFSFile) Read(_ []byte) (int, error) { return 0, io.EOF }
func (stubFSFile) Stat() (fs.FileInfo, error) { return nil, nil }
func (stubFSFile) Close() error               { return nil }

type errorWriter struct {
	err error
	n   int
}

func (w *errorWriter) Write(p []byte) (n int, err error) {
	if w.n > 0 {
		n = w.n
		if n > len(p) {
			n = len(p)
		}
	}
	return n, w.err
}

func newTestState(cfg *ModuleConfig) (*State, []byte) {
	buf := make([]byte, 65536)
	s := New(func() []byte { return buf }, cfg)
	return s, buf
}

func insertTestFileAt(s *State, fd int32, fe *wasi.FileEntry) {
	if s.ctx == nil || s.ctx.FSContext() == nil {
		panic("insertTestFileAt requires ctx.FSContext")
	}
	if errno := s.ctx.FSContext().InsertFileAt(fe, fd); errno != 0 {
		panic(errno)
	}
}

type testLegacyFile struct {
	readFn  func([]byte) (int, error)
	writeFn func([]byte) (int, error)
	statFn  func() (fs.FileInfo, error)
	readAt  func([]byte, int64) (int, error)
	writeAt func([]byte, int64) (int, error)
}

func (f *testLegacyFile) Read(buf []byte) (int, wasi.Errno) {
	if f.readFn == nil {
		return 0, 0
	}
	n, err := f.readFn(buf)
	return n, unwrapLegacyIO(err)
}

func (f *testLegacyFile) Write(buf []byte) (int, wasi.Errno) {
	if f.writeFn == nil {
		return len(buf), 0
	}
	n, err := f.writeFn(buf)
	return n, unwrapLegacyIO(err)
}

func (f *testLegacyFile) Pread(buf []byte, off int64) (int, wasi.Errno) {
	if f.readAt == nil {
		return 0, wasi.ENOTSUP
	}
	n, err := f.readAt(buf, off)
	return n, unwrapLegacyIO(err)
}

func (f *testLegacyFile) Pwrite(buf []byte, off int64) (int, wasi.Errno) {
	if f.writeAt == nil {
		return 0, wasi.ENOTSUP
	}
	n, err := f.writeAt(buf, off)
	return n, unwrapLegacyIO(err)
}

func (f *testLegacyFile) Stat() (wasi.Stat_t, wasi.Errno) {
	if f.statFn == nil {
		return wasi.Stat_t{Mode: 0o100000}, 0
	}
	fi, err := f.statFn()
	if err != nil {
		return wasi.Stat_t{}, wasi.EIO
	}
	if fi == nil {
		return wasi.Stat_t{Mode: 0o100000}, 0
	}
	return wasi.NewStat_t(fi), 0
}

func (f *testLegacyFile) IsDir() (bool, wasi.Errno) {
	st, errno := f.Stat()
	if errno != 0 {
		return false, errno
	}
	return st.Mode&fs.ModeDir != 0, 0
}

func (f *testLegacyFile) Close() wasi.Errno { return 0 }

func (f *testLegacyFile) Dev() (uint64, wasi.Errno)               { return 0, 0 }
func (f *testLegacyFile) Ino() (wasi.Inode, wasi.Errno)           { return 0, 0 }
func (f *testLegacyFile) IsAppend() bool                          { return false }
func (f *testLegacyFile) SetAppend(bool) wasi.Errno               { return 0 }
func (f *testLegacyFile) Seek(int64, int) (int64, wasi.Errno)     { return 0, wasi.ENOTSUP }
func (f *testLegacyFile) Readdir(int) ([]wasi.Dirent, wasi.Errno) { return nil, wasi.ENOTSUP }
func (f *testLegacyFile) Truncate(int64) wasi.Errno               { return wasi.ENOTSUP }
func (f *testLegacyFile) Sync() wasi.Errno                        { return 0 }
func (f *testLegacyFile) Datasync() wasi.Errno                    { return 0 }
func (f *testLegacyFile) Utimens(int64, int64) wasi.Errno         { return wasi.ENOTSUP }

func unwrapLegacyIO(err error) wasi.Errno {
	if err == nil || err == io.EOF {
		return 0
	}
	return wasi.EIO
}

func insertStubFD(s *State, fd int32, file wasi.File, rightsBase uint64) {
	insertTestFileAt(s, fd, &wasi.FileEntry{File: file, RightsBase: rightsBase})
}

func allocStubFD(s *State, file wasi.File, rightsBase uint64) int32 {
	if s.ctx == nil || s.ctx.FSContext() == nil {
		panic("allocStubFD requires ctx.FSContext")
	}
	fd, errno := s.ctx.FSContext().AllocFile(&wasi.FileEntry{File: file, RightsBase: rightsBase})
	if errno != 0 {
		panic(errno)
	}
	return fd
}

// allocHostPipeReaderFD wraps the read end of an OS pipe as a Pollable host file.
// The returned *os.File is the write end; the caller must close it.
func allocHostPipeReaderFD(t *testing.T, s *State) (fd int32, writeEnd *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	path := fmt.Sprintf("/dev/fd/%d", r.Fd())
	wf, errno := sysfs.OpenOSFile(path, wasi.O_RDONLY, 0)
	if errno != 0 {
		_ = r.Close()
		_ = w.Close()
		t.Fatalf("OpenOSFile(%q): errno %d", path, errno)
	}
	rights := uint64(rightFDRead | rightPollFDReadwrite | rightFDFdstatSetFlags)
	return allocStubFD(s, wf, rights), w
}

// wrapOSFile opens a second, wasi.File handle on f's path and closes it when
// the test ends (before t.TempDir removal, which Windows needs).
func wrapOSFile(t testing.TB, f *os.File) wasi.File {
	t.Helper()
	wf, errno := sysfs.OpenOSFile(f.Name(), wasi.O_RDWR, 0)
	if errno != 0 {
		t.Fatalf("OpenOSFile(%q): errno %d", f.Name(), errno)
	}
	t.Cleanup(func() { _ = wf.Close() })
	return wf
}

func emptyMapFS() fs.FS { return fstest.MapFS{} }

func setupWritableMount(t *testing.T, s *State, buf []byte) string {
	t.Helper()
	dir := t.TempDir()
	_ = buf
	installPreopenAt(t, s, 3, "tmp", dir, true)
	return dir
}

func installPreopenAt(t *testing.T, s *State, fd int32, guestPath, hostDir string, writable bool) {
	t.Helper()
	var fsys wasi.FS = sysfs.DirFS{Path: hostDir}
	if !writable {
		fsys = sysfs.AdaptFS{FS: os.DirFS(hostDir)}
	}
	wf, errno := fsys.OpenFile(".", wasi.O_RDONLY|wasi.O_DIRECTORY, 0)
	if errno != 0 {
		t.Fatalf("OpenFile preopen: %v", errno)
	}
	// Close the handle before t.TempDir's cleanup removes hostDir; Windows
	// cannot delete a directory that is still open.
	t.Cleanup(func() { _ = wf.Close() })
	rb := rightsReadOnlyDirPreopen
	ri := rightsReadOnlyDirPreopen
	if writable {
		rb = rightsWritableDirPreopenBase
		ri = rightsWritableDirPreopenInheriting
	}
	fe := &wasi.FileEntry{
		FS:               fsys,
		Name:             guestPath,
		GuestPath:        guestPath,
		IsPreopen:        true,
		File:             wf,
		RightsBase:       rb,
		RightsInheriting: ri,
	}
	if existing, ok := s.lookupFD(fd); ok {
		*existing = *fe
		return
	}
	insertTestFileAt(s, fd, fe)
}

func lookupFETest(s *State, fd int32) *wasi.FileEntry {
	fe, ok := s.lookupFD(fd)
	if !ok {
		panic("lookupFETest: fd not open")
	}
	return fe
}

func wrapStubFSFile(stub stubFSFile) wasi.File {
	return &testLegacyFile{readFn: stub.Read, statFn: stub.Stat}
}
