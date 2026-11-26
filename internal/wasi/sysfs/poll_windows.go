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
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// pollInterval is the interval between each call to peekNamedPipe in poll.
const pollInterval = 100 * time.Millisecond

// poll implements poll on Windows, for a subset of cases.
//
// Only POLLIN is supported; other flags return ENOTSUP. Regular files, character
// devices (including consoles) and other non-pipe handles are always reported
// as ready. Named pipes are checked with PeekNamedPipe. Sockets are not
// supported. A negative timeout is EINVAL, as on Unix.
func poll(fd uintptr, flag wasi.Pflag, timeoutMillis int32) (ready bool, errno wasi.Errno) {
	if flag != wasi.POLLIN {
		return false, wasi.ENOTSUP
	}
	if timeoutMillis < 0 {
		return false, wasi.EINVAL
	}
	if ready, errno = pollReady(fd); errno != 0 || ready {
		return ready, errno
	}
	if timeoutMillis == 0 {
		return false, 0
	}

	// Poll every pollInterval until the timeout expires.
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	after := time.NewTimer(time.Duration(timeoutMillis) * time.Millisecond)
	defer after.Stop()
	for {
		select {
		case <-after.C:
			return false, 0
		case <-tick.C:
			if ready, errno = pollReady(fd); errno != 0 || ready {
				return ready, errno
			}
		}
	}
}

// pollReady reports whether fd is readable without blocking.
func pollReady(fd uintptr) (bool, wasi.Errno) {
	h := syscall.Handle(fd)
	t, err := syscall.GetFileType(h)
	if err != nil {
		return false, wasi.UnwrapOSError(err)
	}
	if t != syscall.FILE_TYPE_PIPE {
		// Regular files and character devices are always ready.
		return true, 0
	}
	n, errno := peekNamedPipe(h)
	if errno != 0 {
		return false, wasi.UnwrapOSError(errno)
	}
	return n > 0, 0
}
