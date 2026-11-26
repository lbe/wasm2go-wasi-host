//go:build freebsd

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

// _UTIME_OMIT is FreeBSD's utimensat(2) omit sentinel.
const _UTIME_OMIT = -2

// utimens sets times on path; syscall.UtimesNano uses utimensat on FreeBSD,
// which honors _UTIME_OMIT.
func utimens(path string, atim, mtim int64) wasi.Errno {
	times := timesToTimespecs(wasi.EpochNanos(atim), wasi.EpochNanos(mtim))
	if times == nil {
		return 0
	}
	return wasi.UnwrapOSError(syscall.UtimesNano(path, times[:]))
}

// futimens sets times on an open fd with futimes(2). Like Darwin this is
// best-effort without _UTIME_OMIT; callers pass both times. Do not wrap fd in
// an *os.File here: its GC cleanup would close the caller's fd.
func futimens(fd uintptr, atim, mtim int64) error {
	times := timesToTimespecs(wasi.EpochNanos(atim), wasi.EpochNanos(mtim))
	if times == nil {
		return nil
	}
	tv := [2]syscall.Timeval{
		syscall.NsecToTimeval(times[0].Nano()),
		syscall.NsecToTimeval(times[1].Nano()),
	}
	return syscall.Futimes(int(fd), tv[:])
}
