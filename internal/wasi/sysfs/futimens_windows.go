//go:build windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"os"
	"syscall"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// wasiUTIME_OMIT is the sentinel for an omitted timestamp, as in futimens.go.
const wasiUTIME_OMIT = wasi.EpochNanos(-2)

func utimens(path string, atim, mtim int64) wasi.Errno {
	return chtimes(path, atim, mtim)
}

func futimens(fd uintptr, atim, mtim int64) error {
	// Per docs, zero isn't a valid timestamp as it cannot be differentiated
	// from nil. In both cases, it is a marker like wasiUTIME_OMIT.
	// See https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-setfiletime
	a, w := timespecToFiletime(atim, mtim)

	if a == nil && w == nil {
		return nil // both omitted, so nothing to change
	}

	// Attempt to get the stat by handle, which works for normal files
	h := syscall.Handle(fd)

	// Note: This returns ERROR_ACCESS_DENIED when the input is a directory.
	return syscall.SetFileTime(h, nil, a, w)
}

func timespecToFiletime(atim, mtim int64) (a, w *syscall.Filetime) {
	a = timespecToFileTime(atim)
	w = timespecToFileTime(mtim)
	return
}

func timespecToFileTime(tim int64) *syscall.Filetime {
	if tim == wasiUTIME_OMIT {
		return nil
	}
	ft := syscall.NsecToFiletime(tim)
	return &ft
}

// chtimes sets the access and modification times of path. It follows
// symlinks, as os.Chtimes does.
func chtimes(path string, atim, mtim int64) (errno wasi.Errno) {
	// When both inputs are omitted, there is nothing to change.
	if atim == wasiUTIME_OMIT && mtim == wasiUTIME_OMIT {
		return
	}

	// UTIME_OMIT is expensive until progress is made in Go, as it requires a
	// stat to read-back the value to re-apply.
	// - https://github.com/golang/go/issues/32558.
	// - https://go-review.googlesource.com/c/go/+/219638 (unmerged)
	var st wasi.Stat_t
	if atim == wasiUTIME_OMIT || mtim == wasiUTIME_OMIT {
		if st, errno = stat(path); errno != 0 {
			return
		}
	}

	var atime, mtime time.Time
	switch {
	case atim == wasiUTIME_OMIT:
		atime = time.Unix(0, st.Atim)
		mtime = time.Unix(0, mtim)
	case mtim == wasiUTIME_OMIT:
		atime = time.Unix(0, atim)
		mtime = time.Unix(0, st.Mtim)
	default:
		atime = time.Unix(0, atim)
		mtime = time.Unix(0, mtim)
	}
	return wasi.UnwrapOSError(os.Chtimes(path, atime, mtime))
}
