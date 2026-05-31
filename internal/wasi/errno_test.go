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
