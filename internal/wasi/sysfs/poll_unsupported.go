//go:build !unix && !windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import "github.com/lbe/wasm2go-wasi-host/internal/wasi"

func poll(uintptr, wasi.Pflag, int32) (bool, wasi.Errno) {
	return false, wasi.ENOSYS
}
