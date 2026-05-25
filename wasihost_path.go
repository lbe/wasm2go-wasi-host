package wasihost

import (
	"encoding/binary"
	"io/fs"
	"os"
	"runtime"
)

// Xpath_create_directory implements path_create_directory. Creates a
// directory at the resolved host path via os.Mkdir. Returns EROFS if the
// mount is read-only, EEXIST if the directory already exists, ENOENT if
// the parent does not exist.
func (s *State) Xpath_create_directory(dirfd, pathPtr, pathLen int32) int32 {
	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	if err := os.Mkdir(path, 0755); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_remove_directory implements path_remove_directory. Removes an
// empty directory at the resolved host path. Returns EROFS if the mount
// is read-only, ENOTDIR if the target is a file, ENOTEMPTY if the
// directory is not empty.
func (s *State) Xpath_remove_directory(dirfd, pathPtr, pathLen int32) int32 {
	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return mapOSError(err)
	}
	if !fi.IsDir() {
		return wasiENotDir
	}
	if err := os.Remove(path); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_unlink_file implements path_unlink_file. Removes a file at the
// resolved host path. Returns EROFS if the mount is read-only, EISDIR if
// the target is a directory, ENOENT if the file does not exist, and ENOTDIR
// if the path has a trailing slash but the target is not a directory.
func (s *State) Xpath_unlink_file(dirfd, pathPtr, pathLen int32) int32 {
	hasTrailingSlash := s.pathHasTrailingSlash(pathPtr, pathLen)

	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return mapOSError(err)
	}
	if fi.IsDir() {
		return wasiEIsdir
	}
	if hasTrailingSlash {
		return wasiENotDir
	}
	if err := os.Remove(path); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_readlink implements path_readlink. Reads the target of a symbolic
// link at the resolved host path into guest memory at bufPtr, capped at
// bufLen bytes, and writes the actual byte count to nreadPtr. Returns
// EROFS if the mount is read-only (embedded fs.FS mounts have no symlinks).
func (s *State) Xpath_readlink(dirfd, pathPtr, pathLen, bufPtr, bufLen, nreadPtr int32) int32 {
	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	target, err := os.Readlink(path)
	if err != nil {
		return mapOSError(err)
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

// Xpath_symlink implements path_symlink. Creates a symbolic link at the
// resolved host path for newPath pointing to the raw string oldPath.
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

	path, errno := s.resolveWritable(dirfd, newPathPtr, newPathLen)
	if errno != wasiESuccess {
		return errno
	}

	if hasTrailingSlash {
		fi, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return wasiENoEnt
			}
			return mapOSError(err)
		}
		if fi.IsDir() {
			return wasiEExist
		}
		return wasiENotDir
	}

	if err := os.Symlink(oldPath, path); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_link implements path_link. Creates a hard link from the resolved
// old path to the resolved new path. Returns EROFS if either mount is read-only.
func (s *State) Xpath_link(oldDirfd, oldFlags, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen int32) int32 {
	// Resolve paths
	oldPath, oldErr := s.resolveWritable(oldDirfd, oldPathPtr, oldPathLen)
	if oldErr != wasiESuccess {
		return oldErr
	}
	newPath, newErr := s.resolveWritable(newDirfd, newPathPtr, newPathLen)
	if newErr != wasiESuccess {
		return newErr
	}

	// Validate parameters early
	if errno := s.validateLinkParams(oldPath, newPath, oldFlags, newPathPtr, newPathLen); errno != wasiESuccess {
		return errno
	}

	// Check if old path is a symlink
	oldFi, err := os.Lstat(oldPath)
	if err != nil {
		return mapOSError(err)
	}
	if oldFi.Mode()&os.ModeSymlink != 0 {
		return s.createHardLinkToSymlink(oldPath, newPath, oldFi)
	}

	// For regular files, create a hard link
	return s.createHardLinkToFile(oldPath, newPath)
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

	// Check for self-link (oldPath == newPath). This is a simple check that catches
	// obvious self-reference before we do more expensive operations.
	if oldPath == newPath {
		return wasiEExist
	}

	return wasiESuccess
}

// createHardLinkToSymlink creates a hard link to a symlink inode.
// This uses platform-specific linkSymlinkInode to link the symlink itself,
// not the target it points to.
func (s *State) createHardLinkToSymlink(oldPath, newPath string, oldFi fs.FileInfo) int32 {
	// Check if the symlink already exists at newPath.
	if s.pathExists(newPath) {
		return wasiEExist
	}
	if err := linkSymlinkInode(oldPath, newPath); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// createHardLinkToFile creates a hard link to a regular file.
func (s *State) createHardLinkToFile(oldPath, newPath string) int32 {
	// Check if the target already exists.
	if s.pathExists(newPath) {
		return wasiEExist
	}

	// Check if oldPath is a directory. Hard links to directories are not permitted.
	if s.pathIsDir(oldPath) {
		return wasiEPerm
	}

	// Check if newPath would be a directory. Hard links to directories are not permitted.
	if s.pathIsDir(newPath) {
		return wasiEPerm
	}

	if err := os.Link(oldPath, newPath); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// pathExists checks if a path exists (as a file, directory, or symlink).
func (s *State) pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// pathIsDir checks if a path exists and is a directory.
func (s *State) pathIsDir(path string) bool {
	fi, err := os.Stat(path) // use Stat to follow symlinks, per spec directories should not be hard linked even if they are symlinks
	if err != nil {
		return false
	}
	return fi.IsDir()
}

// validateRename checks if a rename operation from oldPath to newPath is valid.
// It returns an error code if the operation should be rejected, or ESUCCESS if valid.
func (s *State) validateRename(oldPath, newPath string) int32 {
	// Stat both paths to determine their types
	oldInfo, oldStatErr := os.Lstat(oldPath)
	newInfo, newStatErr := os.Lstat(newPath)

	// If both stat calls succeed, we can perform detailed validation
	if oldStatErr == nil && newStatErr == nil {
		oldIsDir := oldInfo.IsDir()
		newIsDir := newInfo.IsDir()

		// Check if target directory is not empty (when source is a directory)
		if oldIsDir && newIsDir {
			if hasDirectoryContents(newPath) {
				return s.dirNotEmptyErr()
			}
		}

		// Case: Source is a file, target is a directory (always invalid)
		if !oldIsDir && newIsDir {
			return wasiEIsdir
		}
	}

	return wasiESuccess
}

// dirNotEmptyErr returns the appropriate error for a non-empty directory target.
func (s *State) dirNotEmptyErr() int32 {
	if runtime.GOOS == "windows" {
		return wasiEAcces
	}
	return wasiENotEmpty
}

// hasDirectoryContents checks if a directory has any entries (files or subdirectories).
func hasDirectoryContents(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// Xpath_rename implements path_rename. Resolves both old and new paths
// and calls os.Rename. Returns EROFS if either mount is read-only or if
// the resolved paths are not beneath a writable preopen root.
func (s *State) Xpath_rename(oldDirfd, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen int32) int32 {
	// Resolve mounts and check they exist
	oldMount, _ := s.resolveDirfdPath(oldDirfd, oldPathPtr, oldPathLen)
	newMount, _ := s.resolveDirfdPath(newDirfd, newPathPtr, newPathLen)
	if oldMount == nil || newMount == nil {
		return wasiENoEnt
	}

	// Resolve the actual host paths, ensuring they are writable
	oldPath, oldErr := s.resolveWritable(oldDirfd, oldPathPtr, oldPathLen)
	if oldErr != wasiESuccess {
		return oldErr
	}
	newPath, newErr := s.resolveWritable(newDirfd, newPathPtr, newPathLen)
	if newErr != wasiESuccess {
		return newErr
	}

	// Validate the rename operation before attempting it
	if errno := s.validateRename(oldPath, newPath); errno != wasiESuccess {
		return errno
	}

	// Perform the actual rename
	if err := renamePath(oldPath, newPath); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}
