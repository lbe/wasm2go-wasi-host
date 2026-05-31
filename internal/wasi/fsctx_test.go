// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
)

// osDirFS wraps an os.DirFS as a minimal wasi.FS for testing purposes.
// Defined locally to avoid the import cycle: wasi_test → sysfs → wasi.
type osDirFS struct {
	FS fs.FS
}

func (o osDirFS) PathFor() string {
	return fmt.Sprintf("%v", o.FS)
}

func (o osDirFS) OpenFile(p string, flag Oflag, perm fs.FileMode) (File, Errno) {
	cleaned := p
	if len(cleaned) > 0 && cleaned[0] == '/' {
		cleaned = cleaned[1:]
	}
	cleaned = path.Clean(cleaned)
	f, err := o.FS.Open(cleaned)
	if err != nil {
		return nil, UnwrapOSError(err)
	}
	return &testFile{file: f}, 0
}

func (o osDirFS) Lstat(p string) (Stat_t, Errno) {
	return o.Stat(p)
}

func (o osDirFS) Stat(p string) (Stat_t, Errno) {
	f, errno := o.OpenFile(p, O_RDONLY, 0)
	if errno != 0 {
		return Stat_t{}, errno
	}
	defer f.Close()
	return f.Stat()
}

func (osDirFS) Mkdir(string, fs.FileMode) WasiErrno    { return ENOSYS }
func (osDirFS) Chmod(string, fs.FileMode) WasiErrno    { return ENOSYS }
func (osDirFS) Rename(string, string) WasiErrno        { return ENOSYS }
func (osDirFS) Rmdir(string) WasiErrno                 { return ENOSYS }
func (osDirFS) Link(string, string) WasiErrno          { return ENOSYS }
func (osDirFS) Symlink(string, string) WasiErrno       { return ENOSYS }
func (osDirFS) Unlink(string) WasiErrno                { return ENOSYS }
func (osDirFS) Readlink(string) (string, WasiErrno)    { return "", ENOSYS }
func (osDirFS) Utimens(string, int64, int64) WasiErrno { return ENOSYS }

// Errno alias for local adapter methods to avoid wasi.Errno on unimpl methods.
type WasiErrno = Errno

// testFile wraps an fs.File to implement wasi.File for testing.
type testFile struct {
	file fs.File
}

func (f *testFile) Dev() (uint64, Errno) { return 0, 0 }
func (f *testFile) Ino() (Inode, Errno)  { return 0, 0 }
func (f *testFile) IsDir() (bool, Errno) {
	fi, err := f.file.Stat()
	return fi.IsDir(), UnwrapOSError(err)
}
func (f *testFile) IsAppend() bool       { return false }
func (f *testFile) SetAppend(bool) Errno { return 0 }
func (f *testFile) Stat() (Stat_t, Errno) {
	if sf, ok := f.file.(interface{ Stat() (fs.FileInfo, error) }); ok {
		info, err := sf.Stat()
		return NewStat_t(info), UnwrapOSError(err)
	}
	return Stat_t{}, ENOSYS
}
func (f *testFile) Read(b []byte) (int, Errno) {
	n, err := f.file.Read(b)
	return n, UnwrapOSError(err)
}
func (f *testFile) Pread(b []byte, off int64) (int, Errno) {
	if ra, ok := f.file.(interface {
		ReadAt([]byte, int64) (int, error)
	}); ok {
		n, err := ra.ReadAt(b, off)
		return n, UnwrapOSError(err)
	}
	return 0, ENOSYS
}
func (f *testFile) Seek(off int64, whence int) (int64, Errno) {
	if s, ok := f.file.(interface {
		Seek(int64, int) (int64, error)
	}); ok {
		n, err := s.Seek(off, whence)
		return n, UnwrapOSError(err)
	}
	return 0, ENOSYS
}
func (f *testFile) Readdir(n int) ([]Dirent, Errno) {
	return nil, ENOSYS
}
func (f *testFile) Write(b []byte) (int, Errno)             { return 0, EBADF }
func (f *testFile) Pwrite(b []byte, off int64) (int, Errno) { return 0, EBADF }
func (f *testFile) Truncate(int64) Errno                    { return EBADF }
func (f *testFile) Sync() Errno                             { return 0 }
func (f *testFile) Datasync() Errno                         { return 0 }
func (f *testFile) Utimens(int64, int64) Errno              { return 0 }
func (f *testFile) Close() Errno {
	return UnwrapOSError(f.file.Close())
}

// TestFSContextManagesFdTable verifies that FSContext correctly manages an fd
// table with init, open, close, renumber, and lookup operations, and that
// DirentCache dot-entry synthesis works during readdir.
func TestFSContextManagesFdTable(t *testing.T) {
	dirFS := osDirFS{FS: os.DirFS("testdata")}
	var buf bytes.Buffer

	// InitFSContext with DirFS and guestPath "/" → preopen at fd 3
	fsc, err := InitFSContext(nil, &buf, &buf, []FS{dirFS}, []string{"/"})
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}

	// fd 0, 1, 2 should be stdio; fd 3 should be preopen
	fe3, ok := fsc.LookupFile(3)
	if !ok {
		t.Fatal("LookupFile(3): preopen dir not found at fd 3")
	}
	if !fe3.IsPreopen {
		t.Fatal("LookupFile(3): entry is not marked IsPreopen")
	}

	// OpenFile on the preopen opens "animals.txt" → fd 4, can Pread contents
	fd4, errno := fsc.OpenFile(dirFS, "animals.txt", O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("OpenFile(animals.txt): unexpected errno %d", errno)
	}
	if fd4 != 4 {
		t.Fatalf("OpenFile(animals.txt): got fd %d, want 4", fd4)
	}

	fe4, ok := fsc.LookupFile(fd4)
	if !ok {
		t.Fatalf("LookupFile(%d): animals.txt not found", fd4)
	}

	// Pread contents of animals.txt
	buf4 := make([]byte, 1024)
	n, errno := fe4.File.Pread(buf4, 0)
	if errno != 0 {
		t.Fatalf("Pread: unexpected errno %d", errno)
	}
	got := string(buf4[:n])
	want := "dog\ncat\nbird\nfish\ncow\n"
	if got != want {
		t.Errorf("Pread contents:\n  got:  %q\n  want: %q", got, want)
	}

	// LookupFile(fd 4) returns the FileEntry
	fe4lookup, ok := fsc.LookupFile(4)
	if !ok {
		t.Fatal("LookupFile(4): animals.txt entry not found")
	}
	if fe4lookup.File == nil {
		t.Fatal("LookupFile(4): File is nil")
	}

	// CloseFile(fd 4) → LookupFile(fd 4) returns false
	errno = fsc.CloseFile(4)
	if errno != 0 {
		t.Fatalf("CloseFile(4): unexpected errno %d", errno)
	}
	_, ok = fsc.LookupFile(4)
	if ok {
		t.Fatal("LookupFile(4) after CloseFile: should return false, got true")
	}

	// Renumber(fd 3, fd 5) → LookupFile(5) returns preopen, LookupFile(3) returns false
	errno = fsc.Renumber(3, 5)
	if errno != 0 {
		t.Fatalf("Renumber(3, 5): unexpected errno %d", errno)
	}
	fe5, ok := fsc.LookupFile(5)
	if !ok {
		t.Fatal("LookupFile(5) after Renumber: preopen dir not found at fd 5")
	}
	if !fe5.IsPreopen {
		t.Fatal("LookupFile(5): entry is not marked IsPreopen")
	}
	_, ok = fsc.LookupFile(3)
	if ok {
		t.Fatal("LookupFile(3) after Renumber: should return false, got true")
	}

	// Close() cleans up all open files
	err = fsc.Close()
	if err != nil {
		t.Fatalf("Close(): %v", err)
	}
}

// TestDirentCacheSynthesizesDotEntries verifies that a DirentCache returns
// dot (".") and dotdot ("..") entries at position 0.
func TestDirentCacheSynthesizesDotEntries(t *testing.T) {
	dirFS := osDirFS{FS: os.DirFS("testdata")}
	var buf bytes.Buffer

	fsc, err := InitFSContext(nil, &buf, &buf, []FS{dirFS}, []string{"/"})
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}
	defer fsc.Close()

	// fd 3 is the preopen dir
	fe3, ok := fsc.LookupFile(3)
	if !ok {
		t.Fatal("LookupFile(3): preopen dir not found at fd 3")
	}

	// Get or create DirentCache
	dc, errno := fe3.DirentCache()
	if errno != 0 {
		t.Fatalf("DirentCache: unexpected errno %d", errno)
	}

	// Read at pos 0 should return dot entries + real entries
	dirents, errno := dc.Read(0, 100)
	if errno != 0 {
		t.Fatalf("DirentCache.Read: unexpected errno %d", errno)
	}

	if len(dirents) < 2 {
		t.Fatalf("DirentCache.Read: got %d dirents, want at least 2 dot entries", len(dirents))
	}

	if dirents[0].Name != "." {
		t.Errorf("dirents[0].Name = %q, want %q", dirents[0].Name, ".")
	}
	if !dirents[0].IsDir() {
		t.Errorf("dirents[0] should be a directory")
	}

	if dirents[1].Name != ".." {
		t.Errorf("dirents[1].Name = %q, want %q", dirents[1].Name, "..")
	}
	if !dirents[1].IsDir() {
		t.Errorf("dirents[1] should be a directory")
	}

	// Also check that animals.txt appears in the listing
	found := false
	for _, d := range dirents {
		if d.Name == "animals.txt" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(dirents))
		for i, d := range dirents {
			names[i] = d.Name
		}
		t.Errorf("DirentCache.Read: animals.txt not found in entries: %s", strings.Join(names, ", "))
	}
}

// TestDirentCacheSlidingWindow verifies that DirentCache.Read with a bounded
// count returns a subset and can be called again to get more entries.
func TestDirentCacheSlidingWindow(t *testing.T) {
	dirFS := osDirFS{FS: os.DirFS("testdata")}
	var buf bytes.Buffer

	fsc, err := InitFSContext(nil, &buf, &buf, []FS{dirFS}, []string{"/"})
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}
	defer fsc.Close()

	fe3, ok := fsc.LookupFile(3)
	if !ok {
		t.Fatal("LookupFile(3): preopen dir not found at fd 3")
	}

	dc, errno := fe3.DirentCache()
	if errno != 0 {
		t.Fatalf("DirentCache: unexpected errno %d", errno)
	}

	// Read only 2 entries (should get just . and ..)
	dirents, errno := dc.Read(0, 2)
	if errno != 0 {
		t.Fatalf("DirentCache.Read(0, 2): unexpected errno %d", errno)
	}
	if len(dirents) != 2 {
		t.Fatalf("Read(0, 2): got %d dirents, want 2", len(dirents))
	}

	// Read from position 2 with count 100 should get remaining entries
	dirents2, errno := dc.Read(2, 100)
	if errno != 0 {
		t.Fatalf("DirentCache.Read(2, 100): unexpected errno %d", errno)
	}
	if len(dirents2) == 0 {
		t.Fatal("Read(2, 100): expected non-zero remaining dirents")
	}

	// Combined count should equal a full read
	dc2, errno := fe3.DirentCache()
	if errno != 0 {
		t.Fatalf("DirentCache (second call): unexpected errno %d", errno)
	}
	allDirents, errno := dc2.Read(0, 100)
	if errno != 0 {
		t.Fatalf("DirentCache.Read(0, 100): unexpected errno %d", errno)
	}
	if len(dirents)+len(dirents2) != len(allDirents) {
		t.Errorf("sliding window total: %d + %d = %d, want %d",
			len(dirents), len(dirents2), len(dirents)+len(dirents2), len(allDirents))
	}
}

// TestDirentCacheFallbackUsesEntryPath verifies that when File.Readdir
// returns ENOSYS, the DirentCache fallback reads from the opened directory
// path, not always the filesystem root.
func TestDirentCacheFallbackUsesEntryPath(t *testing.T) {
	// Create temp dir layout: animals.txt at root, sub/nested.txt in subdirectory
	tmpDir := t.TempDir()
	if err := os.WriteFile(tmpDir+"/animals.txt", []byte("dog"), 0644); err != nil {
		t.Fatalf("failed to create animals.txt: %v", err)
	}
	if err := os.Mkdir(tmpDir+"/sub", 0755); err != nil {
		t.Fatalf("failed to create sub: %v", err)
	}
	if err := os.WriteFile(tmpDir+"/sub/nested.txt", []byte("cat"), 0644); err != nil {
		t.Fatalf("failed to create nested.txt: %v", err)
	}

	// InitFSContext with DirFS preopen at "/"
	dirFS := osDirFS{FS: os.DirFS(tmpDir)}
	var buf bytes.Buffer
	fsc, err := InitFSContext(nil, &buf, &buf, []FS{dirFS}, []string{"/"})
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}
	defer fsc.Close()

	// OpenFile on "sub" → fd for subdirectory
	fd, errno := fsc.OpenFile(dirFS, "sub", O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("OpenFile(sub): unexpected errno %d", errno)
	}

	// Get the FileEntry and DirentCache
	fe, ok := fsc.LookupFile(fd)
	if !ok {
		t.Fatalf("LookupFile(%d): sub not found", fd)
	}

	dc, errno := fe.DirentCache()
	if errno != 0 {
		t.Fatalf("DirentCache: unexpected errno %d", errno)
	}

	// Read at pos 0 should return dot entries + nested.txt, NOT animals.txt from root
	dirents, errno := dc.Read(0, 100)
	if errno != 0 {
		t.Fatalf("DirentCache.Read: unexpected errno %d", errno)
	}

	// Check that nested.txt appears in the listing
	foundNested := false
	for _, d := range dirents {
		if d.Name == "nested.txt" {
			foundNested = true
			break
		}
	}
	if !foundNested {
		names := make([]string, len(dirents))
		for i, d := range dirents {
			names[i] = d.Name
		}
		t.Errorf("DirentCache.Read: nested.txt not found in entries: %s", strings.Join(names, ", "))
	}

	// Check that animals.txt does NOT appear (it's in the parent, not the subdirectory)
	for _, d := range dirents {
		if d.Name == "animals.txt" {
			t.Errorf("DirentCache.Read: animals.txt should NOT appear (it's in parent directory, not sub)")
		}
	}
}

// TestInitFSContextMultiplePreopens verifies that each preopened filesystem
// gets a distinct file descriptor starting from 3.
func TestInitFSContextMultiplePreopens(t *testing.T) {
	dirFS := osDirFS{FS: os.DirFS("testdata")}
	var buf bytes.Buffer

	// Create two distinct filesystems for preopening
	fsA := dirFS
	fsB := dirFS

	// InitFSContext with two preopens at "/a" and "/b"
	fsc, err := InitFSContext(nil, &buf, &buf, []FS{fsA, fsB}, []string{"/a", "/b"})
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}
	defer fsc.Close()

	// LookupFile(3) should return the first preopen
	fe3, ok := fsc.LookupFile(3)
	if !ok {
		t.Fatal("LookupFile(3): first preopen not found at fd 3")
	}
	if !fe3.IsPreopen {
		t.Fatal("LookupFile(3): entry is not marked IsPreopen")
	}
	if fe3.Name != "/a" {
		t.Fatalf("LookupFile(3): got guest path %q, want %q", fe3.Name, "/a")
	}

	// LookupFile(4) should return the second preopen
	fe4, ok := fsc.LookupFile(4)
	if !ok {
		t.Fatal("LookupFile(4): second preopen not found at fd 4")
	}
	if !fe4.IsPreopen {
		t.Fatal("LookupFile(4): entry is not marked IsPreopen")
	}
	if fe4.Name != "/b" {
		t.Fatalf("LookupFile(4): got guest path %q, want %q", fe4.Name, "/b")
	}

	// Verify that both entries remain valid (neither was overwritten)
	// They should have different FS values (even though same underlying dir)
	// The key check is that both lookups succeed independently
	if fe3 == fe4 {
		t.Fatal("LookupFile(3) and LookupFile(4) returned the same entry")
	}

	// Verify that closing one doesn't affect the other
	if err := fsc.CloseFile(4); err != 0 {
		t.Fatalf("CloseFile(4): unexpected errno %d", err)
	}
	_, ok = fsc.LookupFile(4)
	if ok {
		t.Fatal("LookupFile(4) after CloseFile: should return false")
	}
	fe3After, ok := fsc.LookupFile(3)
	if !ok {
		t.Fatal("LookupFile(3) after CloseFile(4): should still be valid")
	}
	if !fe3After.IsPreopen {
		t.Fatal("LookupFile(3) after CloseFile(4): entry should still be IsPreopen")
	}
}

// Check that osDirFS satisfies FS at compile time.
var _ FS = osDirFS{}
