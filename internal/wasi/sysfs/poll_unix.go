//go:build unix

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"golang.org/x/sys/unix"
)

func pollEvents(flag wasi.Pflag) (int16, wasi.Errno) {
	switch flag {
	case wasi.POLLIN:
		return unix.POLLIN, 0
	case wasi.POLLOUT:
		return unix.POLLOUT, 0
	default:
		return 0, wasi.ENOTSUP
	}
}

func poll(fd uintptr, flag wasi.Pflag, timeoutMillis int32) (ready bool, errno wasi.Errno) {
	events, errno := pollEvents(flag)
	if errno != 0 {
		return false, errno
	}
	if timeoutMillis < 0 {
		return false, wasi.EINVAL
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: events}}
	n, err := unix.Poll(fds, int(timeoutMillis))
	if err != nil {
		return false, wasi.UnwrapOSError(err)
	}
	if n == 0 {
		return false, 0
	}
	return fds[0].Revents&events != 0, 0
}
