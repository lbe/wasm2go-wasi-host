package wasihost

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// openDevNull opens a /dev/null character device and returns its fd.
func (s *State) openDevNull() int32 {
	fd := s.allocFD()
	s.fds[fd] = fdEntry{fdType: fdCharDev, path: "/dev/null", rightsBase: rightsCharDev, rightsInheriting: 0}
	return fd
}

// fileRightsForOpen returns rights_base and rights_inheriting for a regular file
// opened via path_open. rights_inheriting is always parentInh.
// req is rights_base already intersected with parentInh; rights_base is then
// the intersection of rightsRegularFile, parentInh, and the effective read/write
// intent from req (WASI preview1: read-only and write-only opens collapse to a single bit).
func fileRightsForOpen(parentInh, req uint64) (base uint64, inheriting uint64) {
	inheriting = parentInh
	maxFile := rightsRegularFile & parentInh
	effective := req
	switch req & (rightFDRead | rightFDWrite) {
	case rightFDRead | rightFDWrite:
		effective = rightsRegularFile
	case rightFDRead:
		effective = rightFDRead
	case rightFDWrite:
		effective = rightFDWrite
	}
	base = effective & maxFile
	base |= req & parentInh &^ maxFile
	return base, inheriting
}

// pathOpenStoredRights returns the rights_base and rights_inheriting actually
// stored for an fd created by path_open. fdRightsBase and fdRightsInheriting are
// clamped to parentInh so bits the parent cannot pass on are dropped without
// failing the open. Regular files are further reduced via fileRightsForOpen.
// Directories have FD_SEEK stripped because seek/tell are not defined on them.
func pathOpenStoredRights(parentBase, parentInh uint64, openedType byte, fdRightsBase, fdRightsInheriting int64) (base uint64, inheriting uint64) {
	reqBase := uint64(fdRightsBase)
	base = reqBase & parentInh
	if openedType == fdDir && fdRightsInheriting == 0 {
		inheriting = parentInh
	} else {
		inheriting = uint64(fdRightsInheriting) & parentInh
	}
	switch openedType {
	case fdFile:
		newBase, _ := fileRightsForOpen(parentInh, base)
		base = newBase
		if fdRightsInheriting == 0 {
			inheriting = parentInh
		}
	case fdDir:
		// Path bits in directory rights_base (e.g. PATH_FILESTAT_SET_SIZE) live in
		// the parent's base mask but not inheriting; propagate them when requested.
		base |= reqBase & parentBase & rightsDirectoryInherited
		base &^= rightFDSeek
	}
	return base, inheriting
}

// Xpath_open implements path_open. Resolves the guest path and allocates
// a new fd. For writable mounts, opens via os.OpenFile using oflags and
// fdRightsBase to determine OS open flags; falls back to the overlay
// fs.FS for read-only opens that do not create or truncate. When O_DIRECTORY
// is set on a writable host-backed path, an existing non-directory returns
// ENOTDIR before open. Trailing slashes in the guest path are preserved on
// the host path so the OS returns ENOTDIR when the final component is a file.
// The special path "/dev/null" is handled as a character-device fd without
// mount resolution. Absolute guest paths stored on directory fds enable
// correct nested resolution during directory recursion. fd_rights_base and
// fd_rights_inheriting are clamped to the directory fd's rights_inheriting
// when recording the new fd (bits outside that mask are dropped). Other
// checks still return ENOTCAPABLE, e.g. write on a read-only mount, O_TRUNC
// without PATH_FILESTAT_SET_SIZE in the dirfd rights_base, or sandbox escape
// on a preopen directory. Symlink following is controlled by lookupFlags:
// without SYMLINK_FOLLOW, a final-component symlink yields ELOOP; with it,
// symlinks are resolved and confined to the preopen root.
//
// FS open errors (overlay fallback after a missing host file, embedded fs.FS
// mounts, and read-only preopens) are passed through [mapOSError], so well-known
// errors such as permission denied map to the appropriate WASI errno rather than
// a single ENOENT for every failure.
func (s *State) Xpath_open(dirfd int32, lookupFlags int32, pathPtr int32, pathLen int32, oflags int32, fdRightsBase int64, fdRightsInheriting int64, fdFlags int32, fdPtr int32) int32 {
	s.assertSingleOwner()
	pathBytes := s.readBytes(pathPtr, pathLen)
	for _, b := range pathBytes {
		if b == 0 {
			return wasiEInval
		}
	}
	guestPath := string(pathBytes)
	mem := s.mem()
	if guestPath == "/dev/null" {
		fd := s.openDevNull()
		binary.LittleEndian.PutUint32(mem[fdPtr:], uint32(fd))
		return wasiESuccess
	}
	mount, relPath, parentBase, parentInh, errno := s.resolveForOpen(dirfd, pathPtr, pathLen, guestPath)
	if errno != wasiESuccess {
		return errno
	}

	if uint32(oflags)&oflagTrunc != 0 {
		if errno := errnoIfFDRightsMissing(parentBase, rightPathFilestatSetSize); errno != 0 {
			return errno
		}
	}

	if mount.readonly {
		if (uint32(oflags)&(oflagCreat|oflagTrunc)) != 0 || (uint64(fdRightsBase)&rightFDWrite) != 0 {
			return wasiENotCap
		}
	}

	if s.preopenDirfdLexicallyEscapes(dirfd, relPath) {
		return wasiENotCap
	}
	if errno := s.errnoIfNonPreopenDirfdEscapes(dirfd, mount, relPath); errno != 0 {
		return errno
	}
	var f fs.File
	var err error

	if mount.writable && mount.hostRoot != "" {
		var hostPath, hostFallback string
		if isRootMount(mount) {
			primary, fallback, errno := writableMountHostPaths(mount, relPath, lookupFlags&wasiLookupSymlinkFollow != 0)
			if errno != wasiESuccess {
				return errno
			}
			hostPath = primary
			hostFallback = fallback
			if hostPath == "" {
				hostPath = relPath
			}
		} else {
			var errno int32
			hostPath, errno = joinWritableHostPathForLookup(mount.hostRoot, relPath, lookupFlags)
			if errno != wasiESuccess {
				return errno
			}
		}
		wantDirectory := uint32(oflags)&oflagDir != 0
		if wantDirectory {
			if errno := errnoIfHostPathNotADirectory(hostPath); errno != 0 {
				return errno
			}
			if guestPath == "." {
				uBase := uint64(fdRightsBase)
				if uBase&(rightFDRead|rightFDWrite) == (rightFDRead|rightFDWrite) &&
					uBase&rightsDirectoryInherited == 0 {
					return wasiEIsdir
				}
			}
		}
		osFlags := os.O_RDONLY
		if (uint64(fdRightsBase)&rightFDWrite) != 0 || (uint32(oflags)&(oflagCreat|oflagTrunc|oflagExcl)) != 0 {
			osFlags = os.O_RDWR
		}
		if uint32(oflags)&oflagCreat != 0 {
			osFlags |= os.O_CREATE
		}
		if uint32(oflags)&oflagTrunc != 0 {
			osFlags |= os.O_TRUNC
		}
		if uint32(oflags)&oflagExcl != 0 {
			osFlags |= os.O_EXCL
		}
		if wantDirectory {
			osFlags = os.O_RDONLY
		}
		// If the guest path ends with a slash, the final component must be a
		// directory. Preserve the trailing slash in the host path so the OS
		// returns ENOTDIR for non-directories.
		if strings.HasSuffix(guestPath, "/") {
			hostPath += string(filepath.Separator)
		}
		hostFile, osErr := os.OpenFile(hostPath, osFlags, 0o666)
		if osErr != nil && hostFallback != "" && errors.Is(osErr, os.ErrNotExist) {
			hostFile, osErr = os.OpenFile(hostFallback, osFlags, 0o666)
		}
		if osErr != nil {
			if uint32(oflags)&(oflagCreat|oflagTrunc|oflagExcl) == 0 &&
				errors.Is(osErr, os.ErrNotExist) {
				f, err = mount.root.Open(relPath)
				if err != nil {
					// Preserve overlay errno (mapOSError); do not collapse to ENOENT.
					return mapOSError(err)
				}
				fi, _ := f.Stat()
				return s.storeOpenedFD(&FSFileWrap{File: f}, mountGuestPath(mount, relPath), fdTypeFromInfo(fi), parentBase, parentInh, fdRightsBase, fdRightsInheriting, fdFlags, fdPtr)
			}
			return mapOSError(osErr)
		}
		fi, _ := hostFile.Stat()
		return s.storeOpenedFD(&osFile{File: hostFile}, mountGuestPath(mount, relPath), fdTypeFromInfo(fi), parentBase, parentInh, fdRightsBase, fdRightsInheriting, fdFlags, fdPtr)
	}
	f, err = mount.root.Open(relPath)
	if err != nil {
		// mount.root ([fs.FS]): preserve errno via [mapOSError], same as host opens.
		return mapOSError(err)
	}
	fi, _ := f.Stat()
	return s.storeOpenedFD(&FSFileWrap{File: f}, s.guestAbsPathForFDEntry(dirfd, guestPath), fdTypeFromInfo(fi), parentBase, parentInh, fdRightsBase, fdRightsInheriting, fdFlags, fdPtr)
}
