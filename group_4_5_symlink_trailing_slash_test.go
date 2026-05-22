package wasihost

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPathSymlinkTrailingSlashSemantics verifies that path_symlink rejects or
// accepts link paths with trailing slashes according to whether the destination
// exists and its type, matching the behavior the wasi-testsuite expects.
func TestPathSymlinkTrailingSlashSemantics(t *testing.T) {
	t.Parallel()

	const (
		oldPathOff = 900
		pathOff    = 1000
	)

	// --- Dangling symlink cases ---
	t.Run("dangling", func(t *testing.T) {
		t.Parallel()
		if os.Getenv("NO_DANGLING_FILESYSTEM") != "" {
			t.Skip("NO_DANGLING_FILESYSTEM is set")
		}

		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)
		copy(buf[oldPathOff:], "source")

		// "target/" on a non-existent destination must fail with ENOENT.
		copy(buf[pathOff:], "target/")
		errno := s.Xpath_symlink(oldPathOff, 6, dirfd, pathOff, 7)
		if errno != wasiENoEnt {
			t.Fatalf("Xpath_symlink(source, target/) = %d; want ENOENT (%d)", errno, wasiENoEnt)
		}

		// "target" without trailing slash must succeed.
		copy(buf[pathOff:], "target")
		errno = s.Xpath_symlink(oldPathOff, 6, dirfd, pathOff, 6)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_symlink(source, target) = %d, want ESUCCESS", errno)
		}
		got, err := os.Readlink(filepath.Join(hostDir, "target"))
		if err != nil {
			t.Fatalf("os.Readlink(target): %v", err)
		}
		if got != "source" {
			t.Fatalf("symlink target = %q, want %q", got, "source")
		}

		// Clean up.
		copy(buf[pathOff:], "target")
		if rmErrno := s.Xpath_unlink_file(dirfd, pathOff, 6); rmErrno != wasiESuccess {
			t.Fatalf("Xpath_unlink_file(target) = %d, want ESUCCESS", rmErrno)
		}
	})

	// --- Existing directory cases ---
	t.Run("existing directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)
		_ = hostDir
		copy(buf[oldPathOff:], "source")

		copy(buf[pathOff:], "target")
		if mkdirErrno := s.Xpath_create_directory(dirfd, pathOff, 6); mkdirErrno != wasiESuccess {
			t.Fatalf("Xpath_create_directory(target) = %d, want ESUCCESS", mkdirErrno)
		}

		// "target/" when target is an existing directory must fail with EEXIST or ENOENT.
		copy(buf[pathOff:], "target/")
		errno := s.Xpath_symlink(oldPathOff, 6, dirfd, pathOff, 7)
		if errno != wasiEExist && errno != wasiENoEnt {
			t.Fatalf("Xpath_symlink(source, target/) on existing dir = %d; want one of EEXIST (%d), ENOENT (%d)",
				errno, wasiEExist, wasiENoEnt)
		}

		// "target" without trailing slash must fail with EEXIST.
		copy(buf[pathOff:], "target")
		errno = s.Xpath_symlink(oldPathOff, 6, dirfd, pathOff, 6)
		if errno != wasiEExist {
			t.Fatalf("Xpath_symlink(source, target) on existing dir = %d; want EEXIST (%d)", errno, wasiEExist)
		}

		// Clean up.
		copy(buf[pathOff:], "target")
		if rmErrno := s.Xpath_remove_directory(dirfd, pathOff, 6); rmErrno != wasiESuccess {
			t.Fatalf("Xpath_remove_directory(target) = %d, want ESUCCESS", rmErrno)
		}
	})

	// --- Existing file cases ---
	t.Run("existing file", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)
		copy(buf[oldPathOff:], "source")

		if err := os.WriteFile(filepath.Join(hostDir, "target"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}

		// "target/" when target is an existing file must fail with ENOTDIR, ENOENT, or EEXIST.
		copy(buf[pathOff:], "target/")
		errno := s.Xpath_symlink(oldPathOff, 6, dirfd, pathOff, 7)
		if errno != wasiENotDir && errno != wasiENoEnt && errno != wasiEExist {
			t.Fatalf("Xpath_symlink(source, target/) on existing file = %d; want one of ENOTDIR (%d), ENOENT (%d), EEXIST (%d)",
				errno, wasiENotDir, wasiENoEnt, wasiEExist)
		}

		// "target" without trailing slash must fail with EEXIST.
		copy(buf[pathOff:], "target")
		errno = s.Xpath_symlink(oldPathOff, 6, dirfd, pathOff, 6)
		if errno != wasiEExist {
			t.Fatalf("Xpath_symlink(source, target) on existing file = %d; want EEXIST (%d)", errno, wasiEExist)
		}

		// Clean up.
		copy(buf[pathOff:], "target")
		if rmErrno := s.Xpath_unlink_file(dirfd, pathOff, 6); rmErrno != wasiESuccess {
			t.Fatalf("Xpath_unlink_file(target) = %d, want ESUCCESS", rmErrno)
		}
	})
}
