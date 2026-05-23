package wasihost

import (
	"encoding/binary"
	"io"
)

// Xfd_pread implements fd_pread. Reads from fd at the given offset
// without updating the fd's WASI-level offset (entry.offset). Requires
// the underlying file to implement ReadAt; if it does not, no bytes are
// read and ENOTSUP is returned. Returns EINVAL for fds 0-2 (positioned
// reads on stdio are not defined by WASI). Returns EISDIR for directory
// fds, ENOTCAPABLE when FD_READ is not set in the fd's rights_base.
func (s *State) Xfd_pread(fd, iovsPtr, iovsCount int32, offset int64, nreadPtr int32) int32 {
	if fd <= StderrFD {
		return wasiEInval
	}
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDRead); errno != 0 {
		return errno
	}
	mem := s.mem()
	var total uint32
	curOff := offset
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		ra, ok := entry.file.(interface {
			ReadAt([]byte, int64) (int, error)
		})
		if !ok {
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return wasiENotSup
		}
		n, err := ra.ReadAt(mem[bufPtr:bufPtr+bufLen], curOff)
		total += uint32(n)
		curOff += int64(n)
		if err != nil {
			if err == io.EOF {
				break
			}
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return wasiEIo
		}
	}
	binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
	return wasiESuccess
}

// Xfd_pwrite implements fd_pwrite. Writes to fd at the given offset
// without updating the fd's WASI-level offset (entry.offset). Requires
// the underlying file to implement WriteAt; if it does not, no bytes are
// written and ENOTSUP is returned. If WriteAt returns an error, fd_pwrite
// returns EIO and preserves the partial byte count in guest memory.
// Returns EINVAL for fds 0-2 (positioned writes on stdio are not
// defined by WASI). Returns EISDIR for directory fds, ENOTCAPABLE when
// FD_WRITE is not set in the fd's rights_base.

// Xfd_pwrite implements fd_pwrite. Writes to fd at the given offset
// without updating the fd's WASI-level offset (entry.offset). Requires
// the underlying file to implement WriteAt; if it does not, no bytes are
// written and ENOTSUP is returned. If WriteAt returns an error, fd_pwrite
// returns EIO and preserves the partial byte count in guest memory.
// Returns EINVAL for fds 0-2 (positioned writes on stdio are not
// defined by WASI). Returns EISDIR for directory fds, ENOTCAPABLE when
// FD_WRITE is not set in the fd's rights_base.
func (s *State) Xfd_pwrite(fd, iovsPtr, iovsCount int32, offset int64, nwrittenPtr int32) int32 {
	if fd <= StderrFD {
		return wasiEInval
	}
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDWrite); errno != 0 {
		return errno
	}

	mem := s.mem()
	var total uint32
	curOff := offset
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		wa, ok := entry.file.(interface {
			WriteAt([]byte, int64) (int, error)
		})
		if !ok {
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
			return wasiENotSup
		}

		n, err := wa.WriteAt(mem[bufPtr:bufPtr+bufLen], curOff)
		total += uint32(n)
		curOff += int64(n)
		if err != nil {
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
			return wasiEIo
		}
	}
	binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
	return wasiESuccess
}

// Xfd_tell implements fd_tell. Returns the WASI-level fd offset
// (entry.offset) rather than the kernel file position. This is necessary
// because fd_write uses WriteAt, which does not advance the kernel
// position; reading it back via Seek(0, SeekCurrent) would return a
// stale value. Returns EISDIR for directory fds.

// Xfd_tell implements fd_tell. Returns the WASI-level fd offset
// (entry.offset) rather than the kernel file position. This is necessary
// because fd_write uses WriteAt, which does not advance the kernel
// position; reading it back via Seek(0, SeekCurrent) would return a
// stale value. Returns EISDIR for directory fds.
func (s *State) Xfd_tell(fd, offsetPtr int32) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	binary.LittleEndian.PutUint64(s.mem()[offsetPtr:], uint64(entry.offset))
	return wasiESuccess
}

// Xsched_yield implements sched_yield. This host is synchronous;
// yielding calls the [runtime.Gosched] seam and returns ESUCCESS.

// Xfd_datasync implements fd_datasync. Validates that fd is a valid
// open file descriptor index. For sync-capable fds (those whose underlying
// file implements a Sync() error method, such as osFile), invokes host Sync
// and maps any error. For other files, returns ESUCCESS without mutation.
func (s *State) Xfd_datasync(fd int32) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil && entry.fdType == 0 {
		return wasiEBadf
	}
	if syncer, ok := entry.file.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			return mapOSError(err)
		}
	}
	return wasiESuccess
}

// Xfd_sync implements fd_sync. Always returns ESUCCESS.

// Xfd_sync implements fd_sync. Always returns ESUCCESS.
func (s *State) Xfd_sync(fd int32) int32 {
	return wasiESuccess
}

// Xfd_fdstat_set_flags implements fd_fdstat_set_flags.
// Supported flags: APPEND, DSYNC, NONBLOCK, RSYNC, SYNC.

// Xfd_fdstat_set_flags implements fd_fdstat_set_flags.
// Supported flags: APPEND, DSYNC, NONBLOCK, RSYNC, SYNC.
func (s *State) Xfd_fdstat_set_flags(fd, flags int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := &s.fds[fd]
	if entry.file == nil && entry.fdType == 0 {
		return wasiEBadf
	}
	// WASI only allows setting these 5 flags.
	if (flags & ^(fdFlagsAppend | fdFlagsDSync | fdFlagsNonBlock | fdFlagsRSync | fdFlagsSync)) != 0 {
		return wasiEInval
	}
	entry.fdFlags = uint16(flags)
	return wasiESuccess
}

// Xfd_advise implements fd_advise (POSIX posix_fadvise). Always returns
// ESUCCESS. There is no portable Go equivalent for this hint; it is
// silently ignored.

// Xfd_advise implements fd_advise (POSIX posix_fadvise). Always returns
// ESUCCESS. There is no portable Go equivalent for this hint; it is
// silently ignored.
func (s *State) Xfd_advise(fd int32, offset, length int64, advice int32) int32 { return wasiESuccess }

// Xfd_allocate implements fd_allocate (fallocate). Returns EISDIR for
// directory fds. For regular files, disk-space pre-reservation is advisory;
// this is an intentional no-op.

// Xfd_allocate implements fd_allocate (fallocate). Returns EISDIR for
// directory fds. For regular files, disk-space pre-reservation is advisory;
// this is an intentional no-op.
func (s *State) Xfd_allocate(fd int32, offset, length int64) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	return wasiESuccess
}

func (s *State) Xfd_fdstat_set_rights(fd int32, base, inheriting int64) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := &s.fds[fd]
	if entry.file == nil && entry.fdType == 0 {
		return wasiEBadf
	}
	uBase := uint64(base)
	uInheriting := uint64(inheriting)
	if (uBase & ^entry.rightsBase) != 0 || (uInheriting & ^entry.rightsInheriting) != 0 {
		return wasiENotCap
	}
	entry.rightsBase = uBase
	entry.rightsInheriting = uInheriting
	return wasiESuccess
}

// Xproc_raise implements proc_raise. Always returns ENOSYS. Raising a
// signal inside a WASM guest has no meaningful host mapping.
