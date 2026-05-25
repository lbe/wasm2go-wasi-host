package wasihost

import (
	"errors"
	"math"
	"os"
	"syscall"
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
// subsequent OpenFile (or overlay fallback) can map errors as usual.

// errnoIfHostPathNotADirectory is the path_open O_DIRECTORY pre-check for a
// resolved writable host path. When the path exists and is not a directory,
// it returns wasiENotDir. Stat errors (including ENOENT) return 0 so the
// subsequent OpenFile (or overlay fallback) can map errors as usual.
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

// errnoForDirectoryFDOp returns EISDIR when the fd entry refers to a
// directory, because byte-oriented I/O (fd_read, fd_pread, fd_write,
// fd_pwrite) and position/size operations (fd_seek, fd_tell,
// fd_allocate, fd_filestat_set_size) are not defined on directories.
// Returns 0 for non-directory entries.
func errnoForDirectoryFDOp(entry fdEntry) int32 {
	if entry.fdType == fdDir {
		return wasiEIsdir
	}
	return 0
}

// mapOSError returns the closest WASI snapshot-preview1 errno for a host
// error from os/syscall (including *os.PathError and wrapped errors).
// Errors from [fs.FS] Open are supported as well: the standard library uses
// the same sentinels for fs and os (e.g. [fs.ErrNotExist] == [os.ErrNotExist],
// [fs.ErrPermission] == [os.ErrPermission]), so [errors.Is] matches both.
//
// Mappings use [errors.Is] against well-known errors:
//
//   - [os.ErrNotExist] → ENOENT (44)
//   - [syscall.ENOTEMPTY] → ENOTEMPTY (55)
//   - [os.ErrExist] → EEXIST (20)
//   - [syscall.ENOTDIR] → ENOTDIR (54)
//   - [syscall.EISDIR] → EISDIR (31)
//   - [syscall.EACCES] → EACCES (2)
//   - [syscall.EPERM] → EPERM (63)
//   - [os.ErrPermission] → EACCES (2)
//   - [syscall.EROFS] → EROFS (66)
//   - [syscall.EXDEV] → EXDEV (75)
//   - [syscall.EINVAL] → EINVAL (28)
//
// Any other error maps to EIO (29).
func mapOSError(err error) int32 {
	if errors.Is(err, os.ErrNotExist) {
		return wasiENoEnt
	}
	if errors.Is(err, syscall.ENOTEMPTY) {
		return wasiENotEmpty
	}
	if errors.Is(err, os.ErrExist) {
		return wasiEExist
	}
	if errors.Is(err, syscall.ENOTDIR) {
		return wasiENotDir
	}
	if errors.Is(err, syscall.EISDIR) {
		return wasiEIsdir
	}
	if errors.Is(err, syscall.EACCES) {
		return wasiEAcces
	}
	if errors.Is(err, syscall.EPERM) {
		return wasiEPerm
	}
	if errors.Is(err, os.ErrPermission) {
		return wasiEAcces
	}
	if errors.Is(err, syscall.EROFS) {
		return wasiEROFS
	}
	if errors.Is(err, syscall.EXDEV) {
		return wasiEXdev
	}
	if errors.Is(err, syscall.EINVAL) {
		return wasiEInval
	}
	return wasiEIo
}
