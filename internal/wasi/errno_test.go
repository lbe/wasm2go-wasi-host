// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"io"
	"os"
	"syscall"
	"testing"
)

func TestUnwrapOSError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected Errno
	}{
		{
			name:     "io.EOF is not an error",
			input:    io.EOF,
			expected: 0,
		},
		{
			name:     "PathError ErrPermission",
			input:    &os.PathError{Err: os.ErrPermission},
			expected: EACCES,
		},
		{
			name:     "PathError ErrNotExist",
			input:    &os.PathError{Err: os.ErrNotExist},
			expected: ENOENT,
		},
		{
			name:     "PathError unknown == EIO",
			input:    &os.PathError{Err: os.ErrClosed},
			expected: EIO,
		},
		// syscall.Errno.Is reports ENOTEMPTY as fs.ErrExist and EPERM as
		// fs.ErrPermission; the specific errno must win over those sentinels.
		{
			name:     "PathError ENOTEMPTY is not EEXIST",
			input:    &os.PathError{Op: "rmdir", Err: syscall.ENOTEMPTY},
			expected: ENOTEMPTY,
		},
		{
			name:     "PathError EPERM is not EACCES",
			input:    &os.PathError{Op: "unlink", Err: syscall.EPERM},
			expected: EPERM,
		},
		{
			name:     "PathError EEXIST",
			input:    &os.PathError{Op: "mkdir", Err: syscall.EEXIST},
			expected: EEXIST,
		},
		{
			name:     "PathError ENOENT",
			input:    &os.PathError{Op: "open", Err: syscall.ENOENT},
			expected: ENOENT,
		},
		{
			name:     "LinkError EXDEV",
			input:    &os.LinkError{Op: "rename", Err: syscall.EXDEV},
			expected: EXDEV,
		},
		{
			name:     "PathError EBADF",
			input:    &os.PathError{Op: "read", Err: syscall.EBADF},
			expected: EBADF,
		},
		{
			name:     "PathError ENAMETOOLONG",
			input:    &os.PathError{Op: "open", Err: syscall.ENAMETOOLONG},
			expected: ENAMETOOLONG,
		},
		{
			name:     "bare ENOSYS",
			input:    syscall.ENOSYS,
			expected: ENOSYS,
		},
		{
			name:     "bare EINTR",
			input:    syscall.EINTR,
			expected: EINTR,
		},
		{
			name:     "bare ENOTSUP",
			input:    syscall.ENOTSUP,
			expected: ENOTSUP,
		},
		{
			name:     "bare ENOTSOCK",
			input:    syscall.ENOTSOCK,
			expected: ENOTSOCK,
		},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			if want, have := tc.expected, UnwrapOSError(tc.input); have != want {
				t.Fatalf("unexpected errno: %v != %v", have, want)
			}
		})
	}
}
