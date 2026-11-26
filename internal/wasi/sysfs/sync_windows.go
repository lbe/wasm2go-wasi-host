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

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func fsync(f *os.File) wasi.Errno {
	errno := wasi.UnwrapOSError(f.Sync())
	// Coerce error performing stat on a directory to 0, as it won't work
	// on Windows.
	switch errno {
	case wasi.EACCES /* Go 1.20 */, wasi.EBADF /* Go 1.19 */ :
		if st, err := f.Stat(); err == nil && st.IsDir() {
			errno = 0
		}
	}
	return errno
}
