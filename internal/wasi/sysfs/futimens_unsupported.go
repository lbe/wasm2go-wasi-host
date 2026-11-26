//go:build !linux && !darwin && !freebsd && !windows

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

// utimens is not implemented on this platform.
func utimens(string, int64, int64) wasi.Errno {
	return wasi.ENOSYS
}

// futimens is not implemented on this platform.
func futimens(uintptr, int64, int64) error {
	return syscall.ENOSYS
}
