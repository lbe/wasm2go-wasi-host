package wasihost

import (
	"os"
	"path/filepath"
	"testing"
)

// openRootDirFD opens "." on the root preopen at fd 3 and returns the
// resulting non-preopen directory fd. This mirrors what the new
// wasi-testsuite Rust helper root_directory() does.
func openRootDirFD(t *testing.T, s *State, buf []byte) int32 {
	t.Helper()
	const pathPtr = 100
	var fdPtr int32 = 200
	copy(buf[pathPtr:], ".")
	errno := s.Xpath_open(3, 0, pathPtr, 1, int32(oflagDir),
		int64(rightsWritableDirPreopenBase),
		int64(rightsWritableDirPreopenInheriting),
		0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(.) on root preopen returned %d, want ESUCCESS", errno)
	}
	return int32(buf[fdPtr]) | int32(buf[fdPtr+1])<<8 |
		int32(buf[fdPtr+2])<<16 | int32(buf[fdPtr+3])<<24
}

func newRootMountState(t *testing.T) (*State, []byte, string) {
	t.Helper()
	dir := t.TempDir()
	s, buf := newTestState(NewModuleConfig().WithHostDirectoryPreopen("/", dir))
	setupMountAtDirfd(t, s, "/", dir, true, rightsWritableDirPreopenBase, rightsWritableDirPreopenInheriting)
	return s, buf, dir
}

func TestRootPreopenDirfdPathCreateDirectory(t *testing.T) {
	s, buf, hostDir := newRootMountState(t)
	opened := openRootDirFD(t, s, buf)

	const namePtr = 300
	copy(buf[namePtr:], "newdir")
	errno := s.Xpath_create_directory(opened, namePtr, 6)
	if errno != wasiESuccess {
		t.Fatalf("path_create_directory through root-opened dirfd returned %d, want ESUCCESS", errno)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "newdir")); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestRootPreopenDirfdPathOpen(t *testing.T) {
	s, buf, hostDir := newRootMountState(t)
	opened := openRootDirFD(t, s, buf)

	const namePtr = 300
	var fdPtr int32 = 400
	copy(buf[namePtr:], "newfile")
	errno := s.Xpath_open(opened, 0, namePtr, 7, int32(oflagCreat),
		int64(rightFDRead|rightFDWrite),
		0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open through root-opened dirfd returned %d, want ESUCCESS", errno)
	}
	openedFd := int32(buf[fdPtr]) | int32(buf[fdPtr+1])<<8 |
		int32(buf[fdPtr+2])<<16 | int32(buf[fdPtr+3])<<24
	if openedFd <= 3 {
		t.Errorf("opened fd = %d, want > 3", openedFd)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "newfile")); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestRootPreopenDirfdPathUnlinkFile(t *testing.T) {
	s, buf, hostDir := newRootMountState(t)
	f := filepath.Join(hostDir, "delfile")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	opened := openRootDirFD(t, s, buf)
	const namePtr = 300
	copy(buf[namePtr:], "delfile")
	errno := s.Xpath_unlink_file(opened, namePtr, 7)
	if errno != wasiESuccess {
		t.Fatalf("path_unlink_file through root-opened dirfd returned %d, want ESUCCESS", errno)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("file still exists or unexpected error: %v", err)
	}
}

func TestRootPreopenDirfdPathReadlink(t *testing.T) {
	s, buf, hostDir := newRootMountState(t)
	if err := os.Symlink("target", filepath.Join(hostDir, "link")); err != nil {
		t.Fatal(err)
	}

	opened := openRootDirFD(t, s, buf)
	const namePtr = 300
	const bufPtr = 400
	const nreadPtr = 500
	copy(buf[namePtr:], "link")
	errno := s.Xpath_readlink(opened, namePtr, 4, bufPtr, 100, nreadPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_readlink through root-opened dirfd returned %d, want ESUCCESS", errno)
	}
	nread := int32(buf[nreadPtr]) | int32(buf[nreadPtr+1])<<8 |
		int32(buf[nreadPtr+2])<<16 | int32(buf[nreadPtr+3])<<24
	if nread != 6 {
		t.Errorf("readlink nread = %d, want 6", nread)
	}
	if string(buf[bufPtr:bufPtr+6]) != "target" {
		t.Errorf("readlink target = %q, want %q", string(buf[bufPtr:bufPtr+6]), "target")
	}
}
