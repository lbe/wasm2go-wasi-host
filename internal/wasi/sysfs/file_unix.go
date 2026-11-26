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

const (
	nonBlockingFileReadSupported  = true
	nonBlockingFileWriteSupported = true
)

func rmdir(path string) wasi.Errno {
	err := syscall.Rmdir(path)
	return wasi.UnwrapOSError(err)
}

func readFd(fd uintptr, buf []byte) (int, wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := syscall.Read(int(fd), buf)
	return n, wasi.UnwrapOSError(err)
}

func writeFd(fd uintptr, buf []byte) (int, wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := syscall.Write(int(fd), buf)
	return n, wasi.UnwrapOSError(err)
}

func preadFd(fd uintptr, buf []byte, off int64) (int, wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := syscall.Pread(int(fd), buf, off)
	return n, wasi.UnwrapOSError(err)
}

func pwriteFd(fd uintptr, buf []byte, off int64) (int, wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := syscall.Pwrite(int(fd), buf, off)
	return n, wasi.UnwrapOSError(err)
}
