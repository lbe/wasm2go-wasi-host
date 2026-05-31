// Package testfixture provides test fixtures for WASI VFS testing.
package testfixture

import (
	"embed"
	"io/fs"
	"path/filepath"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

//go:embed animals.txt sub/test.txt dir/-
var embeddedFiles embed.FS

// MapFS is a test filesystem implementing wasi.FS interface.
// See design-improvements-2 Tier 2 for the file structure.
type MapFS struct{}

// ReadFile reads a file from the embedded filesystem (helper for testing).
func (m MapFS) ReadFile(name string) ([]byte, error) {
	cleaned := filepath.FromSlash(name)
	if len(cleaned) > 0 && cleaned[0] == filepath.Separator {
		cleaned = cleaned[1:]
	}
	return embeddedFiles.ReadFile(cleaned)
}

// Open opens a file in the embedded filesystem (implements fs.FS).
func (m MapFS) Open(name string) (fs.File, error) {
	cleaned := filepath.FromSlash(name)
	if len(cleaned) > 0 && cleaned[0] == filepath.Separator {
		cleaned = cleaned[1:]
	}
	return embeddedFiles.Open(cleaned)
}

// PathFor returns a descriptive path for this embedded filesystem.
func (m MapFS) PathFor() string {
	return "testfixture.MapFS"
}

// OpenFile opens a file in the embedded filesystem.
func (m MapFS) OpenFile(path string, flag wasi.Oflag, perm fs.FileMode) (wasi.File, wasi.Errno) {
	// Clean the path
	cleaned := filepath.FromSlash(path)
	if len(cleaned) > 0 && cleaned[0] == filepath.Separator {
		cleaned = cleaned[1:]
	}

	f, err := embeddedFiles.Open(cleaned)
	if err != nil {
		return nil, wasi.UnwrapOSError(err)
	}
	return &mapFile{file: f}, 0
}

// Lstat returns file status.
func (m MapFS) Lstat(path string) (wasi.Stat_t, wasi.Errno) {
	return m.Stat(path)
}

// Stat returns file status.
func (m MapFS) Stat(path string) (wasi.Stat_t, wasi.Errno) {
	f, errno := m.OpenFile(path, wasi.O_RDONLY, 0)
	if errno != 0 {
		return wasi.Stat_t{}, errno
	}
	defer f.Close()
	return f.Stat()
}

// Mkdir returns ENOSYS (not implemented for embedded FS).
func (m MapFS) Mkdir(string, fs.FileMode) wasi.Errno {
	return wasi.ENOSYS
}

// Chmod returns ENOSYS.
func (m MapFS) Chmod(string, fs.FileMode) wasi.Errno {
	return wasi.ENOSYS
}

// Rename returns ENOSYS.
func (m MapFS) Rename(string, string) wasi.Errno {
	return wasi.ENOSYS
}

// Rmdir returns ENOSYS.
func (m MapFS) Rmdir(string) wasi.Errno {
	return wasi.ENOSYS
}

// Unlink returns ENOSYS.
func (m MapFS) Unlink(string) wasi.Errno {
	return wasi.ENOSYS
}

// Link returns ENOSYS.
func (m MapFS) Link(string, string) wasi.Errno {
	return wasi.ENOSYS
}

// Symlink returns ENOSYS.
func (m MapFS) Symlink(string, string) wasi.Errno {
	return wasi.ENOSYS
}

// Readlink returns ENOSYS.
func (m MapFS) Readlink(string) (string, wasi.Errno) {
	return "", wasi.ENOSYS
}

// Utimens returns ENOSYS.
func (m MapFS) Utimens(string, int64, int64) wasi.Errno {
	return wasi.ENOSYS
}

// mapFile wraps an fs.File to implement wasi.File.
type mapFile struct {
	file fs.File
}

func (f *mapFile) Dev() (uint64, wasi.Errno)     { return 0, 0 }
func (f *mapFile) Ino() (wasi.Inode, wasi.Errno) { return 0, 0 }
func (f *mapFile) IsDir() (bool, wasi.Errno) {
	fi, err := f.file.Stat()
	return fi.IsDir(), wasi.UnwrapOSError(err)
}
func (f *mapFile) IsAppend() bool            { return false }
func (f *mapFile) SetAppend(bool) wasi.Errno { return 0 }
func (f *mapFile) Stat() (wasi.Stat_t, wasi.Errno) {
	fi, err := f.file.Stat()
	if err != nil {
		return wasi.Stat_t{}, wasi.UnwrapOSError(err)
	}
	return wasi.NewStat_t(fi), wasi.UnwrapOSError(err)
}
func (f *mapFile) Read(b []byte) (int, wasi.Errno) {
	n, err := f.file.Read(b)
	return n, wasi.UnwrapOSError(err)
}
func (f *mapFile) Pread(b []byte, off int64) (int, wasi.Errno) {
	if ra, ok := f.file.(interface {
		ReadAt([]byte, int64) (int, error)
	}); ok {
		n, err := ra.ReadAt(b, off)
		return n, wasi.UnwrapOSError(err)
	}
	return 0, wasi.ENOSYS
}
func (f *mapFile) Seek(off int64, whence int) (int64, wasi.Errno) {
	if s, ok := f.file.(interface {
		Seek(int64, int) (int64, error)
	}); ok {
		n, err := s.Seek(off, whence)
		return n, wasi.UnwrapOSError(err)
	}
	return 0, wasi.ENOSYS
}
func (f *mapFile) Readdir(n int) ([]wasi.Dirent, wasi.Errno) {
	return nil, wasi.ENOSYS
}
func (f *mapFile) Write([]byte) (int, wasi.Errno)         { return 0, wasi.EBADF }
func (f *mapFile) Pwrite([]byte, int64) (int, wasi.Errno) { return 0, wasi.EBADF }
func (f *mapFile) Truncate(int64) wasi.Errno              { return wasi.EBADF }
func (f *mapFile) Sync() wasi.Errno                       { return 0 }
func (f *mapFile) Datasync() wasi.Errno                   { return 0 }
func (f *mapFile) Utimens(int64, int64) wasi.Errno        { return 0 }
func (f *mapFile) Close() wasi.Errno {
	return wasi.UnwrapOSError(f.file.Close())
}
