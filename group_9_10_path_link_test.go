package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func assertMatchingFilestats(t *testing.T, buf []byte, offA, offB int32) {
	t.Helper()
	for _, tc := range []struct {
		name string
		off  int
		size int
	}{
		{"dev", filestatDevOff, 8},
		{"ino", filestatInoOff, 8},
		{"size", filestatSizeOff, 8},
	} {
		gotA := binary.LittleEndian.Uint64(buf[offA+int32(tc.off) : offA+int32(tc.off+tc.size)])
		gotB := binary.LittleEndian.Uint64(buf[offB+int32(tc.off) : offB+int32(tc.off+tc.size)])
		if gotA != gotB {
			t.Errorf("filestat %s = %d vs %d, want equal", tc.name, gotA, gotB)
		}
	}
	if gotA := buf[offA+filestatTypeOff]; gotA != buf[offB+filestatTypeOff] {
		t.Errorf("filestat filetype = %d vs %d, want equal", gotA, buf[offB+filestatTypeOff])
	}
	nlinkA := binary.LittleEndian.Uint64(buf[offA+filestatNlinkOff : offA+filestatNlinkOff+8])
	nlinkB := binary.LittleEndian.Uint64(buf[offB+filestatNlinkOff : offB+filestatNlinkOff+8])
	if nlinkA != 0 && nlinkB != 0 && nlinkA != nlinkB {
		t.Errorf("filestat nlink = %d vs %d, want equal", nlinkA, nlinkB)
	}
}

func assertMatchingFdstats(t *testing.T, buf []byte, offA, offB int32) {
	t.Helper()
	if buf[offA] != buf[offB] {
		t.Errorf("fdstat fs_filetype = %d vs %d, want equal", buf[offA], buf[offB])
	}
	flagsA := binary.LittleEndian.Uint16(buf[offA+2 : offA+4])
	flagsB := binary.LittleEndian.Uint16(buf[offB+2 : offB+4])
	if flagsA != flagsB {
		t.Errorf("fdstat fs_flags = %d vs %d, want equal", flagsA, flagsB)
	}
	rbA := binary.LittleEndian.Uint64(buf[offA+8 : offA+16])
	rbB := binary.LittleEndian.Uint64(buf[offB+8 : offB+16])
	if rbA != rbB {
		t.Errorf("fdstat fs_rights_base = 0x%x vs 0x%x, want equal", rbA, rbB)
	}
	riA := binary.LittleEndian.Uint64(buf[offA+16 : offA+24])
	riB := binary.LittleEndian.Uint64(buf[offB+16 : offB+24])
	if riA != riB {
		t.Errorf("fdstat fs_rights_inheriting = 0x%x vs 0x%x, want equal", riA, riB)
	}
}

func TestGroup9_10PathLink(t *testing.T) {
	const (
		pathOff1 = 100
		pathOff2 = 200
		fdPtr    = 400
		statOff1 = 500
		statOff2 = 600
		fdstat1  = 700
		fdstat2  = 800
	)

	t.Run("hard link success same dir and subdir", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		if errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatalf("Xpath_link(file, link) = %d, want ESUCCESS", errno)
		}
		origInfo, err1 := os.Stat(filepath.Join(hostDir, "file"))
		linkInfo, err2 := os.Stat(filepath.Join(hostDir, "link"))
		if err1 != nil || err2 != nil {
			t.Fatalf("stat: %v, %v", err1, err2)
		}
		if !os.SameFile(origInfo, linkInfo) {
			t.Error("hard link is not the same file as original")
		}

		openRights := int64(rightFDRead | rightFDWrite | rightFDFilestatGet)
		if errno := s.Xpath_open(dirfd, 0, fileOff, fileLen, 0, openRights, 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open(file) = %d", errno)
		}
		fdOrig := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xpath_open(dirfd, 0, linkOff, linkLen, 0, openRights, 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open(link) = %d", errno)
		}
		fdLink := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xfd_filestat_get(fdOrig, statOff1); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get(orig) = %d", errno)
		}
		if errno := s.Xfd_filestat_get(fdLink, statOff2); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get(link) = %d", errno)
		}
		assertMatchingFilestats(t, buf, statOff1, statOff2)
		if errno := s.Xfd_fdstat_get(fdOrig, fdstat1); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get(orig) = %d", errno)
		}
		if errno := s.Xfd_fdstat_get(fdLink, fdstat2); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get(link) = %d", errno)
		}
		assertMatchingFdstats(t, buf, fdstat1, fdstat2)
		s.Xfd_close(fdOrig)
		s.Xfd_close(fdLink)

		const pathOffSubdir = 300
		subdirFD := openSubdirViaPathOpen(t, s, buf, hostDir, pathOffSubdir, fdPtr)
		if errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, subdirFD, linkOff, linkLen); errno != wasiESuccess {
			t.Fatalf("Xpath_link(file, subdir/link) = %d, want ESUCCESS", errno)
		}
		subLinkInfo, err := os.Stat(filepath.Join(hostDir, "subdir", "link"))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(origInfo, subLinkInfo) {
			t.Error("subdir hard link is not the same file as original")
		}
		if errno := s.Xpath_open(subdirFD, 0, linkOff, linkLen, 0, openRights, 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open(subdir/link) = %d", errno)
		}
		fdSubLink := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xfd_filestat_get(fdSubLink, statOff2); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get(subdir link) = %d", errno)
		}
		assertMatchingFilestats(t, buf, statOff1, statOff2)
		if errno := s.Xfd_fdstat_get(fdSubLink, fdstat2); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get(subdir link) = %d", errno)
		}
		assertMatchingFdstats(t, buf, fdstat1, fdstat2)
		s.Xfd_close(fdSubLink)
		s.Xfd_close(subdirFD)

		subLinkPathOff, subLinkPathLen := writePath(buf, pathOffSubdir, "subdir/link")
		if errno := s.Xpath_unlink_file(dirfd, subLinkPathOff, subLinkPathLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
		if errno := s.Xpath_unlink_file(dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
		if errno := s.Xpath_unlink_file(dirfd, fileOff, fileLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
		if err := os.Remove(filepath.Join(hostDir, "subdir")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Xpath_link returns EEXIST when link already exists", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create a file to link to.
		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create a link to the file.
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_link(file, link) = %d, want ESUCCESS", errno)
		}

		// Try to create the same link again - should return EEXIST.
		errno = s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiEExist {
			t.Errorf("Xpath_link(file, link) second call = %d, want EEXIST (%d)", errno, wasiEExist)
		}
	})

	t.Run("Xpath_link returns EEXIST when new path is an existing directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		if errno := s.Xpath_create_directory(dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatalf("Xpath_create_directory(link) = %d, want ESUCCESS", errno)
		}
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiEExist {
			t.Errorf("Xpath_link(file, existing dir link) = %d, want EEXIST (%d)", errno, wasiEExist)
		}
		if errno := s.Xpath_remove_directory(dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
	})

	t.Run("Xpath_link returns EEXIST for self-link", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create a file.
		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Try to link the file to itself - should return EEXIST.
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, fileOff, fileLen)
		if errno != wasiEExist {
			t.Errorf("Xpath_link(file, file) = %d, want EEXIST (%d)", errno, wasiEExist)
		}
	})

	t.Run("Xpath_link returns EPERM or EACCES when linking to a directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create a directory to link to.
		if err := os.Mkdir(filepath.Join(hostDir, "dir"), 0755); err != nil {
			t.Fatal(err)
		}

		// Try to create a hard link to the directory - should return EPERM or EACCES.
		dirOff, dirLen := writePath(buf, pathOff1, "dir")
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		errno := s.Xpath_link(dirfd, 0, dirOff, dirLen, dirfd, linkOff, linkLen)
		if errno != wasiEPerm && errno != wasiEAcces {
			t.Errorf("Xpath_link(dir, link) = %d, want EPERM (%d) or EACCES (%d)", errno, wasiEPerm, wasiEAcces)
		}
	})

	t.Run("Xpath_link returns ENOENT when trailing slash on new path for a non-directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create a file.
		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Try to link to a path with trailing slash - should return ENOENT.
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		linkOff, linkLen := writePath(buf, pathOff2, "link/")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiENoEnt {
			t.Errorf("Xpath_link(file, link/) = %d, want ENOENT (%d)", errno, wasiENoEnt)
		}
	})

	// Cycle 3 behavior: path_link hard-links symlink inodes without following and rejects SYMLINK_FOLLOW lookup on symlink paths
	t.Run("Xpath_link returns ESUCCESS for symlink hard-link and rejects SYMLINK_FOLLOW", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Skip if NO_DANGLING_FILESYSTEM is set (parity with wasi-testsuite)
		if os.Getenv("NO_DANGLING_FILESYSTEM") != "" {
			t.Skip("NO_DANGLING_FILESYSTEM set; parity with wasi-testsuite symlink tests")
		}

		const (
			targetOff  = 100
			symlinkOff = 200
			linkOff    = 300
		)

		t.Run("hard-link to symlink succeeds", func(t *testing.T) {
			// Create a symlink to a target
			copy(buf[targetOff:], "target")
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
			}

			// Xpath_link(dirfd, 0, \"symlink\", dirfd, \"link\") should return ESUCCESS
			// when linking a symlink to a target (hard link to symlink)
			copy(buf[linkOff:], "link")
			errno = s.Xpath_link(dirfd, 0, symlinkOff, 7, dirfd, linkOff, 4)
			if errno != wasiESuccess {
				t.Errorf("Xpath_link(symlink, link) = %d, want ESUCCESS", errno)
			}

			// Cleanup
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if errno := s.Xpath_unlink_file(dirfd, linkOff, 4); errno != wasiESuccess {
				t.Fatal(errno)
			}
		})

		t.Run("hard-link to symlink loop succeeds", func(t *testing.T) {
			// Create a symlink that points to itself (loop)
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(symlinkOff, 7, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(symlink, symlink) = %d, want ESUCCESS", errno)
			}

			// Xpath_link(dirfd, 0, \"symlink\", dirfd, \"link\") should return ESUCCESS
			// for symlink loop (linking symlink to itself)
			copy(buf[linkOff:], "link")
			errno = s.Xpath_link(dirfd, 0, symlinkOff, 7, dirfd, linkOff, 4)
			if errno != wasiESuccess {
				t.Errorf("Xpath_link(symlink, link) = %d, want ESUCCESS", errno)
			}

			// Cleanup
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if errno := s.Xpath_unlink_file(dirfd, linkOff, 4); errno != wasiESuccess {
				t.Fatal(errno)
			}
		})

		t.Run("linking file to existing symlink returns EEXIST", func(t *testing.T) {
			// Create a symlink to a target
			copy(buf[targetOff:], "target")
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
			}

			// Create a file to link
			if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
				t.Fatal(err)
			}
			copy(buf[targetOff:], "file")

			// Xpath_link(dirfd, 0, \"file\", dirfd, \"symlink\") should return EEXIST
			// when linking a file to a symlink (symlink already exists)
			errno = s.Xpath_link(dirfd, 0, targetOff, 4, dirfd, symlinkOff, 7)
			if errno != wasiEExist {
				t.Errorf("Xpath_link(file, symlink) = %d, want EEXIST (%d)", errno, wasiEExist)
			}

			// Cleanup
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if err := os.Remove(filepath.Join(hostDir, "file")); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("SYMLINK_FOLLOW flag on symlink path returns EINVAL or ENOENT", func(t *testing.T) {
			// Create a symlink to a target
			copy(buf[targetOff:], "target")
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
			}

			// Xpath_link(dirfd, wasiLookupSymlinkFollow, \"symlink\", dirfd, \"link\") should
			// return EINVAL or ENOENT (not ESUCCESS) when using SYMLINK_FOLLOW flag
			copy(buf[linkOff:], "link")
			errno = s.Xpath_link(dirfd, wasiLookupSymlinkFollow, symlinkOff, 7, dirfd, linkOff, 4)
			if errno == wasiESuccess {
				t.Errorf("Xpath_link(symlink, link) with SYMLINK_FOLLOW = ESUCCESS, want EINVAL (%d) or ENOENT (%d)", wasiEInval, wasiENoEnt)
			} else if errno != wasiEInval && errno != wasiENoEnt {
				t.Errorf("Xpath_link(symlink, link) with SYMLINKFollow = %d, want EINVAL (%d) or ENOENT (%d)", errno, wasiEInval, wasiENoEnt)
			}

			// Cleanup (link may not exist when Xpath_link correctly rejected SYMLINK_FOLLOW)
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if errno := s.Xpath_unlink_file(dirfd, linkOff, 4); errno != wasiESuccess && errno != wasiENoEnt {
				t.Fatal(errno)
			}
		})
	})
}
