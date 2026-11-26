//go:build !linux && !darwin && !windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import "github.com/lbe/wasm2go-wasi-host/internal/wasi"

// withSyscallOflag leaves flag unchanged on platforms without a mapping for
// O_DIRECTORY, O_DSYNC, O_NOFOLLOW, O_NONBLOCK or O_RSYNC.
func withSyscallOflag(_ wasi.Oflag, flag int) int {
	return flag
}
