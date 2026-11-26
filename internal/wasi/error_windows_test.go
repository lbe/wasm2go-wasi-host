//go:build windows

package wasi

import (
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestUnwrapOSErrorWindowsCodes(t *testing.T) {
	tests := []struct {
		name string
		err  syscall.Errno
		want Errno
	}{
		{"file not found", windows.ERROR_FILE_NOT_FOUND, ENOENT},
		{"path not found", windows.ERROR_PATH_NOT_FOUND, ENOTDIR},
		{"already exists", windows.ERROR_ALREADY_EXISTS, EEXIST},
		{"file exists", windows.ERROR_FILE_EXISTS, EEXIST},
		{"dir not empty", windows.ERROR_DIR_NOT_EMPTY, ENOTEMPTY},
		{"rmdir on file", windows.ERROR_DIRECTORY, ENOTDIR},
		{"invalid handle", windows.ERROR_INVALID_HANDLE, EBADF},
		{"access denied", windows.ERROR_ACCESS_DENIED, EBADF},
		{"privilege not held", windows.ERROR_PRIVILEGE_NOT_HELD, EPERM},
		{"negative seek", windows.ERROR_NEGATIVE_SEEK, EINVAL},
		{"not a reparse point", windows.ERROR_NOT_A_REPARSE_POINT, EINVAL},
		{"invalid name", windows.ERROR_INVALID_NAME, EINVAL},
		{"cross device", windows.ERROR_NOT_SAME_DEVICE, EXDEV},
		{"name too long", windows.ERROR_FILENAME_EXCED_RANGE, ENAMETOOLONG},
		{"not supported", windows.ERROR_NOT_SUPPORTED, ENOTSUP},
		{"write protect", windows.ERROR_WRITE_PROTECT, EROFS},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := &os.PathError{Op: "open", Path: "x", Err: tc.err}
			if got := UnwrapOSError(err); got != tc.want {
				t.Errorf("UnwrapOSError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
