package wasihost

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// resolveForOpen resolves (dirfd, path) for path_open, returning the mount,
// mount-relative path, inherited rights from the directory fd, and an errno.
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

		mpPrefix := mp + "/"
		if mp == "/" {
			mpPrefix = "/"
		}
		if clean == mp && len(mp) > bestLen {
			best = m
			bestLen = len(mp)
			bestRel = ""
		} else if strings.HasPrefix(clean, mpPrefix) && len(mp) > bestLen {
			rel := strings.TrimPrefix(clean, mpPrefix)
			best = m
			bestLen = len(mp)
			bestRel = rel
		}

		raw := "/" + strings.TrimPrefix(guestPath, "/")
		if raw == mp && len(mp) > bestLen {
			best = m
			bestLen = len(mp)
			bestRel = ""
		} else if strings.HasPrefix(raw, mpPrefix) && len(mp) > bestLen {
			rel := strings.TrimPrefix(raw, mpPrefix)
			best = m
			bestLen = len(mp)
			bestRel = rel
		}
	}
	return best, bestRel
}

// isRootMount reports whether m covers the guest root directory "/".
func isRootMount(m *mountEntry) bool {
	return path.Clean("/"+m.guestPath) == "/"
}

// writableMountHostPaths returns the primary and fallback host filesystem paths
// for a mount-relative path rel within a writable host-backed mount m.
// For root mounts, the primary path is the re-absolutized form ("/" + rel),
// recovering the original absolute host path. The joined path
// (filepath.Join(hostRoot, rel)) is returned as fallback for cwd-relative
// paths. For non-root mounts, only the joined path is returned.
// For root mounts with symlink-following lookups, the primary path is
// confined via filepath.EvalSymlinks to detect escapes; ENOTCAPABLE is
// returned as the errno when confinement fails.
// Returns ("", "", _) if m is nil or has no hostRoot.
func writableMountHostPaths(m *mountEntry, rel string, followSymlinks bool) (primary, fallback string, errno int32) {
	if m == nil || m.hostRoot == "" {
		return "", "", 0
	}
	joined := filepath.Join(m.hostRoot, filepath.FromSlash(rel))
	if isRootMount(m) {
		abs := "/" + filepath.FromSlash(rel)
		if abs != joined {
			if followSymlinks {
				resolved, err := filepath.EvalSymlinks(abs)
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return abs, joined, wasiESuccess
					}
					return "", "", wasiENoEnt
				}
				_ = resolved
			}
			return abs, joined, wasiESuccess
		}
	}
	return joined, "", 0
}

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
func preopenMountRelEscapes(rel string) bool {
	relLex := strings.TrimLeft(rel, "/")
	cleanLex := path.Clean(relLex)
	return cleanLex == ".." || strings.HasPrefix(cleanLex, "../")
}

// preopenDirfdLexicallyEscapes reports whether dirfd refers to a directory
// preopen and mountRel would lexically escape that preopen's root (see
// preopenMountRelEscapes). Matches the guard used before host-backed path
// operations in resolveWritable, Xpath_open, and Xpath_filestat_get.
func (s *State) preopenDirfdLexicallyEscapes(dirfd int32, mountRel string) bool {
	entry, ok := s.fdEntry(dirfd)
	return ok && entry.preopen && preopenMountRelEscapes(mountRel)
}

// fdEntry returns the fdEntry for dirfd if it is in bounds.
func (s *State) fdEntry(dirfd int32) (fdEntry, bool) {
	if dirfd < 0 || int(dirfd) >= len(s.fds) {
		return fdEntry{}, false
	}
	return s.fds[dirfd], true
}

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
// path_open and resolveWritable from accessing paths above the dirfd's
// resolved directory using ".." segments.
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

func (s *State) errnoIfNonPreopenDirfdEscapes(dirfd int32, mount *mountEntry, rel string) int32 {
	if s.nonPreopenDirfdResolvedPathEscapes(dirfd, mount, rel) {
		return wasiENotCap
	}
	return 0
}

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

// guestPathNeedsSymlinkTraversal reports whether the path argument passed to the
// WASI syscall (not the mount-relative path after dirfd resolution) has more than
// one segment. Nested directory fds lengthen mountRel for single-component guest
// paths (for example "symlink" -> "scratch/symlink"); confinement must follow the
// guest segment count so mutations that address the final component directly
// (path_link on a symlink inode) do not run EvalSymlinks on symlink loops.
func guestPathNeedsSymlinkTraversal(guestPath, mountRel string) bool {
	if strings.HasPrefix(guestPath, "/") {
		return writableHostPathNeedsSymlinkTraversal(mountRel)
	}
	return writableHostPathNeedsSymlinkTraversal(guestPath)
}

// joinWritableHostPathForMutation joins hostRoot with a mount-relative path for
// writable host-backed mutations (path_create_directory, path_remove, etc.).
// Multi-segment guest paths run writableHostSymlinkFollowConfinementErrno so symlink
// traversal in intermediate directories cannot escape hostRoot. Single-segment
// guest paths join only (no EvalSymlinks), even when mountRel has multiple segments
// after nested dirfd resolution.
func joinWritableHostPathForMutation(hostRoot, mountRel, guestPathArg string) (hostPath string, errno int32) {
	hostPath = filepath.Join(hostRoot, filepath.FromSlash(mountRel))
	if guestPathNeedsSymlinkTraversal(guestPathArg, mountRel) {
		errno = writableHostSymlinkFollowConfinementErrno(hostRoot, hostPath)
	}
	return hostPath, errno
}

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
func statPath(path string, follow bool) (fs.FileInfo, error) {
	if follow {
		return os.Stat(path)
	}
	return os.Lstat(path)
}

// filestatFdTypeFromInfo maps os.FileInfo / fs.FileInfo to a WASI preview1
// filetype for the filestat struct.

// writableHostPathNeedsSymlinkTraversal reports whether rel has more than one
// path segment after path.Clean, meaning host resolution must walk through at
// least one intermediate directory (and may follow symlinks in those steps).
func writableHostPathNeedsSymlinkTraversal(rel string) bool {
	clean := path.Clean(rel)
	if clean == "." || clean == "" {
		return false
	}
	return strings.Contains(clean, "/")
}

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
// and other host-backed operations. Before joining hostRoot it rejects paths
// with ENOTCAPABLE when:
//   - a directory preopen would lexically escape its root (preopenDirfdLexicallyEscapes), or
//   - a nested non-preopen dirfd would resolve outside its subtree (errnoIfNonPreopenDirfdEscapes).
//
// Host path joining uses joinWritableHostPathForMutation: multi-segment guest
// paths run symlink confinement; single-segment guest paths join only (no EvalSymlinks).
func (s *State) resolveWritable(dirfd, pathPtr, pathLen int32) (string, int32) {
	guestPath := string(s.readBytes(pathPtr, pathLen))
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
	if errno := s.errnoIfNonPreopenDirfdEscapes(dirfd, m, rel); errno != 0 {
		return "", errno
	}

	if !m.writable || m.hostRoot == "" {
		return "", wasiEROFS
	}

	if isRootMount(m) {
		if strings.HasPrefix(guestPath, "/") {
			// Absolute guest path: recover the original host absolute path
			return "/" + filepath.FromSlash(rel), wasiESuccess
		}
		// Relative guest path: simple join against hostRoot
		return filepath.Join(m.hostRoot, filepath.FromSlash(rel)), wasiESuccess
	}
	hostPath, errno := joinWritableHostPathForMutation(m.hostRoot, rel, guestPath)
	if errno != 0 {
		return "", errno
	}
	return hostPath, wasiESuccess
}
