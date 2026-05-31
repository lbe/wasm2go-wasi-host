//go:build !windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"io/fs"
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func openFile(path string, oflag wasi.Oflag, perm fs.FileMode) (*os.File, wasi.Errno) {
	f, err := os.OpenFile(path, toOsOpenFlag(oflag), perm)
	return f, wasi.UnwrapOSError(err)
}
