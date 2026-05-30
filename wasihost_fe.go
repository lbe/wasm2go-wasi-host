package wasihost

import (
	"path"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func fileAppendEnabled(fe *wasi.FileEntry) bool {
	return fe != nil && fe.File != nil && (fe.File.IsAppend() || fe.FdFlags&uint16(fdFlagsAppend) != 0)
}

func fileEntryGuestPath(fe *wasi.FileEntry) string {
	if fe.GuestPath != "" {
		return fe.GuestPath
	}
	return fe.Name
}

func fileEntryFdType(fe *wasi.FileEntry) (byte, int32) {
	if fe == nil || fe.File == nil {
		return 0, wasiEBadf
	}
	if isDir, errno := fe.File.IsDir(); errno != 0 {
		return 0, errnoFromWasi(errno)
	} else if isDir {
		return fdDir, 0
	}
	st, errno := fe.File.Stat()
	if errno != 0 {
		return 0, errnoFromWasi(errno)
	}
	return filestatFdTypeFromMode(st.Mode), 0
}

func fileEntryFdFlags(fe *wasi.FileEntry) uint16 {
	if fe == nil || fe.File == nil {
		return 0
	}
	flags := fe.FdFlags
	if fe.File.IsAppend() {
		flags |= uint16(fdFlagsAppend)
	}
	if pf, ok := fe.File.(wasi.PollableFile); ok && pf.IsNonblock() {
		flags |= uint16(fdFlagsNonBlock)
	}
	return flags
}

func (s *State) preopenIndex(fd int32) (int, bool) {
	fe, ok := s.lookupFD(fd)
	if !ok || !fe.IsPreopen || fd < 3 {
		return 0, false
	}
	return int(fd - 3), true
}

func (s *State) guestAbsPathForFE(dirfd int32, guestPath string, fe *wasi.FileEntry) string {
	if guestPath != "" && guestPath[0] == '/' {
		return guestPath
	}
	if gp := fileEntryGuestPath(fe); gp != "" {
		return path.Join(gp, guestPath)
	}
	return guestPath
}

func bytesToStrings(items [][]byte) []string {
	out := make([]string, len(items))
	for i, b := range items {
		out[i] = string(b)
	}
	return out
}
