//go:build linux || darwin

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

func withSyscallOflag(oflag wasi.Oflag, flag int) int {
	if oflag&wasi.O_DIRECTORY != 0 {
		flag |= syscall.O_DIRECTORY
	}
	if oflag&wasi.O_DSYNC != 0 {
		flag |= syscall.O_DSYNC
	}
	if oflag&wasi.O_NOFOLLOW != 0 {
		flag |= syscall.O_NOFOLLOW
	}
	if oflag&wasi.O_NONBLOCK != 0 {
		flag |= syscall.O_NONBLOCK
	}
	return flag
}
