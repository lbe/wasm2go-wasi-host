//go:build unix

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func unlink(name string) (errno wasi.Errno) {
	err := syscall.Unlink(name)
	if errno = wasi.UnwrapOSError(err); errno == wasi.EPERM {
		errno = wasi.EISDIR
	}
	return errno
}
