//go:build linux

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"strconv"
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// _UTIME_OMIT is the Linux utimensat(2) omit sentinel (see UTIME_OMIT in utime.h).
const _UTIME_OMIT = int64(1<<30 - 2)

func utimens(path string, atim, mtim int64) wasi.Errno {
	return wasi.UnwrapOSError(utimesNano(path, atim, mtim))
}

func futimens(fd uintptr, atim, mtim int64) error {
	return utimesNano(procSelfFDPath(fd), atim, mtim)
}

func utimesNano(path string, atim, mtim int64) error {
	times := timesToTimespecs(wasi.EpochNanos(atim), wasi.EpochNanos(mtim))
	if times == nil {
		return nil
	}
	return syscall.UtimesNano(path, times[:])
}

func procSelfFDPath(fd uintptr) string {
	return "/proc/self/fd/" + strconv.FormatUint(uint64(fd), 10)
}
