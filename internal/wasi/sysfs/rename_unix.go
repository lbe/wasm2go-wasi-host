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

func rename(from, to string) wasi.Errno {
	if from == to {
		return 0
	}
	return wasi.UnwrapOSError(syscall.Rename(from, to))
}
