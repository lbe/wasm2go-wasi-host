package wasihost

import (
	"math"
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// fileRangeEnd returns offset+length when offset and length are non-negative
// and the sum does not overflow int64; otherwise ok is false.
func fileRangeEnd(offset, length int64) (end int64, ok bool) {
	if offset < 0 || length < 0 {
		return 0, false
	}
	if length > 0 && offset > math.MaxInt64-length {
		return 0, false
	}
	return offset + length, true
}

// errnoIfFDRightsMissing returns wasiENotCap if rightsBase does not include
// every bit set in required; otherwise it returns 0.
func errnoIfFDRightsMissing(rightsBase, required uint64) int32 {
	if (rightsBase & required) != required {
		return wasiENotCap
	}
	return wasiESuccess
}

// errnoIfHostPathNotADirectory is the path_open O_DIRECTORY pre-check for a
// resolved writable host path. When the path exists and is not a directory,
// it returns wasiENotDir. Stat errors (including ENOENT) return 0 so the
// subsequent OpenFile can map errors as usual.
func errnoIfHostPathNotADirectory(hostPath string) int32 {
	fi, err := os.Stat(hostPath)
	if err == nil && !fi.IsDir() {
		return wasiENotDir
	}
	return 0
}

// errnoIfContradictoryFstFlags returns wasiEInval when fstFlags contains both
// ATIM and ATIM_NOW or both MTIM and MTIM_NOW; otherwise it returns
// wasiESuccess.
func errnoIfContradictoryFstFlags(fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstAtimNow) == fstAtim|fstAtimNow {
		return wasiEInval
	}
	if fstFlags&(fstMtim|fstMtimNow) == fstMtim|fstMtimNow {
		return wasiEInval
	}
	return wasiESuccess
}

// errnoForDirectoryFDOp returns EISDIR when fe refers to a directory, because
// byte-oriented I/O (fd_read, fd_pread, fd_write, fd_pwrite), position/size
// operations (fd_seek, fd_tell, fd_allocate, fd_filestat_set_size,
// fd_filestat_set_times), and similar ops are not defined on directories.
func errnoForDirectoryFDOp(fe *wasi.FileEntry) int32 {
	if fe == nil || fe.File == nil {
		return 0
	}
	if isDir, errno := fe.File.IsDir(); errno != 0 {
		return errnoFromWasi(errno)
	} else if isDir {
		return wasiEIsdir
	}
	return 0
}

// mapOSError returns the WASI snapshot-preview1 errno for a host error from
// os/syscall (including *os.PathError, *os.LinkError and wrapped errors) or
// from [fs.FS]. It delegates to [wasi.UnwrapOSError] so host-path helpers and
// the VFS layer share one translation table, then converts the result with
// [errnoFromWasi]. Unrecognized errors map to EIO (29).
func mapOSError(err error) int32 {
	return errnoFromWasi(wasi.UnwrapOSError(err))
}

// errnoFromWasi maps internal/wasi.Errno (sys.File, FSContext) to WASI
// snapshot-preview1 errno values returned to the guest. Used by fd_read,
// fd_write, fd_seek and other methods that call fe.File.
func errnoFromWasi(errno wasi.Errno) int32 {
	if errno == 0 {
		return wasiESuccess
	}
	switch errno {
	case wasi.EACCES:
		return wasiEAcces
	case wasi.EAGAIN:
		return wasiEAgain
	case wasi.EBADF:
		return wasiEBadf
	case wasi.EEXIST:
		return wasiEExist
	case wasi.EFAULT:
		return wasiEFault
	case wasi.EINTR:
		return wasiEIntr
	case wasi.EINVAL:
		return wasiEInval
	case wasi.EIO:
		return wasiEIo
	case wasi.EISDIR:
		return wasiEIsdir
	case wasi.ELOOP:
		return wasiELoop
	case wasi.ENAMETOOLONG:
		return wasiENameTooLong
	case wasi.ENOENT:
		return wasiENoEnt
	case wasi.ENOSYS:
		return wasiENoSys
	case wasi.ENOTDIR:
		return wasiENotDir
	case wasi.ENOTEMPTY:
		return wasiENotEmpty
	case wasi.ENOTSOCK:
		return wasiENotSock
	case wasi.ENOTSUP:
		return wasiENotSup
	case wasi.EPERM:
		return wasiEPerm
	case wasi.ERANGE:
		return wasiERange
	case wasi.EROFS:
		return wasiEROFS
	case wasi.EXDEV:
		return wasiEXdev
	default:
		return wasiEIo
	}
}
