package wasihost

import (
	"encoding/binary"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

type devNullFile struct{ wasi.UnimplementedFile }

func (devNullFile) Read([]byte) (int, wasi.Errno)  { return 0, 0 }
func (devNullFile) Write([]byte) (int, wasi.Errno) { return len([]byte{}), 0 }
func (devNullFile) Stat() (wasi.Stat_t, wasi.Errno) {
	return wasi.Stat_t{Mode: fs.ModeCharDevice}, 0
}
func (devNullFile) IsDir() (bool, wasi.Errno) { return false, 0 }
func (devNullFile) Close() wasi.Errno         { return 0 }

func (s *State) openDevNull() int32 {
	if s.ctx == nil || s.ctx.FSContext() == nil {
		return 0
	}
	fd, werrno := s.ctx.FSContext().AllocFile(&wasi.FileEntry{
		Name:       "/dev/null",
		GuestPath:  "/dev/null",
		RightsBase: rightsCharDev,
		File:       devNullFile{},
	})
	if werrno != 0 {
		return 0
	}
	return fd
}

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
		base |= reqBase & parentBase & rightsDirectoryInherited
		base &^= rightFDSeek
	}
	return base, inheriting
}

func pathOpenWantsWrite(oflags int32, fdRightsBase int64) bool {
	return (uint64(fdRightsBase)&rightFDWrite) != 0 || (uint32(oflags)&(oflagCreat|oflagTrunc|oflagExcl)) != 0
}

func pathOpenWasiFlags(oflags int32, fdRightsBase int64, lookupFlags int32, fdFlags int32) wasi.Oflag {
	flag := wasi.O_RDONLY
	if pathOpenWantsWrite(oflags, fdRightsBase) {
		flag = wasi.O_RDWR
	}
	if uint32(oflags)&oflagCreat != 0 {
		flag |= wasi.O_CREAT
	}
	if uint32(oflags)&oflagTrunc != 0 {
		flag |= wasi.O_TRUNC
	}
	if uint32(oflags)&oflagExcl != 0 {
		flag |= wasi.O_EXCL
	}
	if uint32(oflags)&oflagDir != 0 {
		flag = wasi.O_RDONLY | wasi.O_DIRECTORY
	}
	if fdFlags&fdFlagsAppend != 0 {
		flag |= wasi.O_APPEND
	}
	if lookupFlags&wasiLookupSymlinkFollow == 0 {
		flag |= wasi.O_NOFOLLOW
	}
	return flag
}

func pathOpenWritableHostPrecheck(preopen *PreopenEntry, relPath, guestPath string, lookupFlags, oflags int32, fdRightsBase int64) int32 {
	if !preopenWritable(preopen) {
		return wasiESuccess
	}
	hostRoot := preopenHostRoot(preopen)
	if hostRoot == "" {
		return wasiESuccess
	}
	wantDirectory := uint32(oflags)&oflagDir != 0
	if wantDirectory {
		hostPath := filepathJoinForPathOpen(preopen, relPath, guestPath, lookupFlags)
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
	if _, lookupErrno := joinWritableHostPathForLookup(hostRoot, relPath, lookupFlags); lookupErrno != wasiESuccess {
		return lookupErrno
	}
	return wasiESuccess
}

func pathOpenRelWithTrailingSlash(relPath, guestPath string) string {
	if strings.HasSuffix(guestPath, "/") {
		return relPath + "/"
	}
	return relPath
}

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
	preopen, relPath, parentBase, parentInh, errno := s.resolveForOpen(dirfd, pathPtr, pathLen, guestPath)
	if errno != wasiESuccess {
		return errno
	}

	if uint32(oflags)&oflagTrunc != 0 {
		if errno := errnoIfFDRightsMissing(parentBase, rightPathFilestatSetSize); errno != 0 {
			return errno
		}
	}

	if preopenReadonly(preopen) {
		if (uint32(oflags)&(oflagCreat|oflagTrunc)) != 0 || (uint64(fdRightsBase)&rightFDWrite) != 0 {
			return wasiENotCap
		}
	}

	if s.preopenDirfdLexicallyEscapes(dirfd, relPath) {
		return wasiENotCap
	}
	if errno := s.errnoIfNonPreopenDirfdEscapes(dirfd, preopen, relPath); errno != 0 {
		return errno
	}

	wantDirectory := uint32(oflags)&oflagDir != 0
	if errno := pathOpenWritableHostPrecheck(preopen, relPath, guestPath, lookupFlags, oflags, fdRightsBase); errno != wasiESuccess {
		return errno
	}

	openPath := pathOpenRelWithTrailingSlash(relPath, guestPath)
	if strings.HasSuffix(guestPath, "/") && !wantDirectory {
		checkPath := strings.TrimSuffix(openPath, "/")
		if st, werrno := preopen.fsys.Stat(checkPath); werrno == 0 && st.Mode&fs.ModeDir == 0 {
			return wasiENotDir
		}
	}

	if s.ctx == nil || s.ctx.FSContext() == nil || preopen.fsys == nil {
		return wasiENoEnt
	}
	fd, werrno := s.ctx.FSContext().OpenFile(preopen.fsys, openPath, pathOpenWasiFlags(oflags, fdRightsBase, lookupFlags, fdFlags), 0o666)
	if werrno != 0 {
		return errnoFromWasi(werrno)
	}
	return s.bridgePathOpenFD(fd, preopen, relPath, guestPath, dirfd, parentBase, parentInh, fdRightsBase, fdRightsInheriting, fdFlags, fdPtr)
}

func filepathJoinForPathOpen(preopen *PreopenEntry, relPath, guestPath string, lookupFlags int32) string {
	hostRoot := preopenHostRoot(preopen)
	if isRootPreopen(preopen) {
		primary, _, _ := writableMountHostPaths(preopen, relPath, lookupFlags&wasiLookupSymlinkFollow != 0)
		if primary != "" {
			return pathOpenHostPathWithTrailingSlash(primary, guestPath)
		}
	}
	hostPath, _ := joinWritableHostPathForLookup(hostRoot, relPath, lookupFlags)
	return pathOpenHostPathWithTrailingSlash(hostPath, guestPath)
}

func pathOpenHostPathWithTrailingSlash(hostPath, guestPath string) string {
	if strings.HasSuffix(guestPath, "/") {
		return hostPath + string(filepath.Separator)
	}
	return hostPath
}
