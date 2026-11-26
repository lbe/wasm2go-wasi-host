//go:build windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"io/fs"
	"path"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// inoFromFileInfo uses stat to get the inode information of the file.
func inoFromFileInfo(dirPath string, info fs.FileInfo) (ino wasi.Inode, errno wasi.Errno) {
	if v, ok := info.Sys().(*wasi.Stat_t); ok {
		return v.Ino, 0
	}
	if dirPath == "" {
		// This is a FS.File backed implementation which doesn't have access to
		// the original file path.
		return
	}
	// Ino is no not in Win32FileAttributeData
	inoPath := path.Clean(path.Join(dirPath, info.Name()))
	var st wasi.Stat_t
	if st, errno = lstat(inoPath); errno == 0 {
		ino = st.Ino
	}
	return
}
