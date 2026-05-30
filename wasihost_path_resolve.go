package wasihost

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

// PreopenEntry is one guest preopened directory (wasi.FS + guest path prefix).
type PreopenEntry struct {
	fsys      wasi.FS
	guestPath string
}

func (s *State) ctxPreopens() []PreopenEntry {
	if s.ctx == nil {
		return nil
	}
	ps := s.ctx.FS()
	if len(ps) > 0 {
		out := make([]PreopenEntry, len(ps))
		for i, p := range ps {
			out[i] = PreopenEntry{fsys: p.FS, guestPath: p.GuestPath}
		}
		return out
	}
	fsc := s.ctx.FSContext()
	if fsc == nil {
		return nil
	}
	var out []PreopenEntry
	for fd := int32(3); ; fd++ {
		fe, ok := fsc.LookupFile(fd)
		if !ok {
			break
		}
		if !fe.IsPreopen || fe.FS == nil {
			continue
		}
		gp := fe.GuestPath
		if gp == "" {
			gp = fe.Name
		}
		out = append(out, PreopenEntry{fsys: fe.FS, guestPath: gp})
	}
	return out
}

func (s *State) preopenAt(index int) *PreopenEntry {
	preopens := s.ctxPreopens()
	if index < 0 || index >= len(preopens) {
		return nil
	}
	pe := preopens[index]
	return &pe
}

func preopenGuestPathClean(p *PreopenEntry) string {
	if p == nil {
		return ""
	}
	mp := path.Clean("/" + p.guestPath)
	if mp == "." {
		return "/"
	}
	return mp
}

func isRootPreopen(p *PreopenEntry) bool {
	return preopenGuestPathClean(p) == "/"
}

func preopenGuestPathForRel(p *PreopenEntry, relPath string) string {
	return path.Clean("/" + p.guestPath + "/" + relPath)
}

func fsysReadOnly(fsys wasi.FS) bool {
	switch fsys.(type) {
	case sysfs.ReadFS, *sysfs.ReadFS, sysfs.AdaptFS, *sysfs.AdaptFS:
		return true
	default:
		return false
	}
}

func preopenReadonly(p *PreopenEntry) bool {
	if p == nil {
		return true
	}
	return fsysReadOnly(p.fsys)
}

func preopenWritable(p *PreopenEntry) bool {
	return preopenHostRoot(p) != "" && !preopenReadonly(p)
}

func preopenHostRoot(p *PreopenEntry) string {
	if p == nil || p.fsys == nil {
		return ""
	}
	switch f := p.fsys.(type) {
	case sysfs.DirFS:
		return f.Path
	case *sysfs.DirFS:
		return f.Path
	case sysfs.ReadFS:
		return preopenHostRootFromFS(f.FS)
	case *sysfs.ReadFS:
		return preopenHostRootFromFS(f.FS)
	default:
		return p.fsys.PathFor()
	}
}

func preopenHostRootFromFS(fsys wasi.FS) string {
	switch f := fsys.(type) {
	case sysfs.DirFS:
		return f.Path
	case *sysfs.DirFS:
		return f.Path
	default:
		if fsys != nil {
			return fsys.PathFor()
		}
	}
	return ""
}

func preopenLegacyRoot(p *PreopenEntry) fs.FS {
	if p == nil {
		return nil
	}
	switch f := p.fsys.(type) {
	case sysfs.AdaptFS:
		return f.FS
	case *sysfs.AdaptFS:
		return f.FS
	case sysfs.DirFS:
		return os.DirFS(f.Path)
	case *sysfs.DirFS:
		return os.DirFS(f.Path)
	case sysfs.ReadFS:
		return preopenLegacyRoot(&PreopenEntry{fsys: f.FS})
	case *sysfs.ReadFS:
		return preopenLegacyRoot(&PreopenEntry{fsys: f.FS})
	default:
		return nil
	}
}

// overlayRootForPreopen returns an embedded fs.FS overlay for read-only preopens
// or optional Overlay on writable host preopens.
func (s *State) overlayRootForPreopen(p *PreopenEntry) fs.FS {
	if p == nil {
		return nil
	}
	for fd := int32(3); ; fd++ {
		fe, ok := s.lookupFD(fd)
		if !ok {
			break
		}
		if fe.FS == p.fsys && fe.Overlay != nil {
			return fe.Overlay
		}
	}
	return preopenLegacyRoot(p)
}

// resolveForOpen resolves (dirfd, path) for path_open, returning the preopen,
// mount-relative path, inherited rights from the directory fd, and an errno.
func (s *State) resolveForOpen(dirfd, pathPtr, pathLen int32, guestPath string) (preopen *PreopenEntry, relPath string, parentBase, parentInh uint64, errno int32) {
	preopen, relPath = s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if preopen == nil {
		if fe, ok := s.lookupFD(dirfd); ok {
			if fdType, _ := fileEntryFdType(fe); fdType == fdFile && guestPath != "" && guestPath[0] != '/' {
				return nil, "", 0, 0, wasiENotDir
			}
		}
		if s.isNonPreopenDirfd(dirfd) && guestPath != "" && guestPath[0] == '/' {
			return nil, "", 0, 0, wasiEPerm
		}
		return nil, "", 0, 0, wasiENoEnt
	}
	if fe, ok := s.lookupFD(dirfd); ok {
		parentBase = fe.RightsBase
		parentInh = fe.RightsInheriting
	}
	return preopen, relPath, parentBase, parentInh, wasiESuccess
}

// preopenLongestPrefixMatch reports whether candidate lies under preopen p's
// guest prefix and returns the prefix length and mount-relative remainder.
func preopenLongestPrefixMatch(p *PreopenEntry, candidate string) (prefixLen int, rel string, ok bool) {
	mp := preopenGuestPathClean(p)
	mpPrefix := mp + "/"
	if mp == "/" {
		mpPrefix = "/"
	}
	if candidate == mp {
		return len(mp), "", true
	}
	if strings.HasPrefix(candidate, mpPrefix) {
		return len(mp), strings.TrimPrefix(candidate, mpPrefix), true
	}
	return 0, "", false
}

// resolvePath resolves a guest-absolute path to the best-matching preopen
// and a mount-relative path string using longest-prefix matching.
// Returns (nil, "") if no preopen covers the path.
func (s *State) resolvePath(guestPath string) (*PreopenEntry, string) {
	var best *PreopenEntry
	bestLen := -1
	bestRel := ""
	matchCandidate := func(candidate string) {
		for i := range s.ctxPreopens() {
			p := s.preopenAt(i)
			if prefixLen, rel, ok := preopenLongestPrefixMatch(p, candidate); ok && prefixLen > bestLen {
				best = p
				bestLen = prefixLen
				bestRel = rel
			}
		}
	}
	clean := path.Clean("/" + guestPath)
	if clean == "." {
		clean = "/"
	}
	matchCandidate(clean)
	matchCandidate("/" + strings.TrimPrefix(guestPath, "/"))
	return best, bestRel
}

// writableMountHostPaths returns the primary and fallback host filesystem paths
// for a mount-relative path rel within a writable host-backed preopen p.
// For root mounts, the primary path is the re-absolutized form ("/" + rel),
// recovering the original absolute host path. The joined path
// (filepath.Join(hostRoot, rel)) is returned as fallback for cwd-relative
// paths. For non-root mounts, only the joined path is returned.
// For root mounts with symlink-following lookups, the primary path is
// confined via filepath.EvalSymlinks to detect escapes; ENOTCAPABLE is
// returned as the errno when confinement fails.
// Returns ("", "", _) if p is nil or has no host root.
func writableMountHostPaths(p *PreopenEntry, rel string, followSymlinks bool) (primary, fallback string, errno int32) {
	hostRoot := preopenHostRoot(p)
	if p == nil || hostRoot == "" {
		return "", "", 0
	}
	joined := filepath.Join(hostRoot, filepath.FromSlash(rel))
	if isRootPreopen(p) {
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
	fe, ok := s.lookupFD(dirfd)
	return ok && fe.IsPreopen && preopenMountRelEscapes(mountRel)
}

func (s *State) isNonPreopenDirfd(dirfd int32) bool {
	fe, ok := s.lookupFD(dirfd)
	if !ok || fe.IsPreopen {
		return false
	}
	fdType, errno := fileEntryFdType(fe)
	return errno == 0 && fdType == fdDir
}

// nonPreopenDirfdResolvedPathEscapes reports whether resolving relPath
// through the given mount produces a guest-absolute path that falls
// outside the subtree of a non-preopen directory fd. This prevents
// path_open and resolveWritable from accessing paths above the dirfd's
// resolved directory using ".." segments.
func (s *State) nonPreopenDirfdResolvedPathEscapes(dirfd int32, preopen *PreopenEntry, relPath string) bool {
	if !s.isNonPreopenDirfd(dirfd) {
		return false
	}
	resolvedGuest := preopenGuestPathForRel(preopen, relPath)
	fe, ok := s.lookupFD(dirfd)
	if !ok {
		return false
	}
	prefix := fileEntryGuestPath(fe)
	if prefix == "" {
		return false
	}
	return resolvedGuest != prefix && !strings.HasPrefix(resolvedGuest, prefix+"/")
}

func (s *State) errnoIfNonPreopenDirfdEscapes(dirfd int32, preopen *PreopenEntry, rel string) int32 {
	if s.nonPreopenDirfdResolvedPathEscapes(dirfd, preopen, rel) {
		return wasiENotCap
	}
	return 0
}

// preopenEntryByFD returns the FileEntry for preopen fd if valid.
func (s *State) preopenEntryByFD(fd int32) (*wasi.FileEntry, bool) {
	idx := fd - 3
	if idx < 0 {
		return nil, false
	}
	fe, ok := s.lookupFD(fd)
	if !ok || !fe.IsPreopen {
		return nil, false
	}
	return fe, true
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

// statPath returns FileInfo for path. When follow is true it follows symlinks
// (os.Stat); when false it returns info for the symlink itself (os.Lstat).
func statPath(path string, follow bool) (fs.FileInfo, error) {
	if follow {
		return os.Stat(path)
	}
	return os.Lstat(path)
}

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

// TODO(post-merge): inherent TOCTOU — symlink target may change between this
// EvalSymlinks confinement check and a later open. Hardening needs per-component
// openat(O_NOFOLLOW) or equivalent; path replay alone cannot close the race.
//
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
// PreopenEntry and a mount-relative path string.
//
// Absolute guest paths bypass dirfd and are resolved via resolvePath.
// For relative paths, preopen fds resolve directly against their preopen.
// Non-preopen directory fds join the relative path against the absolute
// guest path stored in the fd entry, enabling correct nested path_open
// resolution during directory recursion.
// Non-directory fds (including regular files) yield a nil preopen.
func (s *State) resolveDirfdPath(dirfd, pathPtr, pathLen int32) (*PreopenEntry, string) {
	pathBytes := s.readBytes(pathPtr, pathLen)
	guestPath := string(pathBytes)
	if strings.HasPrefix(guestPath, "/") {
		if s.isNonPreopenDirfd(dirfd) {
			return nil, ""
		}
		return s.resolvePath(guestPath)
	}
	if fe, ok := s.lookupFD(dirfd); ok {
		if fe.IsPreopen {
			if idx, ok := s.preopenIndex(dirfd); ok {
				return s.preopenAt(idx), guestPath
			}
		}
		if fdType, errno := fileEntryFdType(fe); errno == 0 && fdType == fdDir {
			gp := fileEntryGuestPath(fe)
			if gp != "" {
				full := path.Join(gp, guestPath)
				return s.resolvePath(full)
			}
		}
	}
	return nil, ""
}

// writableMutation is a writable preopen resolved for a path_* mutation syscall.
type writableMutation struct {
	preopen   *PreopenEntry
	rel       string
	guestPath string
	vpath     string
}

// writableMutationPair holds old/new writable paths for path_link and path_rename.
type writableMutationPair struct {
	old writableMutation
	new writableMutation
}

// resolveWritable resolves a (dirfd, path) pair to a writable preopen and a
// mount-relative path for VFS mutation. Rejects paths with ENOTCAPABLE when:
//   - a directory preopen would lexically escape its root (preopenDirfdLexicallyEscapes), or
//   - a nested non-preopen dirfd would resolve outside its subtree (errnoIfNonPreopenDirfdEscapes).
//
// Multi-segment guest paths on non-root preopens run symlink confinement before
// returning; pass guestPath to vfsPathForMutation when calling wasi.FS methods.
func (s *State) resolveWritable(dirfd, pathPtr, pathLen int32) (*PreopenEntry, string, int32) {
	m, errno := s.resolveWritableMutation(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return nil, "", errno
	}
	return m.preopen, m.rel, wasiESuccess
}

// resolveWritableMutation resolves a writable preopen and the wasi.FS mutation path
// in one guest-path read (vpath from vfsPathForMutation).
func (s *State) resolveWritableMutation(dirfd, pathPtr, pathLen int32) (writableMutation, int32) {
	guestPath := string(s.readBytes(pathPtr, pathLen))
	p, rel, errno := s.resolveWritableForGuest(guestPath, dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return writableMutation{}, errno
	}
	return writableMutation{
		preopen:   p,
		rel:       rel,
		guestPath: guestPath,
		vpath:     vfsPathForMutation(p, rel, guestPath),
	}, wasiESuccess
}

// resolveWritableMutationPair resolves old/new writable paths for dual-path mutations.
func (s *State) resolveWritableMutationPair(oldDirfd, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen int32) (writableMutationPair, int32) {
	oldGuest := string(s.readBytes(oldPathPtr, oldPathLen))
	newGuest := string(s.readBytes(newPathPtr, newPathLen))
	oldP, oldRel, oldErr := s.resolveWritableForGuest(oldGuest, oldDirfd, oldPathPtr, oldPathLen)
	if oldErr != wasiESuccess {
		return writableMutationPair{}, oldErr
	}
	newP, newRel, newErr := s.resolveWritableForGuest(newGuest, newDirfd, newPathPtr, newPathLen)
	if newErr != wasiESuccess {
		return writableMutationPair{}, newErr
	}
	return writableMutationPair{
		old: writableMutation{
			preopen:   oldP,
			rel:       oldRel,
			guestPath: oldGuest,
			vpath:     vfsPathForMutation(oldP, oldRel, oldGuest),
		},
		new: writableMutation{
			preopen:   newP,
			rel:       newRel,
			guestPath: newGuest,
			vpath:     vfsPathForMutation(newP, newRel, newGuest),
		},
	}, wasiESuccess
}

func (s *State) resolveWritableForGuest(guestPath string, dirfd, pathPtr, pathLen int32) (*PreopenEntry, string, int32) {
	if len(s.ctxPreopens()) == 0 {
		return nil, "", wasiEROFS
	}
	p, rel := s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if p == nil {
		if _, ok := s.lookupFD(dirfd); !ok {
			return nil, "", wasiEBadf
		}
		return nil, "", wasiEROFS
	}

	if s.preopenDirfdLexicallyEscapes(dirfd, rel) {
		return nil, "", wasiENotCap
	}
	if errno := s.errnoIfNonPreopenDirfdEscapes(dirfd, p, rel); errno != 0 {
		return nil, "", errno
	}

	if !preopenWritable(p) || p.fsys == nil {
		return nil, "", wasiEROFS
	}

	if !isRootPreopen(p) && guestPathNeedsSymlinkTraversal(guestPath, rel) {
		hostRoot := preopenHostRoot(p)
		hostPath := filepath.Join(hostRoot, filepath.FromSlash(rel))
		if errno := writableHostSymlinkFollowConfinementErrno(hostRoot, hostPath); errno != 0 {
			return nil, "", errno
		}
	}

	return p, rel, wasiESuccess
}

// vfsPathForMutation returns the path argument for wasi.FS mutation methods.
// Root preopens with absolute guest paths re-absolutize to the host path.
func vfsPathForMutation(p *PreopenEntry, rel, guestPath string) string {
	if isRootPreopen(p) && strings.HasPrefix(guestPath, "/") {
		return "/" + filepath.FromSlash(rel)
	}
	return rel
}

// vfsLstat returns Lstat metadata or a mapped WASI errno.
func vfsLstat(p *PreopenEntry, vpath string) (wasi.Stat_t, int32) {
	st, werrno := p.fsys.Lstat(vpath)
	if werrno != 0 {
		return st, errnoFromWasi(werrno)
	}
	return st, wasiESuccess
}

func wasiStatIsDir(st wasi.Stat_t) bool {
	return st.Mode&fs.ModeDir != 0
}

func wasiStatIsSymlink(st wasi.Stat_t) bool {
	return st.Mode&fs.ModeSymlink != 0
}

// vfsHostPath maps a VFS mutation path to an absolute host path for platform
// syscalls that are not exposed on wasi.FS (e.g. linkat on symlink inodes).
func vfsHostPath(p *PreopenEntry, guestPath, vpath string) string {
	if isRootPreopen(p) && strings.HasPrefix(guestPath, "/") {
		return vpath
	}
	return filepath.Join(preopenHostRoot(p), filepath.FromSlash(vpath))
}

// statFSWithOverlay stats via preopen.fsys, falling back to overlay fs.FS.
func (s *State) statFSWithOverlay(p *PreopenEntry, vpath, rel string, follow bool) (wasi.Stat_t, int32) {
	var st wasi.Stat_t
	var werrno wasi.Errno
	if follow {
		st, werrno = p.fsys.Stat(vpath)
	} else {
		st, werrno = p.fsys.Lstat(vpath)
	}
	if werrno == 0 {
		return st, wasiESuccess
	}
	if werrno != wasi.ENOENT {
		return st, errnoFromWasi(werrno)
	}
	overlay := s.overlayRootForPreopen(p)
	if overlay == nil {
		return st, wasiENoEnt
	}
	fi, err := fs.Stat(overlay, rel)
	if err != nil {
		return st, wasiENoEnt
	}
	return wasi.NewStat_t(fi), wasiESuccess
}
