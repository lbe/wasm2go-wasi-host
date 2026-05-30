package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

const dirfd = int32(3)

func setupMountAtDirfd(t *testing.T, s *State, guestPath, hostDir string, writable bool, rightsBase, rightsInheriting uint64) {
	t.Helper()
	fe, ok := s.lookupFD(3)
	if !ok || fe == nil {
		t.Fatal("preopen fd 3 not found")
	}
	fe.RightsBase = rightsBase
	fe.RightsInheriting = rightsInheriting
	fe.Name = guestPath
	fe.GuestPath = guestPath
	if writable {
		fe.FS = sysfs.DirFS{Path: hostDir}
	}
}

func newWritableMountState(t *testing.T) (*State, []byte, string) {
	t.Helper()
	dir := t.TempDir()
	s, buf := newTestState(NewModuleConfig().WithHostDirectoryPreopen("tmp", dir))
	setupMountAtDirfd(t, s, "tmp", dir, true, rightsWritableDirPreopenBase, rightsWritableDirPreopenInheriting)
	return s, buf, dir
}

func newReadOnlyMountState(t *testing.T, dir string) (*State, []byte) {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	s, buf := newTestState(NewModuleConfig().WithReadOnlyFS("tmp", os.DirFS(dir)))
	setupMountAtDirfd(t, s, "tmp", dir, false, rightsReadOnlyDirPreopen, rightsReadOnlyDirPreopen)
	return s, buf
}

// writePath writes a path string into the guest buffer at offset off. Returns (off, len).
func writePath(buf []byte, off int32, p string) (int32, int32) {
	copy(buf[off:], p)
	return off, int32(len(p))
}

func TestGroupBFilesystemMutations(t *testing.T) {
	const (
		pathOff1 = 100
		pathOff2 = 200
		bufOff   = 300
		nreadOff = 400
	)

	t.Run("Xpath_create_directory creates dir", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		nameOff, nameLen := writePath(buf, pathOff1, "newdir")
		errno := s.Xpath_create_directory(dirfd, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("create_directory returned %d, want ESUCCESS", errno)
		}
		if _, err := os.Stat(filepath.Join(hostDir, "newdir")); err != nil {
			t.Errorf("directory not created: %v", err)
		}
	})

	t.Run("Xpath_create_directory second call returns EEXIST", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		if err := os.Mkdir(filepath.Join(hostDir, "existing"), 0755); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "existing")
		errno := s.Xpath_create_directory(dirfd, nameOff, nameLen)
		if errno != wasiEExist {
			t.Errorf("got errno %d, want EEXIST (%d)", errno, wasiEExist)
		}
	})

	t.Run("Xpath_remove_directory removes empty dir", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		if err := os.Mkdir(filepath.Join(hostDir, "emptydir"), 0755); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "emptydir")
		errno := s.Xpath_remove_directory(dirfd, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("remove_directory returned %d, want ESUCCESS", errno)
		}
		if _, err := os.Stat(filepath.Join(hostDir, "emptydir")); !os.IsNotExist(err) {
			t.Errorf("directory still exists after remove")
		}
	})

	t.Run("Xpath_remove_directory on non-empty dir returns ENOTEMPTY", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		if err := os.MkdirAll(filepath.Join(hostDir, "nonempty/child"), 0755); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "nonempty")
		errno := s.Xpath_remove_directory(dirfd, nameOff, nameLen)
		if errno != wasiENotEmpty {
			t.Errorf("got errno %d, want ENOTEMPTY (%d)", errno, wasiENotEmpty)
		}
	})

	t.Run("Xpath_remove_directory on file returns ENOTDIR", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		if err := os.WriteFile(filepath.Join(hostDir, "afile"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "afile")
		errno := s.Xpath_remove_directory(dirfd, nameOff, nameLen)
		if errno != wasiENotDir {
			t.Errorf("got errno %d, want ENOTDIR (%d)", errno, wasiENotDir)
		}
	})

	t.Run("Xpath_unlink_file removes a file", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		if err := os.WriteFile(filepath.Join(hostDir, "todelete"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "todelete")
		errno := s.Xpath_unlink_file(dirfd, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("unlink_file returned %d, want ESUCCESS", errno)
		}
		if _, err := os.Stat(filepath.Join(hostDir, "todelete")); !os.IsNotExist(err) {
			t.Errorf("file still exists after unlink")
		}
	})

	t.Run("Xpath_unlink_file second call returns ENOENT", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		_ = hostDir
		nameOff, nameLen := writePath(buf, pathOff1, "nonexistent")
		errno := s.Xpath_unlink_file(dirfd, nameOff, nameLen)
		if errno != wasiENoEnt {
			t.Errorf("got errno %d, want ENOENT (%d)", errno, wasiENoEnt)
		}
	})

	t.Run("Xpath_unlink_file on directory returns EISDIR", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		if err := os.Mkdir(filepath.Join(hostDir, "adir"), 0755); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "adir")
		errno := s.Xpath_unlink_file(dirfd, nameOff, nameLen)
		if errno != wasiEIsdir {
			t.Errorf("got errno %d, want EISDIR (%d)", errno, wasiEIsdir)
		}
	})

	t.Run("Xpath_readlink returns target and nread", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		// Create a symlink on the host
		if err := os.Symlink("/target/path", filepath.Join(hostDir, "mylink")); err != nil {
			t.Fatal(err)
		}
		nameOff, nameLen := writePath(buf, pathOff1, "mylink")
		errno := s.Xpath_readlink(dirfd, nameOff, nameLen, bufOff, 256, nreadOff)
		if errno != wasiESuccess {
			t.Fatalf("readlink returned %d, want ESUCCESS", errno)
		}
		nread := binary.LittleEndian.Uint32(buf[nreadOff : nreadOff+4])
		if nread != uint32(len("/target/path")) {
			t.Errorf("nread = %d, want %d", nread, len("/target/path"))
		}
		got := string(buf[bufOff : bufOff+int(nread)])
		if got != "/target/path" {
			t.Errorf("got %q, want %q", got, "/target/path")
		}
	})

	t.Run("Xpath_symlink creates a symlink", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		targetOff, targetLen := writePath(buf, pathOff1, "/some/target")
		nameOff, nameLen := writePath(buf, pathOff2, "newsym")
		errno := s.Xpath_symlink(targetOff, targetLen, dirfd, nameOff, nameLen)
		if errno != wasiESuccess {
			t.Fatalf("symlink returned %d, want ESUCCESS", errno)
		}
		got, err := os.Readlink(filepath.Join(hostDir, "newsym"))
		if err != nil {
			t.Errorf("os.Readlink failed: %v", err)
		} else if got != "/some/target" {
			t.Errorf("symlink target = %q, want %q", got, "/some/target")
		}
	})

	t.Run("Xpath_link creates a hard link", func(t *testing.T) {
		s, buf, hostDir := newWritableMountState(t)
		origPath := filepath.Join(hostDir, "original")
		if err := os.WriteFile(origPath, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		origOff, origLen := writePath(buf, pathOff1, "original")
		linkOff, linkLen := writePath(buf, pathOff2, "hardlink")
		errno := s.Xpath_link(dirfd, 0, origOff, origLen, dirfd, linkOff, linkLen)
		if errno != wasiESuccess {
			t.Fatalf("link returned %d, want ESUCCESS", errno)
		}
		origInfo, err1 := os.Stat(origPath)
		linkInfo, err2 := os.Stat(filepath.Join(hostDir, "hardlink"))
		if err1 != nil || err2 != nil {
			t.Fatalf("stat failed: %v, %v", err1, err2)
		}
		if !os.SameFile(origInfo, linkInfo) {
			t.Error("hardlink is not the same file as original")
		}
	})

	t.Run("all six functions return EROFS on read-only mount", func(t *testing.T) {
		s, buf := newReadOnlyMountState(t, t.TempDir())

		pathOff, pathLen := writePath(buf, pathOff1, "name")
		path2Off, path2Len := writePath(buf, pathOff2, "name2")

		cases := []struct {
			name  string
			errno int32
		}{
			{"mkdir", s.Xpath_create_directory(dirfd, pathOff, pathLen)},
			{"rmdir", s.Xpath_remove_directory(dirfd, pathOff, pathLen)},
			{"unlink", s.Xpath_unlink_file(dirfd, pathOff, pathLen)},
			{"readlink", s.Xpath_readlink(dirfd, pathOff, pathLen, bufOff, 256, nreadOff)},
			{"symlink", s.Xpath_symlink(pathOff, pathLen, dirfd, path2Off, path2Len)},
			{"link", s.Xpath_link(dirfd, 0, pathOff, pathLen, dirfd, path2Off, path2Len)},
		}
		for _, c := range cases {
			if c.errno != wasiEROFS {
				t.Errorf("%s on read-only mount: got errno %d, want EROFS (%d)", c.name, c.errno, wasiEROFS)
			}
		}
	})
}
