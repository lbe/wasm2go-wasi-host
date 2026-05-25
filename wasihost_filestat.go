package wasihost

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// fdTypeFromInfo returns fdDir when fi is non-nil and represents a directory;
// otherwise it returns fdFile.
func fdTypeFromInfo(fi fs.FileInfo) byte {
	if fi != nil && fi.IsDir() {
		return fdDir
	}
	return fdFile
}

// writeFdstat writes a 24-byte WASI fdstat struct at statPtr in mem.
// Layout: fdtype(2) + flags(2) + padding(4) + rights_base(8) + rights_inheriting(8).
func writeFdstat(mem []byte, statPtr int32, fdType byte, fdFlags uint16, rightsBase, rightsInheriting uint64) {
	var buf [24]byte
	binary.LittleEndian.PutUint16(buf[0:], uint16(fdType))
	binary.LittleEndian.PutUint16(buf[2:], fdFlags)
	binary.LittleEndian.PutUint32(buf[4:], 0)
	binary.LittleEndian.PutUint64(buf[8:], rightsBase)
	binary.LittleEndian.PutUint64(buf[16:], rightsInheriting)
	copy(mem[statPtr:], buf[:])
}

// statDevIno extracts dev and ino from an fs.FileInfo's underlying syscall.Stat_t.
// Returns zero values if the underlying type is not *syscall.Stat_t.
func statDevIno(fi fs.FileInfo) (dev uint64, ino uint64) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev), st.Ino
	}
	return 0, 0
}

// statAtimNs returns the access time in nanoseconds for fi when the host
// stat buffer is available; otherwise falls back to modification time.
func statAtimNs(fi fs.FileInfo) int64 {
	if _, ok := fi.Sys().(*syscall.Stat_t); ok {
		return getAtimeFromStat(fi).UnixNano()
	}
	return fi.ModTime().UnixNano()
}

// writeFilestat writes a 64-byte WASI filestat struct at bufPtr in mem.
// fdType is a WASI snapshot-preview1 filetype tag (same values as fdstat.fs_filetype),
// e.g. fdDir, fdFile, fdSymlink.
func writeFilestat(mem []byte, bufPtr int32, fdType byte, size int64, atimNs, mtimeNs int64, dev uint64, ino uint64) {
	var buf [filestatSize]byte
	binary.LittleEndian.PutUint64(buf[filestatDevOff:], dev)
	binary.LittleEndian.PutUint64(buf[filestatInoOff:], ino)
	binary.LittleEndian.PutUint64(buf[filestatTypeOff:], uint64(fdType))
	binary.LittleEndian.PutUint64(buf[filestatNlinkOff:], 1)
	binary.LittleEndian.PutUint64(buf[filestatSizeOff:], uint64(size))
	binary.LittleEndian.PutUint64(buf[filestatAtimOff:], uint64(atimNs))
	binary.LittleEndian.PutUint64(buf[filestatMtimOff:], uint64(mtimeNs))
	binary.LittleEndian.PutUint64(buf[filestatCtimOff:], uint64(mtimeNs))
	copy(mem[bufPtr:], buf[:])
}

// writeFilestatFromInfo writes a WASI filestat struct for fi at bufPtr.
func writeFilestatFromInfo(mem []byte, bufPtr int32, fdType byte, fi fs.FileInfo) {
	dev, ino := statDevIno(fi)
	writeFilestat(mem, bufPtr, fdType, fi.Size(), statAtimNs(fi), fi.ModTime().UnixNano(), dev, ino)
}

// filestatFdTypeFromInfo maps os.FileInfo / fs.FileInfo to a WASI preview1
// filetype for the filestat struct.
func filestatFdTypeFromInfo(fi fs.FileInfo) byte {
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return fdSymlink
	case fi.IsDir():
		return fdDir
	default:
		return fdFile
	}
}

// Xfd_filestat_get implements fd_filestat_get. Writes a 64-byte filestat
// struct for the open fd. For preopen directory fds, stats the mount root
// via fs.Stat.
func (s *State) Xfd_filestat_get(fd, bufPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.preopen {
		if entry.mount < 0 || entry.mount >= len(s.mounts) {
			return wasiEBadf
		}
		mnt := s.mounts[entry.mount]
		if mnt.writable && mnt.hostRoot != "" {
			hostFi, err := os.Stat(mnt.hostRoot)
			if err != nil {
				return wasiEIo
			}
			writeFilestatFromInfo(s.mem(), bufPtr, fdDir, hostFi)
		} else {
			fi, err := fs.Stat(mnt.root, ".")
			if err != nil {
				return wasiEIo
			}
			writeFilestatFromInfo(s.mem(), bufPtr, fdDir, fi)
		}
		return wasiESuccess
	}
	if entry.file == nil {
		return wasiEBadf
	}
	fi, err := entry.file.Stat()
	if err != nil {
		return wasiEIo
	}
	writeFilestatFromInfo(s.mem(), bufPtr, entry.fdType, fi)
	return wasiESuccess
}

// Xpath_filestat_get implements path_filestat_get. Resolves the path and
// writes a 64-byte filestat struct. On writable host-backed mounts, when
// SYMLINK_FOLLOW is absent the final component is examined with Lstat-like
// semantics (a symlink is reported as filetype symbolic link); when it is
// set, symlinks in the final component are followed (Stat). Both paths try
// the host filesystem first, then fall back to fs.Stat on the overlay for
// embedded-only files. Read-only mounts use fs.Stat on the mount root only.
// Writable host directory preopens return ENOTCAPABLE for mount-relative paths
// that lexically escape the preopen (same check as path_open and resolveWritable).
func (s *State) Xpath_filestat_get(dirfd, flags, pathPtr, pathLen, bufPtr int32) int32 {
	mount, relPath := s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if mount == nil {
		return wasiENoEnt
	}

	var fi fs.FileInfo
	var err error
	if mount.writable && mount.hostRoot != "" {
		if s.preopenDirfdLexicallyEscapes(dirfd, relPath) {
			return wasiENotCap
		}
		follow := flags&wasiLookupSymlinkFollow != 0
		var hostPath string
		if follow {
			var errno int32
			hostPath, errno = joinWritableHostPathForLookup(mount.hostRoot, relPath, flags)
			if errno != wasiESuccess {
				return errno
			}
		} else {
			hostPath = filepath.Join(mount.hostRoot, filepath.FromSlash(relPath))
		}
		fi, err = statHostPathOrOverlay(hostPath, mount.root, relPath, follow)
		if err != nil {
			return wasiENoEnt
		}
	} else {
		fi, err = fs.Stat(mount.root, relPath)
		if err != nil {
			return wasiENoEnt
		}
	}
	writeFilestatFromInfo(s.mem(), bufPtr, filestatFdTypeFromInfo(fi), fi)
	return wasiESuccess
}

// Xfd_filestat_set_size implements fd_filestat_set_size. Returns EISDIR
// for directory fds. For osFile-backed fds, truncates the file to size bytes
// via (*os.File).Truncate when FD_FILESTAT_SET_SIZE is set in rights_base;
// otherwise returns ENOTCAPABLE. For fs.FS-backed fds, returns ESUCCESS
// without mutation (embedded files are read-only by construction).
func (s *State) Xfd_filestat_set_size(fd int32, size int64) int32 {
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
	if of, ok := entry.file.(*osFile); ok {
		if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDFilestatSetSize); errno != 0 {
			return errno
		}
		if err := of.Truncate(size); err != nil {
			return mapOSError(err)
		}
	}
	return wasiESuccess
}

// Xfd_filestat_set_times implements fd_filestat_set_times.
//
// For osFile-backed fds, calls os.Chtimes with the specified nanosecond
// values. Honors ATIM, MTIM, ATIM_NOW, and MTIM_NOW flags. Rejects
// contradictory flag combinations (ATIM together with ATIM_NOW, or MTIM
// together with MTIM_NOW) with EINVAL. For fs.FS-backed fds, returns
// ESUCCESS without mutation.
func (s *State) Xfd_filestat_set_times(fd int32, atim, mtim int64, fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstMtim|fstAtimNow|fstMtimNow) == 0 {
		return wasiESuccess
	}
	if errno := errnoIfContradictoryFstFlags(fstFlags); errno != 0 {
		return errno
	}
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil {
		return wasiEBadf
	}
	of, ok := entry.file.(*osFile)
	if !ok {
		return wasiESuccess
	}

	fi, err := of.Stat()
	if err != nil {
		return mapOSError(err)
	}

	targetAtim, targetMtim := computeTargetTimes(fi, atim, mtim, fstFlags)

	if err := os.Chtimes(of.Name(), targetAtim, targetMtim); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

func computeTargetTimes(fi fs.FileInfo, atim, mtim int64, fstFlags int32) (time.Time, time.Time) {
	targetAtim := getAtimeFromStat(fi)
	if fstFlags&fstAtimNow != 0 {
		targetAtim = time.Now()
	} else if fstFlags&fstAtim != 0 {
		targetAtim = time.Unix(0, atim)
	}

	targetMtim := fi.ModTime()
	if fstFlags&fstMtimNow != 0 {
		targetMtim = time.Now()
	} else if fstFlags&fstMtim != 0 {
		targetMtim = time.Unix(0, mtim)
	}
	return targetAtim, targetMtim
}

// setTimesAtPath sets the access and modification times on path. When follow
// is true it follows symlinks (os.Chtimes); when false it sets times on the
// symlink inode itself (unix.UtimesNanoAt with AT_SYMLINK_NOFOLLOW).
func setTimesAtPath(path string, atim, mtim time.Time, follow bool) error {
	if follow {
		return os.Chtimes(path, atim, mtim)
	}
	ts := []unix.Timespec{
		unix.NsecToTimespec(atim.UnixNano()),
		unix.NsecToTimespec(mtim.UnixNano()),
	}
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW)
}

// Xpath_filestat_set_times implements path_filestat_set_times.
//
// ATIM (bit 0), ATIM_NOW (bit 1), MTIM (bit 2), and MTIM_NOW (bit 3) flags
// are acted upon. Rejects contradictory flag combinations (ATIM together with
// ATIM_NOW, or MTIM together with MTIM_NOW) with EINVAL. Resolves paths with
// resolveWritable (including ENOTCAPABLE when a directory preopen path
// lexically escapes the mount). When LOOKUPFLAGS_SYMLINK_FOLLOW is set in
// flags, follows symlinks in the final path component; otherwise sets times on
// the symlink inode itself. Returns EROFS when the path is read-only or cannot
// be resolved to a writable host path.
func (s *State) Xpath_filestat_set_times(dirfd, flags, pathPtr, pathLen int32, atim, mtim int64, fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstMtim|fstAtimNow|fstMtimNow) == 0 {
		return wasiESuccess
	}
	if errno := errnoIfContradictoryFstFlags(fstFlags); errno != 0 {
		return errno
	}
	primary, werrno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if werrno != wasiESuccess {
		return werrno
	}

	follow := flags&wasiLookupSymlinkFollow != 0
	fi, err := statPath(primary, follow)
	if err != nil {
		return mapOSError(err)
	}

	targetAtim, targetMtim := computeTargetTimes(fi, atim, mtim, fstFlags)

	if err := setTimesAtPath(primary, targetAtim, targetMtim, follow); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}
