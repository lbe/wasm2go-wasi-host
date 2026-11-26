//go:build windows

package wasihost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWindowsBackslashGuestPathCannotEscapeSandbox checks that a guest path
// using '\' separators cannot reach a host file outside a sandboxed preopen.
// WASI paths only use '/', but Windows also treats '\' as a separator, and
// path_filestat_set_times without SYMLINK_FOLLOW builds a host path directly.
func TestWindowsBackslashGuestPathCannotEscapeSandbox(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmpDir, "outside.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(outside, old, old); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 512)
	s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/sandbox", root))

	const dirfd int32 = 3
	const pathOff int32 = 64
	guest := `..\outside.txt`
	copy(buf[pathOff:], guest)

	newTime := time.Unix(2_000_000_000, 0).UnixNano()
	errno := s.Xpath_filestat_set_times(dirfd, 0, pathOff, int32(len(guest)), newTime, newTime, fstAtim|fstMtim)
	if errno == wasiESuccess {
		t.Errorf("path_filestat_set_times(%q) succeeded, want failure", guest)
	}
	fi, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.ModTime().Equal(old) {
		t.Fatalf("mtime of file outside the sandbox changed to %v", fi.ModTime())
	}
}
