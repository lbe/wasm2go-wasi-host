package wasi

import (
	"errors"
	"io"
	"os"
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
	return EIO
}

func mapOSError(err error) Errno {
	switch err {
	case os.ErrPermission:
		return EPERM
	case os.ErrNotExist:
		return ENOENT
	default:
		return EIO
	}
}
