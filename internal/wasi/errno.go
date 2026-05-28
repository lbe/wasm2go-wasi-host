package wasi

import "strconv"

// Errno is a subset of POSIX errno values.
type Errno uint16

const (
	EACCES Errno = iota + 1
	EAGAIN
	EBADF
	EEXIST
	EFAULT
	EINTR
	EINVAL
	EIO
	EISDIR
	ELOOP
	ENAMETOOLONG
	ENOENT
	ENOSYS
	ENOTDIR
	ERANGE
	ENOTEMPTY
	ENOTSOCK
	ENOTSUP
	EPERM
	EROFS
)

func (e Errno) Error() string {
	switch e {
	case 0:
		return "success"
	case EACCES:
		return "permission denied"
	case EAGAIN:
		return "resource unavailable, try again"
	case EBADF:
		return "bad file descriptor"
	case EEXIST:
		return "file exists"
	case EFAULT:
		return "bad address"
	case EINTR:
		return "interrupted function"
	case EINVAL:
		return "invalid argument"
	case EIO:
		return "input/output error"
	case EISDIR:
		return "is a directory"
	case ELOOP:
		return "too many levels of symbolic links"
	case ENAMETOOLONG:
		return "filename too long"
	case ENOENT:
		return "no such file or directory"
	case ENOSYS:
		return "functionality not supported"
	case ENOTDIR:
		return "not a directory or a symbolic link to a directory"
	case ERANGE:
		return "result too large"
	case ENOTEMPTY:
		return "directory not empty"
	case ENOTSOCK:
		return "not a socket"
	case ENOTSUP:
		return "not supported (may be the same value as [EOPNOTSUPP])"
	case EPERM:
		return "operation not permitted"
	case EROFS:
		return "read-only file system"
	default:
		return "Errno(" + strconv.Itoa(int(e)) + ")"
	}
}
