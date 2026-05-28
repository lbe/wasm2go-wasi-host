package sysfs

import (
	"io/fs"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// ReadFS is a read-only wrapper around an FS that denies all mutations with EROFS.
type ReadFS struct {
	wasi.FS
}

func (r ReadFS) PathFor() string {
	return ""
}

func (r ReadFS) OpenFile(path string, flag wasi.Oflag, perm fs.FileMode) (wasi.File, wasi.Errno) {
	switch flag & (wasi.O_RDONLY | wasi.O_WRONLY | wasi.O_RDWR) {
	case wasi.O_WRONLY, wasi.O_RDWR:
		if flag&wasi.O_DIRECTORY != 0 {
			return nil, wasi.EISDIR
		}
		return nil, wasi.EROFS
	}

	f, errno := r.FS.OpenFile(path, flag, perm)
	if errno != 0 {
		return nil, errno
	}
	return &readFile{File: f}, 0
}

func (r ReadFS) Mkdir(string, fs.FileMode) wasi.Errno    { return wasi.EROFS }
func (r ReadFS) Chmod(string, fs.FileMode) wasi.Errno    { return wasi.EROFS }
func (r ReadFS) Rename(string, string) wasi.Errno        { return wasi.EROFS }
func (r ReadFS) Rmdir(string) wasi.Errno                 { return wasi.EROFS }
func (r ReadFS) Link(string, string) wasi.Errno          { return wasi.EROFS }
func (r ReadFS) Symlink(string, string) wasi.Errno       { return wasi.EROFS }
func (r ReadFS) Unlink(string) wasi.Errno                { return wasi.EROFS }
func (r ReadFS) Utimens(string, int64, int64) wasi.Errno { return wasi.EROFS }

// readFile wraps a File to deny writes and metadata mutations.
type readFile struct {
	wasi.File
}

func (r *readFile) Write([]byte) (int, wasi.Errno)         { return 0, r.writeErr() }
func (r *readFile) Pwrite([]byte, int64) (int, wasi.Errno) { return 0, r.writeErr() }
func (r *readFile) Truncate(int64) wasi.Errno              { return r.writeErr() }
func (r *readFile) Sync() wasi.Errno                       { return wasi.EBADF }
func (r *readFile) Datasync() wasi.Errno                   { return wasi.EBADF }
func (r *readFile) Utimens(int64, int64) wasi.Errno        { return wasi.EBADF }

func (r *readFile) writeErr() wasi.Errno {
	if isDir, errno := r.IsDir(); errno != 0 {
		return errno
	} else if isDir {
		return wasi.EISDIR
	}
	return wasi.EBADF
}
