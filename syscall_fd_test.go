package wasihost

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

// newWMState creates a State with a single writable mount at guest path "/tmp"
// rooted at a fresh t.TempDir(). fd 3 is the preopen for "/tmp".
func newWMState(t *testing.T) (*State, []byte, string) {
	t.Helper()
	buf := make([]byte, 65536)
	tmpDir := t.TempDir()
	s := New(func() []byte { return buf },
		NewModuleConfig().WithHostDirectoryPreopen("/tmp", tmpDir),
	)
	return s, buf, tmpDir
}

// openHostFile opens a named file relative to fd 3 (the "/tmp" preopen) with
// read-only rights. Returns the allocated guest fd number.
func openHostFile(t *testing.T, s *State, buf []byte, pathOff int32, name string, fdPtr int32) int32 {
	t.Helper()
	copy(buf[pathOff:], name)
	errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(name)), 0, int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(%q) = %d, want ESUCCESS", name, errno)
	}
	return int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
}

// TestMem verifies that Mem() returns the slice provided by the mem callback.
func TestMem(t *testing.T) {
	t.Parallel()
	buf := make([]byte, 65536)
	s := New(func() []byte { return buf }, nil)
	got := s.Mem()
	if len(got) != len(buf) || &got[0] != &buf[0] {
		t.Error("Mem() did not return the slice from the mem callback")
	}
}

// TestAllocFD exercises allocFD slot allocation, slot reuse after fd_close,
// and slice growth beyond the initial capacity.
func TestAllocFD(t *testing.T) {
	t.Parallel()
	const (
		fdPtr   = 1000
		pathOff = 2000
	)

	s, buf, tmpDir := newWMState(t)

	// Open 3 files -> allocates fds 4, 5, 6 (0-2=stdio, 3=preopen).
	var openedFDs []int32
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("alloc%d.txt", i)
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		off := int32(pathOff + i*32)
		copy(buf[off:], name)
		errno := s.Xpath_open(dirfd, 0, off, int32(len(name)), 0, int64(rightFDRead), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(%q) = %d", name, errno)
		}
		openedFDs = append(openedFDs, int32(binary.LittleEndian.Uint32(buf[fdPtr:fdPtr+4])))
	}

	if openedFDs[0] != 4 {
		t.Errorf("first allocFD = %d, want 4", openedFDs[0])
	}

	// Close fd 4 and re-open -> slot 4 must be reused.
	if errno := s.Xfd_close(openedFDs[0]); errno != wasiESuccess {
		t.Fatalf("Xfd_close = %d", errno)
	}
	name := "reuse.txt"
	if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	copy(buf[pathOff:], name)
	errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(name)), 0, int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(reuse) = %d", errno)
	}
	if reused := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4])); reused != 4 {
		t.Errorf("reused fd = %d, want 4", reused)
	}

	// Slice growth: open 10 more files to exceed initial cap (8+1=9).
	for i := 0; i < 10; i++ {
		n := fmt.Sprintf("grow%d.txt", i)
		os.WriteFile(filepath.Join(tmpDir, n), []byte("g"), 0644) //nolint
		off := int32(pathOff + 100 + i*24)
		copy(buf[off:], n)
		s.Xpath_open(dirfd, 0, off, int32(len(n)), 0, int64(rightFDRead), 0, 0, fdPtr) // no panic = pass
	}
}

// TestWriteFilestat verifies the 64-byte filestat struct layout via Xfd_filestat_get.
func TestWriteFilestat(t *testing.T) {
	t.Parallel()
	const (
		statPtr = 1000
		pathOff = 2000
		fdPtr   = 3000
	)

	s, buf, tmpDir := newWMState(t)

	content := []byte("hello world")
	if err := os.WriteFile(filepath.Join(tmpDir, "stat.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}

	fd := openHostFile(t, s, buf, pathOff, "stat.txt", fdPtr)

	if errno := s.Xfd_filestat_get(fd, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_get(file) = %d", errno)
	}

	// filetype at offset 16 (uint64) == fdFile (4)
	ftype := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	if byte(ftype) != fdFile {
		t.Errorf("filetype = %d, want fdFile (%d)", ftype, fdFile)
	}
	// size at offset 32
	size := binary.LittleEndian.Uint64(buf[statPtr+32 : statPtr+40])
	if size != uint64(len(content)) {
		t.Errorf("size = %d, want %d", size, len(content))
	}
	// mtime at offset 40 > 0
	mtime := binary.LittleEndian.Uint64(buf[statPtr+40 : statPtr+48])
	if mtime == 0 {
		t.Error("mtime = 0, want > 0")
	}

	// Preopen dir fd (3) -> fdDir.
	if errno := s.Xfd_filestat_get(3, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_get(preopen) = %d", errno)
	}
	ftype = binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	if byte(ftype) != fdDir {
		t.Errorf("preopen filetype = %d, want fdDir (%d)", ftype, fdDir)
	}

	// Invalid fd -> wasiEBadf.
	if errno := s.Xfd_filestat_get(999, statPtr); errno != wasiEBadf {
		t.Errorf("invalid fd filestat_get = %d, want EBADF", errno)
	}
}

// TestFdWriteSeekCur verifies that Xfd_write on an osFile-backed fd uses
// Seek+Write (which syncs the OS kernel file position) instead of WriteAt
// (which bypasses it). Each sub-test independently opens the file and writes
// 100 bytes before checking Seek/Tell behavior.
func TestFdWriteSeekCur(t *testing.T) {
	t.Parallel()
	const (
		iovsOff     = 1024
		dataBuf     = 2048
		nwrittenOff = 512
		fdPtr       = 3000
		pathOff     = 4000
		seekPtr     = 6000
		tellPtr     = 7000
		seekResult  = 8000
	)

	// setupWrite creates an empty file, opens it with write+read+seek rights,
	// and writes 100 bytes before returning the fd for the test.
	setupEmptyWrite := func(t *testing.T) (*State, []byte, int32) {
		t.Helper()
		s, buf, tmpDir := newWMState(t)
		fname := "wseekcur.txt"
		if err := os.WriteFile(filepath.Join(tmpDir, fname), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		copy(buf[pathOff:], fname)
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
			int32(oflagTrunc), int64(rightFDWrite|rightFDRead|rightFDSeek), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// Write 100 bytes.
		for i := int32(0); i < 100; i++ {
			buf[dataBuf+i] = 'A'
		}
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 100)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("Xfd_write (100 bytes) = %d", errno)
		}
		if n := binary.LittleEndian.Uint32(buf[nwrittenOff : nwrittenOff+4]); n != 100 {
			t.Fatalf("nwritten = %d, want 100", n)
		}

		return s, buf, fd
	}

	t.Run("tell after write returns correct offset", func(t *testing.T) {
		s, buf, fd := setupEmptyWrite(t)
		if errno := s.Xfd_tell(fd, tellPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_tell = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[tellPtr : tellPtr+8]); pos != 100 {
			t.Errorf("tell = %d, want 100", pos)
		}
	})

	t.Run("seek cur backward after write returns correct offset", func(t *testing.T) {
		s, buf, fd := setupEmptyWrite(t)
		// Xfd_seek(-50, WHENCE_CUR) must return 50 because Xfd_write advanced
		// the OS kernel position (via Seek+Write) from 0 to 100.
		if errno := s.Xfd_seek(fd, -50, 1, seekPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_seek(-50, CUR) = %d, want ESUCCESS", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[seekPtr : seekPtr+8]); pos != 50 {
			t.Errorf("SEEK_CUR = %d, want 50", pos)
		}
	})

	t.Run("seek set then write updates tell correctly", func(t *testing.T) {
		s, buf, fd := setupEmptyWrite(t)
		// Seek to offset 0.
		if errno := s.Xfd_seek(fd, 0, 0, seekPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_seek(0, SET) = %d", errno)
		}
		// Write 50 bytes at offset 0.
		for i := int32(0); i < 50; i++ {
			buf[dataBuf+i] = 'B'
		}
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 50)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("Xfd_write (50 bytes) = %d", errno)
		}
		if n := binary.LittleEndian.Uint32(buf[nwrittenOff : nwrittenOff+4]); n != 50 {
			t.Fatalf("nwritten = %d, want 50", n)
		}
		// With Seek+Write, position should be at offset 50 (start of write + bytes written).
		if errno := s.Xfd_tell(fd, tellPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_tell = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[tellPtr : tellPtr+8]); pos != 50 {
			t.Errorf("tell after seek(0,SET)+write(50) = %d, want 50", pos)
		}
	})

	t.Run("fdFlagsAppend causes tell to reflect actual OS position", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "wappend.txt"
		if err := os.WriteFile(filepath.Join(tmpDir, fname), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		copy(buf[pathOff:], fname)
		// Open with fdFlagsAppend so writes go to the end regardless of seek.
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
			int32(oflagTrunc), int64(rightFDWrite|rightFDRead|rightFDSeek), 0, fdFlagsAppend, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(APPEND) = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// Write 2 bytes.
		copy(buf[dataBuf:], "AB")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 2)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("first Xfd_write = %d", errno)
		}

		// Seek to offset 0.
		if errno := s.Xfd_seek(fd, 0, 0, seekResult); errno != wasiESuccess {
			t.Fatalf("Xfd_seek(0, SET) = %d", errno)
		}

		// Write 2 more bytes; APPEND should write at end (offset 2).
		copy(buf[dataBuf:], "CD")
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("second Xfd_write = %d", errno)
		}

		// With Seek+Write + APPEND, the OS position after the second write is at
		// offset 4 (end of 4-byte file). Tell must reflect this.
		if errno := s.Xfd_tell(fd, tellPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_tell = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[tellPtr : tellPtr+8]); pos != 4 {
			t.Errorf("tell after APPEND seeks + writes = %d, want 4", pos)
		}
	})
}

// TestFdClose exercises fd_close success, double-close, preopen protection,
// and out-of-range fd.
func TestFdClose(t *testing.T) {
	t.Parallel()
	const (
		fdPtr   = 1000
		pathOff = 2000
	)

	t.Run("success and slot cleared", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		os.WriteFile(filepath.Join(tmpDir, "cl.txt"), []byte("x"), 0644) //nolint
		fd := openHostFile(t, s, buf, pathOff, "cl.txt", fdPtr)
		if errno := s.Xfd_close(fd); errno != wasiESuccess {
			t.Errorf("first close = %d, want ESUCCESS", errno)
		}
		if errno := s.Xfd_close(fd); errno != wasiEBadf {
			t.Errorf("second close = %d, want EBADF", errno)
		}
	})

	t.Run("preopen directory can be closed and invalidated", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)

		copy(buf[pathOff:], ".")
		if errno := s.Xpath_open(dirfd, 0, pathOff, 1, int32(oflagDir),
			int64(rightsWritableDirPreopen), int64(rightFDRead), 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open(scratch dir) = %d, want ESUCCESS", errno)
		}
		scratchFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		if errno := s.Xfd_close(3); errno != wasiESuccess {
			t.Fatalf("Xfd_close(preopen) = %d, want ESUCCESS", errno)
		}

		const statPtr = 4000
		if errno := s.Xfd_fdstat_get(3, statPtr); errno != wasiEBadf {
			t.Fatalf("Xfd_fdstat_get(closed preopen) = %d, want EBADF", errno)
		}

		if err := os.WriteFile(filepath.Join(tmpDir, "still-open.txt"), []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
		copy(buf[2000:], "still-open.txt")
		if errno := s.Xpath_open(scratchFD, 0, 2000, int32(len("still-open.txt")), 0, int64(rightFDRead), 0, 0, 5000); errno != wasiESuccess {
			t.Fatalf("Xpath_open(still-open.txt via scratch fd) = %d, want ESUCCESS", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[5000 : 5000+4]))
		if errno := s.Xfd_fdstat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get(other fd) = %d, want ESUCCESS", errno)
		}
	})

	t.Run("out-of-range fd", func(t *testing.T) {
		s, _, _ := newWMState(t)
		if errno := s.Xfd_close(-1); errno != wasiEBadf {
			t.Errorf("close -1 = %d, want EBADF", errno)
		}
		if errno := s.Xfd_close(9999); errno != wasiEBadf {
			t.Errorf("close 9999 = %d, want EBADF", errno)
		}
	})
}

// TestFdRead covers stdin, nil-stdin, osFile Seek+Read sequential reads, and EBADF.
func TestFdRead(t *testing.T) {
	t.Parallel()
	const (
		iovsOff  = 1024
		dataBuf  = 2048
		nreadOff = 512
		fdPtr    = 3000
		pathOff  = 4000
	)

	t.Run("stdin path", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, NewModuleConfig().WithStdin(strings.NewReader("hello")))
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_read(0, iovsOff, 1, nreadOff); errno != wasiESuccess {
			t.Fatalf("Xfd_read stdin = %d", errno)
		}
		if got := string(buf[dataBuf : dataBuf+5]); got != "hello" {
			t.Errorf("data = %q, want hello", got)
		}
		if n := binary.LittleEndian.Uint32(buf[nreadOff : nreadOff+4]); n != 5 {
			t.Errorf("nread = %d, want 5", n)
		}
	})

	t.Run("stdin nil returns success nread 0", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil) // no WithStdin
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_read(0, iovsOff, 1, nreadOff); errno != wasiESuccess {
			t.Errorf("nil stdin read = %d, want ESUCCESS", errno)
		}
		if n := binary.LittleEndian.Uint32(buf[nreadOff : nreadOff+4]); n != 0 {
			t.Errorf("nread nil stdin = %d, want 0", n)
		}
	})

	t.Run("osFile sequential read advances offset", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		os.WriteFile(filepath.Join(tmpDir, "rtest.txt"), []byte("ABCDEFGHIJ"), 0644) //nolint
		fd := openHostFile(t, s, buf, pathOff, "rtest.txt", fdPtr)

		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)

		if errno := s.Xfd_read(fd, iovsOff, 1, nreadOff); errno != wasiESuccess {
			t.Fatalf("first read = %d", errno)
		}
		if got := string(buf[dataBuf : dataBuf+4]); got != "ABCD" {
			t.Errorf("first read = %q, want ABCD", got)
		}
		// Second read continues from offset 4.
		if errno := s.Xfd_read(fd, iovsOff, 1, nreadOff); errno != wasiESuccess {
			t.Fatalf("second read = %d", errno)
		}
		if got := string(buf[dataBuf : dataBuf+4]); got != "EFGH" {
			t.Errorf("second read = %q, want EFGH", got)
		}
	})

	t.Run("EBADF invalid fd", func(t *testing.T) {
		s, buf, _ := newWMState(t)
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
		if errno := s.Xfd_read(999, iovsOff, 1, nreadOff); errno != wasiEBadf {
			t.Errorf("invalid fd read = %d, want EBADF", errno)
		}
	})

	t.Run("EBADF nil file slot", func(t *testing.T) {
		s, buf, _ := newWMState(t)
		insertTestFileAt(s, 5, &wasi.FileEntry{File: nil})
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
		if errno := s.Xfd_read(5, iovsOff, 1, nreadOff); errno != wasiEBadf {
			t.Errorf("nil file read = %d, want EBADF", errno)
		}
	})
}

// TestFdWrite covers stdout, stderr, nil-stdout, osFile Seek+Write, EBADF,
// and error/partial write reporting.
func TestFdWrite(t *testing.T) {
	t.Parallel()
	const (
		iovsOff     = 1024
		dataBuf     = 2048
		nwrittenOff = 512
		fdPtr       = 3000
		pathOff     = 4000
	)

	t.Run("stdout path", func(t *testing.T) {
		buf := make([]byte, 65536)
		var out bytes.Buffer
		s := New(func() []byte { return buf }, NewModuleConfig().WithStdout(&out))
		copy(buf[dataBuf:], "hello")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_write(1, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("Xfd_write stdout = %d", errno)
		}
		if got := out.String(); got != "hello" {
			t.Errorf("stdout = %q, want hello", got)
		}
		if n := binary.LittleEndian.Uint32(buf[nwrittenOff : nwrittenOff+4]); n != 5 {
			t.Errorf("nwritten = %d, want 5", n)
		}
	})

	t.Run("reports partial write before error", func(t *testing.T) {
		buf := make([]byte, 65536)
		// errorWriter will return n=2 then error
		ew := &errorWriter{err: fmt.Errorf("disk full"), n: 2}
		s := New(func() []byte { return buf }, NewModuleConfig().WithStdout(ew))

		copy(buf[dataBuf:], "hello") // 5 bytes
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)

		errno := s.Xfd_write(1, iovsOff, 1, nwrittenOff)
		if errno != wasiEIo {
			t.Errorf("errno = %d, want EIO (%d)", errno, wasiEIo)
		}

		nwritten := binary.LittleEndian.Uint32(buf[nwrittenOff : nwrittenOff+4])
		if nwritten != 2 {
			t.Errorf("nwritten = %d, want 2", nwritten)
		}
	})

	t.Run("stderr path", func(t *testing.T) {
		buf := make([]byte, 65536)
		var errOut bytes.Buffer
		s := New(func() []byte { return buf }, NewModuleConfig().WithStderr(&errOut))
		copy(buf[dataBuf:], "world")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_write(2, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("Xfd_write stderr = %d", errno)
		}
		if got := errOut.String(); got != "world" {
			t.Errorf("stderr = %q, want world", got)
		}
	})

	t.Run("stdout nil no panic", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil) // no WithStdout
		copy(buf[dataBuf:], "data")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
		if errno := s.Xfd_write(1, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Errorf("nil stdout write = %d, want ESUCCESS", errno)
		}
		if n := binary.LittleEndian.Uint32(buf[nwrittenOff : nwrittenOff+4]); n != 0 {
			t.Errorf("nwritten nil stdout = %d, want 0", n)
		}
	})

	t.Run("osFile sequential write", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "wtest.txt"
		os.WriteFile(filepath.Join(tmpDir, fname), []byte{}, 0644) //nolint
		copy(buf[pathOff:], fname)
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
			int32(oflagTrunc), int64(rightFDWrite|rightFDRead), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open write = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		copy(buf[dataBuf:], "XYZ")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 3)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("Xfd_write osFile = %d", errno)
		}
		data, _ := os.ReadFile(filepath.Join(tmpDir, fname))
		if string(data) != "XYZ" {
			t.Errorf("file content = %q, want XYZ", data)
		}
		const tellOff = 8000
		s.Xfd_tell(fd, tellOff)
		if pos := binary.LittleEndian.Uint64(buf[tellOff : tellOff+8]); pos != 3 {
			t.Errorf("tell after write = %d, want 3", pos)
		}
	})

	t.Run("EBADF invalid fd", func(t *testing.T) {
		s, buf, _ := newWMState(t)
		copy(buf[dataBuf:], "data")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
		if errno := s.Xfd_write(999, iovsOff, 1, nwrittenOff); errno != wasiEBadf {
			t.Errorf("invalid fd write = %d, want EBADF", errno)
		}
	})
}

// TestFdReadSeekCur verifies that Xfd_read on an osFile-backed fd uses
// Seek+Read (which syncs the OS kernel file position) instead of ReadAt
// (which bypasses it). Each sub-test independently opens the file and reads
// 4 bytes before checking Seek/Tell behavior.
func TestFdReadSeekCur(t *testing.T) {
	t.Parallel()
	const (
		iovsOff  = 1024
		dataBuf  = 2048
		nreadOff = 512
		fdPtr    = 3000
		pathOff  = 4000
		seekPtr  = 6000
		tellPtr  = 7000
	)

	setup := func(t *testing.T) (*State, []byte, int32) {
		t.Helper()
		s, buf, tmpDir := newWMState(t)
		if err := os.WriteFile(filepath.Join(tmpDir, "seekcur.txt"), []byte("ABCDEFGHIJ"), 0644); err != nil {
			t.Fatal(err)
		}
		fd := openHostFile(t, s, buf, pathOff, "seekcur.txt", fdPtr)

		// Read 4 bytes from offset 0.
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
		if errno := s.Xfd_read(fd, iovsOff, 1, nreadOff); errno != wasiESuccess {
			t.Fatalf("Xfd_read = %d", errno)
		}
		if got := string(buf[dataBuf : dataBuf+4]); got != "ABCD" {
			t.Fatalf("read data = %q, want ABCD", got)
		}
		return s, buf, fd
	}

	t.Run("tell after read returns correct offset", func(t *testing.T) {
		s, buf, fd := setup(t)
		if errno := s.Xfd_tell(fd, tellPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_tell = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[tellPtr : tellPtr+8]); pos != 4 {
			t.Errorf("tell = %d, want 4", pos)
		}
	})

	t.Run("seek cur after read returns correct offset", func(t *testing.T) {
		s, buf, fd := setup(t)
		// Xfd_seek(0, WHENCE_CUR) must return 4 because Xfd_read advanced
		// the OS kernel position (via Seek+Read) from 0 to 4.
		if errno := s.Xfd_seek(fd, 0, 1, seekPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_seek(0, CUR) = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[seekPtr : seekPtr+8]); pos != 4 {
			t.Errorf("SEEK_CUR = %d, want 4", pos)
		}
	})

	t.Run("seek beyond EOF succeeds", func(t *testing.T) {
		s, buf, fd := setup(t)
		// Seek(1000, WHENCE_CUR) after reading 4 bytes must go to offset 1004.
		if errno := s.Xfd_seek(fd, 1000, 1, seekPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_seek(1000, CUR) = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[seekPtr : seekPtr+8]); pos != 1004 {
			t.Errorf("SEEK_CUR = %d, want 1004", pos)
		}
	})

	t.Run("seek negative from cur returns EINVAL", func(t *testing.T) {
		s, buf, fd := setup(t)
		// Seek(-2000, WHENCE_CUR) would result in a negative offset, which
		// MUST return EINVAL and leave the fd offset unchanged at 4.
		if errno := s.Xfd_seek(fd, -2000, 1, seekPtr); errno != wasiEInval {
			t.Errorf("Xfd_seek(-2000, CUR) = %d, want EINVAL (%d)", errno, wasiEInval)
		}
		// entry.offset must stay at 4 after the failed seek.
		if errno := s.Xfd_tell(fd, tellPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_tell after failed seek = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[tellPtr : tellPtr+8]); pos != 4 {
			t.Errorf("tell after failed seek = %d, want 4", pos)
		}
	})
}

// TestFdSeek covers SEEK_SET, SEEK_CUR, SEEK_END, EISDIR, EINVAL, and EBADF.
func TestFdSeek(t *testing.T) {
	t.Parallel()
	const (
		resultPtr = 500
		fdPtr     = 1000
		pathOff   = 2000
	)

	setupSeekFile := func(t *testing.T) (*State, []byte, int32) {
		t.Helper()
		s, buf, tmpDir := newWMState(t)
		os.WriteFile(filepath.Join(tmpDir, "seek.txt"), []byte("ABCDEFGHIJ"), 0644) //nolint
		fd := openHostFile(t, s, buf, pathOff, "seek.txt", fdPtr)
		return s, buf, fd
	}

	t.Run("SEEK_SET", func(t *testing.T) {
		s, buf, fd := setupSeekFile(t)
		if errno := s.Xfd_seek(fd, 4, 0, resultPtr); errno != wasiESuccess {
			t.Fatalf("SEEK_SET = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[resultPtr : resultPtr+8]); pos != 4 {
			t.Errorf("SEEK_SET result = %d, want 4", pos)
		}
	})

	t.Run("SEEK_CUR", func(t *testing.T) {
		s, buf, fd := setupSeekFile(t)
		s.Xfd_seek(fd, 4, 0, resultPtr) // move to 4
		if errno := s.Xfd_seek(fd, 2, 1, resultPtr); errno != wasiESuccess {
			t.Fatalf("SEEK_CUR = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[resultPtr : resultPtr+8]); pos != 6 {
			t.Errorf("SEEK_CUR result = %d, want 6", pos)
		}
	})

	t.Run("SEEK_END", func(t *testing.T) {
		s, buf, fd := setupSeekFile(t)
		if errno := s.Xfd_seek(fd, -3, 2, resultPtr); errno != wasiESuccess {
			t.Fatalf("SEEK_END = %d", errno)
		}
		if pos := binary.LittleEndian.Uint64(buf[resultPtr : resultPtr+8]); pos != 7 {
			t.Errorf("SEEK_END result = %d, want 7", pos)
		}
	})

	t.Run("EISDIR on directory fd", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		os.Mkdir(filepath.Join(tmpDir, "adir"), 0755) //nolint
		copy(buf[pathOff:], "adir")
		errno := s.Xpath_open(dirfd, 0, pathOff, 4, int32(oflagDir), 0, 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open dir = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xfd_seek(fd, 0, 0, resultPtr); errno != wasiEIsdir {
			t.Errorf("seek dir = %d, want EISDIR", errno)
		}
	})

	t.Run("EINVAL non-seeker fd", func(t *testing.T) {
		// stubFSFile (defined in group_a_test.go) does not implement io.Seeker.
		s, _, _ := newWMState(t)
		insertStubFD(s, 5, wrapStubFSFile(stubFSFile{}), 0)
		if errno := s.Xfd_seek(5, 0, 0, resultPtr); errno != wasiEInval {
			t.Errorf("non-seeker seek = %d, want EINVAL", errno)
		}
	})

	t.Run("EBADF invalid fd", func(t *testing.T) {
		s, _, _ := newWMState(t)
		if errno := s.Xfd_seek(999, 0, 0, resultPtr); errno != wasiEBadf {
			t.Errorf("invalid fd seek = %d, want EBADF", errno)
		}
	})
}

// TestFdReaddir covers basic dirent listing, tiny buffer, preopen lazy load,
// and EBADF.
func TestFdReaddir(t *testing.T) {
	t.Parallel()
	const (
		rdBufPtr = 1000
		rdBufLen = 4096
		usedPtr  = 8000
		fdPtr    = 9000
		pathOff  = 9500
	)

	t.Run("lists entries and parses first dirent", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		os.Mkdir(filepath.Join(tmpDir, "sub"), 0755)                           //nolint
		os.WriteFile(filepath.Join(tmpDir, "sub", "a.txt"), []byte("a"), 0644) //nolint
		os.WriteFile(filepath.Join(tmpDir, "sub", "b.txt"), []byte("b"), 0644) //nolint

		copy(buf[pathOff:], "sub")
		errno := s.Xpath_open(dirfd, 0, pathOff, 3, int32(oflagDir), 0, 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open sub = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		errno = s.Xfd_readdir(fd, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_readdir = %d", errno)
		}
		used := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if used == 0 {
			t.Fatal("bufUsed = 0, expected entries")
		}
		// Parse first dirent: nameLen at offset 16, name at offset 24.
		nameLen := binary.LittleEndian.Uint32(buf[rdBufPtr+16 : rdBufPtr+20])
		if nameLen == 0 {
			t.Fatal("first dirent nameLen = 0")
		}
		name := string(buf[rdBufPtr+24 : rdBufPtr+24+int32(nameLen)])
		if name != "." && name != ".." && name != "a.txt" && name != "b.txt" {
			t.Errorf("first entry = %q, want ., .., a.txt, or b.txt", name)
		}
	})

	t.Run("buffer too small returns bufUsed 0", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		os.Mkdir(filepath.Join(tmpDir, "sub2"), 0755)                                  //nolint
		os.WriteFile(filepath.Join(tmpDir, "sub2", "longname.txt"), []byte("x"), 0644) //nolint

		copy(buf[pathOff:], "sub2")
		s.Xpath_open(dirfd, 0, pathOff, 4, int32(oflagDir), 0, 0, 0, fdPtr) //nolint
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// 1-byte buffer -- too small for any dirent.
		errno := s.Xfd_readdir(fd, rdBufPtr, 1, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("readdir tiny = %d", errno)
		}
		if used := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4]); used != 0 {
			t.Errorf("bufUsed = %d, want 0 for tiny buffer", used)
		}
	})

	t.Run("preopen lazy load does not panic", func(t *testing.T) {
		s, buf, _ := newWMState(t)
		// fd 3 is the preopen for "/tmp"; readdir lazy-loads on first call.
		errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess && errno != wasiEIo {
			t.Errorf("preopen readdir = %d, want ESUCCESS or EIO", errno)
		}
		_ = buf // used via s.mem closure
	})

	t.Run("cookies honor stream position across calls", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		// Create 3 files to ensure we have enough entries to split across calls.
		for _, name := range []string{"f1", "f2", "f3"} {
			if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}

		// fd 3 is preopen for tmpDir.
		const (
			rdBufPtr = 1000
			rdBufLen = 4096 // plenty of space
			usedPtr  = 8000
		)

		// First call: cookie=0, read some entries.
		// We expect f1, f2, f3...
		// First, get all entries to know what to expect.
		errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("first readdir = %d", errno)
		}
		used := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])

		type entry struct {
			next uint64
			name string
			dlen uint32
		}
		var entries []entry
		curr := uint32(rdBufPtr)
		for curr < rdBufPtr+used {
			next := binary.LittleEndian.Uint64(buf[curr : curr+8])
			nameLen := binary.LittleEndian.Uint32(buf[curr+16 : curr+20])
			name := string(buf[curr+24 : curr+24+nameLen])
			entries = append(entries, entry{next, name, 24 + nameLen})
			curr += 24 + nameLen
		}

		if len(entries) < 2 {
			t.Fatalf("expected at least 2 entries, got %d: %v", len(entries), entries)
		}

		// Second call: cookie = entries[0].next
		cookie := entries[0].next
		// Clear buffer to be sure.
		for i := uint32(rdBufPtr); i < rdBufPtr+rdBufLen; i++ {
			buf[i] = 0
		}

		errno = s.Xfd_readdir(3, rdBufPtr, rdBufLen, int64(cookie), usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("second readdir = %d", errno)
		}
		used2 := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if used2 == 0 {
			t.Fatal("second readdir returned 0 entries")
		}

		nameLen2 := binary.LittleEndian.Uint32(buf[rdBufPtr+16 : rdBufPtr+20])
		name2 := string(buf[rdBufPtr+24 : rdBufPtr+24+int32(nameLen2)])

		if name2 != entries[1].name {
			t.Errorf("second call (cookie=%d) got %q, want %q", cookie, name2, entries[1].name)
		}
	})

	t.Run("EBADF invalid fd", func(t *testing.T) {
		s, buf, _ := newWMState(t)
		if errno := s.Xfd_readdir(999, rdBufPtr, rdBufLen, 0, usedPtr); errno != wasiEBadf {
			t.Errorf("invalid fd readdir = %d, want EBADF", errno)
		}
		_ = buf
	})
}

// TestXpathFilestatGet exercises path_filestat_get for read-only mounts,
// writable mounts, and ENOENT.
func TestXpathFilestatGet(t *testing.T) {
	t.Parallel()
	const (
		pathOff = 1000
		statPtr = 2000
	)

	t.Run("read-only mount file", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf },
			NewModuleConfig().WithReadOnlyFS("/tmp", fstest.MapFS{
				"a.txt": &fstest.MapFile{Data: []byte("content")},
			}),
		)
		copy(buf[pathOff:], "a.txt")
		if errno := s.Xpath_filestat_get(3, 0, pathOff, 5, statPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_get ro = %d", errno)
		}
		ftype := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
		if byte(ftype) != fdFile {
			t.Errorf("filetype = %d, want fdFile (%d)", ftype, fdFile)
		}
	})

	t.Run("writable mount host file", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		content := []byte("real data")
		os.WriteFile(filepath.Join(tmpDir, "real.txt"), content, 0644) //nolint
		copy(buf[pathOff:], "real.txt")
		if errno := s.Xpath_filestat_get(3, 0, pathOff, 8, statPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_get writable = %d", errno)
		}
		size := binary.LittleEndian.Uint64(buf[statPtr+32 : statPtr+40])
		if size != uint64(len(content)) {
			t.Errorf("size = %d, want %d", size, len(content))
		}
	})

	t.Run("ENOENT missing path", func(t *testing.T) {
		s, buf, _ := newWMState(t)
		copy(buf[pathOff:], "missing.txt")
		if errno := s.Xpath_filestat_get(3, 0, pathOff, 11, statPtr); errno != wasiENoEnt {
			t.Errorf("missing = %d, want ENOENT", errno)
		}
	})
}

// TestXpathOpen covers /dev/null, read-only mount, writable mount, oflagCreat,
// directory open, and ENOENT.
func TestXpathOpen(t *testing.T) {
	t.Parallel()
	const (
		pathOff = 1000
		fdPtr   = 2000
		statPtr = 3000
	)

	t.Run("/dev/null", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil)
		p := "/dev/null"
		copy(buf[pathOff:], p)
		if errno := s.Xpath_open(0, 0, pathOff, int32(len(p)), 0, 0, 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open /dev/null = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		fe, ok := s.lookupFD(fd)
		if !ok {
			t.Fatal("lookupFD failed")
		}
		fdType, errno := fileEntryFdType(fe)
		if errno != 0 || fdType != fdCharDev {
			t.Errorf("fdType = %d, want fdCharDev (%d)", fdType, fdCharDev)
		}
	})

	t.Run("read-only mount file", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf },
			NewModuleConfig().WithReadOnlyFS("/tmp", fstest.MapFS{
				"a.txt": &fstest.MapFile{Data: []byte("hello")},
			}),
		)
		copy(buf[pathOff:], "a.txt")
		if errno := s.Xpath_open(3, 0, pathOff, 5, 0, int64(rightFDRead), 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open ro = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xfd_filestat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get = %d", errno)
		}
		if size := binary.LittleEndian.Uint64(buf[statPtr+32 : statPtr+40]); size == 0 {
			t.Error("size = 0, want > 0")
		}
	})

	t.Run("writable mount existing host file backed by osFile", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		os.WriteFile(filepath.Join(tmpDir, "host.txt"), []byte("data"), 0644) //nolint
		copy(buf[pathOff:], "host.txt")
		if errno := s.Xpath_open(dirfd, 0, pathOff, 8, 0, int64(rightFDRead), 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open host = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		fe, _ := s.lookupFD(fd)
		if _, ok := sysfs.UnderlyingOSFile(fe.File); !ok {
			t.Errorf("expected underlying os file, got %T", fe.File)
		}
	})

	t.Run("oflagCreat creates new file", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		copy(buf[pathOff:], "new.txt")
		errno := s.Xpath_open(dirfd, 0, pathOff, 7, int32(oflagCreat),
			int64(rightFDWrite|rightFDRead), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open creat = %d", errno)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "new.txt")); err != nil {
			t.Errorf("file not created: %v", err)
		}
	})

	t.Run("directory open stores fdDir", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		os.Mkdir(filepath.Join(tmpDir, "mydir"), 0755) //nolint
		copy(buf[pathOff:], "mydir")
		errno := s.Xpath_open(dirfd, 0, pathOff, 5, int32(oflagDir), 0, 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open dir = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		fe, ok := s.lookupFD(fd)
		if !ok {
			t.Fatal("lookupFD failed")
		}
		fdType, errno := fileEntryFdType(fe)
		if errno != 0 || fdType != fdDir {
			t.Errorf("fdType = %d, want fdDir (%d)", fdType, fdDir)
		}
	})

	t.Run("ENOENT not found", func(t *testing.T) {
		s, buf, _ := newWMState(t)
		copy(buf[pathOff:], "nonexistent.txt")
		if errno := s.Xpath_open(dirfd, 0, pathOff, 15, 0, int64(rightFDRead), 0, 0, fdPtr); errno != wasiENoEnt {
			t.Errorf("ENOENT = %d, want ENOENT", errno)
		}
	})
}

// TestPathOpenDistinguishesPermissionDeniedFromMissingOnReadOnlyFS verifies that
// path_open against a read-only fs.FS preopen maps fs.ErrPermission to EACCES and
// leaves missing paths as ENOENT.
func TestPathOpenDistinguishesPermissionDeniedFromMissingOnReadOnlyFS(t *testing.T) {
	t.Parallel()
	const (
		pathOff = 1100
		fdPtr   = 2100
	)
	buf := make([]byte, 65536)
	s := New(func() []byte { return buf },
		NewModuleConfig().WithReadOnlyFS("/data", errorFS{name: "secret", err: fs.ErrPermission}),
	)

	copy(buf[pathOff:], "secret")
	if errno := s.Xpath_open(3, 0, pathOff, 6, 0, int64(rightFDRead), 0, 0, fdPtr); errno != wasiEAcces {
		t.Fatalf("path_open(%q) = %d, want EACCES (%d)", "secret", errno, wasiEAcces)
	}

	copy(buf[pathOff:], "absent.txt")
	if errno := s.Xpath_open(3, 0, pathOff, 10, 0, int64(rightFDRead), 0, 0, fdPtr); errno != wasiENoEnt {
		t.Fatalf("path_open(%q) = %d, want ENOENT (%d)", "absent.txt", errno, wasiENoEnt)
	}
}

// TestPathOpenOnWritablePreopenMapsHostOpenErrorsToDistinctErrnos verifies that
// path_open on a writable host-backed preopen surfaces host open failures as
// distinct WASI errnos (EACCES, EISDIR, ENOENT, ENOTDIR) instead of collapsing
// unrelated errors to ENOENT or incorrectly succeeding via fs.FS fallback.
func TestPathOpenOnWritablePreopenMapsHostOpenErrorsToDistinctErrnos(t *testing.T) {
	t.Parallel()
	const (
		pathOff = 4200
		fdPtr   = 5200
	)

	cases := []struct {
		name string
		skip func(t *testing.T)
		prep func(t *testing.T, tmpDir string) (guestRelPath string)
		// path_open args (after dirfd, lookupFlags, pathPtr, pathLen).
		oflags    int32
		rights    int64
		wantErrno int32
	}{
		{
			name: "EACCES when host denies read on a mode-000 file",
			skip: func(t *testing.T) {
				if runtime.GOOS == "windows" {
					t.Skip("chmod does not reliably produce EACCES for this scenario on Windows")
				}
				if os.Geteuid() == 0 {
					t.Skip("EACCES is not reliably observable when running as root")
				}
			},
			prep: func(t *testing.T, tmpDir string) string {
				t.Helper()
				p := filepath.Join(tmpDir, "mode000.txt")
				if err := os.WriteFile(p, []byte("x"), 0o000); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(p, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
				return "mode000.txt"
			},
			oflags:    0,
			rights:    int64(rightFDRead),
			wantErrno: wasiEAcces,
		},
		{
			name: "EISDIR when opening a directory as a non-directory file with write",
			prep: func(t *testing.T, tmpDir string) string {
				t.Helper()
				if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0o755); err != nil {
					t.Fatal(err)
				}
				return "subdir"
			},
			oflags:    0,
			rights:    int64(rightFDWrite),
			wantErrno: wasiEIsdir,
		},
		{
			name: "ENOENT for a path that is actually missing without O_CREAT",
			prep: func(t *testing.T, _ string) string {
				t.Helper()
				return "surely-missing-path.txt"
			},
			oflags:    0,
			rights:    int64(rightFDRead),
			wantErrno: wasiENoEnt,
		},
		{
			name: "ENOTDIR when a path component is a regular file",
			prep: func(t *testing.T, tmpDir string) string {
				t.Helper()
				if err := os.WriteFile(filepath.Join(tmpDir, "leaf.txt"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				return "leaf.txt/beneath-file"
			},
			oflags:    0,
			rights:    int64(rightFDRead),
			wantErrno: wasiENotDir,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.skip != nil {
				tc.skip(t)
			}
			s, buf, tmpDir := newWMState(t)
			rel := tc.prep(t, tmpDir)
			copy(buf[pathOff:], rel)
			errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(rel)), tc.oflags, tc.rights, 0, 0, fdPtr)
			if errno != tc.wantErrno {
				t.Fatalf("Xpath_open(%q) errno=%d, want %d", rel, errno, tc.wantErrno)
			}
		})
	}
}

// TestPathOpenDirectoryFlagRejectsPlainFiles verifies that path_open with the
// WASI O_DIRECTORY oflag returns ENOTDIR for a regular file beneath a host
// preopen and leaves no usable fd, while opening a real directory succeeds and
// fd_fdstat_get reports a directory file type.
func TestPathOpenDirectoryFlagRejectsPlainFiles(t *testing.T) {
	t.Parallel()
	const (
		pathOff     = 1000
		fdFilePtr   = 2000
		fdSubdirPtr = 2100
		fdstatPtr   = 3000
	)

	s, buf, tmpDir := newWMState(t)

	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	copy(buf[pathOff:], "file.txt")
	binary.LittleEndian.PutUint32(buf[fdFilePtr:], 0xffffffff)

	errno := s.Xpath_open(dirfd, 0, pathOff, 8, int32(oflagDir), 0, 0, 0, fdFilePtr)
	if errno != wasiENotDir {
		t.Fatalf("Xpath_open regular file with O_DIRECTORY: errno=%d, want ENOTDIR (%d)", errno, wasiENotDir)
	}
	if got := int32(binary.LittleEndian.Uint32(buf[fdFilePtr : fdFilePtr+4])); got >= 0 {
		t.Fatalf("fd output after ENOTDIR: got fd %d, want invalid (negative)", got)
	}

	copy(buf[pathOff:], "subdir")
	errno = s.Xpath_open(dirfd, 0, pathOff, 6, int32(oflagDir), 0, 0, 0, fdSubdirPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open subdir with O_DIRECTORY: errno=%d, want ESUCCESS", errno)
	}
	subFD := int32(binary.LittleEndian.Uint32(buf[fdSubdirPtr : fdSubdirPtr+4]))
	if errno := s.Xfd_fdstat_get(subFD, fdstatPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(dir): errno=%d", errno)
	}
	if ft := buf[fdstatPtr]; ft != fdDir {
		t.Fatalf("fd_fdstat_get.fs_filetype = %d, want directory (%d)", ft, fdDir)
	}
}

// TestXpathRename covers success, EROFS on read-only mount, and ENOENT with no
// mounts.
func TestXpathRename(t *testing.T) {
	t.Parallel()
	const (
		srcOff = 100
		dstOff = 200
	)

	t.Run("success", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		src := filepath.Join(tmpDir, "src.txt")
		os.WriteFile(src, []byte("data"), 0644) //nolint
		srcPathOff, srcLen := writePath(buf, srcOff, "src.txt")
		dstPathOff, dstLen := writePath(buf, dstOff, "dst.txt")
		if errno := s.Xpath_rename(dirfd, srcPathOff, srcLen, dirfd, dstPathOff, dstLen); errno != wasiESuccess {
			t.Fatalf("Xpath_rename = %d", errno)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Error("src still exists after rename")
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "dst.txt")); err != nil {
			t.Errorf("dst not found: %v", err)
		}
	})

	t.Run("EROFS read-only mount", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, NewModuleConfig().WithReadOnlyFS("/tmp", fstest.MapFS{}))
		srcPathOff, srcLen := writePath(buf, srcOff, "a.txt")
		dstPathOff, dstLen := writePath(buf, dstOff, "b.txt")
		if errno := s.Xpath_rename(3, srcPathOff, srcLen, 3, dstPathOff, dstLen); errno != wasiEROFS {
			t.Errorf("EROFS = %d, want EROFS", errno)
		}
	})

	t.Run("ENOENT no mounts", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil)
		srcPathOff, srcLen := writePath(buf, srcOff, "a.txt")
		dstPathOff, dstLen := writePath(buf, dstOff, "b.txt")
		if errno := s.Xpath_rename(0, srcPathOff, srcLen, 0, dstPathOff, dstLen); errno != wasiENoEnt {
			t.Errorf("ENOENT = %d, want ENOENT", errno)
		}
	})
}

// TestXpollOneoff covers clock subscription, fd_read with valid/invalid fd,
// and multiple subscriptions.
func TestXpollOneoff(t *testing.T) {
	t.Parallel()
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 2000
	)

	t.Run("clock subscription", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil)
		// tag=0 (clock) at offset 8, timeout=1ns at offset 24
		buf[inPtr+8] = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], 1)
		if errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr); errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff clock = %d", errno)
		}
		if n := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4]); n != 1 {
			t.Errorf("nevents = %d, want 1", n)
		}
		if e := binary.LittleEndian.Uint16(buf[outPtr+8 : outPtr+10]); e != 0 {
			t.Errorf("event errno = %d, want 0", e)
		}
	})

	t.Run("fd_read valid fd", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 1)  // tag: fd_read
		binary.LittleEndian.PutUint32(buf[inPtr+16:], 0) // fd=0 (exists)
		if errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr); errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff fd_read = %d", errno)
		}
		if e := binary.LittleEndian.Uint16(buf[outPtr+8 : outPtr+10]); e != 0 {
			t.Errorf("event errno = %d, want 0", e)
		}
	})

	t.Run("fd_read invalid fd", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 1)   // tag: fd_read
		binary.LittleEndian.PutUint32(buf[inPtr+16:], 99) // invalid fd
		if errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr); errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff = %d", errno)
		}
		if n := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4]); n != 1 {
			t.Errorf("nevents = %d, want 1", n)
		}
		if e := binary.LittleEndian.Uint16(buf[outPtr+8 : outPtr+10]); e != uint16(wasiEBadf) {
			t.Errorf("event errno = %d, want EBADF (%d)", e, wasiEBadf)
		}
	})

	t.Run("multiple subscriptions", func(t *testing.T) {
		buf := make([]byte, 65536)
		s := New(func() []byte { return buf }, nil)
		// Sub 0: clock, 1ns
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 0)  // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+24:], 1) // timeout: 1ns
		// Sub 1: fd_read, fd=0
		binary.LittleEndian.PutUint32(buf[inPtr+48+8:], 1)  // tag: fd_read
		binary.LittleEndian.PutUint32(buf[inPtr+48+16:], 0) // fd=0
		if errno := s.Xpoll_oneoff(inPtr, outPtr, 2, neventsPtr); errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff multi = %d", errno)
		}
		if n := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4]); n != 2 {
			t.Errorf("nevents = %d, want 2", n)
		}
	})
}

// TestPathOpenRejectsNonDirectoryBaseFd verifies that path_open with a regular
// file fd as dirfd returns ENOTDIR or ENOTCAPABLE, not ENOENT.
func TestPathOpenRejectsNonDirectoryBaseFd(t *testing.T) {
	t.Parallel()
	const (
		pathOff = 1000
		fdPtr   = 2000
	)

	s, buf, tmpDir := newWMState(t)

	// Create a regular file under the writable preopen via path_open.
	copy(buf[pathOff:], "file")
	errno := s.Xpath_open(dirfd, 0, pathOff, 4, int32(oflagCreat), 0, 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(create file) = %d, want ESUCCESS", errno)
	}
	fileFd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// Attempt to open a relative path using the regular file fd as dirfd.
	copy(buf[pathOff:], "foo")
	errno = s.Xpath_open(fileFd, 0, pathOff, 3, int32(oflagCreat), 0, 0, 0, fdPtr)
	if errno != wasiENotDir && errno != wasiENotCap {
		t.Fatalf("Xpath_open with file as dirfd = %d, want ENOTDIR (%d) or ENOTCAPABLE (%d)", errno, wasiENotDir, wasiENotCap)
	}

	// Cleanup.
	if errno := s.Xfd_close(fileFd); errno != wasiESuccess {
		t.Fatalf("Xfd_close = %d", errno)
	}
	if err := os.Remove(filepath.Join(tmpDir, "file")); err != nil {
		t.Fatalf("remove file: %v", err)
	}
}

// TestFdAllocate verifies Xfd_allocate (fd_allocate / fallocate) behavior.
// The function should extend the file via Truncate when offset+length exceeds
// the current size, and leave it unchanged when the range is already within
// the current allocation.
func TestFdAllocate(t *testing.T) {
	const (
		fdPtr   = 1000
		pathOff = 2000
		statPtr = 3000
	)

	s, buf, tmpDir := newWMState(t)

	// Create an empty file.
	fname := "alloc.txt"
	if err := os.WriteFile(filepath.Join(tmpDir, fname), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	// Open with write + allocate rights. rightFDAllocate is in
	// rightsWritableDirPreopenInheriting and will be preserved by
	// fileRightsForOpen when explicitly requested.
	copy(buf[pathOff:], fname)
	errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
		int32(oflagTrunc), int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open = %d", errno)
	}
	fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// getSize returns the file size from Xfd_filestat_get.
	getSize := func() int64 {
		t.Helper()
		if errno := s.Xfd_filestat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get = %d", errno)
		}
		return int64(binary.LittleEndian.Uint64(buf[statPtr+filestatSizeOff : statPtr+filestatSizeOff+8]))
	}

	// Verify initial size is 0.
	if size := getSize(); size != 0 {
		t.Fatalf("initial file size = %d, want 0", size)
	}

	// 1. allocate(0, 100) on empty file -> size = 100
	t.Run("allocate from zero extends file", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 0, 100)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(0, 100) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 100 {
			t.Errorf("size after allocate(0,100) = %d, want 100", size)
		}
	})

	// 2. allocate(10, 10) within existing size -> no growth, size stays 100
	t.Run("allocate within existing size does not grow", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 10, 10)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(10, 10) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 100 {
			t.Errorf("size after allocate(10,10) = %d, want 100", size)
		}
	})

	// 3. allocate(90, 20) -> offset+length=110 > 100, size=110
	t.Run("allocate beyond existing size grows file", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 90, 20)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(90, 20) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 110 {
			t.Errorf("size after allocate(90,20) = %d, want 110", size)
		}
	})

	// 4. allocate(100, 100) -> offset+length=200 > 110, size=200
	t.Run("allocate far beyond existing size grows file further", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 100, 100)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(100, 100) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 200 {
			t.Errorf("size after allocate(100,100) = %d, want 200", size)
		}
	})

	// 5. invalid offset/length returns EINVAL and does not change the file.
	t.Run("negative offset returns EINVAL", func(t *testing.T) {
		s2, buf2, tmp2 := newWMState(t)
		if err := os.WriteFile(filepath.Join(tmp2, "bad-off.txt"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		const p = 4100
		copy(buf2[p:], "bad-off.txt")
		errno := s2.Xpath_open(dirfd, 0, p, 11, int32(oflagTrunc),
			int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d", errno)
		}
		badFd := int32(binary.LittleEndian.Uint32(buf2[fdPtr : fdPtr+4]))
		errno = s2.Xfd_allocate(badFd, -1, 10)
		if errno != wasiEInval {
			t.Errorf("Xfd_allocate(-1, 10) = %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	t.Run("negative length returns EINVAL", func(t *testing.T) {
		s2, buf2, tmp2 := newWMState(t)
		if err := os.WriteFile(filepath.Join(tmp2, "bad-len.txt"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		const p = 4200
		copy(buf2[p:], "bad-len.txt")
		errno := s2.Xpath_open(dirfd, 0, p, 11, int32(oflagTrunc),
			int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d", errno)
		}
		badFd := int32(binary.LittleEndian.Uint32(buf2[fdPtr : fdPtr+4]))
		errno = s2.Xfd_allocate(badFd, 0, -1)
		if errno != wasiEInval {
			t.Errorf("Xfd_allocate(0, -1) = %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	t.Run("offset plus length overflow returns EINVAL", func(t *testing.T) {
		s2, buf2, tmp2 := newWMState(t)
		if err := os.WriteFile(filepath.Join(tmp2, "bad-ovf.txt"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		const p = 4300
		copy(buf2[p:], "bad-ovf.txt")
		errno := s2.Xpath_open(dirfd, 0, p, 11, int32(oflagTrunc),
			int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d", errno)
		}
		badFd := int32(binary.LittleEndian.Uint32(buf2[fdPtr : fdPtr+4]))
		errno = s2.Xfd_allocate(badFd, math.MaxInt64, 1)
		if errno != wasiEInval {
			t.Errorf("Xfd_allocate(MaxInt64, 1) = %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	// 6. fd_allocate without FD_ALLOCATE in rights_base returns ENOTCAPABLE.
	t.Run("without FD_ALLOCATE returns ENOTCAPABLE", func(t *testing.T) {
		const woFdPtr = 4000
		copy(buf[pathOff:], fname)
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
			0, int64(rightFDWrite), 0, 0, woFdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(write-only) = %d", errno)
		}
		woFd := int32(binary.LittleEndian.Uint32(buf[woFdPtr : woFdPtr+4]))
		errno = s.Xfd_fdstat_get(woFd, statPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d", errno)
		}
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
		if rightsBase&rightFDAllocate != 0 {
			t.Fatalf("rightsBase = %#x, FD_ALLOCATE must not be granted", rightsBase)
		}
		errno = s.Xfd_allocate(woFd, 0, 64)
		if errno != wasiENotCap {
			t.Errorf("Xfd_allocate without FD_ALLOCATE = %d, want ENOTCAPABLE (%d)", errno, wasiENotCap)
		}
	})

	// 7. fsFile with FD_ALLOCATE returns ENOSYS (Truncate not implemented).
	t.Run("fsFile returns ENOSYS for allocate", func(t *testing.T) {
		const wrapFd = 5
		m := fstest.MapFS{"ro.txt": &fstest.MapFile{Data: []byte("hello")}}
		wf, openErrno := sysfs.OpenFSFile(m, "ro.txt", wasi.O_RDONLY, 0)
		if openErrno != 0 {
			t.Fatalf("OpenFSFile: %v", openErrno)
		}
		s2, _ := newTestState(nil)
		insertStubFD(s2, wrapFd, wf, rightFDAllocate)
		errno := s2.Xfd_allocate(wrapFd, 0, 10)
		if errno != wasiENoSys {
			t.Errorf("Xfd_allocate on fsFile = %d, want ENOSYS (%d)", errno, wasiENoSys)
		}
	})
}

// TestFdRenumberClosesDestination covers fd_renumber: closes the destination fd,
// copies the source fd table entry, and invalidates the source fd.
func TestFdRenumberClosesDestination(t *testing.T) {
	t.Parallel()
	const (
		fdPtr   = 1000
		pathOff = 2000
		statPtr = 3000
	)

	// Create a state with a writable preopen at "/tmp" backed by a temp dir.
	s, buf, tmpDir := newWMState(t)

	// Create two files with FD_READ|FD_WRITE using path_open CREAT.
	// We'll use fd 4 and fd 5 for the source and destination.
	const (
		file1 = "file1.txt"
		file2 = "file2.txt"
	)
	copy(buf[pathOff:], file1)
	if errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(file1)), int32(oflagCreat), int64(rightFDRead|rightFDWrite), 0, 0, fdPtr); errno != wasiESuccess {
		t.Fatalf("Xpath_open(%q) = %d, want ESUCCESS", file1, errno)
	}
	fdFrom := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	copy(buf[pathOff:], file2)
	if errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(file2)), int32(oflagCreat), int64(rightFDRead|rightFDWrite), 0, 0, fdPtr); errno != wasiESuccess {
		t.Fatalf("Xpath_open(%q) = %d, want ESUCCESS", file2, errno)
	}
	fdTo := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// Record fdstat of source fd (fd_from).
	if errno := s.Xfd_fdstat_get(fdFrom, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(fdFrom) = %d", errno)
	}
	recordedType := buf[statPtr]
	recordedFlags := binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4])
	recordedRightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	recordedRightsInheriting := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])

	prevDestFE, _ := s.lookupFD(fdTo)
	var prevDestFile wasi.File
	if prevDestFE != nil {
		prevDestFile = prevDestFE.File
	}

	// Call Xfd_renumber(fd_from, fd_to) should return ESUCCESS.
	if errno := s.Xfd_renumber(fdFrom, fdTo); errno != wasiESuccess {
		t.Fatalf("Xfd_renumber(fdFrom=%d, fdTo=%d) = %d, want ESUCCESS", fdFrom, fdTo, errno)
	}

	if prevDestFile != nil {
		if _, errno := prevDestFile.Read(make([]byte, 1)); errno == 0 {
			t.Fatal("destination open file still readable after renumber; fd_to was not closed")
		}
	}

	// Call Xfd_close(fd_from) should return EBADF (source fd invalidated).
	if errno := s.Xfd_close(fdFrom); errno != wasiEBadf {
		t.Fatalf("Xfd_close(fdFrom) = %d, want EBADF", errno)
	}

	// Call Xfd_fdstat_get(fd_to) should match the recorded fdstat.
	if errno := s.Xfd_fdstat_get(fdTo, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(fdTo) = %d", errno)
	}
	if gotType := buf[statPtr]; gotType != recordedType {
		t.Errorf("fd_to.fs_filetype = %d, want %d", gotType, recordedType)
	}
	if gotFlags := binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4]); gotFlags != recordedFlags {
		t.Errorf("fd_to.fs_flags = %d, want %d", gotFlags, recordedFlags)
	}
	if gotRightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16]); gotRightsBase != recordedRightsBase {
		t.Errorf("fd_to.fs_rights_base = 0x%x, want 0x%x", gotRightsBase, recordedRightsBase)
	}
	if gotRightsInheriting := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24]); gotRightsInheriting != recordedRightsInheriting {
		t.Errorf("fd_to.fs_rights_inheriting = 0x%x, want 0x%x", gotRightsInheriting, recordedRightsInheriting)
	}

	// Call Xfd_renumber(fd_to, fd_from) should return EBADF (destination already closed).
	if errno := s.Xfd_renumber(fdTo, fdFrom); errno != wasiEBadf {
		t.Fatalf("Xfd_renumber(fdTo, fdFrom) = %d, want EBADF", errno)
	}

	// Call Xfd_close(fd_to) should return ESUCCESS.
	if errno := s.Xfd_close(fdTo); errno != wasiESuccess {
		t.Fatalf("Xfd_close(fdTo) = %d, want ESUCCESS", errno)
	}

	// Verify host files are still unlinkable (host file system operations still work).
	// The files should still exist and be removable.
	if _, err := os.Stat(filepath.Join(tmpDir, file1)); err != nil {
		t.Fatalf("file1.txt missing after fd_renumber: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, file2)); err != nil {
		t.Fatalf("file2.txt missing after fd_renumber: %v", err)
	}
	// Clean up by removing the files.
	if err := os.Remove(filepath.Join(tmpDir, file1)); err != nil {
		t.Fatalf("failed to remove file1.txt: %v", err)
	}
	if err := os.Remove(filepath.Join(tmpDir, file2)); err != nil {
		t.Fatalf("failed to remove file2.txt: %v", err)
	}
}

func TestFdRenumberOverPreopen(t *testing.T) {
	const (
		pathOff = 2000
		fdPtr   = 1000
	)
	s, buf, tmpDir := newWMState(t)
	// Mimic open_scratch_directory: path_open(".") on preopen fd 3.
	copy(buf[pathOff:], ".")
	errno := s.Xpath_open(3, 0, pathOff, 1, int32(oflagDir), int64(rightFDRead|rightFDReaddir), int64(rightFDRead), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(.) = %d", errno)
	}
	baseDirFd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
	// Mimic create_tmp_dir: mkdir + path_open subdir with directory rights.
	const dirName = "overwrite_preopen_dir.cleanup"
	copy(buf[pathOff:], dirName)
	if errno = s.Xpath_create_directory(baseDirFd, pathOff, int32(len(dirName))); errno != wasiESuccess {
		t.Fatalf("Xpath_create_directory = %d", errno)
	}
	errno = s.Xpath_open(baseDirFd, 0, pathOff, int32(len(dirName)), int32(oflagDir),
		int64(rightsWritableDirPreopen), int64(rightFDRead|rightFDWrite|rightFDReaddir), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(subdir) = %d", errno)
	}
	dirFd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
	_ = tmpDir
	if errno := s.Xfd_renumber(dirFd, 3); errno != wasiESuccess {
		t.Fatalf("Xfd_renumber(dirFd, preopen) = %d, want ESUCCESS", errno)
	}
}

// TestFdRenumberToHighEmptySlot verifies fd_renumber can target an unallocated
// fd >= 64; FileTable grows via InsertAt rather than rejecting high slots.
func TestFdRenumberToHighEmptySlot(t *testing.T) {
	t.Parallel()
	s, _ := newTestState(nil)
	const (
		fdFrom int32 = 5
		fdTo   int32 = 100
	)
	insertStubFD(s, fdFrom, wrapStubFSFile(stubFSFile{}), rightFDRead)
	if errno := s.Xfd_renumber(fdFrom, fdTo); errno != wasiESuccess {
		t.Fatalf("Xfd_renumber(%d, %d) = %d, want ESUCCESS", fdFrom, fdTo, errno)
	}
	if _, ok := s.lookupFD(fdFrom); ok {
		t.Errorf("fd %d still open after renumber", fdFrom)
	}
	if _, ok := s.lookupFD(fdTo); !ok {
		t.Errorf("fd %d not open after renumber to empty high slot", fdTo)
	}
}

func TestFdFlags(t *testing.T) {
	t.Parallel()

	const (
		fdPtr       = 1000
		pathOff     = 2000
		iovsOff     = 3000
		dataBuf     = 4000
		nwrittenOff = 5000
		statPtr     = 6000
		tellOff     = 7000
	)

	// fd_flags values from WASI spec
	const (
		fdFlagsAppend   int32 = 1 << 0
		fdFlagsDSync    int32 = 1 << 1
		fdFlagsNonBlock int32 = 1 << 2
		fdFlagsRSync    int32 = 1 << 3
		fdFlagsSync     int32 = 1 << 4
	)

	t.Run("FDFLAGS_APPEND causes fd_write to append regardless of offset", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "append.txt"
		initialData := []byte("HELLO")
		if err := os.WriteFile(filepath.Join(tmpDir, fname), initialData, 0644); err != nil {
			t.Fatal(err)
		}

		copy(buf[pathOff:], fname)
		// Open with FDFLAGS_APPEND
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
			0, int64(rightFDWrite|rightFDRead|rightFDSeek), 0, fdFlagsAppend, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open append = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// Seek to middle of file
		var newOffPtr int32 = 8000
		if errno := s.Xfd_seek(fd, 2, 0, newOffPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_seek = %d", errno)
		}

		// Write "WORLD"
		copy(buf[dataBuf:], "WORLD")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("Xfd_write = %d", errno)
		}

		// Verify content is "HELLOWORLD" not "HEWORLO"
		data, err := os.ReadFile(filepath.Join(tmpDir, fname))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "HELLOWORLD" {
			t.Errorf("content = %q, want HELLOWORLD (append behavior)", string(data))
		}
	})

	t.Run("fd_fdstat_get reports stored fd flags", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "statflags.txt"
		os.WriteFile(filepath.Join(tmpDir, fname), []byte("x"), 0644)
		copy(buf[pathOff:], fname)

		// Open with APPEND and DSYNC
		requestedFlags := fdFlagsAppend | fdFlagsDSync
		s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(rightFDRead), 0, requestedFlags, fdPtr)
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		if errno := s.Xfd_fdstat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d", errno)
		}

		// fd_flags is uint16 at offset 2 of fdstat
		gotFlags := int32(binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4]))
		if gotFlags != requestedFlags {
			t.Errorf("got fd_flags = %d, want %d", gotFlags, requestedFlags)
		}
	})

	t.Run("fd_fdstat_set_flags updates supported flags and rejects unsupported", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "setflags.txt"
		os.WriteFile(filepath.Join(tmpDir, fname), []byte("x"), 0644)
		copy(buf[pathOff:], fname)
		s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(rightFDRead), 0, 0, fdPtr)
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// Set APPEND
		if errno := s.Xfd_fdstat_set_flags(fd, fdFlagsAppend); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_set_flags(APPEND) = %d", errno)
		}

		// Verify it was set
		s.Xfd_fdstat_get(fd, statPtr)
		gotFlags := int32(binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4]))
		if (gotFlags & fdFlagsAppend) == 0 {
			t.Error("APPEND flag was not set")
		}

		// Try to set an invalid/unsupported flag (WASI only allows setting APPEND, DSYNC, NONBLOCK, RSYNC, SYNC)
		// We use a bit that is definitely not in the spec for fdflags.
		const unknownFlag int32 = 1 << 15
		if errno := s.Xfd_fdstat_set_flags(fd, unknownFlag); errno != wasiEInval {
			t.Errorf("Xfd_fdstat_set_flags(unknown) = %d, want EInval", errno)
		}
	})
}

type readAtStub struct {
	wasi.UnimplementedFile
	readAt func([]byte, int64) (int, error)
}

func (r *readAtStub) Pread(buf []byte, off int64) (int, wasi.Errno) {
	n, err := r.readAt(buf, off)
	return n, unwrapLegacyIO(err)
}

type writeAtStub struct {
	wasi.UnimplementedFile
	writeAt func([]byte, int64) (int, error)
}

func (w *writeAtStub) Pwrite(buf []byte, off int64) (int, wasi.Errno) {
	n, err := w.writeAt(buf, off)
	return n, unwrapLegacyIO(err)
}

// noPosIOFile is a minimal fs.File without ReaderAt, ReadSeeker, or WriterAt.
type noPosIOFile struct{}

func (*noPosIOFile) Read([]byte) (int, error)   { return 0, io.EOF }
func (*noPosIOFile) Close() error               { return nil }
func (*noPosIOFile) Stat() (fs.FileInfo, error) { return noPosIOFileInfo{}, nil }

type noPosIOFileInfo struct{}

func (noPosIOFileInfo) Name() string       { return "f.txt" }
func (noPosIOFileInfo) Size() int64        { return 0 }
func (noPosIOFileInfo) Mode() fs.FileMode  { return 0 }
func (noPosIOFileInfo) ModTime() time.Time { return time.Time{} }
func (noPosIOFileInfo) IsDir() bool        { return false }
func (noPosIOFileInfo) Sys() any           { return nil }

type noPosIOFS struct{}

func (noPosIOFS) Open(name string) (fs.File, error) {
	if name == "f.txt" {
		return &noPosIOFile{}, nil
	}
	return nil, fs.ErrNotExist
}

type syncStub struct {
	wasi.UnimplementedFile
	syncCalled int
	syncErr    error
}

func (s *syncStub) Sync() wasi.Errno {
	s.syncCalled++
	if s.syncErr != nil {
		return wasi.EINVAL
	}
	return 0
}

func (s *syncStub) Datasync() wasi.Errno { return s.Sync() }

// setupOsFileFd creates a real OS-backed file fd entry at the given fd slot.
// Returns the file path for later verification.
func setupOsFileFd(t *testing.T, s *State, fdIdx int32, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/testfile"
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	insertTestFileAt(s, fdIdx, &wasi.FileEntry{
		File:       wrapOSFile(f),
		RightsBase: rightsRegularFile | rightFDAllocate,
	})
	return path
}

func TestPositionedIOSyscalls(t *testing.T) {
	const (
		iovDataOff = 1000 // where iov data is written into the guest buf
		iovPtrOff  = 2000 // where the iovec struct lives {bufPtr u32, bufLen u32}
		nresOff    = 3000 // nread / nwritten result pointer
		tellOff    = 4000 // Xfd_tell result pointer
	)

	// ── pread ─────────────────────────────────────────────────────────────
	t.Run("Xfd_pread reads at offset without advancing cursor", func(t *testing.T) {
		s, buf := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte("ABCDEFGHIJ"))

		// Build iovec at iovPtrOff: {bufPtr=iovDataOff, bufLen=4}
		binary.LittleEndian.PutUint32(buf[iovPtrOff:], uint32(iovDataOff))
		binary.LittleEndian.PutUint32(buf[iovPtrOff+4:], 4)

		errno := s.Xfd_pread(5, iovPtrOff, 1, 4, nresOff)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_pread returned %d, want ESUCCESS", errno)
		}
		got := string(buf[iovDataOff : iovDataOff+4])
		if got != "EFGH" {
			t.Errorf("Xfd_pread read %q, want %q", got, "EFGH")
		}
		nread := binary.LittleEndian.Uint32(buf[nresOff : nresOff+4])
		if nread != 4 {
			t.Errorf("nread = %d, want 4", nread)
		}
		// Tell must still be 0
		s.Xfd_tell(5, tellOff)
		offset := binary.LittleEndian.Uint64(buf[tellOff : tellOff+8])
		if offset != 0 {
			t.Errorf("fd_tell after pread = %d, want 0", offset)
		}
	})

	t.Run("Xfd_pread on stdin (fd=0) returns EINVAL", func(t *testing.T) {
		s, buf := newTestState(nil)
		binary.LittleEndian.PutUint32(buf[iovPtrOff:], uint32(iovDataOff))
		binary.LittleEndian.PutUint32(buf[iovPtrOff+4:], 4)
		errno := s.Xfd_pread(0, iovPtrOff, 1, 0, nresOff)
		if errno != wasiEInval {
			t.Errorf("got errno %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	// ── pwrite ────────────────────────────────────────────────────────────
	t.Run("Xfd_pwrite writes at offset without advancing cursor", func(t *testing.T) {
		s, buf := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte{}) // empty file

		// Put "XYZ" into the guest buffer at iovDataOff
		copy(buf[iovDataOff:], "XYZ")
		// Build iovec: {bufPtr=iovDataOff, bufLen=3}
		binary.LittleEndian.PutUint32(buf[iovPtrOff:], uint32(iovDataOff))
		binary.LittleEndian.PutUint32(buf[iovPtrOff+4:], 3)

		errno := s.Xfd_pwrite(5, iovPtrOff, 1, 3, nresOff)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_pwrite returned %d, want ESUCCESS", errno)
		}
		// File should now be "\x00\x00\x00XYZ"
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		want := "\x00\x00\x00XYZ"
		if string(data) != want {
			t.Errorf("file content %q, want %q", data, want)
		}
		// Tell must still be 0
		s.Xfd_tell(5, tellOff)
		offset := binary.LittleEndian.Uint64(buf[tellOff : tellOff+8])
		if offset != 0 {
			t.Errorf("fd_tell after pwrite = %d, want 0", offset)
		}
	})

	t.Run("Xfd_pwrite on stdout (fd=1) returns EINVAL", func(t *testing.T) {
		s, buf := newTestState(nil)
		copy(buf[iovDataOff:], "XYZ")
		binary.LittleEndian.PutUint32(buf[iovPtrOff:], uint32(iovDataOff))
		binary.LittleEndian.PutUint32(buf[iovPtrOff+4:], 3)
		errno := s.Xfd_pwrite(1, iovPtrOff, 1, 0, nresOff)
		if errno != wasiEInval {
			t.Errorf("got errno %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	// ── Group E stubs ─────────────────────────────────────────────────────
	t.Run("Xfd_datasync returns ESUCCESS", func(t *testing.T) {
		s, _ := newTestState(nil)

		t.Run("validates fd", func(t *testing.T) {
			errno := s.Xfd_datasync(99)
			if errno != wasiEBadf {
				t.Errorf("got %d, want EBADF (%d)", errno, wasiEBadf)
			}
		})
	})

	t.Run("Xfd_datasync invokes host Sync for sync-capable fds and maps errors", func(t *testing.T) {
		s, _ := newTestState(nil)

		t.Run("returns ESUCCESS and calls Sync once for sync-capable file", func(t *testing.T) {
			stub := &syncStub{}
			fd := allocStubFD(s, stub, 0)

			if errno := s.Xfd_datasync(fd); errno != wasiESuccess {
				t.Errorf("got %d, want ESUCCESS", errno)
			}
			if stub.syncCalled != 1 {
				t.Errorf("syncCalled = %d, want 1", stub.syncCalled)
			}
		})

		t.Run("returns mapped WASI errno when Sync fails (EINVAL -> EINVAL)", func(t *testing.T) {
			stub := &syncStub{syncErr: syscall.EINVAL}
			fd := allocStubFD(s, stub, 0)

			if errno := s.Xfd_datasync(fd); errno != wasiEInval {
				t.Errorf("got %d, want EINVAL (%d)", errno, wasiEInval)
			}
		})
	})

	t.Run("Xfd_datasync on fsFile-backed fd returns ESUCCESS", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/f.txt", []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		wf, errno := sysfs.OpenFSFile(os.DirFS(dir), "f.txt", wasi.O_RDONLY, 0)
		if errno != 0 {
			t.Fatalf("OpenFSFile: %v", errno)
		}
		t.Cleanup(func() { _ = wf.Close() }) // before dir removal (Windows)
		insertStubFD(s, 5, wf, 0)
		if errno := s.Xfd_datasync(5); errno != wasiESuccess {
			t.Errorf("got %d, want ESUCCESS", errno)
		}
	})

	t.Run("Xfd_advise returns ESUCCESS", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte("data"))
		if s.Xfd_advise(5, 0, 4, 0) != wasiESuccess {
			t.Error("Xfd_advise returned non-zero")
		}
	})

	t.Run("Xfd_allocate returns ESUCCESS when FD_ALLOCATE is granted", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte{})
		if errno := s.Xfd_allocate(5, 0, 16); errno != wasiESuccess {
			t.Errorf("Xfd_allocate = %d, want ESUCCESS", errno)
		}
	})

	t.Run("Xfd_allocate returns ENOTCAPABLE without FD_ALLOCATE", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte{})
		lookupFETest(s, 5).RightsBase &^= rightFDAllocate
		if errno := s.Xfd_allocate(5, 0, 16); errno != wasiENotCap {
			t.Errorf("Xfd_allocate without FD_ALLOCATE = %d, want ENOTCAPABLE (%d)", errno, wasiENotCap)
		}
	})

	t.Run("Xfd_fdstat_set_rights returns ESUCCESS", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte("data"))
		if s.Xfd_fdstat_set_rights(5, 0, 0) != wasiESuccess {
			t.Error("Xfd_fdstat_set_rights returned non-zero")
		}
	})

	t.Run("Xproc_raise returns ENOSYS", func(t *testing.T) {
		s, _ := newTestState(nil)
		if errno := s.Xproc_raise(9); errno != wasiENoSys {
			t.Errorf("got %d, want ENOSYS (%d)", errno, wasiENoSys)
		}
	})

	t.Run("Xsock_* functions return ENOSYS", func(t *testing.T) {
		s, buf := newTestState(nil)
		if s.Xsock_accept(5, 0, nresOff) != wasiENoSys {
			t.Error("Xsock_accept: want ENOSYS")
		}
		if s.Xsock_recv(5, iovPtrOff, 1, 0, nresOff, nresOff+4) != wasiENoSys {
			t.Error("Xsock_recv: want ENOSYS")
		}
		if s.Xsock_send(5, iovPtrOff, 1, 0, nresOff) != wasiENoSys {
			t.Error("Xsock_send: want ENOSYS")
		}
		if s.Xsock_shutdown(5, 0) != wasiEBadf {
			t.Error("Xsock_shutdown: want EBADF")
		}
		_ = buf
	})

	t.Run("Xfd_pread returns EIO and preserves partial byte count when ReadAt returns error", func(t *testing.T) {
		s, buf := newTestState(nil)

		// Build iovec: {bufPtr=iovDataOff, bufLen=10}
		binary.LittleEndian.PutUint32(buf[iovPtrOff:], uint32(iovDataOff))
		binary.LittleEndian.PutUint32(buf[iovPtrOff+4:], 10)

		// stubFile implements ReadAt and returns an error after partial read
		stub := &readAtStub{
			readAt: func(p []byte, off int64) (n int, err error) {
				copy(p, "PARTIAL")
				return 7, errors.New("real error")
			},
		}
		fd := allocStubFD(s, stub, rightFDRead)

		binary.LittleEndian.PutUint32(buf[nresOff:], 999) // poison
		errno := s.Xfd_pread(fd, iovPtrOff, 1, 0, nresOff)

		if errno != wasiEIo {
			t.Errorf("got errno %d, want EIO (%d)", errno, wasiEIo)
		}
		nread := binary.LittleEndian.Uint32(buf[nresOff : nresOff+4])
		if nread != 7 {
			t.Errorf("nread = %d, want 7", nread)
		}
		if got := string(buf[iovDataOff : iovDataOff+7]); got != "PARTIAL" {
			t.Errorf("buffer content = %q, want %q", got, "PARTIAL")
		}
	})

	t.Run("Positioned I/O on fsFile without ReaderAt/WriterAt returns ENOSYS", func(t *testing.T) {
		s, buf := newTestState(nil)
		wf, errno := sysfs.OpenFSFile(noPosIOFS{}, "f.txt", wasi.O_RDONLY, 0)
		if errno != 0 {
			t.Fatalf("OpenFSFile: %v", errno)
		}
		insertStubFD(s, 5, wf, rightFDRead|rightFDWrite)

		binary.LittleEndian.PutUint32(buf[iovPtrOff:], uint32(iovDataOff))
		binary.LittleEndian.PutUint32(buf[iovPtrOff+4:], 4)

		t.Run("Xfd_pread returns ENOSYS", func(t *testing.T) {
			binary.LittleEndian.PutUint32(buf[nresOff:], 999) // poison
			errno := s.Xfd_pread(5, iovPtrOff, 1, 0, nresOff)
			if errno != wasiENoSys {
				t.Errorf("got errno %d, want ENOSYS (%d)", errno, wasiENoSys)
			}
			nread := binary.LittleEndian.Uint32(buf[nresOff : nresOff+4])
			if nread != 0 {
				t.Errorf("nread = %d, want 0 on error", nread)
			}
		})

		t.Run("Xfd_pwrite returns ENOSYS", func(t *testing.T) {
			binary.LittleEndian.PutUint32(buf[nresOff:], 999) // poison
			errno := s.Xfd_pwrite(5, iovPtrOff, 1, 0, nresOff)
			if errno != wasiENoSys {
				t.Errorf("got errno %d, want ENOSYS (%d)", errno, wasiENoSys)
			}
			nwritten := binary.LittleEndian.Uint32(buf[nresOff : nresOff+4])
			if nwritten != 0 {
				t.Errorf("nwritten = %d, want 0 on error", nwritten)
			}
		})
	})

	t.Run("Xfd_pwrite returns EIO and preserves partial byte count when WriteAt returns error", func(t *testing.T) {
		s, buf := newTestState(nil)

		// Put "PARTIALDATA" into the guest buffer at iovDataOff
		copy(buf[iovDataOff:], "PARTIALDATA")
		// Build iovec: {bufPtr=iovDataOff, bufLen=11}
		binary.LittleEndian.PutUint32(buf[iovPtrOff:], uint32(iovDataOff))
		binary.LittleEndian.PutUint32(buf[iovPtrOff+4:], 11)

		var captured []byte
		var capturedOff int64
		// stubFile implements WriteAt and returns an error after partial write
		stub := &writeAtStub{
			writeAt: func(p []byte, off int64) (n int, err error) {
				captured = make([]byte, len(p))
				copy(captured, p)
				capturedOff = off
				return 7, errors.New("real error")
			},
		}
		fd := allocStubFD(s, stub, rightFDWrite)

		binary.LittleEndian.PutUint32(buf[nresOff:], 999) // poison
		errno := s.Xfd_pwrite(fd, iovPtrOff, 1, 100, nresOff)

		if errno != wasiEIo {
			t.Errorf("got errno %d, want EIO (%d)", errno, wasiEIo)
		}
		nwritten := binary.LittleEndian.Uint32(buf[nresOff : nresOff+4])
		if nwritten != 7 {
			t.Errorf("nwritten = %d, want 7", nwritten)
		}
		if string(captured) != "PARTIALDATA" {
			t.Errorf("captured content = %q, want %q", string(captured), "PARTIALDATA")
		}
		if capturedOff != 100 {
			t.Errorf("captured offset = %d, want 100", capturedOff)
		}
	})
}

// TestParseReaddirDirents verifies that the shared parseReaddirDirents helper
// in helpers_test.go correctly parses a synthetic WASI dirent buffer into
// ReaddirDirent structs. This test will fail to compile until the helper and
// the ReaddirDirent type are extracted from the duplicated parseEntries
// closures in filestat_ino_test.go.
func TestParseReaddirDirents(t *testing.T) {
	// Build a synthetic dirent buffer with two entries:
	//   1. "."   (dIno=100, dType=fdDir, dNamlen=1)
	//   2. "foo" (dIno=200, dType=fdFile, dNamlen=3)
	const bufPtr int32 = 0

	buf := make([]byte, 128)

	// Entry 1: "."
	off := bufPtr
	binary.LittleEndian.PutUint64(buf[off:off+8], 24+1)              // dNext: points past this entry
	binary.LittleEndian.PutUint64(buf[off+8:off+16], 100)            // dIno
	binary.LittleEndian.PutUint32(buf[off+16:off+20], 1)             // dNamlen
	binary.LittleEndian.PutUint32(buf[off+20:off+24], uint32(fdDir)) // dType
	copy(buf[off+24:off+25], ".")

	// Entry 2: "foo"
	off2 := bufPtr + 24 + 1                                             // 24 header + 1 name byte
	binary.LittleEndian.PutUint64(buf[off2:off2+8], 24+3)               // dNext
	binary.LittleEndian.PutUint64(buf[off2+8:off2+16], 200)             // dIno
	binary.LittleEndian.PutUint32(buf[off2+16:off2+20], 3)              // dNamlen
	binary.LittleEndian.PutUint32(buf[off2+20:off2+24], uint32(fdFile)) // dType
	copy(buf[off2+24:off2+27], "foo")

	bufUsed := uint32((off2 - bufPtr) + 24 + 3) // total bytes consumed

	entries := parseReaddirDirents(buf, bufUsed, bufPtr)

	if len(entries) != 2 {
		t.Fatalf("parseReaddirDirents returned %d entries, want 2", len(entries))
	}

	// Verify entry 0: "."
	if entries[0].Name != "." {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, ".")
	}
	if entries[0].Ino != 100 {
		t.Errorf("entries[0].Ino = %d, want 100", entries[0].Ino)
	}
	if entries[0].Type != fdDir {
		t.Errorf("entries[0].Type = %d, want fdDir (%d)", entries[0].Type, fdDir)
	}
	if entries[0].Namlen != 1 {
		t.Errorf("entries[0].Namlen = %d, want 1", entries[0].Namlen)
	}

	// Verify entry 1: "foo"
	if entries[1].Name != "foo" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "foo")
	}
	if entries[1].Ino != 200 {
		t.Errorf("entries[1].Ino = %d, want 200", entries[1].Ino)
	}
	if entries[1].Type != fdFile {
		t.Errorf("entries[1].Type = %d, want fdFile (%d)", entries[1].Type, fdFile)
	}
	if entries[1].Namlen != 3 {
		t.Errorf("entries[1].Namlen = %d, want 3", entries[1].Namlen)
	}
}
