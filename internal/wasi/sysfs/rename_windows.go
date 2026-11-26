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

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func rename(from, to string) wasi.Errno {
	if from == to {
		return 0
	}

	var fromIsDir, toIsDir bool
	if fromStat, errno := stat(from); errno != 0 {
		return errno // failed to stat from
	} else {
		fromIsDir = fromStat.Mode.IsDir()
	}
	if toStat, errno := stat(to); errno == wasi.ENOENT {
		return syscallRename(from, to) // file or dir to not-exist is ok
	} else if errno != 0 {
		return errno // failed to stat to
	} else {
		toIsDir = toStat.Mode.IsDir()
	}

	// Now, handle known cases
	switch {
	case !fromIsDir && toIsDir: // file to dir
		return wasi.EISDIR
	case !fromIsDir && !toIsDir: // file to file
		// Use os.Rename instead of syscall.Rename to overwrite a file.
		// This uses MoveFileEx instead of MoveFile (used by syscall.Rename).
		return wasi.UnwrapOSError(os.Rename(from, to))
	case fromIsDir && !toIsDir: // dir to file
		return wasi.ENOTDIR
	default: // dir to dir

		// We can't tell if a directory is empty or not, via stat information.
		// Reading the directory is expensive, as it can buffer large amounts
		// of data on fail. Instead, speculatively try to remove the directory.
		// This is only one syscall and won't buffer anything.
		if errno := rmdir(to); errno == 0 || errno == wasi.ENOENT {
			return syscallRename(from, to)
		} else {
			return errno
		}
	}
}

func syscallRename(from string, to string) wasi.Errno {
	return wasi.UnwrapOSError(syscall.Rename(from, to))
}
