package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func assertHostPathNotExists(t *testing.T, hostDir, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(hostDir, name)); !os.IsNotExist(err) {
		t.Fatalf("%s still exists on host: %v", name, err)
	}
}

// TestPathSymlinkCreateAndFollowOpen verifies that a relative path_symlink can
// be created and then opened with SYMLINK_FOLLOW to reach either a file or a
// directory target inside the same writable preopen.
func TestPathSymlinkCreateAndFollowOpen(t *testing.T) {
	t.Parallel()

	const (
		targetOff  = 100
		symlinkOff = 200
		fdPtr      = 300
		fdstatPtr  = 400
	)

	t.Run("file target", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create the target file via path_open with O_CREAT.
		copy(buf[targetOff:], "target")
		errno := s.Xpath_open(dirfd, 0, targetOff, 6, int32(oflagCreat),
			int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(create target) = %d, want ESUCCESS", errno)
		}
		targetFd := binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4])
		s.Xfd_close(int32(targetFd))

		// Create a relative symlink "symlink" -> "target".
		copy(buf[targetOff:], "target")
		copy(buf[symlinkOff:], "symlink")
		errno = s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
		}

		// Open the symlink with SYMLINK_FOLLOW.
		copy(buf[symlinkOff:], "symlink")
		errno = s.Xpath_open(dirfd, wasiLookupSymlinkFollow, symlinkOff, 7, 0,
			int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(follow symlink to file) = %d, want ESUCCESS", errno)
		}
		fd := binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4])
		if fd <= 2 {
			t.Fatalf("fd = %d, want > 2", fd)
		}

		// Verify fd type is regular file.
		errno = s.Xfd_fdstat_get(int32(fd), fdstatPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d, want ESUCCESS", errno)
		}
		fdType := binary.LittleEndian.Uint16(buf[fdstatPtr : fdstatPtr+2])
		if fdType != uint16(fdFile) {
			t.Fatalf("fdType = %d, want fdFile (%d)", fdType, fdFile)
		}

		// Clean up.
		if errno := s.Xfd_close(int32(fd)); errno != wasiESuccess {
			t.Fatalf("Xfd_close = %d, want ESUCCESS", errno)
		}
		copy(buf[symlinkOff:], "symlink")
		if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
			t.Fatalf("Xpath_unlink_file(symlink) = %d, want ESUCCESS", errno)
		}
		copy(buf[targetOff:], "target")
		if errno := s.Xpath_unlink_file(dirfd, targetOff, 6); errno != wasiESuccess {
			t.Fatalf("Xpath_unlink_file(target) = %d, want ESUCCESS", errno)
		}

		// Verify host state is clean.
		assertHostPathNotExists(t, hostDir, "target")
		assertHostPathNotExists(t, hostDir, "symlink")
	})

	t.Run("directory target", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)

		// Create the target directory.
		copy(buf[targetOff:], "target")
		errno := s.Xpath_create_directory(dirfd, targetOff, 6)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_create_directory(target) = %d, want ESUCCESS", errno)
		}

		// Create a relative symlink "symlink" -> "target".
		copy(buf[targetOff:], "target")
		copy(buf[symlinkOff:], "symlink")
		errno = s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
		}

		// Open the symlink with SYMLINK_FOLLOW and OFLAGS_DIRECTORY.
		copy(buf[symlinkOff:], "symlink")
		errno = s.Xpath_open(dirfd, wasiLookupSymlinkFollow, symlinkOff, 7, int32(oflagDir),
			int64(rightFDRead), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(follow symlink to directory) = %d, want ESUCCESS", errno)
		}
		fd := binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4])
		if fd <= 2 {
			t.Fatalf("fd = %d, want > 2", fd)
		}

		// Verify fd type is directory.
		errno = s.Xfd_fdstat_get(int32(fd), fdstatPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d, want ESUCCESS", errno)
		}
		fdType := binary.LittleEndian.Uint16(buf[fdstatPtr : fdstatPtr+2])
		if fdType != uint16(fdDir) {
			t.Fatalf("fdType = %d, want fdDir (%d)", fdType, fdDir)
		}

		// Clean up.
		if errno := s.Xfd_close(int32(fd)); errno != wasiESuccess {
			t.Fatalf("Xfd_close = %d, want ESUCCESS", errno)
		}
		copy(buf[symlinkOff:], "symlink")
		if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
			t.Fatalf("Xpath_unlink_file(symlink) = %d, want ESUCCESS", errno)
		}
		copy(buf[targetOff:], "target")
		if errno := s.Xpath_remove_directory(dirfd, targetOff, 6); errno != wasiESuccess {
			t.Fatalf("Xpath_remove_directory(target) = %d, want ESUCCESS", errno)
		}

		// Verify host state is clean.
		assertHostPathNotExists(t, hostDir, "target")
		assertHostPathNotExists(t, hostDir, "symlink")
	})
}

// TestPathSymlinkRejectsAbsoluteOldPath verifies that path_symlink rejects an
// absolute old path that would escape the preopen sandbox.
func TestPathSymlinkRejectsAbsoluteOldPath(t *testing.T) {
	t.Parallel()

	const (
		targetOff  = 100
		symlinkOff = 200
	)

	s, buf := newTestState()
	hostDir := setupWritableMount(t, s, buf)

	// Attempt to create a symlink with an absolute old path "/".
	copy(buf[targetOff:], "/")
	copy(buf[symlinkOff:], "symlink")
	errno := s.Xpath_symlink(targetOff, 1, dirfd, symlinkOff, 7)
	if errno == wasiESuccess {
		t.Fatalf("Xpath_symlink(/, symlink) = ESUCCESS, want error")
	}
	// Acceptable errors per acceptance criteria: ENOTCAPABLE, EPERM, EINVAL, ENOENT.
	if errno != wasiENotCap && errno != wasiEPerm && errno != wasiEInval && errno != wasiENoEnt {
		t.Fatalf("Xpath_symlink(/, symlink) = %d, want one of ENOTCAPABLE (%d), EPERM (%d), EINVAL (%d), ENOENT (%d)",
			errno, wasiENotCap, wasiEPerm, wasiEInval, wasiENoEnt)
	}

	// Verify no symlink was created under the writable preopen host root.
	if _, err := os.Lstat(filepath.Join(hostDir, "symlink")); err == nil {
		t.Fatalf("symlink was created under preopen root despite rejection")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error checking for symlink: %v", err)
	}
}
