package wasihost

import (
	"encoding/binary"
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

func (s *State) fdFileSyncOp(fd int32, op func(wasi.File) wasi.Errno) int32 {
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if errno := op(fe.File); errno != 0 {
		return errnoFromWasi(errno)
	}
	return wasiESuccess
}

func (s *State) fdGrowToSize(fe *wasi.FileEntry, minSize int64) int32 {
	if fe == nil || fe.File == nil {
		return wasiENotSup
	}
	st, werrno := fe.File.Stat()
	if werrno != 0 {
		return errnoFromWasi(werrno)
	}
	newSize := minSize
	if st.Size > newSize {
		newSize = st.Size
	}
	if errno := fe.File.Truncate(newSize); errno != 0 {
		return errnoFromWasi(errno)
	}
	return wasiESuccess
}

type fdPosIOStep func(buf []byte, off int64) (n int, newOff int64, stop bool, errno int32)

func (s *State) fdPositionedIOVLoop(mem []byte, iovsPtr, iovsCount int32, startOff int64, nresultPtr int32, shortReadOK bool, step fdPosIOStep) int32 {
	var total uint32
	curOff := startOff
	for i := int32(0); i < iovsCount; i++ {
		buf := guestIOV(mem, iovsPtr, i)
		if buf == nil {
			continue
		}
		n, newOff, stop, errno := step(buf, curOff)
		if stop {
			binary.LittleEndian.PutUint32(mem[nresultPtr:], total)
			return wasiENotSup
		}
		total += uint32(n)
		if errno != 0 {
			binary.LittleEndian.PutUint32(mem[nresultPtr:], total)
			return errno
		}
		curOff = newOff
		if shortReadOK && n < len(buf) {
			break
		}
	}
	binary.LittleEndian.PutUint32(mem[nresultPtr:], total)
	return wasiESuccess
}

func (s *State) Xfd_pread(fd, iovsPtr, iovsCount int32, offset int64, nreadPtr int32) int32 {
	if fd <= StderrFD {
		return wasiEInval
	}
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDRead); errno != 0 {
		return errno
	}
	mem := s.mem()
	return s.fdPositionedIOVLoop(mem, iovsPtr, iovsCount, offset, nreadPtr, true, func(buf []byte, curOff int64) (int, int64, bool, int32) {
		n, werrno := fe.File.Pread(buf, curOff)
		errno := errnoFromWasi(werrno)
		if errno == wasiESuccess {
			return n, curOff + int64(n), false, 0
		}
		return n, curOff, false, errno
	})
}

func (s *State) Xfd_pwrite(fd, iovsPtr, iovsCount int32, offset int64, nwrittenPtr int32) int32 {
	if fd <= StderrFD {
		return wasiEInval
	}
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDWrite); errno != 0 {
		return errno
	}
	writeOff := offset
	if fileAppendEnabled(fe) {
		size, errno := s.fdFileSize(fe)
		if errno != 0 {
			return errno
		}
		writeOff = size
	}
	mem := s.mem()
	return s.fdPositionedIOVLoop(mem, iovsPtr, iovsCount, writeOff, nwrittenPtr, false, func(data []byte, curOff int64) (int, int64, bool, int32) {
		n, werrno := fe.File.Pwrite(data, curOff)
		errno := errnoFromWasi(werrno)
		if errno == wasiESuccess {
			return n, curOff + int64(n), false, 0
		}
		return n, curOff, false, errno
	})
}

func (s *State) Xfd_tell(fd, offsetPtr int32) int32 {
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	binary.LittleEndian.PutUint64(s.mem()[offsetPtr:], uint64(fe.Offset))
	return wasiESuccess
}

func (s *State) Xfd_datasync(fd int32) int32 {
	return s.fdFileSyncOp(fd, func(f wasi.File) wasi.Errno { return f.Datasync() })
}

func (s *State) Xfd_sync(fd int32) int32 {
	return s.fdFileSyncOp(fd, func(f wasi.File) wasi.Errno { return f.Sync() })
}

func (s *State) Xfd_fdstat_set_flags(fd, flags int32) int32 {
	s.assertSingleOwner()
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if fd <= StderrFD {
		return wasiESuccess
	}
	if (flags & ^(fdFlagsAppend | fdFlagsDSync | fdFlagsNonBlock | fdFlagsRSync | fdFlagsSync)) != 0 {
		return wasiEInval
	}
	fe.FdFlags = uint16(flags)
	if errno := fe.File.SetAppend(flags&fdFlagsAppend != 0); errno != 0 {
		return errnoFromWasi(errno)
	}
	if pf, ok := fe.File.(wasi.PollableFile); ok {
		if errno := pf.SetNonblock(flags&fdFlagsNonBlock != 0); errno != 0 {
			return errnoFromWasi(errno)
		}
	}
	return wasiESuccess
}

func (s *State) Xfd_advise(fd int32, offset, length int64, advice int32) int32 { return wasiESuccess }

func (s *State) Xfd_allocate(fd int32, offset, length int64) int32 {
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDAllocate); errno != 0 {
		return errno
	}
	newSize, ok := fileRangeEnd(offset, length)
	if !ok {
		return wasiEInval
	}
	return s.fdGrowToSize(fe, newSize)
}

func (s *State) Xfd_fdstat_set_rights(fd int32, base, inheriting int64) int32 {
	s.assertSingleOwner()
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	uBase := uint64(base)
	uInheriting := uint64(inheriting)
	if (uBase & ^fe.RightsBase) != 0 || (uInheriting & ^fe.RightsInheriting) != 0 {
		return wasiENotCap
	}
	fe.RightsBase = uBase
	fe.RightsInheriting = uInheriting
	return wasiESuccess
}

func (s *State) Xfd_filestat_set_size(fd int32, size int64) int32 {
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	if osf, ok := sysfs.UnderlyingOSFile(fe.File); ok {
		if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDFilestatSetSize); errno != 0 {
			return errno
		}
		if err := osf.Truncate(size); err != nil {
			return mapOSError(err)
		}
	}
	return wasiESuccess
}

func (s *State) Xfd_filestat_set_times(fd int32, atim, mtim int64, fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstMtim|fstAtimNow|fstMtimNow) == 0 {
		return wasiESuccess
	}
	if errno := errnoIfContradictoryFstFlags(fstFlags); errno != 0 {
		return errno
	}
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	osf, ok := sysfs.UnderlyingOSFile(fe.File)
	if !ok {
		return wasiESuccess
	}
	fi, err := osf.Stat()
	if err != nil {
		return mapOSError(err)
	}
	targetAtim, targetMtim := computeTargetTimes(fi, atim, mtim, fstFlags)
	if err := os.Chtimes(osf.Name(), targetAtim, targetMtim); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}
