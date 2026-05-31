// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// UnwrapOSError returns an Errno or zero if the input is nil.
func UnwrapOSError(err error) Errno {
	if err == nil {
		return 0
	}
	if err == io.EOF {
		return 0
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return mapOSError(pathErr.Err)
	}
	return mapOSError(err)
}

var mapSyscallErrno = []struct {
	err   error
	errno Errno
}{
	{syscall.EAGAIN, EAGAIN},
	{syscall.EACCES, EACCES},
	{syscall.EPERM, EPERM},
	{syscall.ENOTDIR, ENOTDIR},
	{syscall.EISDIR, EISDIR},
	{syscall.EROFS, EROFS},
	{syscall.EINVAL, EINVAL},
	{syscall.ELOOP, ELOOP},
}

func mapOSError(err error) Errno {
	if errors.Is(err, os.ErrNotExist) {
		return ENOENT
	}
	if errors.Is(err, os.ErrExist) {
		return EEXIST
	}
	if errors.Is(err, os.ErrPermission) {
		return EACCES
	}
	for _, m := range mapSyscallErrno {
		if errors.Is(err, m.err) {
			return m.errno
		}
	}
	return EIO
}
