package wasihost

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// resolveForOpen resolves the dirfd/path and returns mount, relPath, parentBase, parentInh, and errno.
func (s *State) resolveForOpen(dirfd, pathPtr, pathLen int32, guestPath string) (mount *mountEntry, relPath string, parentBase, parentInh uint64, errno int32) {
	mount, relPath = s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if mount == nil {
		entry, ok := s.fdEntry(dirfd)
		if ok && entry.fdType == fdFile && !strings.HasPrefix(guestPath, "/") {
			return nil, "", 0, 0, wasiENotDir
		}
		if s.isNonPreopenDirfd(dirfd) && strings.HasPrefix(guestPath, "/") {
			return nil, "", 0, 0, wasiEPerm
		}
		return nil, "", 0, 0, wasiENoEnt
	}
	if entry, ok := s.fdEntry(dirfd); ok {
		parentBase = entry.rightsBase
		parentInh = entry.rightsInheriting
	}
	return mount, relPath, parentBase, parentInh, wasiESuccess
}

// resolvePath resolves a guest-absolute path to the best-matching mount
// and a mount-relative path string using longest-prefix matching.
// Returns (nil, "") if no mount covers the path.

// resolvePath resolves a guest-absolute path to the best-matching mount
// and a mount-relative path string using longest-prefix matching.
// Returns (nil, "") if no mount covers the path.
func (s *State) resolvePath(guestPath string) (*mountEntry, string) {
	var best *mountEntry
	bestLen := -1
	bestRel := ""
	for i := range s.mounts {
		m := &s.mounts[i]
		mp := path.Clean("/" + m.guestPath)
		if mp == "." {
			mp = "/"
		}

		clean := path.Clean("/" + guestPath)
		if clean == "." {
			clean = "/"
		}

		if (clean == mp || strings.HasPrefix(clean, mp+"/")) && len(mp) > bestLen {
			rel := strings.TrimPrefix(clean, mp)
			rel = strings.TrimPrefix(rel, "/")
			best = m
			bestLen = len(mp)
			bestRel = rel
		}

		raw := "/" + strings.TrimPrefix(guestPath, "/")
		if (strings.HasPrefix(raw, mp+"/") || raw == mp) && len(mp) > bestLen {
			rel := strings.TrimPrefix(raw, mp)
			rel = strings.TrimPrefix(rel, "/")
			best = m
			bestLen = len(mp)
			bestRel = rel
		}
	}
	return best, bestRel
}

// mountGuestPath returns the normalized guest-absolute path for a
// mount-relative path by joining the mount's guest path with relPath
// and applying path.Clean. For example, mount.guestPath="/data" and
// relPath="dir/../file" yields "/data/file".

// mountGuestPath returns the normalized guest-absolute path for a
// mount-relative path by joining the mount's guest path with relPath
// and applying path.Clean. For example, mount.guestPath="/data" and
// relPath="dir/../file" yields "/data/file".
func mountGuestPath(m *mountEntry, relPath string) string {
	return path.Clean("/" + m.guestPath + "/" + relPath)
}

// preopenMountRelEscapes reports whether a mount-relative guest path
// lexically escapes upward past the preopen root after normalization
// (for example ".." or "../segment").

// preopenMountRelEscapes reports whether a mount-relative guest path
// lexically escapes upward past the preopen root after normalization
// (for example ".." or "../segment").
func preopenMountRelEscapes(rel string) bool {
	relLex := strings.TrimLeft(rel, "/")
	cleanLex := path.Clean(relLex)
	return cleanLex == ".." || strings.HasPrefix(cleanLex, "../")
}

// preopenDirfdLexicallyEscapes reports whether dirfd refers to a directory
// preopen and mountRel would lexically escape that preopen's root (see
// preopenMountRelEscapes). Matches the guard used before host-backed path
// operations in resolveWritable, Xpath_open, and Xpath_filestat_get.

// preopenDirfdLexicallyEscapes reports whether dirfd refers to a directory
// preopen and mountRel would lexically escape that preopen's root (see
// preopenMountRelEscapes). Matches the guard used before host-backed path
// operations in resolveWritable, Xpath_open, and Xpath_filestat_get.
func (s *State) preopenDirfdLexicallyEscapes(dirfd int32, mountRel string) bool {
	entry, ok := s.fdEntry(dirfd)
	return ok && entry.preopen && preopenMountRelEscapes(mountRel)
}

// fdEntry returns the fdEntry for dirfd if it is in bounds.

// fdEntry returns the fdEntry for dirfd if it is in bounds.
func (s *State) fdEntry(dirfd int32) (fdEntry, bool) {
	if dirfd < 0 || int(dirfd) >= len(s.fds) {
		return fdEntry{}, false
	}
	return s.fds[dirfd], true
}

// isNonPreopenDirfd reports whether dirfd refers to an open directory
// that was not a preopen (i.e. it was opened via path_open).

// isNonPreopenDirfd reports whether dirfd refers to an open directory
// that was not a preopen (i.e. it was opened via path_open).
func (s *State) isNonPreopenDirfd(dirfd int32) bool {
	entry, ok := s.fdEntry(dirfd)
	return ok && !entry.preopen && entry.fdType == fdDir
}

// guestAbsPathForFDEntry returns the guest-absolute path to store in an fd
// entry for guestPath opened via dirfd. Absolute paths are returned unchanged;
// relative paths are joined against the dirfd entry's stored guest-absolute
// path (preopen or nested directory).

// guestAbsPathForFDEntry returns the guest-absolute path to store in an fd
// entry for guestPath opened via dirfd. Absolute paths are returned unchanged;
// relative paths are joined against the dirfd entry's stored guest-absolute
// path (preopen or nested directory).
func (s *State) guestAbsPathForFDEntry(dirfd int32, guestPath string) string {
	if strings.HasPrefix(guestPath, "/") {
		return guestPath
	}
	if entry, ok := s.fdEntry(dirfd); ok && entry.path != "" {
		return path.Join(entry.path, guestPath)
	}
	return guestPath
}

// nonPreopenDirfdResolvedPathEscapes reports whether resolving relPath
// through the given mount produces a guest-absolute path that falls
// outside the subtree of a non-preopen directory fd. This prevents
// path_open from accessing paths above the dirfd's resolved directory
// using ".." segments.

// nonPreopenDirfdResolvedPathEscapes reports whether resolving relPath
// through the given mount produces a guest-absolute path that falls
// outside the subtree of a non-preopen directory fd. This prevents
// path_open from accessing paths above the dirfd's resolved directory
// using ".." segments.
func (s *State) nonPreopenDirfdResolvedPathEscapes(dirfd int32, mount *mountEntry, relPath string) bool {
	if !s.isNonPreopenDirfd(dirfd) {
		return false
	}
	resolvedGuest := mountGuestPath(mount, relPath)
	dirEntry, ok := s.fdEntry(dirfd)
	if !ok || dirEntry.path == "" {
		return false
	}
	prefix := dirEntry.path
	return resolvedGuest != prefix && !strings.HasPrefix(resolvedGuest, prefix+"/")
}

// preopenEntryByFD returns the fdEntry for preopen fd if it is valid and
// in use. The ok bool is false when fd is not a preopen or the slot is unused.

// preopenEntryByFD returns the fdEntry for preopen fd if it is valid and
// in use. The ok bool is false when fd is not a preopen or the slot is unused.
func (s *State) preopenEntryByFD(fd int32) (fdEntry, bool) {
	idx := fd - 3
	if idx < 0 || idx >= int32(len(s.preopens)) {
		return fdEntry{}, false
	}
	entry := s.preopens[idx]
	if entry.isUnused() {
		return fdEntry{}, false
	}
	return entry, true
}

// joinWritableHostPathForLookup joins hostRoot with a mount-relative path for a
// host directory preopen. When symlink following is not requested, it Lstats the
// host path and returns ELOOP if the final component is a symlink (matching
// O_NOFOLLOW-style behavior); other Lstat errors are ignored so the caller can
// surface them. When symlink following is requested, it evaluates symlinks and
// returns ENOTCAPABLE if resolution would escape hostRoot.

// joinWritableHostPathForLookup joins hostRoot with a mount-relative path for a
// host directory preopen. When symlink following is not requested, it Lstats the
// host path and returns ELOOP if the final component is a symlink (matching
// O_NOFOLLOW-style behavior); other Lstat errors are ignored so the caller can
// surface them. When symlink following is requested, it evaluates symlinks and
// returns ENOTCAPABLE if resolution would escape hostRoot.
func joinWritableHostPathForLookup(hostRoot, relPath string, lookupFlags int32) (hostPath string, errno int32) {
	hostPath = filepath.Join(hostRoot, filepath.FromSlash(relPath))
	if lookupFlags&wasiLookupSymlinkFollow == 0 {
		// Open without SYMLINK_FOLLOW must not traverse the final symlink; Lstat
		// distinguishes a present symlink from a missing path (Lstat error).
		fi, err := os.Lstat(hostPath)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return hostPath, wasiELoop
		}
		return hostPath, wasiESuccess
	}
	return hostPath, writableHostSymlinkFollowConfinementErrno(hostRoot, hostPath)
}

// statHostPathOrOverlay runs os.Lstat or os.Stat on hostPath (depending on
// followFinalSymlink), and if that fails tries fs.Stat on overlay at relPath.
// Used for writable mounts where some paths exist only in the overlay fs.FS.

// statHostPathOrOverlay runs os.Lstat or os.Stat on hostPath (depending on
// followFinalSymlink), and if that fails tries fs.Stat on overlay at relPath.
// Used for writable mounts where some paths exist only in the overlay fs.FS.
func statHostPathOrOverlay(hostPath string, overlay fs.FS, relPath string, followFinalSymlink bool) (fs.FileInfo, error) {
	stat := os.Lstat
	if followFinalSymlink {
		stat = os.Stat
	}
	fi, err := stat(hostPath)
	if err != nil {
		return fs.Stat(overlay, relPath)
	}
	return fi, nil
}

// statPath returns FileInfo for path. When follow is true it follows symlinks
// (os.Stat); when false it returns info for the symlink itself (os.Lstat).

// statPath returns FileInfo for path. When follow is true it follows symlinks
// (os.Stat); when false it returns info for the symlink itself (os.Lstat).
func statPath(path string, follow bool) (fs.FileInfo, error) {
	if follow {
		return os.Stat(path)
	}
	return os.Lstat(path)
}

// filestatFdTypeFromInfo maps os.FileInfo / fs.FileInfo to a WASI preview1
// filetype for the filestat struct.

// writableHostSymlinkFollowConfinementErrno checks that resolving hostPath with
// symlink awareness stays inside hostRoot (after filepath.Clean / EvalSymlinks).
// It returns ESUCCESS when the resolved path is confined to the preopen,
// ENOTCAPABLE when symlink steps would reach outside the root, and ENOENT when
// symlink resolution fails (non-ErrNotExist errors) or the root cannot be resolved.
//
// When the final path component is missing (as with O_CREAT while following
// symlinks), EvalSymlinks returns ErrNotExist for the full path. The
// implementation then walks up with filepath.Dir, re-running EvalSymlinks on
// each parent, until it finds an existing prefix. That preserves confinement
// checks for symlink chains that point outside the preopen followed by a
// non-existent trailing name.
func writableHostSymlinkFollowConfinementErrno(hostRoot, hostPath string) int32 {
	tryPath := filepath.Clean(hostPath)
	var resolved string
	for {
		var err error
		resolved, err = filepath.EvalSymlinks(tryPath)
		if err == nil {
			break
		}
		if errors.Is(err, fs.ErrNotExist) {
			parent := filepath.Dir(tryPath)
			if parent == tryPath {
				return wasiESuccess
			}
			tryPath = parent
			continue
		}
		return wasiENoEnt
	}
	return resolvedPathConfinementErrno(resolved, hostRoot)
}

// resolvedPathConfinementErrno returns ESUCCESS when resolvedPath is inside or
// equal to root, ENOTCAPABLE when it escapes upward, and ENOENT/EIO for
// filesystem errors encountered while canonicalizing root.

// resolvedPathConfinementErrno returns ESUCCESS when resolvedPath is inside or
// equal to root, ENOTCAPABLE when it escapes upward, and ENOENT/EIO for
// filesystem errors encountered while canonicalizing root.
func resolvedPathConfinementErrno(resolvedPath, root string) int32 {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return wasiENoEnt
	}
	rootAbs, err := filepath.Abs(rootReal)
	if err != nil {
		return wasiEIo
	}
	resAbs, err := filepath.Abs(resolvedPath)
	if err != nil {
		return wasiEIo
	}
	rel, err := filepath.Rel(rootAbs, resAbs)
	if err != nil {
		return wasiENotCap
	}
	if rel == "." {
		return wasiESuccess
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return wasiENotCap
	}
	return wasiESuccess
}

// resolveDirfdPath resolves a WASI (dirfd, pathPtr, pathLen) triple to a
// mountEntry and a mount-relative path string.
//
// Absolute guest paths bypass dirfd and are resolved via resolvePath.
// For relative paths, preopen fds resolve directly against their mount.
// Non-preopen directory fds join the relative path against the absolute
// guest path stored in the fd entry, enabling correct nested path_open
// resolution during directory recursion.
// Non-directory fds (including regular files) yield a nil mount.

// resolveDirfdPath resolves a WASI (dirfd, pathPtr, pathLen) triple to a
// mountEntry and a mount-relative path string.
//
// Absolute guest paths bypass dirfd and are resolved via resolvePath.
// For relative paths, preopen fds resolve directly against their mount.
// Non-preopen directory fds join the relative path against the absolute
// guest path stored in the fd entry, enabling correct nested path_open
// resolution during directory recursion.
// Non-directory fds (including regular files) yield a nil mount.
func (s *State) resolveDirfdPath(dirfd, pathPtr, pathLen int32) (*mountEntry, string) {
	pathBytes := s.readBytes(pathPtr, pathLen)
	guestPath := string(pathBytes)
	if strings.HasPrefix(guestPath, "/") {
		if s.isNonPreopenDirfd(dirfd) {
			return nil, ""
		}
		return s.resolvePath(guestPath)
	}
	if entry, ok := s.fdEntry(dirfd); ok {
		if entry.preopen && entry.mount >= 0 && entry.mount < len(s.mounts) {
			return &s.mounts[entry.mount], guestPath
		}
		if entry.fdType == fdDir && entry.path != "" {
			full := path.Join(entry.path, guestPath)
			return s.resolvePath(full)
		}
	}
	return nil, ""
}

// resolveWritable resolves a (dirfd, path) pair to a host path for mutation
// and other host-backed operations. Directory preopens reject mount-relative
// paths that lexically escape the preopen root with ENOTCAPABLE (see
// preopenDirfdLexicallyEscapes) before joining hostRoot.

// resolveWritable resolves a (dirfd, path) pair to a host path for mutation
// and other host-backed operations. Directory preopens reject mount-relative
// paths that lexically escape the preopen root with ENOTCAPABLE (see
// preopenDirfdLexicallyEscapes) before joining hostRoot.
func (s *State) resolveWritable(dirfd, pathPtr, pathLen int32) (string, int32) {
	m, rel := s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if m == nil {
		if dirfd < 0 || int(dirfd) >= len(s.fds) {
			return "", wasiEBadf
		}
		return "", wasiEROFS
	}

	if s.preopenDirfdLexicallyEscapes(dirfd, rel) {
		return "", wasiENotCap
	}

	if !m.writable || m.hostRoot == "" {
		return "", wasiEROFS
	}

	return filepath.Join(m.hostRoot, filepath.FromSlash(rel)), wasiESuccess
}

// Xpath_create_directory implements path_create_directory. Creates a
// directory at the resolved host path via os.Mkdir. Returns EROFS if the
// mount is read-only, EEXIST if the directory already exists, ENOENT if
// the parent does not exist.
