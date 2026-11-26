//go:build windows

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

func setNonblock(fd uintptr, enable bool) wasi.Errno {
	// We invoke the syscall, but this is currently no-op.
	return wasi.UnwrapOSError(syscall.SetNonblock(syscall.Handle(fd), enable))
}
