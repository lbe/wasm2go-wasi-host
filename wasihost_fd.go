package wasihost

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func (s *State) Xfd_close(fd int32) int32 {
	s.assertSingleOwner()
	if _, ok := s.lookupFD(fd); !ok {
		return wasiEBadf
	}
	if s.ctx == nil || s.ctx.FSContext() == nil {
		return wasiEBadf
	}
	if closeErr := s.ctx.FSContext().CloseFile(fd); closeErr != 0 {
		return errnoFromWasi(closeErr)
	}
	return wasiESuccess
}

func guestIOV(mem []byte, iovsPtr, i int32) []byte {
	off := iovsPtr + i*8
	bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
	bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
	if bufLen == 0 {
		return nil
	}
	return mem[bufPtr : bufPtr+bufLen]
}

func (s *State) fdFileSize(fe *wasi.FileEntry) (size int64, errno int32) {
	if fe == nil || fe.File == nil {
		return 0, wasiEBadf
	}
	st, werrno := fe.File.Stat()
	if werrno != 0 {
		return 0, errnoFromWasi(werrno)
	}
	return st.Size, 0
}

func (s *State) Xfd_read(fd int32, iovsPtr int32, iovsCount int32, nreadPtr int32) int32 {
	s.assertSingleOwner()
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	mem := s.mem()
	if fd == StdinFD {
		if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDRead); errno != 0 {
			return errno
		}
		var total uint32
		for i := int32(0); i < iovsCount; i++ {
			buf := guestIOV(mem, iovsPtr, i)
			if buf == nil {
				continue
			}
			n, werrno := fe.File.Read(buf)
			total += uint32(n)
			if werrno != 0 && werrno != wasi.EAGAIN {
				binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
				if werrno == wasi.EIO {
					return wasiEIo
				}
				return errnoFromWasi(werrno)
			}
			if n < len(buf) {
				break
			}
		}
		binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
		return wasiESuccess
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDRead); errno != 0 {
		return errno
	}
	var total uint32
	for i := int32(0); i < iovsCount; i++ {
		buf := guestIOV(mem, iovsPtr, i)
		if buf == nil {
			continue
		}
		n, werrno := fe.File.Pread(buf, fe.Offset)
		errno := errnoFromWasi(werrno)
		total += uint32(n)
		if errno != 0 {
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return errno
		}
		fe.Offset += int64(n)
		if n < len(buf) {
			break
		}
	}
	binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
	return wasiESuccess
}

func (s *State) Xfd_write(fd int32, iovsPtr int32, iovsCount int32, nwrittenPtr int32) int32 {
	s.assertSingleOwner()
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	mem := s.mem()
	if fd == StdoutFD || fd == StderrFD {
		if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDWrite); errno != 0 {
			return errno
		}
		var total uint32
		for i := int32(0); i < iovsCount; i++ {
			data := guestIOV(mem, iovsPtr, i)
			if data == nil {
				continue
			}
			n, werrno := fe.File.Write(data)
			total += uint32(n)
			if werrno != 0 {
				binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
				return errnoFromWasi(werrno)
			}
		}
		binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
		return wasiESuccess
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(fe.RightsBase, rightFDWrite); errno != 0 {
		return errno
	}
	curOff := fe.Offset
	if fileAppendEnabled(fe) {
		size, errno := s.fdFileSize(fe)
		if errno != 0 {
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], 0)
			return errno
		}
		curOff = size
		fe.Offset = curOff
	}
	var total uint32
	for i := int32(0); i < iovsCount; i++ {
		data := guestIOV(mem, iovsPtr, i)
		if data == nil {
			continue
		}
		n, werrno := fe.File.Pwrite(data, curOff)
		errno := errnoFromWasi(werrno)
		total += uint32(n)
		if errno != 0 {
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
			return errno
		}
		curOff += int64(n)
		fe.Offset = curOff
	}
	binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
	return wasiESuccess
}

func (s *State) Xfd_seek(fd int32, offset int64, whence, newOffsetPtr int32) int32 {
	s.assertSingleOwner()
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	if fe.IsPreopen {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(fe); errno != 0 {
		return errno
	}
	curOff := fe.Offset
	var n int64
	switch int(whence) {
	case io.SeekStart:
		n = offset
	case io.SeekCurrent:
		n = curOff + offset
	case io.SeekEnd:
		size, errno := s.fdFileSize(fe)
		if errno != 0 {
			if errno == wasiENoEnt {
				return wasiEInval
			}
			return errno
		}
		n = size + offset
	default:
		return wasiEInval
	}
	if n < 0 {
		return wasiEInval
	}
	if _, werrno := fe.File.Seek(n, io.SeekStart); werrno != 0 {
		if werrno == wasi.ENOTSUP || werrno == wasi.ENOSYS {
			return wasiEInval
		}
		return errnoFromWasi(werrno)
	}
	fe.Offset = n
	binary.LittleEndian.PutUint64(s.mem()[newOffsetPtr:], uint64(n))
	return wasiESuccess
}

func writeWASIDirent(mem []byte, off int32, cookie uint64, ino uint64, fileType byte, name string) {
	nameLen := uint32(len(name))
	binary.LittleEndian.PutUint64(mem[off:], cookie)
	binary.LittleEndian.PutUint64(mem[off+8:], ino)
	binary.LittleEndian.PutUint32(mem[off+16:], nameLen)
	binary.LittleEndian.PutUint32(mem[off+20:], uint32(fileType))
	copy(mem[off+24:], name)
}

func readdirEntryLen(nameLen uint32) uint32 {
	return 24 + nameLen
}

func readdirMaxEntries(bufLen int32) uint32 {
	return uint32(bufLen)/25 + 1
}

func readdirResumeBufUsed(bufUsed, bufLen uint32, wrotePartial bool) uint32 {
	if wrotePartial && bufUsed > 0 && bufUsed < bufLen {
		return bufLen
	}
	return bufUsed
}

// TODO(post-merge): readdirDotDotIno uses os.Stat on host paths directly, bypassing
// wasi.FS. Prefer a VFS method (e.g. parent Stat/Lstat on guest-relative path) so
// inode lookup stays inside the FS abstraction and works for non-host backends.
func (s *State) readdirDotDotIno(fe *wasi.FileEntry) uint64 {
	if fe == nil || fe.FS == nil || fsysReadOnly(fe.FS) {
		return 0
	}
	hostRoot := preopenHostRootFromFS(fe.FS)
	if hostRoot == "" {
		return 0
	}
	if fe.IsPreopen {
		if fi, err := os.Stat(filepath.Dir(hostRoot)); err == nil {
			_, ino := statDevIno(fi)
			return ino
		}
		return 0
	}
	rel := strings.TrimPrefix(fe.Name, "/")
	parentPath := filepath.Dir(filepath.Join(hostRoot, filepath.FromSlash(rel)))
	if fi, err := os.Stat(parentPath); err == nil {
		_, ino := statDevIno(fi)
		return ino
	}
	return 0
}

func (s *State) Xfd_readdir(fd int32, bufPtr int32, bufLen int32, cookie int64, bufUsedPtr int32) int32 {
	s.assertSingleOwner()
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	cache, werrno := fe.DirentCache()
	if werrno != 0 {
		return errnoFromWasi(werrno)
	}
	if ino := s.readdirDotDotIno(fe); ino != 0 {
		cache.SetDotDotIno(wasi.Inode(ino))
	}
	mem := s.mem()
	if bufLen < 25 {
		binary.LittleEndian.PutUint32(mem[bufUsedPtr:], 0)
		return wasiESuccess
	}

	dirents, werrno := cache.Read(uint64(cookie), readdirMaxEntries(bufLen))
	if werrno != 0 {
		if werrno == wasi.ENOENT {
			binary.LittleEndian.PutUint32(mem[bufUsedPtr:], 0)
			return wasiESuccess
		}
		return errnoFromWasi(werrno)
	}

	var bufUsed uint32
	idx := int(cookie)
	wrotePartial := false
	for i, d := range dirents {
		nameLen := uint32(len(d.Name))
		entryLen := readdirEntryLen(nameLen)
		if bufUsed+entryLen > uint32(bufLen) {
			wrotePartial = i > 0
			break
		}
		ino := uint64(d.Ino)
		writeWASIDirent(mem, bufPtr+int32(bufUsed), uint64(idx+1), ino, filestatFdTypeFromMode(d.Type), d.Name)
		bufUsed += entryLen
		idx++
	}

	binary.LittleEndian.PutUint32(mem[bufUsedPtr:], readdirResumeBufUsed(bufUsed, uint32(bufLen), wrotePartial))
	return wasiESuccess
}
