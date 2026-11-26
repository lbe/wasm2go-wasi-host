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

// mapSyscallErrno is consulted before the os.Err* sentinels. syscall.Errno.Is
// reports ENOTEMPTY as fs.ErrExist and EPERM as fs.ErrPermission, so checking
// the sentinels first would turn ENOTEMPTY into EEXIST and EPERM into EACCES.
// A slice (not a switch) tolerates platforms where two names share a value,
// such as ENOTSUP and EOPNOTSUPP on Linux.
var mapSyscallErrno = []struct {
	err   syscall.Errno
	errno Errno
}{
	{syscall.EACCES, EACCES},
	{syscall.EAGAIN, EAGAIN},
	{syscall.EBADF, EBADF},
	{syscall.EEXIST, EEXIST},
	{syscall.EFAULT, EFAULT},
	{syscall.EINTR, EINTR},
	{syscall.EINVAL, EINVAL},
	{syscall.EIO, EIO},
	{syscall.EISDIR, EISDIR},
	{syscall.ELOOP, ELOOP},
	{syscall.ENAMETOOLONG, ENAMETOOLONG},
	{syscall.ENOENT, ENOENT},
	{syscall.ENOSYS, ENOSYS},
	{syscall.ENOTDIR, ENOTDIR},
	{syscall.ENOTEMPTY, ENOTEMPTY},
	{syscall.ENOTSOCK, ENOTSOCK},
	{syscall.ENOTSUP, ENOTSUP},
	{syscall.EOPNOTSUPP, ENOTSUP},
	{syscall.EPERM, EPERM},
	{syscall.ERANGE, ERANGE},
	{syscall.EROFS, EROFS},
	{syscall.EXDEV, EXDEV},
}

func mapOSError(err error) Errno {
	var sysErr syscall.Errno
	if errors.As(err, &sysErr) {
		if errno, ok := mapPlatformErrno(sysErr); ok {
			return errno
		}
		for _, m := range mapSyscallErrno {
			if sysErr == m.err {
				return m.errno
			}
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return ENOENT
	}
	if errors.Is(err, os.ErrExist) {
		return EEXIST
	}
	if errors.Is(err, os.ErrPermission) {
		return EACCES
	}
	return EIO
}
