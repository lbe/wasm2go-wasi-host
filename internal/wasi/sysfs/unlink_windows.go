//go:build windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"os"
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func unlink(name string) wasi.Errno {
	err := syscall.Unlink(name)
	if err == nil {
		return 0
	}
	errno := wasi.UnwrapOSError(err)
	if errno == wasi.EBADF {
		lstat, errLstat := os.Lstat(name)
		if errLstat == nil && lstat.Mode()&os.ModeSymlink != 0 {
			errno = wasi.UnwrapOSError(os.Remove(name))
		} else {
			errno = wasi.EISDIR
		}
	}
	return errno
}
