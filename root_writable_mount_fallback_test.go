package wasihost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootWritableMountFallback(t *testing.T) {
	const (
		pathOff1 = 100
		pathOff2 = 200
		bufOff   = 300
		nreadOff = 400
	)

	// We use WithWritableMount("/", hostRoot, os.DirFS(hostRoot))
	// where hostRoot is a temporary directory.
	// When we call a mutation with a relative path "rel", it should:
	// 1. Resolve to "/" mount with rel="rel".
	// 2. resolvePrimary should return Join(hostRoot, "rel").
	// Currently, resolvePrimary calls mountHostPaths, which for root mount
	// returns "/" + rel as primary. This test asserts it uses hostRoot/rel.

	setup := func(t *testing.T) (*State, []byte, string) {
		hostRoot := t.TempDir()
		s, buf := newTestState()
		// Root mount at /
		s.mounts = []mountEntry{{guestPath: "/", writable: true, hostRoot: hostRoot, root: os.DirFS(hostRoot)}}
		// FD 3 is the preopen for "/"
		s.preopens = []fdEntry{{path: "/", fdType: 3, mount: 0, preopen: true}}
		for len(s.fds) < 4 {
			s.fds = append(s.fds, fdEntry{})
		}
		s.fds[3] = fdEntry{path: "/", fdType: 3, mount: 0, preopen: true}
		return s, buf, hostRoot
	}

	t.Run("path_create_directory uses hostRoot fallback", func(t *testing.T) {
		s, buf, hostRoot := setup(t)
		nameOff, nameLen := writePath(buf, pathOff1, "newdir")
		
		// If it attempts to create "/newdir" (root of host OS), it will likely fail with EACCES or create in real root.
		// We want it to create hostRoot/newdir.
		errno := s.Xpath_create_directory(3, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("create_directory returned %d, want ESUCCESS", errno)
		}
		
		target := filepath.Join(hostRoot, "newdir")
		if _, err := os.Stat(target); err != nil {
			t.Errorf("directory not created at %s: %v", target, err)
		}
	})

	t.Run("path_remove_directory uses hostRoot fallback", func(t *testing.T) {
		s, buf, hostRoot := setup(t)
		target := filepath.Join(hostRoot, "remdir")
		if err := os.Mkdir(target, 0755); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "remdir")
		
		errno := s.Xpath_remove_directory(3, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("remove_directory returned %d, want ESUCCESS", errno)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("directory still exists at %s", target)
		}
	})

	t.Run("path_unlink_file uses hostRoot fallback", func(t *testing.T) {
		s, buf, hostRoot := setup(t)
		target := filepath.Join(hostRoot, "file.txt")
		if err := os.WriteFile(target, []byte("hi"), 0644); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "file.txt")
		
		errno := s.Xpath_unlink_file(3, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("unlink_file returned %d, want ESUCCESS", errno)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("file still exists at %s", target)
		}
	})

	t.Run("path_readlink uses hostRoot fallback", func(t *testing.T) {
		s, buf, hostRoot := setup(t)
		target := filepath.Join(hostRoot, "link")
		if err := os.Symlink("target", target); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "link")
		
		errno := s.Xpath_readlink(3, nameOff, nameLen, bufOff, 100, nreadOff)
		if errno != wasiESuccess {
			t.Fatalf("readlink returned %d, want ESUCCESS", errno)
		}
	})

	t.Run("path_symlink uses hostRoot fallback", func(t *testing.T) {
		s, buf, hostRoot := setup(t)
		nameOff, nameLen := writePath(buf, pathOff1, "newsym")
		targetOff, targetLen := writePath(buf, pathOff2, "target")
		
		errno := s.Xpath_symlink(targetOff, targetLen, 3, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("symlink returned %d, want ESUCCESS", errno)
		}
		if _, err := os.Readlink(filepath.Join(hostRoot, "newsym")); err != nil {
			t.Errorf("symlink not created in hostRoot: %v", err)
		}
	})

	t.Run("path_link uses hostRoot fallback", func(t *testing.T) {
		s, buf, hostRoot := setup(t)
		oldPath := filepath.Join(hostRoot, "old")
		if err := os.WriteFile(oldPath, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		oldOff, oldLen := writePath(buf, pathOff1, "old")
		newOff, newLen := writePath(buf, pathOff2, "newlink")
		
		errno := s.Xpath_link(3, 0, oldOff, oldLen, 3, newOff, newLen)
		if errno != wasiESuccess {
			t.Fatalf("link returned %d, want ESUCCESS", errno)
		}
		if _, err := os.Stat(filepath.Join(hostRoot, "newlink")); err != nil {
			t.Errorf("hardlink not created in hostRoot: %v", err)
		}
	})

	t.Run("invalid FD returns EBADF", func(t *testing.T) {
		s, buf, _ := setup(t)
		nameOff, nameLen := writePath(buf, pathOff1, "foo")
		// Xpath_create_directory is currently not checking for fd presence before using it
		// resulting in 66 (ENOTDIR or others) or other errnos depending on internal state.
		errno := s.Xpath_create_directory(99, nameOff, nameLen)
		if errno != wasiEBadf {
			t.Errorf("expected EBADF for invalid fd, got %d", errno)
		}
	})

	t.Run("FFI escape via .. uses hostRoot fallback", func(t *testing.T) {
		// Verify that ".." can escape hostRoot if the host OS allows it, 
		// because we resolve it relative to hostRoot.
		s, buf, hostRoot := setup(t)
		
		// Create a directory outside hostRoot
		parentDir := filepath.Dir(hostRoot)
		outsideDirName := "outside_" + filepath.Base(hostRoot)
		outsidePath := filepath.Join(parentDir, outsideDirName)
		t.Cleanup(func() { os.RemoveAll(outsidePath) })

		// Path: "../" + outsideDirName
		relPath := filepath.Join("..", outsideDirName)
		nameOff, nameLen := writePath(buf, pathOff1, relPath)
		
		errno := s.Xpath_create_directory(3, nameOff, nameLen)
		// This should fail in RED because it currently resolves to /../outside_... 
		// which is /outside_... on host root, likely failing with EACCES.
		// After fix, it should resolve to hostRoot/../outside_... which works.
		if errno != wasiESuccess {
			t.Fatalf("create_directory with .. returned %d, want ESUCCESS", errno)
		}
		
		if _, err := os.Stat(outsidePath); err != nil {
			t.Errorf("directory not created outside hostRoot: %v", err)
		}
	})
}
