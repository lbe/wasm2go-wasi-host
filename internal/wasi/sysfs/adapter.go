// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"fmt"
	"io/fs"
	"path"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// AdaptFS wraps an fs.FS as a read-only wasi.FS.
type AdaptFS struct {
	FS fs.FS
}

func (a AdaptFS) PathFor() string {
	return fmt.Sprintf("%v", a.FS)
}

func (a AdaptFS) OpenFile(p string, flag wasi.Oflag, perm fs.FileMode) (wasi.File, wasi.Errno) {
	return OpenFSFile(a.FS, cleanPath(p), flag, perm)
}

func (a AdaptFS) Lstat(p string) (wasi.Stat_t, wasi.Errno) {
	return a.Stat(p)
}

func (a AdaptFS) Stat(p string) (wasi.Stat_t, wasi.Errno) {
	f, errno := a.OpenFile(p, wasi.O_RDONLY, 0)
	if errno != 0 {
		return wasi.Stat_t{}, errno
	}
	defer f.Close()
	return f.Stat()
}

func (a AdaptFS) Mkdir(string, fs.FileMode) wasi.Errno    { return wasi.ENOSYS }
func (a AdaptFS) Chmod(string, fs.FileMode) wasi.Errno    { return wasi.ENOSYS }
func (a AdaptFS) Rename(string, string) wasi.Errno        { return wasi.ENOSYS }
func (a AdaptFS) Rmdir(string) wasi.Errno                 { return wasi.ENOSYS }
func (a AdaptFS) Link(string, string) wasi.Errno          { return wasi.ENOSYS }
func (a AdaptFS) Symlink(string, string) wasi.Errno       { return wasi.ENOSYS }
func (a AdaptFS) Unlink(string) wasi.Errno                { return wasi.ENOSYS }
func (a AdaptFS) Readlink(string) (string, wasi.Errno)    { return "", wasi.ENOSYS }
func (a AdaptFS) Utimens(string, int64, int64) wasi.Errno { return wasi.ENOSYS }

// cleanPath converts a path to the form expected by fs.FS.Open:
// no leading slash, cleaned of . and .. components.
func cleanPath(name string) string {
	cleaned := name
	if len(cleaned) > 0 && cleaned[0] == '/' {
		cleaned = cleaned[1:]
	}
	return path.Clean(cleaned)
}
