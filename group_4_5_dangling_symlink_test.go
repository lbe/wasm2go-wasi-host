package wasihost

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDanglingSymlinkPathOpenNoFollow verifies that path_open without
// SYMLINK_FOLLOW on a dangling symlink returns ELOOP (or ENOTDIR when
// OFLAGS_DIRECTORY is set), not ESUCCESS.
func TestDanglingSymlinkPathOpenNoFollow(t *testing.T) {
	t.Parallel()

	if os.Getenv("NO_DANGLING_FILESYSTEM") != "" {
		t.Skip("NO_DANGLING_FILESYSTEM is set")
	}

	const (
		targetOff  = 100
		symlinkOff = 200
		fdPtr      = 300
	)

	s, buf := newTestState()
	hostDir := setupWritableMount(t, s, buf)

	// Create a dangling symlink "symlink" -> "target" (target does not exist).
	copy(buf[targetOff:], "target")
	copy(buf[symlinkOff:], "symlink")
	errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
	}

	// path_open without SYMLINK_FOLLOW and with OFLAGS_DIRECTORY must return
	// ENOTDIR or ELOOP.
	copy(buf[symlinkOff:], "symlink")
	errno = s.Xpath_open(dirfd, 0, symlinkOff, 7, int32(oflagDir),
		int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiENotDir && errno != wasiELoop {
		t.Fatalf("Xpath_open(no-follow, directory) = %d; want ENOTDIR (%d) or ELOOP (%d)",
			errno, wasiENotDir, wasiELoop)
	}

	// path_open without SYMLINK_FOLLOW and no OFLAGS_DIRECTORY must return ELOOP.
	copy(buf[symlinkOff:], "symlink")
	errno = s.Xpath_open(dirfd, 0, symlinkOff, 7, 0,
		int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
	if errno != wasiELoop {
		t.Fatalf("Xpath_open(no-follow, no flags) = %d; want ELOOP (%d)",
			errno, wasiELoop)
	}

	// Cleanup.
	copy(buf[symlinkOff:], "symlink")
	if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
		t.Fatalf("Xpath_unlink_file(symlink) = %d, want ESUCCESS", errno)
	}

	// Verify host state is clean.
	if _, err := os.Lstat(filepath.Join(hostDir, "symlink")); !os.IsNotExist(err) {
		t.Errorf("symlink still exists on host")
	}
}
