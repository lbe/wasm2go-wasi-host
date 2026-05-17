package wasihost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
	s := New(mem, WithHostDirectoryPreopen("/data", preopenDir))

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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
				s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
			s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", preopenDir))
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
