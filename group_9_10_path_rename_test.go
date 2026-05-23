package wasihost

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGroup9_10PathRename(t *testing.T) {
	const (
		pathOff1 = 100
		pathOff2 = 200
	)

	t.Run("moving a directory: source gone, target openable", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create a source directory
		srcDir := filepath.Join(hostDir, "src_dir")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create a target directory path
		srcOff, srcLen := writePath(buf, pathOff1, "src_dir")
		targetOff, targetLen := writePath(buf, pathOff2, "target_dir")

		// Xpath_rename should move the directory
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_rename(src, target) = %d, want ESUCCESS", errno)
		}

		// Verify source directory is gone
		if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
			t.Errorf("Source directory still exists after rename")
		}

		// Verify target directory exists and is openable
		targetPath := filepath.Join(hostDir, "target_dir")
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			t.Errorf("Target directory does not exist after rename")
		}

		// Cleanup
		if err := os.RemoveAll(targetPath); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("moving a file: source gone, target openable", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create a source file
		srcFile := filepath.Join(hostDir, "src_file")
		if err := os.WriteFile(srcFile, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create a target file path
		srcOff, srcLen := writePath(buf, pathOff1, "src_file")
		targetOff, targetLen := writePath(buf, pathOff2, "target_file")

		// Xpath_rename should move the file
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_rename(src, target) = %d, want ESUCCESS", errno)
		}

		// Verify source file is gone
		if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
			t.Errorf("Source file still exists after rename")
		}

		// Verify target file exists and is openable
		targetPath := filepath.Join(hostDir, "target_file")
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			t.Errorf("Target file does not exist after rename")
		}

		// Verify we can read the file content
		content, err := os.ReadFile(targetPath)
		if err != nil {
			t.Errorf("Cannot read target file: %v", err)
		} else if string(content) != "data" {
			t.Errorf("Target file content mismatch")
		}

		// Cleanup
		if err := os.Remove(targetPath); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("atomic replace: existing target replaced", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create source file
		srcFile := filepath.Join(hostDir, "src")
		if err := os.WriteFile(srcFile, []byte("src_data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create target file (will be replaced)
		targetFile := filepath.Join(hostDir, "target")
		if err := os.WriteFile(targetFile, []byte("old_data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "src")
		targetOff, targetLen := writePath(buf, pathOff2, "target")

		// Xpath_rename should replace the target file atomically
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_rename(src, target) = %d, want ESUCCESS", errno)
		}

		// Verify source file is gone
		if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
			t.Errorf("Source file still exists after rename")
		}

		// Verify only target exists (not both)
		if _, err := os.Stat(srcFile); os.IsNotExist(err) {
			// Source is gone, good
		} else {
			t.Errorf("Source file should not exist")
		}
		if _, err := os.Stat(targetFile); os.IsNotExist(err) {
			t.Errorf("Target file does not exist after rename")
		}

		// Verify we can read the file content (should be src_data)
		content, err := os.ReadFile(targetFile)
		if err != nil {
			t.Errorf("Cannot read target file: %v", err)
		} else if string(content) != "src_data" {
			t.Errorf("Target file content should be from source, got: %s", string(content))
		}

		// Cleanup
		if err := os.Remove(targetFile); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("empty directory rename: OS-specific behavior", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create empty source directory
		srcDir := filepath.Join(hostDir, "empty_src")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create empty target directory (existing)
		targetDir := filepath.Join(hostDir, "empty_target")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "empty_src")
		targetOff, targetLen := writePath(buf, pathOff2, "empty_target")

		// Xpath_rename should behave differently based on OS
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)

		if runtime.GOOS == "windows" {
			// On Windows, renaming a directory over an existing directory should fail
			if errno == wasiESuccess {
				t.Errorf("Xpath_rename on empty directory (over existing) should fail on Windows, got ESUCCESS")
			} else {
				// Cleanup would be tricky because we might not have permission; skip cleanup if error
				return
			}
		} else {
			// On darwin/linux, renaming a directory over an existing directory should succeed
			if errno != wasiESuccess {
				t.Fatalf("Xpath_rename on empty directory (over existing) should succeed on darwin/linux, got %d", errno)
			}

			// Verify source directory is gone
			if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
				t.Errorf("Source directory still exists after rename")
			}

			// Verify target directory exists
			if _, err := os.Stat(targetDir); os.IsNotExist(err) {
				t.Errorf("Target directory does not exist after rename")
			}
		}

		// Cleanup only if successful (or on non-Windows)
		if runtime.GOOS != "windows" || errno == wasiESuccess {
			if err := os.RemoveAll(targetDir); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("source directory, target directory with file inside: should return ENOTEMPTY or EACCES", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create source directory
		srcDir := filepath.Join(hostDir, "src_dir")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create target directory with a file inside
		targetDir := filepath.Join(hostDir, "target_dir")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}
		targetFile := filepath.Join(targetDir, "file_inside")
		if err := os.WriteFile(targetFile, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "src_dir")
		targetOff, targetLen := writePath(buf, pathOff2, "target_dir")

		// Xpath_rename should fail because target directory is not empty
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)

		if runtime.GOOS == "windows" {
			// On Windows, renaming a directory over a non-empty directory should fail with EACCES
			if errno == wasiESuccess {
				t.Errorf("Xpath_rename on non-empty directory should fail on Windows, got ESUCCESS")
			}
		} else {
			// On Unix, should return ENOTEMPTY
			if errno != wasiENotEmpty {
				t.Fatalf("Xpath_rename on non-empty directory should return ENOTEMPTY on Unix, got %d", errno)
			}
		}
	})

	t.Run("source file, target directory: should return EISDIR or EACCES", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create source file
		srcFile := filepath.Join(hostDir, "src_file")
		if err := os.WriteFile(srcFile, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create target directory
		targetDir := filepath.Join(hostDir, "target_dir")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "src_file")
		targetOff, targetLen := writePath(buf, pathOff2, "target_dir")

		// Xpath_rename should fail because target is a directory, not a file
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)

		if runtime.GOOS == "windows" {
			// On Windows, renaming a file into a directory should fail with EACCES
			if errno == wasiESuccess {
				t.Errorf("Xpath_rename(file into dir) should fail on Windows, got ESUCCESS")
			}
		} else {
			// On Unix, should return EISDIR
			if errno != wasiEIsdir {
				t.Fatalf("Xpath_rename(file into dir) should return EISDIR on Unix, got %d", errno)
			}
		}
	})

	t.Run("source directory to target/file returns ENOTDIR on unix", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("path_rename.rs ENOTDIR case is unix-only in this plan")
		}
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		srcDir := filepath.Join(hostDir, "source")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}
		targetDir := filepath.Join(hostDir, "target")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, "file_inside"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		srcOff, srcLen := writePath(buf, pathOff1, "source")
		newOff, newLen := writePath(buf, pathOff2, "target/file_inside")
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, newOff, newLen)
		if errno != wasiENotDir {
			t.Fatalf("Xpath_rename(source, target/file_inside) = %d, want ENOTDIR (%d)", errno, wasiENotDir)
		}
	})

}
