package wasihost

import (
	"encoding/binary"
	"os"
	"strings"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

// Xpath_create_directory implements path_create_directory. Creates a
// directory at the resolved path via preopen.fsys.Mkdir. Returns EROFS if the
// mount is read-only, EEXIST if the directory already exists, ENOENT if
// the parent does not exist.
func (s *State) Xpath_create_directory(dirfd, pathPtr, pathLen int32) int32 {
	m, errno := s.resolveWritableMutation(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	return errnoFromWasi(m.preopen.fsys.Mkdir(m.vpath, 0755))
}

// Xpath_remove_directory implements path_remove_directory. Removes an
// empty directory at the resolved path. Returns EROFS if the mount
// is read-only, ENOTDIR if the target is a file, ENOTEMPTY if the
// directory is not empty.
func (s *State) Xpath_remove_directory(dirfd, pathPtr, pathLen int32) int32 {
	m, errno := s.resolveWritableMutation(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	st, errno := vfsLstat(m.preopen, m.vpath)
	if errno != wasiESuccess {
		return errno
	}
	if !wasiStatIsDir(st) {
		return wasiENotDir
	}
	if s.hasDirectoryContents(m.preopen, m.rel, m.vpath, m.guestPath) {
		return s.dirNotEmptyErr()
	}
	return errnoFromWasi(m.preopen.fsys.Rmdir(m.vpath))
}

// Xpath_unlink_file implements path_unlink_file. Removes a file at the
// resolved path. Returns EROFS if the mount is read-only, EISDIR if
// the target is a directory, ENOENT if the file does not exist, and ENOTDIR
// if the path has a trailing slash but the target is not a directory.
func (s *State) Xpath_unlink_file(dirfd, pathPtr, pathLen int32) int32 {
	hasTrailingSlash := s.pathHasTrailingSlash(pathPtr, pathLen)
	m, errno := s.resolveWritableMutation(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	st, errno := vfsLstat(m.preopen, m.vpath)
	if errno != wasiESuccess {
		return errno
	}
	if wasiStatIsDir(st) {
		return wasiEIsdir
	}
	if hasTrailingSlash {
		return wasiENotDir
	}
	return errnoFromWasi(m.preopen.fsys.Unlink(m.vpath))
}

// Xpath_readlink implements path_readlink. Reads the target of a symbolic
// link at the resolved path into guest memory at bufPtr, capped at
// bufLen bytes, and writes the actual byte count to nreadPtr. Uses VFS
// Readlink via resolveReadlinkLookup (same resolution shape as path_filestat_get);
// read-only AdaptFS ENOSYS is mapped to EROFS.
func (s *State) Xpath_readlink(dirfd, pathPtr, pathLen, bufPtr, bufLen, nreadPtr int32) int32 {
	if len(s.ctxPreopens()) == 0 {
		return wasiEROFS
	}
	lookup, errno := s.resolveReadlinkLookup(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	target, werrno := lookup.preopen.fsys.Readlink(lookup.vpath)
	if werrno != 0 {
		return errnoReadlinkFromWasi(werrno, preopenReadonly(lookup.preopen))
	}
	mem := s.mem()
	n := copy(mem[bufPtr:bufPtr+bufLen], target)
	binary.LittleEndian.PutUint32(mem[nreadPtr:], uint32(n))
	return wasiESuccess
}

// symlinkTargetErrno returns EINVAL when oldPath is the root absolute path
// "/", which would escape any preopen sandbox. It returns ESUCCESS for all
// other targets, leaving further validation to the host symlink layer.
func symlinkTargetErrno(oldPath string) int32 {
	if oldPath == "/" {
		return wasiEInval
	}
	return wasiESuccess
}

// symlinkTrailingSlashErrno validates newPath when it has a trailing slash.
func symlinkTrailingSlashErrno(p *PreopenEntry, vpath string) int32 {
	st, errno := vfsLstat(p, vpath)
	if errno != wasiESuccess {
		return errno
	}
	if wasiStatIsDir(st) {
		return wasiEExist
	}
	return wasiENotDir
}

// Xpath_symlink implements path_symlink. Creates a symbolic link at the
// resolved path for newPath pointing to the raw string oldPath.
// Returns EROFS if the mount is read-only, EINVAL if oldPath is the absolute
// root path "/", EEXIST if the link path already exists, ENOENT if newPath
// has a trailing slash and does not exist, and ENOTDIR if newPath has a
// trailing slash but names a non-directory.
func (s *State) Xpath_symlink(oldPathPtr, oldPathLen, dirfd, newPathPtr, newPathLen int32) int32 {
	oldPath := string(s.readBytes(oldPathPtr, oldPathLen))
	hasTrailingSlash := s.pathHasTrailingSlash(newPathPtr, newPathLen)

	if errno := symlinkTargetErrno(oldPath); errno != wasiESuccess {
		return errno
	}

	m, errno := s.resolveWritableMutation(dirfd, newPathPtr, newPathLen)
	if errno != wasiESuccess {
		return errno
	}

	if hasTrailingSlash {
		return symlinkTrailingSlashErrno(m.preopen, m.vpath)
	}

	if strings.HasPrefix(oldPath, "/") {
		hostPath := vfsHostPath(m.preopen, m.guestPath, m.vpath)
		if _, err := os.Lstat(hostPath); err == nil {
			return wasiEExist
		}
		if err := os.Symlink(oldPath, hostPath); err != nil {
			return mapOSError(err)
		}
		return wasiESuccess
	}

	return errnoFromWasi(m.preopen.fsys.Symlink(oldPath, m.vpath))
}

// Xpath_link implements path_link. Creates a hard link from the resolved
// old path to the resolved new path. Returns EROFS if either mount is read-only.
func (s *State) Xpath_link(oldDirfd, oldFlags, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen int32) int32 {
	paths, errno := s.resolveWritableMutationPair(oldDirfd, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen)
	if errno != wasiESuccess {
		return errno
	}

	if linkErr := s.validateLinkParams(paths.old.vpath, paths.new.vpath, oldFlags, newPathPtr, newPathLen); linkErr != wasiESuccess {
		return linkErr
	}

	oldSt, errno := vfsLstat(paths.old.preopen, paths.old.vpath)
	if errno != wasiESuccess {
		return errno
	}
	if wasiStatIsSymlink(oldSt) {
		return s.createHardLinkToSymlink(paths.old.preopen, paths.new.preopen, paths.old.guestPath, paths.new.guestPath, paths.old.vpath, paths.new.vpath)
	}

	return s.createHardLinkToFile(paths.old.preopen, paths.new.preopen, paths.old.vpath, paths.new.vpath)
}

// validateLinkParams performs early validation for path_link.
// It checks for invalid flags, trailing slashes, and self-links.
func (s *State) validateLinkParams(oldPath, newPath string, oldFlags, newPathPtr, newPathLen int32) int32 {
	// Reject SYMLINK_FOLLOW flag (WASI allows EINVAL or ENOENT; host returns ENOENT).
	if oldFlags&wasiLookupSymlinkFollow != 0 {
		return wasiENoEnt
	}

	// Check for trailing slash on new path.
	if s.pathHasTrailingSlash(newPathPtr, newPathLen) {
		// When the new path has a trailing slash, it must point to an existing directory.
		// However, hard links to directories are not permitted, so this should fail.
		// Return ENOENT.
		return wasiENoEnt
	}

	// Reject a self-link, where oldPath equals newPath, before the more
	// expensive checks.
	if oldPath == newPath {
		return wasiEExist
	}

	return wasiESuccess
}

// createHardLinkToSymlink creates a hard link to a symlink inode.
// This uses sysfs.LinkNoFollow to link the symlink itself,
// not the target it points to.
func (s *State) createHardLinkToSymlink(oldP, newP *PreopenEntry, oldGuest, newGuest, oldVpath, newVpath string) int32 {
	if s.vfsPathExists(newP, newVpath) {
		return wasiEExist
	}
	oldHost := vfsHostPath(oldP, oldGuest, oldVpath)
	newHost := vfsHostPath(newP, newGuest, newVpath)
	if err := sysfs.LinkNoFollow(oldHost, newHost); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// createHardLinkToFile creates a hard link to a regular file.
func (s *State) createHardLinkToFile(oldP, newP *PreopenEntry, oldVpath, newVpath string) int32 {
	if s.vfsPathExists(newP, newVpath) {
		return wasiEExist
	}

	if s.vfsPathIsDir(oldP, oldVpath) {
		return wasiEPerm
	}

	if s.vfsPathIsDir(newP, newVpath) {
		return wasiEPerm
	}

	if oldP.fsys == newP.fsys {
		return errnoFromWasi(oldP.fsys.Link(oldVpath, newVpath))
	}
	oldHost := vfsHostPath(oldP, "", oldVpath)
	newHost := vfsHostPath(newP, "", newVpath)
	if err := os.Link(oldHost, newHost); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// vfsPathExists checks if a path exists via preopen.fsys.Lstat.
func (s *State) vfsPathExists(p *PreopenEntry, vpath string) bool {
	_, werrno := p.fsys.Lstat(vpath)
	return werrno == 0
}

// vfsPathIsDir checks if a path exists and is a directory via preopen.fsys.Stat
// It follows symlinks, because the spec forbids hard links to directories,
// including through a symlink.
func (s *State) vfsPathIsDir(p *PreopenEntry, vpath string) bool {
	st, werrno := p.fsys.Stat(vpath)
	if werrno != 0 {
		return false
	}
	return wasiStatIsDir(st)
}

// validateRename checks if a rename operation from oldPath to newPath is valid.
// It returns an error code if the operation should be rejected, or ESUCCESS if valid.
func (s *State) validateRename(oldP, newP *PreopenEntry, newRel, newGuest, oldVpath, newVpath string) int32 {
	oldSt, oldErr := oldP.fsys.Lstat(oldVpath)
	newSt, newErr := newP.fsys.Lstat(newVpath)

	if oldErr == 0 && newErr == 0 {
		oldIsDir := wasiStatIsDir(oldSt)
		newIsDir := wasiStatIsDir(newSt)

		if oldIsDir && newIsDir {
			if s.hasDirectoryContents(newP, newRel, newVpath, newGuest) {
				return s.dirNotEmptyErr()
			}
		}

		if !oldIsDir && newIsDir {
			return wasiEIsdir
		}
	}

	return wasiESuccess
}

// dirNotEmptyErr returns the errno for a non-empty directory target. The host
// reports Unix-style errnos on every platform, Windows included.
func (s *State) dirNotEmptyErr() int32 {
	return wasiENotEmpty
}

// hasDirectoryContents checks if a directory has any entries (files or subdirectories).
func (s *State) hasDirectoryContents(p *PreopenEntry, rel, vpath, guestPath string) bool {
	hostPath := vfsHostPath(p, guestPath, vpath)
	entries, err := os.ReadDir(hostPath)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// renameAcrossMounts renames through wasi.FS when both paths are on one mount,
// and through renamePath otherwise.
func renameAcrossMounts(oldP, newP *PreopenEntry, oldGuest, newGuest, oldVpath, newVpath string) int32 {
	if oldP.fsys == newP.fsys {
		return errnoFromWasi(oldP.fsys.Rename(oldVpath, newVpath))
	}
	oldHost := vfsHostPath(oldP, oldGuest, oldVpath)
	newHost := vfsHostPath(newP, newGuest, newVpath)
	if err := renamePath(oldHost, newHost); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_rename implements path_rename. Resolves both old and new paths
// and calls preopen.fsys.Rename. Returns EROFS if either mount is read-only or if
// the resolved paths are not beneath a writable preopen root.
func (s *State) Xpath_rename(oldDirfd, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen int32) int32 {
	oldMount, _ := s.resolveDirfdPath(oldDirfd, oldPathPtr, oldPathLen)
	newMount, _ := s.resolveDirfdPath(newDirfd, newPathPtr, newPathLen)
	if oldMount == nil || newMount == nil {
		return wasiENoEnt
	}

	paths, errno := s.resolveWritableMutationPair(oldDirfd, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen)
	if errno != wasiESuccess {
		return errno
	}

	if renameErr := s.validateRename(paths.old.preopen, paths.new.preopen, paths.new.rel, paths.new.guestPath, paths.old.vpath, paths.new.vpath); renameErr != wasiESuccess {
		return renameErr
	}

	return renameAcrossMounts(paths.old.preopen, paths.new.preopen, paths.old.guestPath, paths.new.guestPath, paths.old.vpath, paths.new.vpath)
}
