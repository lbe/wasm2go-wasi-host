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
	if errors.Is(err, syscall.EACCES) {
		return EACCES
	}
	if errors.Is(err, syscall.EPERM) {
		return EPERM
	}
	if errors.Is(err, syscall.ENOTDIR) {
		return ENOTDIR
	}
	if errors.Is(err, syscall.EISDIR) {
		return EISDIR
	}
	if errors.Is(err, syscall.EROFS) {
		return EROFS
	}
	if errors.Is(err, syscall.EINVAL) {
		return EINVAL
	}
	return EIO
}
