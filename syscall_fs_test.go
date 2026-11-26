package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
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

func TestFilesystemMutationSyscalls(t *testing.T) {
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
		} else if got = filepath.ToSlash(got); got != "/some/target" {
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

// TestReadonlyPathReadlink verifies path_readlink on a read-only ReadFS(DirFS)
// preopen returns the host symlink target with ESUCCESS, not EROFS.
func TestReadonlyPathReadlink(t *testing.T) {
	s, buf := newTestState(nil)
	dir := t.TempDir()

	const wantTarget = "/target/path"
	if err := os.Symlink(wantTarget, filepath.Join(dir, "mylink")); err != nil {
		t.Fatal(err)
	}

	fsys := &sysfs.ReadFS{FS: sysfs.DirFS{Path: dir}}
	wf, openErrno := fsys.OpenFile(".", wasi.O_RDONLY|wasi.O_DIRECTORY, 0)
	if openErrno != 0 {
		t.Fatalf("OpenFile preopen: errno %d", openErrno)
	}
	t.Cleanup(func() { _ = wf.Close() }) // before t.TempDir removal (Windows)
	insertTestFileAt(s, 3, &wasi.FileEntry{
		FS:               fsys,
		Name:             "ro",
		GuestPath:        "ro",
		IsPreopen:        true,
		File:             wf,
		RightsBase:       rightsReadOnlyDirPreopen,
		RightsInheriting: rightsReadOnlyDirPreopen,
	})

	const (
		dirfd    = int32(3)
		pathOff  = 100
		bufOff   = 300
		nreadOff = 400
	)
	pathLen := int32(len("mylink"))
	copy(buf[pathOff:], "mylink")

	rc := s.Xpath_readlink(dirfd, pathOff, pathLen, bufOff, 256, nreadOff)
	if rc != wasiESuccess {
		t.Fatalf("Xpath_readlink = %d, want ESUCCESS (not EROFS on read-only DirFS)", rc)
	}
	nread := binary.LittleEndian.Uint32(buf[nreadOff : nreadOff+4])
	if nread != uint32(len(wantTarget)) {
		t.Errorf("nread = %d, want %d", nread, len(wantTarget))
	}
	got := string(buf[bufOff : bufOff+int(nread)])
	if got != wantTarget {
		t.Errorf("readlink target = %q, want %q", got, wantTarget)
	}
}

func TestPathMutationSyscallsConfinement(t *testing.T) {
	// Cycle 5: Path mutation syscalls mutate only the writable host directory preopen root.
	// Acceptance criteria: path_create_directory, path_remove_directory, path_unlink_file,
	// path_rename, path_link, path_symlink, and path_readlink operate on paths beneath
	// the preopen root, return mapped WASI errnos for host failures, and never use
	// root fallback or parent escape behavior.

	tmpDir := t.TempDir()
	preopenDir := filepath.Join(tmpDir, "preopen")
	if err := os.Mkdir(preopenDir, 0755); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1024)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))

	const (
		dirfd    = int32(3)
		pathOff1 = 100
		pathOff2 = 200
		bufOff   = 300
		nreadOff = 400
	)

	writePath := func(off int32, p string) (int32, int32) {
		copy(buf[off:], p)
		return off, int32(len(p))
	}

	t.Run("path_create_directory is confined", func(t *testing.T) {
		// Attempting to create a directory outside preopen via .. escape should fail
		off, len := writePath(pathOff1, "../escaped_dir")
		errno := s.Xpath_create_directory(dirfd, off, len)
		if errno != wasiENotCap {
			t.Errorf("path_create_directory(..) = %d, want ENOTCAP (%d)", errno, wasiENotCap)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "escaped_dir")); err == nil {
			t.Errorf("directory created outside preopen root")
		}
	})

	t.Run("path_remove_directory is confined", func(t *testing.T) {
		outsideDir := filepath.Join(tmpDir, "outside_to_remove")
		if err := os.Mkdir(outsideDir, 0755); err != nil {
			t.Fatal(err)
		}
		off, len := writePath(pathOff1, "../outside_to_remove")
		errno := s.Xpath_remove_directory(dirfd, off, len)
		if errno != wasiENotCap {
			t.Errorf("path_remove_directory(..) = %d, want ENOTCAP (%d)", errno, wasiENotCap)
		}
		if _, err := os.Stat(outsideDir); err != nil {
			t.Errorf("outside directory was removed")
		}
	})

	t.Run("path_unlink_file is confined", func(t *testing.T) {
		outsideFile := filepath.Join(tmpDir, "outside_file")
		if err := os.WriteFile(outsideFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
		off, len := writePath(pathOff1, "../outside_file")
		errno := s.Xpath_unlink_file(dirfd, off, len)
		if errno != wasiENotCap {
			t.Errorf("path_unlink_file(..) = %d, want ENOTCAP (%d)", errno, wasiENotCap)
		}
		if _, err := os.Stat(outsideFile); err != nil {
			t.Errorf("outside file was unlinked")
		}
	})

	t.Run("path_readlink is confined", func(t *testing.T) {
		outsideLink := filepath.Join(tmpDir, "outside_link")
		if err := os.Symlink("/etc/passwd", outsideLink); err != nil {
			t.Fatal(err)
		}
		off, len := writePath(pathOff1, "../outside_link")
		errno := s.Xpath_readlink(dirfd, off, len, bufOff, 256, nreadOff)
		if errno != wasiENotCap {
			t.Errorf("path_readlink(..) = %d, want ENOTCAP (%d)", errno, wasiENotCap)
		}
	})

	t.Run("path_symlink is confined", func(t *testing.T) {
		targetOff, targetLen := writePath(pathOff1, "/etc/passwd")
		linkOff, linkLen := writePath(pathOff2, "../outside_new_link")
		errno := s.Xpath_symlink(targetOff, targetLen, dirfd, linkOff, linkLen)
		if errno != wasiENotCap {
			t.Errorf("path_symlink(..) = %d, want ENOTCAP (%d)", errno, wasiENotCap)
		}
		if _, err := os.Lstat(filepath.Join(tmpDir, "outside_new_link")); err == nil {
			t.Errorf("outside symlink was created")
		}
	})

	t.Run("path_link is confined", func(t *testing.T) {
		insideFile := filepath.Join(preopenDir, "inside_file")
		if err := os.WriteFile(insideFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		off1, len1 := writePath(pathOff1, "inside_file")
		off2, len2 := writePath(pathOff2, "../outside_hard_link")

		// Hard link from inside to outside should fail
		errno := s.Xpath_link(dirfd, 0, off1, len1, dirfd, off2, len2)
		if errno != wasiENotCap {
			t.Errorf("path_link(..) = %d, want ENOTCAP (%d)", errno, wasiENotCap)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "outside_hard_link")); err == nil {
			t.Errorf("outside hard link was created")
		}
	})

	t.Run("path_rename is confined", func(t *testing.T) {
		insideFile := filepath.Join(preopenDir, "to_rename")
		if err := os.WriteFile(insideFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		off1, len1 := writePath(pathOff1, "to_rename")
		off2, len2 := writePath(pathOff2, "../renamed_outside")

		errno := s.Xpath_rename(dirfd, off1, len1, dirfd, off2, len2)
		if errno != wasiENotCap {
			t.Errorf("path_rename(..) = %d, want ENOTCAP (%d)", errno, wasiENotCap)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "renamed_outside")); err == nil {
			t.Errorf("file was renamed outside preopen")
		}
	})
}

func TestNestedParentSegmentPathsCannotEscapeWritableHostPreopen(t *testing.T) {
	// Writable host directory preopen: nested ".." segments (not only a leading ..)
	// must be rejected with ENOTCAPABLE and must not touch the host filesystem outside
	// the preopen root.
	guestPaths := []struct {
		name string
		p    string
	}{
		{name: "subdir_then_parents", p: "subdir/../../outside"},
		{name: "deep_tree_then_parents", p: "a/b/../../../outside"},
		{name: "absolute_guest_then_parents", p: "/data/subdir/../../outside"},
	}

	for _, gp := range guestPaths {
		t.Run(gp.name, func(t *testing.T) {
			t.Run("path_open", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				outsideFile := filepath.Join(tmpDir, "outside")
				if err := os.WriteFile(outsideFile, []byte("untouched"), 0644); err != nil {
					t.Fatal(err)
				}

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				const fdPtr = 480
				off := int32(100)
				copy(buf[off:], gp.p)
				errno := s.Xpath_open(3, 0, off, int32(len(gp.p)), 0, int64(rightsRegular), 0, 0, fdPtr)
				if errno != wasiENotCap {
					t.Fatalf("path_open(%q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				got, err := os.ReadFile(outsideFile)
				if err != nil {
					t.Fatalf("outside marker: %v", err)
				}
				if string(got) != "untouched" {
					t.Fatalf("outside file was modified: %q", got)
				}
			})

			t.Run("path_create_directory", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				outsideDir := filepath.Join(tmpDir, "outside")

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				off := int32(100)
				copy(buf[off:], gp.p)
				errno := s.Xpath_create_directory(3, off, int32(len(gp.p)))
				if errno != wasiENotCap {
					t.Fatalf("path_create_directory(%q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				if _, err := os.Stat(outsideDir); !os.IsNotExist(err) {
					t.Fatalf("directory was created outside preopen at %s (err=%v)", outsideDir, err)
				}
			})

			t.Run("path_remove_directory", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				outsideDir := filepath.Join(tmpDir, "outside")
				if err := os.Mkdir(outsideDir, 0755); err != nil {
					t.Fatal(err)
				}

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				off := int32(100)
				copy(buf[off:], gp.p)
				errno := s.Xpath_remove_directory(3, off, int32(len(gp.p)))
				if errno != wasiENotCap {
					t.Fatalf("path_remove_directory(%q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				if _, err := os.Stat(outsideDir); err != nil {
					t.Fatalf("outside directory was removed: %v", err)
				}
			})

			t.Run("path_unlink_file", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				outsideFile := filepath.Join(tmpDir, "outside")
				if err := os.WriteFile(outsideFile, []byte("stay"), 0644); err != nil {
					t.Fatal(err)
				}

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				off := int32(100)
				copy(buf[off:], gp.p)
				errno := s.Xpath_unlink_file(3, off, int32(len(gp.p)))
				if errno != wasiENotCap {
					t.Fatalf("path_unlink_file(%q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				if _, err := os.Stat(outsideFile); err != nil {
					t.Fatalf("outside file was removed: %v", err)
				}
			})

			t.Run("path_rename", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				inside := filepath.Join(preopenDir, "rename_src")
				if err := os.WriteFile(inside, []byte("src"), 0644); err != nil {
					t.Fatal(err)
				}
				outsideDest := filepath.Join(tmpDir, "outside")

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				const pathOff1, pathOff2 = 100, 200
				off1, len1 := int32(pathOff1), int32(copy(buf[pathOff1:], "rename_src"))
				off2, len2 := int32(pathOff2), int32(copy(buf[pathOff2:], gp.p))
				errno := s.Xpath_rename(3, off1, len1, 3, off2, len2)
				if errno != wasiENotCap {
					t.Fatalf("path_rename(to %q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				if _, err := os.Stat(inside); err != nil {
					t.Fatalf("source was moved: %v", err)
				}
				if _, err := os.Stat(outsideDest); !os.IsNotExist(err) {
					t.Fatalf("rename created destination outside root: %v", err)
				}
			})

			t.Run("path_link", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				inside := filepath.Join(preopenDir, "link_src")
				if err := os.WriteFile(inside, []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
				outsideLink := filepath.Join(tmpDir, "outside")

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				const pathOff1, pathOff2 = 100, 200
				off1, len1 := int32(pathOff1), int32(copy(buf[pathOff1:], "link_src"))
				off2, len2 := int32(pathOff2), int32(copy(buf[pathOff2:], gp.p))
				errno := s.Xpath_link(3, 0, off1, len1, 3, off2, len2)
				if errno != wasiENotCap {
					t.Fatalf("path_link(to %q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				if _, err := os.Stat(outsideLink); !os.IsNotExist(err) {
					t.Fatalf("hard link was created outside preopen: %v", err)
				}
			})

			t.Run("path_symlink", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				outsideLink := filepath.Join(tmpDir, "outside")

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				const targetOff, linkOff = 100, 200
				tlen := int32(copy(buf[targetOff:], "/etc/passwd"))
				llen := int32(copy(buf[linkOff:], gp.p))
				errno := s.Xpath_symlink(targetOff, tlen, 3, linkOff, llen)
				if errno != wasiENotCap {
					t.Fatalf("path_symlink(at %q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				if _, err := os.Lstat(outsideLink); !os.IsNotExist(err) {
					t.Fatalf("symlink was created outside preopen: %v", err)
				}
			})

			t.Run("path_readlink", func(t *testing.T) {
				tmpDir := t.TempDir()
				preopenDir := filepath.Join(tmpDir, "preopen")
				if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
					t.Fatal(err)
				}
				outsideLink := filepath.Join(tmpDir, "outside")
				if err := os.Symlink("/etc/passwd", outsideLink); err != nil {
					t.Fatal(err)
				}

				buf := make([]byte, 1024)
				s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
				const bufOff, nreadOff = 300, 400
				off := int32(100)
				copy(buf[off:], gp.p)
				errno := s.Xpath_readlink(3, off, int32(len(gp.p)), bufOff, 256, nreadOff)
				if errno != wasiENotCap {
					t.Fatalf("path_readlink(%q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
				}
				if _, err := os.Lstat(outsideLink); err != nil {
					t.Fatalf("outside symlink was removed or is inaccessible: %v", err)
				}
			})
		})
	}
}

func TestNestedParentSegmentPathsPathFilestatCannotEscapeWritableHostPreopen(t *testing.T) {
	// Writable host preopen: path_filestat_get and path_filestat_set_times must reject
	// nested ".." segments lexically (same as path_open and resolveWritable) with
	// ENOTCAPABLE and must not call os.Stat/os.Chtimes on paths outside the preopen root.
	guestPaths := []struct {
		name string
		p    string
	}{
		{name: "subdir_then_parents", p: "subdir/../../outside"},
		{name: "deep_tree_then_parents", p: "a/b/../../../outside"},
	}

	for _, gp := range guestPaths {
		t.Run(gp.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			preopenDir := filepath.Join(tmpDir, "preopen")
			if err := os.MkdirAll(filepath.Join(preopenDir, "subdir"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(preopenDir, "a", "b"), 0755); err != nil {
				t.Fatal(err)
			}
			outsideFile := filepath.Join(tmpDir, "outside")
			if err := os.WriteFile(outsideFile, []byte("marker"), 0644); err != nil {
				t.Fatal(err)
			}
			fi0, err := os.Stat(outsideFile)
			if err != nil {
				t.Fatal(err)
			}
			origMtime := fi0.ModTime()

			buf := make([]byte, 4096)
			s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", preopenDir))
			const (
				dirfd   int32 = 3
				pathOff int32 = 100
				statOff int32 = 2000
			)
			copy(buf[pathOff:], gp.p)
			pathLen := int32(len(gp.p))

			errno := s.Xpath_filestat_get(dirfd, 0, pathOff, pathLen, statOff)
			if errno != wasiENotCap {
				t.Fatalf("path_filestat_get(%q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
			}
			got, err := os.ReadFile(outsideFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "marker" {
				t.Fatalf("outside file content changed after path_filestat_get: %q", got)
			}

			farFutureMtim := origMtime.Add(48 * time.Hour).UnixNano()
			errno = s.Xpath_filestat_set_times(dirfd, 0, pathOff, pathLen, 0, farFutureMtim, fstMtim)
			if errno != wasiENotCap {
				t.Fatalf("path_filestat_set_times(%q) = %d, want ENOTCAPABLE (%d)", gp.p, errno, wasiENotCap)
			}
			fi1, err := os.Stat(outsideFile)
			if err != nil {
				t.Fatal(err)
			}
			if !fi1.ModTime().Equal(origMtime) {
				t.Fatalf("outside file mtime changed: was %v, now %v", origMtime, fi1.ModTime())
			}
		})
	}
}

func TestReadOnlyFSPreopen(t *testing.T) {
	// Acceptance criteria:
	// Constructing State with a read-only fs.FS preopen allows opening and reading
	// existing files but reports no write/path-mutation rights and returns a
	// read-only/capability error for create, truncate, rename, unlink, mkdir,
	// link, symlink, and writable path_open attempts.

	guestPath := "/data"
	fileName := "hello.txt"
	fileContent := "hello world"
	readOnlyFS := fstest.MapFS{
		fileName: {Data: []byte(fileContent)},
	}

	buf := make([]byte, 1024)
	mem := func() []byte { return buf }

	// API expectation: WithReadOnlyFS
	s := New(mem, NewModuleConfig().WithReadOnlyFS(guestPath, readOnlyFS))

	t.Run("open and read existing file succeeds", func(t *testing.T) {
		const fdPtr = 100
		pathPtr := 200
		copy(buf[pathPtr:], fileName)

		// path_open(dirfd=3, lookupflags=0, path, path_len, oflags=0, rights_base=read, rights_inheriting=0, fdflags=0, fd_ptr)
		errno := s.Xpath_open(3, 0, int32(pathPtr), int32(len(fileName)), 0, int64(rightFDRead), 0, 0, int32(fdPtr))
		if errno != wasiESuccess {
			t.Fatalf("path_open(%q) = %d, want ESUCCESS", fileName, errno)
		}

		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr:]))

		// fd_read
		const iovsPtr = 300
		const nreadPtr = 400
		const readBufPtr = 500
		binary.LittleEndian.PutUint32(buf[iovsPtr:], uint32(readBufPtr))
		binary.LittleEndian.PutUint32(buf[iovsPtr+4:], uint32(len(fileContent)))

		errno = s.Xfd_read(fd, iovsPtr, 1, nreadPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_read = %d, want ESUCCESS", errno)
		}

		nread := binary.LittleEndian.Uint32(buf[nreadPtr:])
		if int(nread) != len(fileContent) {
			t.Errorf("nread = %d, want %d", nread, len(fileContent))
		}
		if string(buf[readBufPtr:readBufPtr+int(nread)]) != fileContent {
			t.Errorf("read content = %q, want %q", string(buf[readBufPtr:readBufPtr+int(nread)]), fileContent)
		}
	})

	t.Run("fd 3 (preopen) reports no write or path-mutation rights", func(t *testing.T) {
		const statPtr = 600
		errno := s.Xfd_fdstat_get(3, statPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_fdstat_get(3) = %d, want ESUCCESS", errno)
		}

		// rights_base (u64) at offset 8
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8:])

		// Characteristic write/mutation rights that SHOULD be missing
		mutationRights := []struct {
			name uint64
			desc string
		}{
			{rightFDWrite, "rightFDWrite"},
		}

		for _, r := range mutationRights {
			if rightsBase&r.name != 0 {
				t.Errorf("rights_base HAS %s, want it missing for read-only FS", r.desc)
			}
		}
	})

	t.Run("mutation attempts return EROFS or NOTCAPABLE", func(t *testing.T) {
		pathPtr := 700
		copy(buf[pathPtr:], "newdir")

		// path_create_directory
		errno := s.Xpath_create_directory(3, int32(pathPtr), 6)
		if errno != wasiEROFS && errno != wasiENotCap {
			t.Errorf("path_create_directory = %d, want EROFS or ENOTCAP", errno)
		}

		// path_unlink_file
		copy(buf[pathPtr:], fileName)
		errno = s.Xpath_unlink_file(3, int32(pathPtr), int32(len(fileName)))
		if errno != wasiEROFS && errno != wasiENotCap {
			t.Errorf("path_unlink_file = %d, want EROFS or ENOTCAP", errno)
		}

		// path_open with oflagCreat on existing file
		const fdPtr = 800
		copy(buf[pathPtr:], fileName)
		// oflagCreat = 1
		errno = s.Xpath_open(3, 0, int32(pathPtr), int32(len(fileName)), 1, int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
		if errno != wasiEROFS && errno != wasiENotCap {
			t.Errorf("path_open(O_CREAT) on %q = %d, want EROFS or ENOTCAP", fileName, errno)
		}
	})
}

func TestFdRights(t *testing.T) {
	t.Parallel()

	const (
		statPtr     = 1000
		fdPtr       = 2000
		pathOff     = 3000
		iovsOff     = 4000
		dataOff     = 5000
		nreadOff    = 6000
		nwrittenOff = 6100
	)

	s, buf, tmpDir := newWMState(t)

	// Create a test file.
	fname := "rights.txt"
	fpath := filepath.Join(tmpDir, fname)
	if err := os.WriteFile(fpath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("read-only rights", func(t *testing.T) {
		// Open with ONLY read rights.
		copy(buf[pathOff:], fname)
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(rightFDRead), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(read-only) = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// 1. Verify fd_fdstat_get reports only requested rights.
		if errno := s.Xfd_fdstat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d", errno)
		}
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
		if rightsBase != rightFDRead {
			t.Errorf("rightsBase = %x, want %x (read only)", rightsBase, rightFDRead)
		}

		// 2. Read should succeed.
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_read(fd, iovsOff, 1, nreadOff); errno != wasiESuccess {
			t.Errorf("fd_read with read rights = %d, want ESUCCESS", errno)
		}

		// 3. Write should fail with ENOTCAPABLE.
		copy(buf[dataOff:], "world")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiENotCap {
			t.Errorf("fd_write without write rights = %d, want ENOTCAPABLE(%d)", errno, wasiENotCap)
		}
	})

	t.Run("write-only rights", func(t *testing.T) {
		// Open with ONLY write rights.
		copy(buf[pathOff:], fname)
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(rightFDWrite), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(write-only) = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// 1. Verify fd_fdstat_get reports only requested rights.
		if errno := s.Xfd_fdstat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d", errno)
		}
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
		if rightsBase != rightFDWrite {
			t.Errorf("rightsBase = %x, want %x (write only)", rightsBase, rightFDWrite)
		}

		// 2. Read should fail with ENOTCAPABLE.
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_read(fd, iovsOff, 1, nreadOff); errno != wasiENotCap {
			t.Errorf("fd_read without read rights = %d, want ENOTCAPABLE(%d)", errno, wasiENotCap)
		}

		// 3. Write should succeed.
		copy(buf[dataOff:], "world")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Errorf("fd_write with write rights = %d, want ESUCCESS", errno)
		}
	})

	t.Run("dropping and adding rights", func(t *testing.T) {
		// Open with read and write rights.
		copy(buf[pathOff:], fname)
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(read-write) = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// 1. Drop write rights.
		if errno := s.Xfd_fdstat_set_rights(fd, int64(rightFDRead), 0); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_set_rights(drop write) = %d", errno)
		}

		// 2. Verify rights are dropped in fdstat.
		s.Xfd_fdstat_get(fd, statPtr)
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
		if rightsBase != rightFDRead {
			t.Errorf("rightsBase after drop = %x, want %x", rightsBase, rightFDRead)
		}

		// 3. Attempting to re-add write rights should fail with ENOTCAPABLE.
		if errno := s.Xfd_fdstat_set_rights(fd, int64(rightFDRead|rightFDWrite), 0); errno != wasiENotCap {
			t.Errorf("Xfd_fdstat_set_rights(re-add write) = %d, want ENOTCAPABLE", errno)
		}
	})
}

// TestWasiIOAndPathOpenRejectRightsBeyondFdAndParent verifies positioned I/O and
// size changes return ENOTCAPABLE when the fd lacks the requested capability.
// It also checks path_open under a narrowed parent inheriting mask: excess
// rights bits are clamped away (open succeeds) rather than failing the syscall.
func TestWasiIOAndPathOpenRejectRightsBeyondFdAndParent(t *testing.T) {
	t.Parallel()

	const (
		fdPtr       = 7100
		fd2Ptr      = 7150
		pathOff     = 7200
		path2Off    = 7250
		iovsOff     = 7300
		dataOff     = 7400
		nreadOff    = 7500
		nwrittenOff = 7550
	)

	s, buf, tmpDir := newWMState(t)

	readOnlyName := "rights-pread.txt"
	readOnlyPath := filepath.Join(tmpDir, readOnlyName)
	if err := os.WriteFile(readOnlyPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	copy(buf[pathOff:], readOnlyName)
	var errno int32
	errno = s.Xpath_open(dirfd, 0, pathOff, int32(len(readOnlyName)), 0, int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(read-only file) = %d, want ESUCCESS", errno)
	}
	fdRO := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
	binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
	errno = s.Xfd_pread(fdRO, iovsOff, 1, 0, nreadOff)
	if errno != wasiESuccess {
		t.Fatalf("fd_pread with FD_READ = %d, want ESUCCESS", errno)
	}
	if n := binary.LittleEndian.Uint32(buf[nreadOff:]); n != 5 {
		t.Fatalf("fd_pread nread = %d, want 5", n)
	}

	copy(buf[dataOff:], "world")
	binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
	binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
	errno = s.Xfd_pwrite(fdRO, iovsOff, 1, 0, nwrittenOff)
	if errno != wasiENotCap {
		t.Fatalf("fd_pwrite without FD_WRITE = %d, want ENOTCAPABLE(%d)", errno, wasiENotCap)
	}

	binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
	binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
	errno = s.Xfd_write(fdRO, iovsOff, 1, nwrittenOff)
	if errno != wasiENotCap {
		t.Fatalf("fd_write without FD_WRITE = %d, want ENOTCAPABLE(%d)", errno, wasiENotCap)
	}

	errno = s.Xfd_filestat_set_size(fdRO, 3)
	if errno != wasiENotCap {
		t.Fatalf("fd_filestat_set_size without FD_FILESTAT_SET_SIZE = %d, want ENOTCAPABLE(%d)", errno, wasiENotCap)
	}

	writeOnlyName := "rights-pwrite.txt"
	writeOnlyPath := filepath.Join(tmpDir, writeOnlyName)
	if err := os.WriteFile(writeOnlyPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	copy(buf[pathOff:], writeOnlyName)
	errno = s.Xpath_open(dirfd, 0, pathOff, int32(len(writeOnlyName)), 0, int64(rightFDWrite), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(write-only file) = %d, want ESUCCESS", errno)
	}
	fdWO := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
	binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
	errno = s.Xfd_pread(fdWO, iovsOff, 1, 0, nreadOff)
	if errno != wasiENotCap {
		t.Fatalf("fd_pread without FD_READ = %d, want ENOTCAPABLE(%d)", errno, wasiENotCap)
	}

	binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
	binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
	errno = s.Xfd_read(fdWO, iovsOff, 1, nreadOff)
	if errno != wasiENotCap {
		t.Fatalf("fd_read without FD_READ = %d, want ENOTCAPABLE(%d)", errno, wasiENotCap)
	}

	subDir := filepath.Join(tmpDir, "sub-inherit")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedName := "nested.txt"
	if err := os.WriteFile(filepath.Join(subDir, nestedName), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	subGuest := "sub-inherit"
	copy(buf[pathOff:], subGuest)
	errno = s.Xpath_open(dirfd, 0, pathOff, int32(len(subGuest)), int32(oflagDir),
		int64(rightsWritableDirPreopen), int64(rightFDRead), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(subdir, narrowed inheriting) = %d, want ESUCCESS", errno)
	}
	subfd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// Requesting rights beyond parent's inheriting mask: runtime clamps (does not reject).
	// The resulting fd should have write clamped away, retaining only rightFDRead.
	copy(buf[path2Off:], nestedName)
	errno = s.Xpath_open(subfd, 0, path2Off, int32(len(nestedName)), 0, int64(rightFDRead|rightFDWrite), 0, 0, fd2Ptr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(child, excess rights clamped to parent inheriting) = %d, want ESUCCESS(%d)", errno, wasiESuccess)
	}
	childFD := int32(binary.LittleEndian.Uint32(buf[fd2Ptr : fd2Ptr+4]))
	const statBuf2 = 9800
	errno = s.Xfd_fdstat_get(childFD, statBuf2)
	if errno != wasiESuccess {
		t.Fatalf("fd_fdstat_get(clamped child fd) = %d", errno)
	}
	gotChildBase := binary.LittleEndian.Uint64(buf[statBuf2+8:])
	if gotChildBase&uint64(rightFDWrite) != 0 {
		t.Fatalf("clamped child fd has FD_WRITE bit set (0x%x); expected it to be absent", gotChildBase)
	}
	if gotChildBase&uint64(rightFDRead) == 0 {
		t.Fatalf("clamped child fd is missing FD_READ bit (0x%x); expected it present", gotChildBase)
	}
}

// TestPathOpenClampsRightsAbsentFromPreopenInheriting verifies path_open clamps
// requested rights to the preopen's inheriting superset (path_open_preopen.rs
// directory_inheriting_rights). Bits inside the superset (e.g. FD_DATASYNC) are
// retained; bits outside it (e.g. SOCK_SHUTDOWN) are stripped.
func TestPathOpenClampsRightsAbsentFromPreopenInheriting(t *testing.T) {
	t.Parallel()

	const testRightSockShutdown uint64 = 1 << 28 // __WASI_RIGHT_SOCK_SHUTDOWN — outside directory_inheriting_rights

	const (
		fdPtr    = 6000
		pathOff  = 6100
		statOff  = 6200
		iovsOff  = 6300
		dataOff  = 6400
		nreadOff = 6500
	)

	s, buf, tmpDir := newWMState(t)

	fileName := "clamp-test.txt"
	if err := os.WriteFile(filepath.Join(tmpDir, fileName), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	copy(buf[pathOff:], fileName)

	inheritingMask := wasiTestsuiteDirectoryInheritingRightsMask()

	// Request rights inside and outside the preopen inheriting superset.
	requestedBase := uint64(rightFDRead) | uint64(rightFDWrite) | rightFDDatasync | testRightSockShutdown
	requestedInh := rightFDDatasync | testRightSockShutdown
	errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fileName)), 0,
		int64(requestedBase), int64(requestedInh), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open with mixed in/out superset rights = %d (%#x requested), want ESUCCESS",
			errno, requestedBase)
	}

	newFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	errno = s.Xfd_fdstat_get(newFD, statOff)
	if errno != wasiESuccess {
		t.Fatalf("fd_fdstat_get on clamped fd = %d", errno)
	}
	gotBase := binary.LittleEndian.Uint64(buf[statOff+8:])

	if gotBase&testRightSockShutdown != 0 {
		t.Errorf("fd rights_base has SOCK_SHUTDOWN set (%#x); expected clamped away (outside inheriting superset)", gotBase)
	}
	if gotBase&rightFDDatasync == 0 {
		t.Errorf("fd rights_base missing FD_DATASYNC (%#x); bit is in directory_inheriting_rights and must be retained", gotBase)
	}
	if gotBase&^inheritingMask != 0 {
		t.Errorf("fd rights_base %#x has bits outside inheriting superset %#x", gotBase, inheritingMask)
	}

	if gotBase&uint64(rightFDRead) == 0 {
		t.Errorf("fd rights_base missing FD_READ after clamping (%#x); fd should be usable for reads", gotBase)
	}

	binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
	binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
	errno = s.Xfd_pread(newFD, iovsOff, 1, 0, nreadOff)
	if errno != wasiESuccess {
		t.Fatalf("fd_pread on clamped fd = %d, want ESUCCESS; fd must be usable after rights clamping", errno)
	}
	if got := string(buf[dataOff : dataOff+5]); got != "hello" {
		t.Fatalf("fd_pread read %q, want %q", got, "hello")
	}
}

func TestFdFdstatGetReportsWASIPreview1RightsBits(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 65536)
	tmpDir := t.TempDir()
	s := New(func() []byte { return buf },
		NewModuleConfig().WithHostDirectoryPreopen("/tmp", tmpDir).WithReadOnlyFS("/ro", fstest.MapFS{}),
	)

	const readOnlyPreopenFD = int32(4) // mounts: 3=/tmp (writable host), 4=/ro (read-only fs.FS)

	const statPtr = 8000
	const pathOff = 8100
	const fdPtr = 8200

	// Bit positions from WASI snapshot-preview1 / wasi crate generated `Rights`.
	const (
		wasiRightsFDRead              = uint64(1 << 1)
		wasiRightsFDSeek              = uint64(1 << 2)
		wasiRightsFDFdstatSetFlags    = uint64(1 << 3)
		wasiRightsFDWrite             = uint64(1 << 6)
		wasiRightsPathCreateDirectory = uint64(1 << 9)
		wasiRightsPathOpen            = uint64(1 << 13)
		wasiRightsFDReaddir           = uint64(1 << 14)
		wasiRightsPathReadlink        = uint64(1 << 15)
		wasiRightsPathFilestatGet     = uint64(1 << 18)
		wasiRightsPathRemoveDirectory = uint64(1 << 25)
		wasiRightsPathUnlinkFile      = uint64(1 << 26)
		wasiRightsFDFilestatGet       = uint64(1 << 21)
		wasiRightsFDFilestatSetSize   = uint64(1 << 22)
		wasiRightsFDFilestatSetTimes  = uint64(1 << 23)
	)

	expectedWritableDirPreopenBase := wasiTestsuiteDirectoryBaseRightsMask()
	expectedWritableDirPreopenInheriting := wasiTestsuiteDirectoryInheritingRightsMask()

	errno := s.Xfd_fdstat_get(dirfd, statPtr)
	if errno != wasiESuccess {
		t.Fatalf("fd_fdstat_get(preopen dir) = %d, want ESUCCESS", errno)
	}
	gotPreBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	gotPreInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	if gotPreBase != expectedWritableDirPreopenBase {
		t.Errorf("preopen fd_fdstat_get rights_base = %#x, want %#x (path_open_preopen directory_base_rights)", gotPreBase, expectedWritableDirPreopenBase)
	}
	if gotPreInh != expectedWritableDirPreopenInheriting {
		t.Errorf("preopen fd_fdstat_get rights_inheriting = %#x, want %#x (path_open_preopen directory_inheriting_rights)", gotPreInh, expectedWritableDirPreopenInheriting)
	}
	if gotPreBase&wasiRightsFDFilestatSetSize != 0 {
		t.Errorf("preopen rights_base has FD_FILESTAT_SET_SIZE (bit 22); directories must omit it (got %#x)", gotPreBase)
	}

	// Read-only fs.FS preopen: directory bundle is read/stat/readdir only (no write or path mutation).
	expectedReadOnlyDirPreopen := wasiRightsFDRead | wasiRightsFDSeek | wasiRightsFDFdstatSetFlags |
		wasiRightsPathOpen | wasiRightsFDReaddir | wasiRightsPathReadlink | wasiRightsPathFilestatGet | wasiRightsFDFilestatGet

	errno = s.Xfd_fdstat_get(readOnlyPreopenFD, statPtr)
	if errno != wasiESuccess {
		t.Fatalf("fd_fdstat_get(read-only fs.FS preopen) = %d, want ESUCCESS", errno)
	}
	gotROBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	gotROInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	if gotROBase != expectedReadOnlyDirPreopen {
		t.Errorf("read-only preopen fd_fdstat_get rights_base = %#x, want %#x (WASI preview1 read-only directory bundle)", gotROBase, expectedReadOnlyDirPreopen)
	}
	if gotROInh != expectedReadOnlyDirPreopen {
		t.Errorf("read-only preopen fd_fdstat_get rights_inheriting = %#x, want %#x (WASI preview1 read-only directory bundle)", gotROInh, expectedReadOnlyDirPreopen)
	}

	for _, check := range []struct {
		name string
		mask uint64
	}{
		{"PATH_OPEN", wasiRightsPathOpen},
		{"PATH_CREATE_DIRECTORY", wasiRightsPathCreateDirectory},
		{"PATH_REMOVE_DIRECTORY", wasiRightsPathRemoveDirectory},
		{"PATH_UNLINK_FILE", wasiRightsPathUnlinkFile},
		{"FD_READDIR", wasiRightsFDReaddir},
		{"PATH_READLINK", wasiRightsPathReadlink},
	} {
		if gotPreBase&check.mask == 0 {
			t.Errorf("preopen rights_base missing %s (%#x)", check.name, check.mask)
		}
	}

	fname := "rights-bits.txt"
	fpath := filepath.Join(tmpDir, fname)
	if err := os.WriteFile(fpath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	expectedRegularFile := wasiRightsFDRead | wasiRightsFDSeek | wasiRightsFDFdstatSetFlags | wasiRightsFDWrite |
		wasiRightsFDFilestatGet | wasiRightsFDFilestatSetSize | wasiRightsFDFilestatSetTimes

	// Request file rights using the host's legacy constants; the fd stores that mask verbatim today.
	// fd_fdstat_get must report the spec-correct regular-file bundle instead (Green phase).
	legacyOpenRights := rightFDRead | rightFDWrite
	copy(buf[pathOff:], fname)
	errno = s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(legacyOpenRights), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(regular file) = %d, want ESUCCESS", errno)
	}
	fileFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	errno = s.Xfd_fdstat_get(fileFD, statPtr)
	if errno != wasiESuccess {
		t.Fatalf("fd_fdstat_get(regular file) = %d, want ESUCCESS", errno)
	}
	gotFileBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	gotFileInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	if gotFileBase != expectedRegularFile {
		t.Errorf("file fd_fdstat_get rights_base = %#x, want %#x (spec mask after opening with legacy request %#x)", gotFileBase, expectedRegularFile, legacyOpenRights)
	}
	if gotFileInh != expectedWritableDirPreopenInheriting {
		t.Errorf("file fd_fdstat_get rights_inheriting = %#x, want %#x", gotFileInh, expectedWritableDirPreopenInheriting)
	}
}

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

	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen(guestPath, root))

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

func TestHostDirectoryPreopen(t *testing.T) {
	// A host directory can be exposed as a WASI preopened directory.
	// Construction with a host directory preopen assigns fd 3 as a directory preopen;
	// fd_prestat_get and fd_prestat_dir_name return the configured guest path,
	// and fd_fdstat_get reports a directory fd with directory/path rights.

	tmpDir := t.TempDir()
	guestPath := "/host-data"
	buf := make([]byte, 1024)
	mem := func() []byte { return buf }

	// RED PHASE: The plan calls for a new WithHostDirectoryPreopen option or similar
	// that we will implement in the next phase. For now, I'll use a hypothetical
	// constructor that encodes the acceptance criteria.
	//
	// The refactor plan likely wants to introduce a more WASI-standard "preopen"
	// concept that specifically maps to fd 3+ in a way that matches WASI expectations.

	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen(guestPath, tmpDir))

	t.Run("fd 3 is a preopen", func(t *testing.T) {
		const prestatPtr = 100
		errno := s.Xfd_prestat_get(3, prestatPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_prestat_get(3) = %d, want ESUCCESS", errno)
		}

		// prestat struct: tag (u32, 0 for dir) + name_len (u32)
		tag := binary.LittleEndian.Uint32(buf[prestatPtr:])
		if tag != 0 {
			t.Errorf("prestat.tag = %d, want 0 (directory)", tag)
		}

		nameLen := binary.LittleEndian.Uint32(buf[prestatPtr+4:])
		if int(nameLen) != len(guestPath) {
			t.Errorf("prestat.name_len = %d, want %d", nameLen, len(guestPath))
		}

		const namePtr = 200
		errno = s.Xfd_prestat_dir_name(3, namePtr, int32(nameLen))
		if errno != wasiESuccess {
			t.Fatalf("fd_prestat_dir_name(3) = %d, want ESUCCESS", errno)
		}

		gotPath := string(buf[namePtr : namePtr+int32(nameLen)])
		if gotPath != guestPath {
			t.Errorf("fd_prestat_dir_name = %q, want %q", gotPath, guestPath)
		}
	})

	t.Run("fd 3 has correct fdstat", func(t *testing.T) {
		const statPtr = 300
		errno := s.Xfd_fdstat_get(3, statPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_fdstat_get(3) = %d, want ESUCCESS", errno)
		}

		// fdstat: fs_filetype (u8) at offset 0
		ftype := buf[statPtr]
		if ftype != fdDir {
			t.Errorf("fdstat.fs_filetype = %d, want %d (directory)", ftype, fdDir)
		}

		// rights_base (u64) at offset 8
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8:])

		// Expected directory/path rights.
		// Exact bits depend on final implementation, but should include at least read/readdir.
		// For now, we'll check some characteristic directory rights.
		if rightsBase&rightFDFilestatGet == 0 {
			t.Error("rights_base missing rightFDFilestatGet")
		}
	})
}

// TestWritablePreopenRightsMatchWasiTestsuite verifies writable directory preopens
// advertise the directory base and inheriting rights masks from path_open_preopen.rs.
func TestWritablePreopenRightsMatchWasiTestsuite(t *testing.T) {
	const (
		statPtr = 9000
		fdPtr   = 9100
		pathOff = 9200
	)

	buf := make([]byte, 65536)
	tmpDir := t.TempDir()
	s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

	const preopenFd = int32(3)

	if errno := s.Xfd_fdstat_get(preopenFd, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(preopen) = %d, want ESUCCESS", errno)
	}
	gotBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	gotInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])

	if gotBase != rightsWritableDirPreopenBase {
		t.Errorf("fs_rights_base = %#x, want %#x (rightsWritableDirPreopenBase)", gotBase, rightsWritableDirPreopenBase)
	}
	if gotInh != rightsWritableDirPreopenInheriting {
		t.Errorf("fs_rights_inheriting = %#x, want %#x (rightsWritableDirPreopenInheriting)", gotInh, rightsWritableDirPreopenInheriting)
	}

	const dot = "."
	copy(buf[pathOff:], dot)

	errno := s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), 0,
		int64(gotBase), int64(gotInh), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., same rights) = %d, want ESUCCESS", errno)
	}

	errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), 0, 0, 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., empty rights) = %d, want ESUCCESS", errno)
	}

	errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), int32(oflagDir), 0, 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., O_DIRECTORY, empty rights) = %d, want ESUCCESS", errno)
	}

	errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), int32(oflagDir),
		int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., O_DIRECTORY, FD_READ) = %d, want ESUCCESS", errno)
	}

	if runtime.GOOS != "windows" {
		errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), int32(oflagDir),
			int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
		if errno != wasiEIsdir {
			t.Fatalf("path_open(., O_DIRECTORY, FD_READ|FD_WRITE) = %d, want EISDIR (%d)", errno, wasiEIsdir)
		}
	}
}

// Bit tables duplicated from wasi-testsuite/tests/rust/wasm32-wasip1/src/bin/path_open_preopen.rs
// (wasi crate 0.11.0 snapshot-preview1). Rights use wasihost.go __wasi_rights_t constants.

type wasiTestsuiteRight struct {
	name  string
	right uint64
}

func wasiTestsuiteDirectoryBaseRights() []wasiTestsuiteRight {
	return []wasiTestsuiteRight{
		{"PATH_CREATE_DIRECTORY", rightPathCreateDirectory},
		{"PATH_CREATE_FILE", rightPathCreateFile},
		{"PATH_LINK_SOURCE", rightPathLinkSource},
		{"PATH_LINK_TARGET", rightPathLinkTarget},
		{"PATH_OPEN", rightPathOpen},
		{"FD_READDIR", rightFDReaddir},
		{"PATH_READLINK", rightPathReadlink},
		{"PATH_RENAME_SOURCE", rightPathRenameSource},
		{"PATH_RENAME_TARGET", rightPathRenameTarget},
		{"PATH_SYMLINK", rightPathSymlink},
		{"PATH_REMOVE_DIRECTORY", rightPathRemoveDirectory},
		{"PATH_UNLINK_FILE", rightPathUnlinkFile},
		{"PATH_FILESTAT_GET", rightPathFilestatGet},
		{"PATH_FILESTAT_SET_SIZE", rightPathFilestatSetSize},
		{"PATH_FILESTAT_SET_TIMES", rightPathFilestatSetTimes},
		{"FD_FILESTAT_GET", rightFDFilestatGet},
		{"FD_FILESTAT_SET_TIMES", rightFDFilestatSetTimes},
	}
}

func wasiTestsuiteDirectoryInheritingRights() []wasiTestsuiteRight {
	base := wasiTestsuiteDirectoryBaseRights()
	rights := make([]wasiTestsuiteRight, 0, len(base)+13)
	for _, r := range base {
		if r.right == rightPathFilestatSetSize {
			continue
		}
		rights = append(rights, r)
	}
	rights = append(rights,
		wasiTestsuiteRight{"FD_DATASYNC", rightFDDatasync},
		wasiTestsuiteRight{"FD_READ", rightFDRead},
		wasiTestsuiteRight{"FD_SEEK", rightFDSeek},
		wasiTestsuiteRight{"FD_FDSTAT_SET_FLAGS", rightFDFdstatSetFlags},
		wasiTestsuiteRight{"FD_SYNC", rightFDSync},
		wasiTestsuiteRight{"FD_TELL", rightFDTell},
		wasiTestsuiteRight{"FD_WRITE", rightFDWrite},
		wasiTestsuiteRight{"FD_ADVISE", rightFDAdvise},
		wasiTestsuiteRight{"FD_ALLOCATE", rightFDAllocate},
		wasiTestsuiteRight{"FD_FILESTAT_GET", rightFDFilestatGet},
		wasiTestsuiteRight{"FD_FILESTAT_SET_SIZE", rightFDFilestatSetSize},
		wasiTestsuiteRight{"FD_FILESTAT_SET_TIMES", rightFDFilestatSetTimes},
		wasiTestsuiteRight{"POLL_FD_READWRITE", rightPollFDReadwrite},
	)
	return rights
}

func wasiTestsuiteDirectoryBaseRightsMask() uint64 {
	var mask uint64
	for _, r := range wasiTestsuiteDirectoryBaseRights() {
		mask |= r.right
	}
	return mask
}

func wasiTestsuiteDirectoryInheritingRightsMask() uint64 {
	var mask uint64
	for _, r := range wasiTestsuiteDirectoryInheritingRights() {
		mask |= r.right
	}
	return mask
}

// TestWritableDirPreopenRightsMatchWasiTestsuiteMasks ties production preopen masks to path_open_preopen.rs.
func TestWritableDirPreopenRightsMatchWasiTestsuiteMasks(t *testing.T) {
	t.Parallel()
	if rightsWritableDirPreopenBase != wasiTestsuiteDirectoryBaseRightsMask() {
		t.Errorf("rightsWritableDirPreopenBase = %#x, want wasi-testsuite base %#x",
			rightsWritableDirPreopenBase, wasiTestsuiteDirectoryBaseRightsMask())
	}
	if rightsWritableDirPreopenInheriting != wasiTestsuiteDirectoryInheritingRightsMask() {
		t.Errorf("rightsWritableDirPreopenInheriting = %#x, want wasi-testsuite inheriting %#x",
			rightsWritableDirPreopenInheriting, wasiTestsuiteDirectoryInheritingRightsMask())
	}
}
