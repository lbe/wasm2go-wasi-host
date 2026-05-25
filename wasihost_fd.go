package wasihost

import (
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Xfd_close implements fd_close. Closes the file associated with fd and
// clears the fd-table slot. Returns EBADF if fd is invalid or invalidated.
func (s *State) Xfd_close(fd int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() || s.invalidated[int(fd)] {
		return wasiEBadf
	}
	if entry.file != nil {
		entry.file.Close()
	}
	s.fds[fd] = fdEntry{}
	// Clear any invalidation entry for this fd.
	delete(s.invalidated, int(fd))
	return wasiESuccess
}

// Xfd_read implements fd_read. For fd 0 (stdin), reads from the
// io.Reader configured by [WithStdin]. For other fds, seeks to the
// current fd offset then reads sequentially. Returns EISDIR for
// directory fds, ENOTCAPABLE when FD_READ is not set in the
// fd's rights_base.
func (s *State) Xfd_read(fd int32, iovsPtr int32, iovsCount int32, nreadPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	mem := s.mem()
	if fd == StdinFD {
		if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDRead); errno != 0 {
			return errno
		}
		var total uint32
		for i := int32(0); i < iovsCount; i++ {
			off := iovsPtr + i*8
			bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
			bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
			if bufLen == 0 {
				continue
			}
			var n int
			var err error
			if s.stdin != nil {
				n, err = s.stdin.Read(mem[bufPtr : bufPtr+bufLen])
			} else {
				n, err = 0, io.EOF
			}
			total += uint32(n)
			if err != nil {
				binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
				if err != io.EOF {
					return wasiEIo
				}
				return wasiESuccess
			}
			if n < int(bufLen) {
				break
			}
		}
		binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
		return wasiESuccess
	}
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDRead); errno != 0 {
		return errno
	}
	var total uint32
	// For io.Seeker-backed fds (e.g., osFile), sync the OS kernel position
	// with entry.offset so that subsequent SEEK_CUR via Xfd_seek is correct.
	if seeker, ok := entry.file.(io.Seeker); ok {
		if _, err := seeker.Seek(entry.offset, io.SeekStart); err != nil {
			s.fds[fd] = entry
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return wasiEIo
		}
	}
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		var n int
		var err error
		n, err = entry.file.Read(mem[bufPtr : bufPtr+bufLen])
		total += uint32(n)
		entry.offset += int64(n)
		if err != nil {
			if err == io.EOF {
				break
			}
			s.fds[fd] = entry
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return wasiEIo
		}
		if n < int(bufLen) {
			break
		}
	}
	s.fds[fd] = entry
	binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
	return wasiESuccess
}

// Xfd_write implements fd_write. For fd 1 and 2 (stdout/stderr), writes
// to the io.Writers configured by [WithStdout] and [WithStderr]. For
// other fds backed by io.Seeker (e.g., osFile), seeks to the current
// fd offset then writes sequentially so that the OS kernel position
// advances; uses WriteAt as a fallback for non-seeker files. Returns
// EISDIR for directory fds, ENOTCAPABLE when FD_WRITE is not set in
// the fd's rights_base.
func (s *State) Xfd_write(fd int32, iovsPtr int32, iovsCount int32, nwrittenPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	mem := s.mem()
	if fd == StdoutFD || fd == StderrFD {
		if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDWrite); errno != 0 {
			return errno
		}
		var total uint32
		for i := int32(0); i < iovsCount; i++ {
			off := iovsPtr + i*8
			bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
			bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
			data := mem[bufPtr : bufPtr+bufLen]
			var n int
			var err error
			if fd == StdoutFD {
				if s.stdout != nil {
					n, err = s.stdout.Write(data)
				}
			} else {
				if s.stderr != nil {
					n, err = s.stderr.Write(data)
				}
			}
			total += uint32(n)
			if err != nil {
				binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
				return wasiEIo
			}
		}
		binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
		return wasiESuccess
	}
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDWrite); errno != 0 {
		return errno
	}
	var total uint32
	// For io.Seeker-backed fds (e.g., osFile), sync the OS kernel position
	// with entry.offset (for normal writes) or the end of the file (for
	// APPEND) so that subsequent SEEK_CUR via Xfd_seek is correct.
	if seeker, ok := entry.file.(io.Seeker); ok {
		if entry.fdFlags&uint16(fdFlagsAppend) != 0 {
			// For APPEND, seek to end before writing so Write() goes to
			// the correct OS position regardless of entry.offset.
			if pos, err := seeker.Seek(0, io.SeekEnd); err == nil {
				entry.offset = pos
			}
		} else {
			if _, err := seeker.Seek(entry.offset, io.SeekStart); err != nil {
				s.fds[fd] = entry
				binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
				return wasiEIo
			}
		}
	}
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		// Use io.Writer (Write) for osFile-backed entries that implement
		// io.Writer (all osFile entries do). Fall back to WriteAt for
		// non-Writer files.
		w, ok := entry.file.(io.Writer)
		if !ok {
			wa, ok2 := entry.file.(interface {
				WriteAt([]byte, int64) (int, error)
			})
			if !ok2 {
				break
			}
			n, err := wa.WriteAt(mem[bufPtr:bufPtr+bufLen], entry.offset)
			entry.offset += int64(n)
			total += uint32(n)
			if err != nil {
				s.fds[fd] = entry
				binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
				return wasiEIo
			}
			continue
		}
		n, err := w.Write(mem[bufPtr : bufPtr+bufLen])
		total += uint32(n)
		if entry.fdFlags&uint16(fdFlagsAppend) != 0 {
			// For APPEND, the actual OS position may differ from
			// entry.offset + n. Seek to 0, SEEK_CUR to capture the
			// real OS position.
			if seeker, ok2 := entry.file.(io.Seeker); ok2 {
				if pos, seekErr := seeker.Seek(0, io.SeekCurrent); seekErr == nil {
					entry.offset = pos
				}
			}
		} else {
			entry.offset += int64(n)
		}
		if err != nil {
			s.fds[fd] = entry
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
			return wasiEIo
		}
	}
	s.fds[fd] = entry
	binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
	return wasiESuccess
}

// Xfd_seek implements fd_seek. Delegates to the underlying io.Seeker, stores
// the resulting position in entry.offset, and writes it to guest memory.
// Subsequent fd_read and fd_write on seeker-backed fds seek to entry.offset
// before I/O. Returns EISDIR for directory fds, EINVAL if the file does not
// implement io.Seeker.
func (s *State) Xfd_seek(fd int32, offset int64, whence, newOffsetPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil || entry.preopen {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	sk, ok := entry.file.(io.Seeker)
	if !ok {
		return wasiEInval
	}
	n, err := sk.Seek(offset, int(whence))
	if err != nil {
		if errors.Is(err, syscall.EINVAL) {
			return wasiEInval
		}
		return wasiEIo
	}
	entry.offset = n
	s.fds[fd] = entry
	binary.LittleEndian.PutUint64(s.mem()[newOffsetPtr:], uint64(n))
	return wasiESuccess
}

// Xfd_readdir implements fd_readdir. Writes WASI dirent structs into
// guest memory starting from the entry at cookie. Each dirent is
// 24 + len(name) bytes. For preopen fds backed by fs.ReadDirFS, the
// directory listing is loaded on first call and cached in the fd entry.
func (s *State) Xfd_readdir(fd int32, bufPtr int32, bufLen int32, cookie int64, bufUsedPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := &s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	mem := s.mem()

	// cookie=0 invalidates the per-fd listing snapshot so the next call
	// re-reads from the host. cookie>0 uses the warm snapshot without
	// touching the host (stable across host directory mutations).
	if cookie == 0 {
		entry.readdirSnapshot = nil
		entry.dirFile = nil
		if _, ok := entry.file.(*DirEntriesFile); ok {
			entry.file = nil
		}
	}

	// If a snapshot already exists (from a prior cookie=0 call), use it
	// instead of re-reading from the host.
	if entry.readdirSnapshot == nil {
		// Cache listing on first call for preopens or any directory fd.
		if entry.dirFile == nil {
			if entry.preopen {
				if entry.mount < 0 || entry.mount >= len(s.mounts) {
					return wasiEBadf
				}
				if entry.file == nil {
					if d, ok := s.mounts[entry.mount].root.(fs.ReadDirFS); ok {
						entries, err := d.ReadDir(".")
						if err != nil {
							return wasiEIo
						}
						entry.file = &DirEntriesFile{Entries: entries}
					}
				}
			}
			if entry.file == nil {
				return wasiEBadf
			}
			var df fs.ReadDirFile
			switch f := entry.file.(type) {
			case fs.ReadDirFile:
				df = f
			case *FSFileWrap:
				df, _ = f.File.(fs.ReadDirFile)
			}
			if df == nil {
				return wasiENotDir
			}
			entry.dirFile = df
		}

		// Seek back to the start before reading: ReadDir(-1) returns all
		// entries from the current file offset. A non-zero offset would
		// produce a truncated listing.
		if seeker, ok := entry.file.(io.Seeker); ok {
			// os.File (writable host paths) always supports seeking;
			// a Seek error on those is a genuine fault.  FSFileWrap
			// may fail Seek when the underlying fs.FS doesn't support
			// it — that is benign for ReadDir.
			if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr != nil {
				if _, isWrap := entry.file.(*FSFileWrap); !isWrap {
					return wasiEIo
				}
			}
		}
		rawEntries, err := entry.dirFile.ReadDir(-1)
		if err != nil && err != io.EOF {
			return wasiEIo
		}

		// Restore entries to the file if it's our own DirEntriesFile adapter,
		// so that subsequent calls with cookies can still access them.
		if de, ok := entry.file.(*DirEntriesFile); ok {
			de.idx = 0
		}

		// Prepend synthetic . and .. entries and store the full listing
		// as the per-fd snapshot.
		entries := make([]fs.DirEntry, 0, 2+len(rawEntries))
		entries = append(entries, synthDirEntry{"."}, synthDirEntry{".."})
		entries = append(entries, rawEntries...)
		entry.readdirSnapshot = entries
	}

	entries := entry.readdirSnapshot

	// Pre-compute inodes for synthetic . and .. entries.
	var selfIno, parentIno uint64
	if m := entry.mount; m >= 0 && m < len(s.mounts) && s.mounts[m].writable && s.mounts[m].hostRoot != "" {
		if entry.preopen {
			if fi, err := os.Stat(s.mounts[m].hostRoot); err == nil {
				_, selfIno = statDevIno(fi)
			}
			if fi, err := os.Stat(filepath.Dir(s.mounts[m].hostRoot)); err == nil {
				_, parentIno = statDevIno(fi)
			}
		} else {
			if fi, err := entry.file.Stat(); err == nil {
				_, selfIno = statDevIno(fi)
			}
			parentPath := filepath.Dir(filepath.Join(s.mounts[m].hostRoot, entry.path))
			if fi, err := os.Stat(parentPath); err == nil {
				_, parentIno = statDevIno(fi)
			}
		}
	}

	if int(cookie) >= len(entries) {
		binary.LittleEndian.PutUint32(mem[bufUsedPtr:], 0)
		return wasiESuccess
	}
	var bufUsed uint32
	var i int
	for i = int(cookie); i < len(entries); i++ {
		name := entries[i].Name()
		var ftype byte
		if entries[i].IsDir() {
			ftype = fdDir
		} else {
			ftype = fdFile
		}
		nameLen := uint32(len(name))
		entryLen := uint32(24 + nameLen)
		if bufUsed+entryLen > uint32(bufLen) {
			break
		}
		off := bufPtr + int32(bufUsed)
		binary.LittleEndian.PutUint64(mem[off:], uint64(i+1))
		var ino uint64
		switch i {
		case 0:
			// Synthetic "." entry: use the directory's own inode.
			ino = selfIno
		case 1:
			// Synthetic ".." entry: use the parent directory inode.
			ino = parentIno
		default:
			// Real entries: extract ino from DirEntry.Info().Sys() when available.
			if info, err := entries[i].Info(); err == nil {
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					ino = st.Ino
				}
			}
		}
		binary.LittleEndian.PutUint64(mem[off+8:], ino)
		binary.LittleEndian.PutUint32(mem[off+16:], nameLen)
		binary.LittleEndian.PutUint32(mem[off+20:], uint32(ftype))
		copy(mem[off+24:], name)
		bufUsed += entryLen
	}
	// WASI cookie-based resume: when more entries remain but the last one did
	// not fit in buf, report bufLen in bufUsed. The caller detects this and
	// resumes via the cookie (otherwise it would think the directory is done).
	if i < len(entries) && bufUsed > 0 && bufUsed < uint32(bufLen) {
		bufUsed = uint32(bufLen)
	}
	binary.LittleEndian.PutUint32(mem[bufUsedPtr:], bufUsed)
	return wasiESuccess
}

// Xfd_filestat_get implements fd_filestat_get. Writes a 64-byte filestat
// struct for the open fd. For preopen directory fds, stats the mount root
// via fs.Stat.
