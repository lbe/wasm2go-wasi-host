package wasihost

import (
	"encoding/binary"
	"io/fs"
	"os"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

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

// writeFilestat writes a 64-byte WASI filestat struct at bufPtr in mem.
// fdType is a WASI snapshot-preview1 filetype tag with the same values as
// fdstat.fs_filetype, for example fdDir, fdFile or fdSymlink.
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

// filestatFdTypeFromMode maps fs.FileMode to a WASI preview1 filetype.
func filestatFdTypeFromMode(mode fs.FileMode) byte {
	switch {
	case mode&fs.ModeSymlink != 0:
		return fdSymlink
	case mode&fs.ModeDir != 0:
		return fdDir
	case mode&fs.ModeCharDevice != 0:
		return fdCharDev
	default:
		return fdFile
	}
}

// writeFilestatFromStat writes a WASI filestat struct from wasi.Stat_t at bufPtr.
func writeFilestatFromStat(mem []byte, bufPtr int32, st wasi.Stat_t) {
	writeFilestat(mem, bufPtr, filestatFdTypeFromMode(st.Mode), st.Size, st.Atim, st.Mtim, st.Dev, st.Ino)
}

// Xfd_filestat_get implements fd_filestat_get. Writes a 64-byte filestat
// struct for the open fd via fe.File.Stat on the FSContext FileEntry.
func (s *State) Xfd_filestat_get(fd, bufPtr int32) int32 {
	s.assertSingleOwner()
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	st, werrno := fe.File.Stat()
	if werrno != 0 {
		return errnoFromWasi(werrno)
	}
	writeFilestatFromStat(s.mem(), bufPtr, st)
	return wasiESuccess
}

// Xpath_filestat_get implements path_filestat_get. Resolves the path and
// writes a 64-byte filestat struct via vfsStat (Lstat when SYMLINK_FOLLOW is
// absent, Stat when set). Writable preopens return ENOTCAPABLE for mount-relative
// paths that lexically escape the preopen; read-only preopens without fsys return ENOENT.
func (s *State) Xpath_filestat_get(dirfd, flags, pathPtr, pathLen, bufPtr int32) int32 {
	guestPath := string(s.readBytes(pathPtr, pathLen))
	preopen, rel := s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if preopen == nil {
		return wasiENoEnt
	}

	follow := flags&wasiLookupSymlinkFollow != 0
	if preopenWritable(preopen) {
		if s.preopenDirfdLexicallyEscapes(dirfd, rel) {
			return wasiENotCap
		}
		if !isRootPreopen(preopen) && follow {
			hostRoot := preopenHostRoot(preopen)
			_, errno := joinWritableHostPathForLookup(hostRoot, rel, flags)
			if errno != wasiESuccess {
				return errno
			}
		}
	} else if preopen.fsys == nil {
		return wasiENoEnt
	}

	vpath := vfsPathForMutation(preopen, rel, guestPath)
	st, errno := vfsStat(preopen, vpath, follow)
	if errno != wasiESuccess {
		return errno
	}
	writeFilestatFromStat(s.mem(), bufPtr, st)
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
// symlink inode itself (sysfs.UtimesNoFollow).
func setTimesAtPath(path string, atim, mtim time.Time, follow bool) error {
	if follow {
		return os.Chtimes(path, atim, mtim)
	}
	return sysfs.UtimesNoFollow(path, atim, mtim)
}

// getAtimeFromStat returns the access time recorded in fi, using the same
// platform stat mapping as the VFS.
func getAtimeFromStat(fi fs.FileInfo) time.Time {
	return time.Unix(0, int64(wasi.NewStat_t(fi).Atim))
}

// Xpath_filestat_set_times implements path_filestat_set_times.
//
// ATIM (bit 0), ATIM_NOW (bit 1), MTIM (bit 2), and MTIM_NOW (bit 3) flags
// are honored. Rejects contradictory flag combinations (ATIM together with
// ATIM_NOW, or MTIM together with MTIM_NOW) with EINVAL. Resolves paths with
// resolveWritable (including ENOTCAPABLE when a directory preopen path
// lexically escapes the mount). When LOOKUPFLAGS_SYMLINK_FOLLOW is set in
// flags, follows symlinks in the final path component; otherwise sets times on
// the symlink inode itself via preopen.fsys.Utimens. Returns EROFS when the
// path is read-only or cannot be resolved to a writable preopen.
func (s *State) Xpath_filestat_set_times(dirfd, flags, pathPtr, pathLen int32, atim, mtim int64, fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstMtim|fstAtimNow|fstMtimNow) == 0 {
		return wasiESuccess
	}
	if errno := errnoIfContradictoryFstFlags(fstFlags); errno != 0 {
		return errno
	}

	follow := flags&wasiLookupSymlinkFollow != 0
	guestPath := string(s.readBytes(pathPtr, pathLen))
	preopen, rel, werrno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if werrno != wasiESuccess {
		return werrno
	}
	if follow && !isRootPreopen(preopen) {
		hostRoot := preopenHostRoot(preopen)
		_, errno := joinWritableHostPathForLookup(hostRoot, rel, flags)
		if errno != wasiESuccess {
			return errno
		}
	}

	vpath := vfsPathForMutation(preopen, rel, guestPath)
	if follow {
		st, statErrno := preopen.fsys.Stat(vpath)
		if statErrno != 0 {
			return errnoFromWasi(statErrno)
		}
		targetAtim, targetMtim := computeTargetTimesFromStat(st, atim, mtim, fstFlags)
		return errnoFromWasi(preopen.fsys.Utimens(vpath, targetAtim, targetMtim))
	}

	hostPath := vfsHostPath(preopen, guestPath, vpath)
	fi, err := statPath(hostPath, false)
	if err != nil {
		return mapOSError(err)
	}
	targetAtim, targetMtim := computeTargetTimes(fi, atim, mtim, fstFlags)
	if err := setTimesAtPath(hostPath, targetAtim, targetMtim, false); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

func computeTargetTimesFromStat(st wasi.Stat_t, atim, mtim int64, fstFlags int32) (int64, int64) {
	targetAtim := st.Atim
	if fstFlags&fstAtimNow != 0 {
		targetAtim = time.Now().UnixNano()
	} else if fstFlags&fstAtim != 0 {
		targetAtim = atim
	}

	targetMtim := st.Mtim
	if fstFlags&fstMtimNow != 0 {
		targetMtim = time.Now().UnixNano()
	} else if fstFlags&fstMtim != 0 {
		targetMtim = mtim
	}
	return targetAtim, targetMtim
}
