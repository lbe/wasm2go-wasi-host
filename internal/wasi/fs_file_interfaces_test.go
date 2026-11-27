package wasi

import (
	"io/fs"
	"testing"
)

// mockFS implements FS for testing interface shape at compile time.
type mockFS struct{}

func (mockFS) PathFor() string { return "" }
func (mockFS) OpenFile(path string, flag Oflag, perm fs.FileMode) (File, Errno) {
	return nil, 0
}
func (mockFS) Lstat(path string) (Stat_t, Errno)           { return Stat_t{}, 0 }
func (mockFS) Stat(path string) (Stat_t, Errno)            { return Stat_t{}, 0 }
func (mockFS) Mkdir(path string, perm fs.FileMode) Errno   { return 0 }
func (mockFS) Chmod(path string, perm fs.FileMode) Errno   { return 0 }
func (mockFS) Rename(from, to string) Errno                { return 0 }
func (mockFS) Rmdir(path string) Errno                     { return 0 }
func (mockFS) Unlink(path string) Errno                    { return 0 }
func (mockFS) Link(oldPath, newPath string) Errno          { return 0 }
func (mockFS) Symlink(oldPath, linkName string) Errno      { return 0 }
func (mockFS) Readlink(path string) (string, Errno)        { return "", 0 }
func (mockFS) Utimens(path string, atim, mtim int64) Errno { return 0 }

// mockFile implements File for testing interface shape at compile time.
type mockFile struct{}

func (mockFile) Dev() (uint64, Errno)                         { return 0, 0 }
func (mockFile) Ino() (Inode, Errno)                          { return 0, 0 }
func (mockFile) IsDir() (bool, Errno)                         { return false, 0 }
func (mockFile) IsAppend() bool                               { return false }
func (mockFile) SetAppend(enable bool) Errno                  { return 0 }
func (mockFile) Stat() (Stat_t, Errno)                        { return Stat_t{}, 0 }
func (mockFile) Read(buf []byte) (int, Errno)                 { return 0, 0 }
func (mockFile) Pread(buf []byte, off int64) (int, Errno)     { return 0, 0 }
func (mockFile) Seek(offset int64, whence int) (int64, Errno) { return 0, 0 }
func (mockFile) Readdir(n int) ([]Dirent, Errno)              { return nil, 0 }
func (mockFile) Write(buf []byte) (int, Errno)                { return 0, 0 }
func (mockFile) Pwrite(buf []byte, off int64) (int, Errno)    { return 0, 0 }
func (mockFile) Truncate(size int64) Errno                    { return 0 }
func (mockFile) Sync() Errno                                  { return 0 }
func (mockFile) Datasync() Errno                              { return 0 }
func (mockFile) Utimens(atim, mtim int64) Errno               { return 0 }
func (mockFile) Close() Errno                                 { return 0 }

// Compile-time interface satisfaction checks.
// They fail to compile if FS or File lacks a method.
var _ FS = mockFS{}
var _ File = mockFile{}

// TestFSInterfaceCompilesAndMethodsExist verifies the FS interface is
// defined with all required methods and is satisfied by mockFS.
func TestFSInterfaceCompilesAndMethodsExist(t *testing.T) {
	// Call every interface method.
	var fsVar FS = mockFS{}

	// Exercises every FS method; if any are missing this won't compile.
	// We also verify runtime behavior for a no-op mock.
	path := fsVar.PathFor()
	if path != "" {
		t.Errorf("mockFS.PathFor: got %q, want %q", path, "")
	}

	_, errno := fsVar.OpenFile("test", O_RDONLY, 0)
	if errno != 0 {
		t.Errorf("mockFS.OpenFile: got errno %d, want 0", errno)
	}

	_, errno = fsVar.Lstat("test")
	if errno != 0 {
		t.Errorf("mockFS.Lstat: got errno %d, want 0", errno)
	}

	_, errno = fsVar.Stat("test")
	if errno != 0 {
		t.Errorf("mockFS.Stat: got errno %d, want 0", errno)
	}

	if errno = fsVar.Mkdir("test", 0); errno != 0 {
		t.Errorf("mockFS.Mkdir: got errno %d, want 0", errno)
	}
	if errno = fsVar.Chmod("test", 0); errno != 0 {
		t.Errorf("mockFS.Chmod: got errno %d, want 0", errno)
	}
	if errno = fsVar.Rename("a", "b"); errno != 0 {
		t.Errorf("mockFS.Rename: got errno %d, want 0", errno)
	}
	if errno = fsVar.Rmdir("test"); errno != 0 {
		t.Errorf("mockFS.Rmdir: got errno %d, want 0", errno)
	}
	if errno = fsVar.Unlink("test"); errno != 0 {
		t.Errorf("mockFS.Unlink: got errno %d, want 0", errno)
	}
	if errno = fsVar.Link("a", "b"); errno != 0 {
		t.Errorf("mockFS.Link: got errno %d, want 0", errno)
	}
	if errno = fsVar.Symlink("a", "b"); errno != 0 {
		t.Errorf("mockFS.Symlink: got errno %d, want 0", errno)
	}
	_, errno = fsVar.Readlink("test")
	if errno != 0 {
		t.Errorf("mockFS.Readlink: got errno %d, want 0", errno)
	}
	if errno = fsVar.Utimens("test", 0, 0); errno != 0 {
		t.Errorf("mockFS.Utimens: got errno %d, want 0", errno)
	}
}

// TestFileInterfaceCompilesAndMethodsExist verifies the File interface is
// defined with all required methods and is satisfied by mockFile.
func TestFileInterfaceCompilesAndMethodsExist(t *testing.T) {
	var f File = mockFile{}

	_, errno := f.Dev()
	if errno != 0 {
		t.Errorf("mockFile.Dev: got errno %d, want 0", errno)
	}

	_, errno = f.Ino()
	if errno != 0 {
		t.Errorf("mockFile.Ino: got errno %d, want 0", errno)
	}

	_, errno = f.IsDir()
	if errno != 0 {
		t.Errorf("mockFile.IsDir: got errno %d, want 0", errno)
	}

	if f.IsAppend() != false {
		t.Errorf("mockFile.IsAppend: got true, want false")
	}

	if errno = f.SetAppend(false); errno != 0 {
		t.Errorf("mockFile.SetAppend: got errno %d, want 0", errno)
	}

	_, errno = f.Stat()
	if errno != 0 {
		t.Errorf("mockFile.Stat: got errno %d, want 0", errno)
	}

	_, errno = f.Read(nil)
	if errno != 0 {
		t.Errorf("mockFile.Read: got errno %d, want 0", errno)
	}

	_, errno = f.Pread(nil, 0)
	if errno != 0 {
		t.Errorf("mockFile.Pread: got errno %d, want 0", errno)
	}

	_, errno = f.Seek(0, 0)
	if errno != 0 {
		t.Errorf("mockFile.Seek: got errno %d, want 0", errno)
	}

	_, errno = f.Readdir(0)
	if errno != 0 {
		t.Errorf("mockFile.Readdir: got errno %d, want 0", errno)
	}

	_, errno = f.Write(nil)
	if errno != 0 {
		t.Errorf("mockFile.Write: got errno %d, want 0", errno)
	}

	_, errno = f.Pwrite(nil, 0)
	if errno != 0 {
		t.Errorf("mockFile.Pwrite: got errno %d, want 0", errno)
	}

	if errno = f.Truncate(0); errno != 0 {
		t.Errorf("mockFile.Truncate: got errno %d, want 0", errno)
	}
	if errno = f.Sync(); errno != 0 {
		t.Errorf("mockFile.Sync: got errno %d, want 0", errno)
	}
	if errno = f.Datasync(); errno != 0 {
		t.Errorf("mockFile.Datasync: got errno %d, want 0", errno)
	}
	if errno = f.Utimens(0, 0); errno != 0 {
		t.Errorf("mockFile.Utimens: got errno %d, want 0", errno)
	}
	if errno = f.Close(); errno != 0 {
		t.Errorf("mockFile.Close: got errno %d, want 0", errno)
	}
}
