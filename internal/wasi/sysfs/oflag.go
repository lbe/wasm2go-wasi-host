// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// toOsOpenFlag converts the input to the flag parameter of os.OpenFile
func toOsOpenFlag(oflag wasi.Oflag) (flag int) {
	switch oflag & (wasi.O_RDONLY | wasi.O_RDWR | wasi.O_WRONLY) {
	case wasi.O_RDONLY:
		flag |= os.O_RDONLY
	case wasi.O_RDWR:
		flag |= os.O_RDWR
	case wasi.O_WRONLY:
		flag |= os.O_WRONLY
	}

	if oflag&wasi.O_APPEND != 0 {
		flag |= os.O_APPEND
	}
	if oflag&wasi.O_CREAT != 0 {
		flag |= os.O_CREATE
	}
	if oflag&wasi.O_EXCL != 0 {
		flag |= os.O_EXCL
	}
	if oflag&wasi.O_SYNC != 0 {
		flag |= os.O_SYNC
	}
	if oflag&wasi.O_TRUNC != 0 {
		flag |= os.O_TRUNC
	}
	return withSyscallOflag(oflag, flag)
}
