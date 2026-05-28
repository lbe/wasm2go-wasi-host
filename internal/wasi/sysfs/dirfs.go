package sysfs

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// DirFS wraps a host directory path as an FS.
type DirFS struct {
	Path string
}

func (d DirFS) PathFor() string {
	return d.Path
}

func (d DirFS) OpenFile(path string, flag wasi.Oflag, perm fs.FileMode) (wasi.File, wasi.Errno) {
	joined, errno := d.join(path)
	if errno != 0 {
		return nil, errno
	}
	return OpenOSFile(joined, flag, perm)
}

func (d DirFS) Lstat(path string) (wasi.Stat_t, wasi.Errno) {
	joined, errno := d.join(path)
	if errno != 0 {
		return wasi.Stat_t{}, errno
	}
	return lstat(joined)
}

func (d DirFS) Stat(path string) (wasi.Stat_t, wasi.Errno) {
	joined, errno := d.join(path)
	if errno != 0 {
		return wasi.Stat_t{}, errno
	}
	return stat(joined)
}

func (d DirFS) Mkdir(path string, perm fs.FileMode) (errno wasi.Errno) {
	joined, errno := d.join(path)
	if errno != 0 {
		return errno
	}
	err := os.Mkdir(joined, perm)
	if errno = wasi.UnwrapOSError(err); errno == wasi.ENOTDIR {
		errno = wasi.ENOENT
	}
	return
}

func (d DirFS) Chmod(path string, perm fs.FileMode) wasi.Errno {
	joined, errno := d.join(path)
	if errno != 0 {
		return errno
	}
	err := os.Chmod(joined, perm)
	return wasi.UnwrapOSError(err)
}

func (d DirFS) Rename(from, to string) wasi.Errno {
	from, fromErrno := d.join(from)
	to, toErrno := d.join(to)
	if fromErrno != 0 {
		return fromErrno
	}
	if toErrno != 0 {
		return toErrno
	}
	return rename(from, to)
}

func (d DirFS) Rmdir(path string) wasi.Errno {
	joined, errno := d.join(path)
	if errno != 0 {
		return errno
	}
	return rmdir(joined)
}

func (d DirFS) Unlink(path string) (err wasi.Errno) {
	joined, errno := d.join(path)
	if errno != 0 {
		return errno
	}
	return unlink(joined)
}

func (d DirFS) Link(oldName, newName string) wasi.Errno {
	oldJoined, oldErrno := d.join(oldName)
	newJoined, newErrno := d.join(newName)
	if oldErrno != 0 {
		return oldErrno
	}
	if newErrno != 0 {
		return newErrno
	}
	err := os.Link(oldJoined, newJoined)
	return wasi.UnwrapOSError(err)
}

func (d DirFS) Symlink(oldName, link string) wasi.Errno {
	if path.IsAbs(oldName) {
		return wasi.EPERM
	}
	joined, errno := d.join(link)
	if errno != 0 {
		return errno
	}
	err := os.Symlink(oldName, joined)
	return wasi.UnwrapOSError(err)
}

func (d DirFS) Readlink(link string) (string, wasi.Errno) {
	joined, errno := d.join(link)
	if errno != 0 {
		return "", errno
	}
	dst, err := os.Readlink(joined)
	if err != nil {
		return "", wasi.UnwrapOSError(err)
	}
	return dst, 0
}

func (d DirFS) Utimens(path string, atim, mtim int64) wasi.Errno {
	joined, errno := d.join(path)
	if errno != 0 {
		return errno
	}
	return utimens(joined, atim, mtim)
}

func (d DirFS) join(guestPath string) (string, wasi.Errno) {
	// Resolve the absolute path of the root directory.
	rootAbs, err := filepath.Abs(d.Path)
	if err != nil {
		return "", wasi.EIO
	}

	// Special case: empty, ".", or "/" paths refer to the root directory itself.
	// This must be handled before the prefix check because cleanedDir adds a
	// trailing separator, and the resolved path won't have one.
	if guestPath == "" || guestPath == "." || guestPath == "/" {
		return rootAbs, 0
	}

	// Join and resolve to absolute path.
	joined := filepath.Join(rootAbs, guestPath)
	resolved, err := filepath.Abs(joined)
	if err != nil {
		return "", wasi.EIO
	}

	// Ensure the resolved path is under the root.
	if !isSubpath(cleanedDir(rootAbs), resolved) {
		return "", wasi.ENOENT
	}

	return resolved, 0
}

// isSubpath reports whether path is equal to or is a subpath of prefix.
// prefix should have a trailing separator.
func isSubpath(prefix, path string) bool {
	return strings.HasPrefix(path, prefix)
}

// cleanedDir ensures the root path has a trailing separator for proper prefix matching.
func cleanedDir(root string) string {
	return strings.TrimSuffix(root, string(filepath.Separator)) + string(filepath.Separator)
}
