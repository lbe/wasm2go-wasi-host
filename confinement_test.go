package wasihost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostDirectoryPreopenConfinement(t *testing.T) {
	// Preopened directory access is confined to the configured host root.
	// For a host directory preopen rooted at a temp directory, operations
	// using absolute guest paths or paths that escape with .. fail with a
	// WASI capability error and do not create, open, rename, or remove
	// files outside the host root.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}

	// Secret file outside the root that we shouldn't be able to touch.
	secretFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("top secret"), 0644); err != nil {
		t.Fatal(err)
	}

	guestPath := "/data"
	buf := make([]byte, 1024)
	mem := func() []byte { return buf }

	s := New(mem, WithHostDirectoryPreopen(guestPath, root))

	// WASI error for capability violations (ENOTCAPABLE = 76).
	const wasiENotCap int32 = 76

	t.Run("path_open cannot escape with ..", func(t *testing.T) {
		const fdPtr = 100
		const pathPtr = 200
		path := "../secret.txt"
		copy(buf[pathPtr:], path)

		// fd 3 is the preopen for /data -> root
		errno := s.Xpath_open(3, 0, pathPtr, int32(len(path)), 0, int64(rightsRegular), 0, 0, fdPtr)

		if errno != wasiENotCap {
			t.Errorf("path_open(%q) errno = %d, want %d (ENOTCAPABLE)", path, errno, wasiENotCap)
		}
	})

	t.Run("path_open cannot use absolute guest path to escape", func(t *testing.T) {
		const fdPtr = 100
		const pathPtr = 200
		// An absolute guest path that resolves through a preopen but then escapes
		// via .. must be rejected with a capability error.
		path := "/data/../secret.txt"
		copy(buf[pathPtr:], path)

		errno := s.Xpath_open(3, 0, pathPtr, int32(len(path)), 0, int64(rightsRegular), 0, 0, fdPtr)

		if errno != wasiENotCap {
			t.Errorf("path_open(%q) errno = %d, want %d (ENOTCAPABLE)", path, errno, wasiENotCap)
		}

		// Verify side effect: secret file still exists and is readable
		content, err := os.ReadFile(secretFile)
		if err != nil {
			t.Errorf("secret.txt is no longer readable: %v", err)
		} else if string(content) != "top secret" {
			t.Errorf("secret.txt content changed: %q", string(content))
		}
	})

	t.Run("path_create_directory cannot escape", func(t *testing.T) {
		const pathPtr = 200
		path := "../evil-dir"
		copy(buf[pathPtr:], path)

		errno := s.Xpath_create_directory(3, pathPtr, int32(len(path)))

		if errno != wasiENotCap {
			t.Errorf("path_create_directory(%q) errno = %d, want %d (ENOTCAPABLE)", path, errno, wasiENotCap)
		}

		// Verify side effect: directory was NOT created
		if _, err := os.Stat(filepath.Join(tmpDir, "evil-dir")); !os.IsNotExist(err) {
			t.Error("evil-dir was created outside host root!")
		}
	})

	t.Run("path_unlink_file cannot escape", func(t *testing.T) {
		const pathPtr = 200
		path := "../secret.txt"
		copy(buf[pathPtr:], path)

		errno := s.Xpath_unlink_file(3, pathPtr, int32(len(path)))

		if errno != wasiENotCap {
			t.Errorf("path_unlink_file(%q) errno = %d, want %d (ENOTCAPABLE)", path, errno, wasiENotCap)
		}

		// Verify side effect: file was NOT removed
		if _, err := os.Stat(secretFile); err != nil {
			t.Errorf("secret.txt was removed or is inaccessible: %v", err)
		}
	})
}
