package wasihost

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPathUnlinkFileTrailingSlashSemantics verifies that path_unlink_file
// rejects trailing slashes on directories and files with the errno values the
// wasi-testsuite expects, and never silently succeeds or deletes the wrong
// entity.
func TestPathUnlinkFileTrailingSlashSemantics(t *testing.T) {
	t.Parallel()

	const pathOff = 1000

	s, buf := newTestState()
	hostDir := setupWritableMount(t, s, buf)

	// --- Directory cases ---

	// Create a directory.
	copy(buf[pathOff:], "dir")
	if errno := s.Xpath_create_directory(dirfd, pathOff, 3); errno != wasiESuccess {
		t.Fatalf("Xpath_create_directory(dir) = %d, want ESUCCESS", errno)
	}

	// Unlinking a directory (no trailing slash) must fail.
	copy(buf[pathOff:], "dir")
	errno := s.Xpath_unlink_file(dirfd, pathOff, 3)
	if errno != wasiEIsdir && errno != wasiEPerm && errno != wasiEAcces {
		t.Fatalf("Xpath_unlink_file(dir) = %d; want one of EISDIR (%d), EPERM (%d), EACCES (%d)",
			errno, wasiEIsdir, wasiEPerm, wasiEAcces)
	}

	// Unlinking a directory with a trailing slash must also fail.
	copy(buf[pathOff:], "dir/")
	errno = s.Xpath_unlink_file(dirfd, pathOff, 4)
	if errno != wasiEIsdir && errno != wasiEPerm && errno != wasiEAcces {
		t.Fatalf("Xpath_unlink_file(dir/) = %d; want one of EISDIR (%d), EPERM (%d), EACCES (%d)",
			errno, wasiEIsdir, wasiEPerm, wasiEAcces)
	}

	// Directory must still exist.
	if _, err := os.Stat(filepath.Join(hostDir, "dir")); err != nil {
		t.Fatalf("directory was removed unexpectedly: %v", err)
	}

	// Clean up the directory properly.
	copy(buf[pathOff:], "dir")
	if rmErrno := s.Xpath_remove_directory(dirfd, pathOff, 3); rmErrno != wasiESuccess {
		t.Fatalf("Xpath_remove_directory(dir) = %d, want ESUCCESS", rmErrno)
	}

	// --- File cases ---

	// Create a file directly on the host.
	if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Unlinking a file with a trailing slash must fail with ENOTDIR or ENOENT.
	copy(buf[pathOff:], "file/")
	errno = s.Xpath_unlink_file(dirfd, pathOff, 5)
	if errno != wasiENotDir && errno != wasiENoEnt {
		t.Fatalf("Xpath_unlink_file(file/) = %d; want ENOTDIR (%d) or ENOENT (%d)",
			errno, wasiENotDir, wasiENoEnt)
	}

	// The file must NOT have been removed.
	if _, err := os.Stat(filepath.Join(hostDir, "file")); err != nil {
		t.Fatalf("file was removed by Xpath_unlink_file(file/): %v", err)
	}

	// Unlinking the file without a trailing slash must succeed.
	copy(buf[pathOff:], "file")
	errno = s.Xpath_unlink_file(dirfd, pathOff, 4)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_unlink_file(file) = %d, want ESUCCESS", errno)
	}

	// The file must now be gone.
	if _, err := os.Stat(filepath.Join(hostDir, "file")); !os.IsNotExist(err) {
		t.Fatalf("file still exists after Xpath_unlink_file(file): %v", err)
	}
}
