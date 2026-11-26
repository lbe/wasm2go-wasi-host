//go:build windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// mapPlatformErrno maps Win32 error codes to the Unix-style WASI errnos this
// host reports on every platform. Codes not listed fall through to the
// portable syscall table (Go defines syscall.ENOENT as ERROR_FILE_NOT_FOUND
// and syscall.ENOTDIR as ERROR_PATH_NOT_FOUND on Windows).
func mapPlatformErrno(err syscall.Errno) (Errno, bool) {
	switch err {
	case windows.ERROR_ALREADY_EXISTS, windows.ERROR_FILE_EXISTS:
		return EEXIST, true
	case windows.ERROR_DIRECTORY:
		// Returned by RemoveDirectory on a non-directory.
		return ENOTDIR, true
	case windows.ERROR_DIR_NOT_EMPTY:
		return ENOTEMPTY, true
	case windows.ERROR_INVALID_HANDLE, windows.WSAENOTSOCK, windows.ERROR_ACCESS_DENIED:
		// POSIX read and write report EBADF, not EACCES, for a handle not
		// open in the needed mode; WSAENOTSOCK comes from winsock_select.
		return EBADF, true
	case windows.ERROR_PRIVILEGE_NOT_HELD:
		return EPERM, true
	case windows.ERROR_NEGATIVE_SEEK, windows.ERROR_NOT_A_REPARSE_POINT, windows.ERROR_INVALID_NAME:
		// os.Truncate, os.Readlink on a non-link, and a trailing slash on
		// open, respectively.
		return EINVAL, true
	case windows.ERROR_NOT_SAME_DEVICE:
		return EXDEV, true
	case windows.ERROR_FILENAME_EXCED_RANGE:
		return ENAMETOOLONG, true
	case windows.ERROR_NOT_SUPPORTED:
		return ENOTSUP, true
	case windows.ERROR_WRITE_PROTECT:
		return EROFS, true
	}
	return 0, false
}
