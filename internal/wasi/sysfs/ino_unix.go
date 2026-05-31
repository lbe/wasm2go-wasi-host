//go:build unix

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"io/fs"
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func inoFromFileInfo(_ string, info fs.FileInfo) (wasi.Inode, wasi.Errno) {
	if s, ok := info.Sys().(*syscall.Stat_t); ok {
		return s.Ino, 0
	}
	return 0, 0
}
