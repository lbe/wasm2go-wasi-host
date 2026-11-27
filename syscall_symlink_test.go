package wasihost

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDanglingSymlinkPathOpenNoFollow verifies that path_open without
// SYMLINK_FOLLOW on a dangling symlink returns ELOOP (or ENOTDIR when
// OFLAGS_DIRECTORY is set), not ESUCCESS.
func TestDanglingSymlinkPathOpenNoFollow(t *testing.T) {
	t.Parallel()

	if os.Getenv("NO_DANGLING_FILESYSTEM") != "" {
		t.Skip("NO_DANGLING_FILESYSTEM is set")
	}

	const (
		targetOff  = 100
		symlinkOff = 200
		fdPtr      = 300
	)

	s, buf := newTestState(nil)
	hostDir := setupWritableMount(t, s, buf)

	// Create a dangling symlink "symlink" that points to "target", which does not exist.
	copy(buf[targetOff:], "target")
	copy(buf[symlinkOff:], "symlink")
	errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
	}

	// path_open without SYMLINK_FOLLOW and with OFLAGS_DIRECTORY must return
	// ENOTDIR or ELOOP.
	copy(buf[symlinkOff:], "symlink")
	errno = s.Xpath_open(dirfd, 0, symlinkOff, 7, int32(oflagDir),
		int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiENotDir && errno != wasiELoop {
		t.Fatalf("Xpath_open(no-follow, directory) = %d; want ENOTDIR (%d) or ELOOP (%d)",
			errno, wasiENotDir, wasiELoop)
	}

	// path_open without SYMLINK_FOLLOW and no OFLAGS_DIRECTORY must return ELOOP.
	copy(buf[symlinkOff:], "symlink")
	errno = s.Xpath_open(dirfd, 0, symlinkOff, 7, 0,
		int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
	if errno != wasiELoop {
		t.Fatalf("Xpath_open(no-follow, no flags) = %d; want ELOOP (%d)",
			errno, wasiELoop)
	}

	// Cleanup.
	copy(buf[symlinkOff:], "symlink")
	if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
		t.Fatalf("Xpath_unlink_file(symlink) = %d, want ESUCCESS", errno)
	}

	// Verify host state is clean.
	if _, err := os.Lstat(filepath.Join(hostDir, "symlink")); !os.IsNotExist(err) {
		t.Errorf("symlink still exists on host")
	}
}

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
		s, buf := newTestState(nil)
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

		// Create a relative symlink "symlink" pointing to "target".
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
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create the target directory.
		copy(buf[targetOff:], "target")
		errno := s.Xpath_create_directory(dirfd, targetOff, 6)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_create_directory(target) = %d, want ESUCCESS", errno)
		}

		// Create a relative symlink "symlink" pointing to "target".
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

	s, buf := newTestState(nil)
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

// TestPathFilestatSymlinkFollowSemantics verifies that path_filestat_get and
// path_filestat_set_times distinguish a symlink from its target based on the
// LOOKUPFLAGS_SYMLINK_FOLLOW flag.
func TestPathFilestatSymlinkFollowSemantics(t *testing.T) {
	s, buf := newTestState(nil)
	hostDir := setupWritableMount(t, s, buf)

	const (
		dirfd      = int32(3)
		fileOff    = 100
		symlinkOff = 200
		statOff    = 300
		fdPtr      = 400
	)

	fileName := "file"
	copy(buf[fileOff:], fileName)

	// Create the target file.
	errno := s.Xpath_open(dirfd, 0, fileOff, int32(len(fileName)), int32(oflagCreat),
		int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(create file) = %d, want ESUCCESS", errno)
	}
	fd := binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4])
	s.Xfd_close(int32(fd))

	// Create symlink "symlink" pointing to "file".
	target := "file"
	copy(buf[fileOff:], target)
	copy(buf[symlinkOff:], "symlink")
	errno = s.Xpath_symlink(fileOff, int32(len(target)), dirfd, symlinkOff, 7)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_symlink = %d, want ESUCCESS", errno)
	}

	// Record file mtim.
	copy(buf[fileOff:], fileName)
	errno = s.Xpath_filestat_get(dirfd, 0, fileOff, int32(len(fileName)), statOff)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(file) = %d, want ESUCCESS", errno)
	}
	fileMtim0 := int64(binary.LittleEndian.Uint64(buf[statOff+48 : statOff+56]))

	// Record symlink mtim (no-follow).
	copy(buf[symlinkOff:], "symlink")
	errno = s.Xpath_filestat_get(dirfd, 0, symlinkOff, 7, statOff)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(symlink, no-follow) = %d, want ESUCCESS", errno)
	}
	symlinkType := binary.LittleEndian.Uint64(buf[statOff+16 : statOff+24])
	if symlinkType != uint64(fdSymlink) {
		t.Errorf("no-follow symlink filetype = %d, want fdSymlink (%d)", symlinkType, fdSymlink)
	}
	symMtim0 := int64(binary.LittleEndian.Uint64(buf[statOff+48 : statOff+56]))

	// Modify symlink mtim via no-follow.
	symNewMtim := symMtim0 - 200
	errno = s.Xpath_filestat_set_times(dirfd, 0, symlinkOff, 7, 0, symNewMtim, fstMtim)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_set_times(symlink, no-follow) = %d, want ESUCCESS", errno)
	}

	// Symlink mtim should be symMtim0 - 200.
	errno = s.Xpath_filestat_get(dirfd, 0, symlinkOff, 7, statOff)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(symlink after set) = %d, want ESUCCESS", errno)
	}
	gotSymMtim := int64(binary.LittleEndian.Uint64(buf[statOff+48 : statOff+56]))
	if gotSymMtim != symNewMtim {
		t.Errorf("symlink mtim = %d, want %d", gotSymMtim, symNewMtim)
	}

	// File mtim should still be fileMtim0.
	copy(buf[fileOff:], fileName)
	errno = s.Xpath_filestat_get(dirfd, 0, fileOff, int32(len(fileName)), statOff)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(file after symlink set) = %d, want ESUCCESS", errno)
	}
	gotFileMtim := int64(binary.LittleEndian.Uint64(buf[statOff+48 : statOff+56]))
	if gotFileMtim != fileMtim0 {
		t.Errorf("file mtim = %d, want %d (unchanged)", gotFileMtim, fileMtim0)
	}

	// Dereferenced symlink mtim should match file mtim.
	copy(buf[symlinkOff:], "symlink")
	errno = s.Xpath_filestat_get(dirfd, wasiLookupSymlinkFollow, symlinkOff, 7, statOff)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(symlink, follow) = %d, want ESUCCESS", errno)
	}
	gotDerefMtim := int64(binary.LittleEndian.Uint64(buf[statOff+48 : statOff+56]))
	if gotDerefMtim != fileMtim0 {
		t.Errorf("dereferenced symlink mtim = %d, want %d (file mtim)", gotDerefMtim, fileMtim0)
	}

	// Set times through symlink (follow) using original symMtim0.
	errno = s.Xpath_filestat_set_times(dirfd, wasiLookupSymlinkFollow, symlinkOff, 7, 0, symMtim0, fstMtim)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_set_times(symlink, follow) = %d, want ESUCCESS", errno)
	}

	// File mtim should now be symMtim0.
	copy(buf[fileOff:], fileName)
	errno = s.Xpath_filestat_get(dirfd, 0, fileOff, int32(len(fileName)), statOff)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(file after follow set) = %d, want ESUCCESS", errno)
	}
	gotFinalFileMtim := int64(binary.LittleEndian.Uint64(buf[statOff+48 : statOff+56]))
	if gotFinalFileMtim != symMtim0 {
		t.Errorf("final file mtim = %d, want %d", gotFinalFileMtim, symMtim0)
	}

	// Cleanup.
	copy(buf[symlinkOff:], "symlink")
	if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
		t.Fatalf("Xpath_unlink_file(symlink) = %d, want ESUCCESS", errno)
	}
	copy(buf[fileOff:], fileName)
	if errno := s.Xpath_unlink_file(dirfd, fileOff, int32(len(fileName))); errno != wasiESuccess {
		t.Fatalf("Xpath_unlink_file(file) = %d, want ESUCCESS", errno)
	}

	// Verify host state is clean.
	if _, err := os.Lstat(filepath.Join(hostDir, "symlink")); !os.IsNotExist(err) {
		t.Errorf("symlink still exists on host")
	}
	if _, err := os.Stat(filepath.Join(hostDir, fileName)); !os.IsNotExist(err) {
		t.Errorf("file still exists on host")
	}
}

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

		s, buf := newTestState(nil)
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
		s, buf := newTestState(nil)
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
		s, buf := newTestState(nil)
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

// TestPathUnlinkFileTrailingSlashSemantics verifies that path_unlink_file
// rejects trailing slashes on directories and files with the errno values the
// wasi-testsuite expects, and never silently succeeds or deletes the wrong
// entity.
func TestPathUnlinkFileTrailingSlashSemantics(t *testing.T) {
	t.Parallel()

	const pathOff = 1000

	s, buf := newTestState(nil)
	hostDir := setupWritableMount(t, s, buf)

	// --- Directory cases ---

	// Create a directory.
	copy(buf[pathOff:], "dir")
	if errno := s.Xpath_create_directory(dirfd, pathOff, 3); errno != wasiESuccess {
		t.Fatalf("Xpath_create_directory(dir) = %d, want ESUCCESS", errno)
	}

	// Unlinking a directory (no trailing slash) must fail.
	copy(buf[pathOff:], "dir")
	errno := s.Xpath_unlink_file(dirfd, pathOff, 3)
	if errno != wasiEIsdir && errno != wasiEPerm && errno != wasiEAcces {
		t.Fatalf("Xpath_unlink_file(dir) = %d; want one of EISDIR (%d), EPERM (%d), EACCES (%d)",
			errno, wasiEIsdir, wasiEPerm, wasiEAcces)
	}

	// Unlinking a directory with a trailing slash must also fail.
	copy(buf[pathOff:], "dir/")
	errno = s.Xpath_unlink_file(dirfd, pathOff, 4)
	if errno != wasiEIsdir && errno != wasiEPerm && errno != wasiEAcces {
		t.Fatalf("Xpath_unlink_file(dir/) = %d; want one of EISDIR (%d), EPERM (%d), EACCES (%d)",
			errno, wasiEIsdir, wasiEPerm, wasiEAcces)
	}

	// Directory must still exist.
	if _, err := os.Stat(filepath.Join(hostDir, "dir")); err != nil {
		t.Fatalf("directory was removed unexpectedly: %v", err)
	}

	// Remove the directory.
	copy(buf[pathOff:], "dir")
	if rmErrno := s.Xpath_remove_directory(dirfd, pathOff, 3); rmErrno != wasiESuccess {
		t.Fatalf("Xpath_remove_directory(dir) = %d, want ESUCCESS", rmErrno)
	}

	// --- File cases ---

	// Create a file directly on the host.
	if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Unlinking a file with a trailing slash must fail with ENOTDIR or ENOENT.
	copy(buf[pathOff:], "file/")
	errno = s.Xpath_unlink_file(dirfd, pathOff, 5)
	if errno != wasiENotDir && errno != wasiENoEnt {
		t.Fatalf("Xpath_unlink_file(file/) = %d; want ENOTDIR (%d) or ENOENT (%d)",
			errno, wasiENotDir, wasiENoEnt)
	}

	// The file must NOT have been removed.
	if _, err := os.Stat(filepath.Join(hostDir, "file")); err != nil {
		t.Fatalf("file was removed by Xpath_unlink_file(file/): %v", err)
	}

	// Unlinking the file without a trailing slash must succeed.
	copy(buf[pathOff:], "file")
	errno = s.Xpath_unlink_file(dirfd, pathOff, 4)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_unlink_file(file) = %d, want ESUCCESS", errno)
	}

	// The file must now be gone.
	if _, err := os.Stat(filepath.Join(hostDir, "file")); !os.IsNotExist(err) {
		t.Fatalf("file still exists after Xpath_unlink_file(file): %v", err)
	}
}

func acceptableSymlinkConfinementErr(errno int32) bool {
	return errno == wasiENotCap || errno == wasiENoEnt
}

func TestRootMountPathOpenSymlinkFollowRejectsEscapeOutsideSandbox(t *testing.T) {
	// Root preopen (guest "/") must confine SYMLINK_FOLLOW opens to the host sandbox.
	// writableMountHostPaths re-absolutizes mount-relative paths; follow lookups must
	// run resolvedPathConfinementErrno before path_open can return an escaped fd.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	secretContent := []byte("top secret")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}

	legitPath := filepath.Join(root, "legit.txt")
	if err := os.WriteFile(legitPath, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkName := filepath.Join(root, "leak")
	if err := os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/", root))

	preopens := s.ctxPreopens()
	if len(preopens) != 1 {
		t.Fatalf("preopen count = %d, want 1", len(preopens))
	}
	if got := preopenGuestPathClean(&preopens[0]); got != "/" {
		t.Fatalf("preopen guest path = %q, want /", got)
	}

	const (
		dirfd        int32 = 3
		fdPtr        int32 = 80
		pathLeakOff  int32 = 100
		pathLegitOff       = 250
		iovPtr       int32 = 350
		nreadPtr     int32 = 360
		readBufOff   int32 = 400
	)

	linkGuest := "leak"
	_, _, wmErrno := writableMountHostPaths(&preopens[0], linkGuest, true)
	if !acceptableSymlinkConfinementErr(wmErrno) {
		t.Fatalf("writableMountHostPaths(SYMLINK_FOLLOW, %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, wmErrno, wasiENotCap, wasiENoEnt)
	}

	const fdSentinel uint32 = 0xfeedd00f
	binary.LittleEndian.PutUint32(buf[fdPtr:], fdSentinel)

	copy(buf[pathLeakOff:], linkGuest)
	errno := s.Xpath_open(dirfd, wasiLookupSymlinkFollow, pathLeakOff, int32(len(linkGuest)), 0,
		int64(rightFDRead), 0, 0, fdPtr)
	if errno == wasiESuccess {
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		binary.LittleEndian.PutUint32(buf[iovPtr:], uint32(readBufOff))
		binary.LittleEndian.PutUint32(buf[iovPtr+4:], 64)
		_ = s.Xfd_read(fd, iovPtr, 1, nreadPtr)
		n := binary.LittleEndian.Uint32(buf[nreadPtr : nreadPtr+4])
		if n > 0 && string(buf[readBufOff:readBufOff+int32(n)]) == string(secretContent) {
			t.Fatalf("path_open(follow %q) returned fd that read outside sandbox", linkGuest)
		}
		t.Fatalf("path_open(follow %q) = ESUCCESS (%d), want ENOTCAPABLE or ENOENT", linkGuest, errno)
	}
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_open(follow %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, errno, wasiENotCap, wasiENoEnt)
	}
	if got := binary.LittleEndian.Uint32(buf[fdPtr:]); got != fdSentinel {
		t.Fatalf("fd slot written (%d) though open failed; want sentinel %d", got, fdSentinel)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(secretContent) {
		t.Fatalf("outside file modified: got %q, want %q", gotSecret, secretContent)
	}

	legitGuest := "legit.txt"
	copy(buf[pathLegitOff:], legitGuest)
	errno = s.Xpath_open(dirfd, 0, pathLegitOff, int32(len(legitGuest)), 0, int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(%q) = %d, want ESUCCESS (legitimate path should still work)", legitGuest, errno)
	}
}

func TestWritableHostPreopenRejectsSymlinkEscapeToOutsidePath(t *testing.T) {
	// A symlink fully inside the guest-visible tree must not grant access to paths
	// outside the preopened host root. Operations that would follow the symlink
	// into ../secret must return ENOTCAPABLE or ENOENT; the outside file must stay
	// untouched, and ordinary files under the root remain reachable.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	legitPath := filepath.Join(root, "legit.txt")
	if err := os.WriteFile(legitPath, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkName := filepath.Join(root, "leak")
	if err := os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	const (
		dirfd        int32 = 3
		fdPtr        int32 = 80
		pathLeakOff  int32 = 100
		pathLegitOff       = 250
		statPtr      int32 = 400
		offOld       int32 = 500
		offNew       int32 = 600
	)

	linkGuest := "leak"
	copy(buf[pathLeakOff:], linkGuest)

	errno := s.Xpath_open(dirfd, wasiLookupSymlinkFollow, pathLeakOff, int32(len(linkGuest)), 0,
		int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_open(write through symlink %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, errno, wasiENotCap, wasiENoEnt)
	}

	errno = s.Xpath_filestat_get(dirfd, wasiLookupSymlinkFollow, pathLeakOff, int32(len(linkGuest)), statPtr)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_filestat_get(follow symlink %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, errno, wasiENotCap, wasiENoEnt)
	}

	copy(buf[offOld:], linkGuest)
	newName := "renamed_leak"
	copy(buf[offNew:], newName)

	errno = s.Xpath_rename(dirfd, offOld, int32(len(linkGuest)), dirfd, offNew, int32(len(newName)))
	// path_rename does not follow symlinks; it renames the symlink inode within the preopen.
	if errno != wasiESuccess {
		t.Fatalf("path_rename(symlink %q) = %d, want ESUCCESS", linkGuest, errno)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != "top secret" {
		t.Fatalf("outside file was created or modified: got %q", gotSecret)
	}

	legitGuest := "legit.txt"
	copy(buf[pathLegitOff:], legitGuest)
	errno = s.Xpath_open(dirfd, 0, pathLegitOff, int32(len(legitGuest)), 0, int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(%q) = %d, want ESUCCESS (legitimate path should still work)", legitGuest, errno)
	}
}

func TestPathOpenWithoutSymlinkFollowReturnsLoopForEscapeSymlinkWithoutTouchingOutsideFile(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(outsideDir, "secret.txt")
	secretContent := []byte("outside payload")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}

	legitPath := filepath.Join(root, "legit.txt")
	if err := os.WriteFile(legitPath, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	confinedLink := filepath.Join(root, "to_legit")
	if err := os.Symlink("legit.txt", confinedLink); err != nil {
		t.Fatal(err)
	}

	escapeLink := filepath.Join(root, "to_outside")
	if err := os.Symlink("../outside/secret.txt", escapeLink); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	const (
		dirfd           int32 = 3
		fdPtrConfined   int32 = 70
		fdPtrEscape     int32 = 80
		pathConfinedOff int32 = 100
		pathEscapeOff   int32 = 200
	)

	confinedGuest := "to_legit"
	copy(buf[pathConfinedOff:], confinedGuest)
	errno := s.Xpath_open(dirfd, wasiLookupSymlinkFollow, pathConfinedOff, int32(len(confinedGuest)),
		0, int64(rightFDRead), 0, 0, fdPtrConfined)
	if errno != wasiESuccess {
		t.Fatalf("path_open(follow confined symlink %q) = %d, want ESUCCESS (%d)",
			confinedGuest, errno, wasiESuccess)
	}

	escapeGuest := "to_outside"
	copy(buf[pathEscapeOff:], escapeGuest)
	const fdSentinel uint32 = 0xfeedd00f
	binary.LittleEndian.PutUint32(buf[fdPtrEscape:], fdSentinel)

	errno = s.Xpath_open(dirfd, 0, pathEscapeOff, int32(len(escapeGuest)), 0,
		int64(rightFDRead), 0, 0, fdPtrEscape)
	if errno != wasiELoop {
		t.Fatalf("path_open(no-follow escape symlink %q) = %d, want ELOOP (%d)",
			escapeGuest, errno, wasiELoop)
	}
	if got := binary.LittleEndian.Uint32(buf[fdPtrEscape:]); got != fdSentinel {
		t.Fatalf("fd slot at fdPtr was written (%d) though open should fail; want sentinel %d",
			got, fdSentinel)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(secretContent) {
		t.Fatalf("outside file was read or modified: got %q, want %q", gotSecret, secretContent)
	}
}

const wasiFiletypeSymbolicLink uint64 = 7 // __WASI_FILETYPE_SYMBOLIC_LINK (preview1)

func TestPathFilestatGetWithoutSymlinkFollowReflectsSymlinkInodeNotEscapedTarget(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const outsideSize = 50_000
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, bytes.Repeat([]byte("Z"), outsideSize), 0o644); err != nil {
		t.Fatal(err)
	}

	escapeLink := filepath.Join(root, "link")
	if err := os.Symlink("../outside/secret.txt", escapeLink); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	const (
		dirfd     int32 = 3
		pathOff   int32 = 120
		statOff   int32 = 400
		linkGuest       = "link"
	)

	copy(buf[pathOff:], linkGuest)
	errno := s.Xpath_filestat_get(dirfd, wasiLookupSymlinkFollow, pathOff, int32(len(linkGuest)), statOff)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_filestat_get(follow escape symlink %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, errno, wasiENotCap, wasiENoEnt)
	}

	errno = s.Xpath_filestat_get(dirfd, 0, pathOff, int32(len(linkGuest)), statOff)
	if errno != wasiESuccess {
		t.Fatalf("path_filestat_get(no-follow on symlink %q) = %d, want ESUCCESS (%d)",
			linkGuest, errno, wasiESuccess)
	}

	filetype := binary.LittleEndian.Uint64(buf[statOff+16 : statOff+24])
	if filetype != wasiFiletypeSymbolicLink {
		t.Fatalf("filestat filetype = %d, want symbolic link (%d)", filetype, wasiFiletypeSymbolicLink)
	}
	stSize := int64(binary.LittleEndian.Uint64(buf[statOff+32 : statOff+40]))
	if stSize == outsideSize {
		t.Fatalf("filestat size matches outside target (%d); expected symlink inode metadata, not the escaped file", outsideSize)
	}
}

func TestPathCreateDirectoryRejectsSymlinkEscapePrefix(t *testing.T) {
	// A path mutation through an in-root symlink that escapes the preopen must not
	// create directories outside hostRoot. path_create_directory on "leak/evildir"
	// when leak points to ../secret.txt must return ENOTCAPABLE or ENOENT. The
	// outside tree must remain untouched.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	secretContent := []byte("top secret")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}

	linkName := filepath.Join(root, "leak")
	if err := os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	const (
		dirfd   int32 = 3
		pathOff int32 = 100
	)

	guestPath := "leak/evildir"
	copy(buf[pathOff:], guestPath)

	errno := s.Xpath_create_directory(dirfd, pathOff, int32(len(guestPath)))
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_create_directory(%q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			guestPath, errno, wasiENotCap, wasiENoEnt)
	}

	outsideEvil := filepath.Join(tmpDir, "evildir")
	if _, err := os.Stat(outsideEvil); err == nil {
		t.Fatalf("directory created outside preopen root at %q", outsideEvil)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(secretContent) {
		t.Fatalf("outside file was modified: got %q, want %q", gotSecret, secretContent)
	}
}

func TestPathFilestatSetTimesRejectsSymlinkEscapeFollow(t *testing.T) {
	// path_filestat_set_times with LOOKUP_SYMLINK_FOLLOW on a single-segment escape
	// symlink must not touch the outside target. With leak pointing to ../secret.txt, it must return
	// ENOTCAPABLE or ENOENT; outside file content and mtimes stay unchanged.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	secretContent := []byte("top secret")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(secretPath, past, past); err != nil {
		t.Fatal(err)
	}
	fi0, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	origMtime := fi0.ModTime()

	linkName := filepath.Join(root, "leak")
	if err = os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	const (
		dirfd   int32 = 3
		pathOff int32 = 100
	)

	guestPath := "leak"
	copy(buf[pathOff:], guestPath)

	errno := s.Xpath_filestat_set_times(dirfd, wasiLookupSymlinkFollow, pathOff, int32(len(guestPath)), 0, 0, fstMtimNow)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_filestat_set_times(follow %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			guestPath, errno, wasiENotCap, wasiENoEnt)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(secretContent) {
		t.Fatalf("outside file was modified: got %q, want %q", gotSecret, secretContent)
	}

	fi1, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.ModTime().Equal(origMtime) {
		t.Fatalf("outside file mtime changed: was %v, now %v", origMtime, fi1.ModTime())
	}
}

type symlinkEscapePrefixSetup struct {
	s             *State
	buf           []byte
	tmpDir        string
	root          string
	secretPath    string
	secretContent []byte
}

const (
	symlinkEscapeDirfd     int32 = 3
	symlinkEscapePathOff   int32 = 100
	symlinkEscapePath2Off  int32 = 200
	symlinkEscapeTargetOff int32 = 300
	symlinkEscapeBufOff    int32 = 400
	symlinkEscapeNreadOff  int32 = 500
)

func setupSymlinkEscapePrefixFixture(t *testing.T) symlinkEscapePrefixSetup {
	t.Helper()

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	secretContent := []byte("top secret")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}

	linkName := filepath.Join(root, "leak")
	if err := os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	return symlinkEscapePrefixSetup{
		s:             s,
		buf:           buf,
		tmpDir:        tmpDir,
		root:          root,
		secretPath:    secretPath,
		secretContent: secretContent,
	}
}

func assertSymlinkEscapeOutsideUntouched(t *testing.T, setup symlinkEscapePrefixSetup) {
	t.Helper()

	gotSecret, err := os.ReadFile(setup.secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(setup.secretContent) {
		t.Fatalf("outside file was modified: got %q, want %q", gotSecret, setup.secretContent)
	}
}

func assertAcceptableSymlinkConfinementErr(t *testing.T, op, guestPath string, errno int32) {
	t.Helper()
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("%s(%q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			op, guestPath, errno, wasiENotCap, wasiENoEnt)
	}
}

func TestPathMutationsRejectSymlinkEscapePrefix(t *testing.T) {
	// Multi-segment guest paths through an in-root escape symlink, leak pointing to ../secret.txt,
	// must not mutate the host tree outside the preopen root.

	guestPaths := []struct {
		name string
		p    string
	}{
		{name: "leak_evildir", p: "leak/evildir"},
		{name: "leak_sub", p: "leak/sub"},
	}

	tests := []struct {
		name string
		run  func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string)
	}{
		{
			name: "path_unlink_file",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_unlink_file(symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)))
				assertAcceptableSymlinkConfinementErr(t, "path_unlink_file", guestPath, errno)
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_remove_directory",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				outsideDir := filepath.Join(setup.tmpDir, "evildir")
				if err := os.Mkdir(outsideDir, 0o755); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_remove_directory(symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)))
				assertAcceptableSymlinkConfinementErr(t, "path_remove_directory", guestPath, errno)
				if _, err := os.Stat(outsideDir); err != nil {
					t.Fatalf("outside directory was removed: %v", err)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_rename",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				const srcPath = "rename_src"
				if err := os.WriteFile(filepath.Join(setup.root, srcPath), []byte("src"), 0o644); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapePathOff:], srcPath)
				copy(setup.buf[symlinkEscapePath2Off:], guestPath)
				errno := setup.s.Xpath_rename(
					symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(srcPath)),
					symlinkEscapeDirfd, symlinkEscapePath2Off, int32(len(guestPath)),
				)
				assertAcceptableSymlinkConfinementErr(t, "path_rename", guestPath, errno)
				if _, err := os.Stat(filepath.Join(setup.root, srcPath)); err != nil {
					t.Fatalf("rename_src was moved despite escape rejection: %v", err)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_symlink",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				const target = "inside_target"
				if err := os.WriteFile(filepath.Join(setup.root, target), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapeTargetOff:], target)
				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_symlink(
					symlinkEscapeTargetOff, int32(len(target)),
					symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)),
				)
				assertAcceptableSymlinkConfinementErr(t, "path_symlink", guestPath, errno)
				outsideMarker := filepath.Join(setup.tmpDir, filepath.Base(guestPath))
				if _, err := os.Lstat(outsideMarker); err == nil {
					t.Fatalf("symlink was created outside preopen root at %q", outsideMarker)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_readlink",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_readlink(
					symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)),
					symlinkEscapeBufOff, 256, symlinkEscapeNreadOff,
				)
				assertAcceptableSymlinkConfinementErr(t, "path_readlink", guestPath, errno)
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_link",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				const oldPath = "link_src"
				if err := os.WriteFile(filepath.Join(setup.root, oldPath), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapePathOff:], oldPath)
				copy(setup.buf[symlinkEscapePath2Off:], guestPath)
				errno := setup.s.Xpath_link(
					symlinkEscapeDirfd, 0, symlinkEscapePathOff, int32(len(oldPath)),
					symlinkEscapeDirfd, symlinkEscapePath2Off, int32(len(guestPath)),
				)
				assertAcceptableSymlinkConfinementErr(t, "path_link", guestPath, errno)
				outsideMarker := filepath.Join(setup.tmpDir, filepath.Base(guestPath))
				if _, err := os.Lstat(outsideMarker); err == nil {
					t.Fatalf("hard link was created outside preopen root at %q", outsideMarker)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
	}

	for _, gp := range guestPaths {
		for _, tt := range tests {
			t.Run(gp.name+"/"+tt.name, func(t *testing.T) {
				setup := setupSymlinkEscapePrefixFixture(t)
				tt.run(t, setup, gp.p)
			})
		}
	}
}

// TestPathMutationsAllowSymlinkInTree verifies that path_create_directory succeeds for
// legitimate paths under a writable preopen when in-tree symlinks stay confined (no escape).
func TestPathMutationsAllowSymlinkInTree(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// In-tree symlink: stays under preopen root (not an escape).
	if err := os.Symlink("subdir", filepath.Join(root, "to_subdir")); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	const (
		dirfd   int32 = 3
		pathOff int32 = 100
	)

	t.Run("single_segment_allowed", func(t *testing.T) {
		const guestPath = "allowed"
		copy(buf[pathOff:], guestPath)
		errno := s.Xpath_create_directory(dirfd, pathOff, int32(len(guestPath)))
		if errno != wasiESuccess {
			t.Fatalf("path_create_directory(%q) = %d, want ESUCCESS (%d)", guestPath, errno, wasiESuccess)
		}
		hostPath := filepath.Join(root, guestPath)
		if _, err := os.Stat(hostPath); err != nil {
			t.Fatalf("host directory %q missing after create: %v", hostPath, err)
		}
	})

	t.Run("multi_segment_subdir_nested", func(t *testing.T) {
		const guestPath = "subdir/nested"
		copy(buf[pathOff:], guestPath)
		errno := s.Xpath_create_directory(dirfd, pathOff, int32(len(guestPath)))
		if errno != wasiESuccess {
			t.Fatalf("path_create_directory(%q) = %d, want ESUCCESS (%d)", guestPath, errno, wasiESuccess)
		}
		hostPath := filepath.Join(root, "subdir", "nested")
		if _, err := os.Stat(hostPath); err != nil {
			t.Fatalf("host directory %q missing after create: %v", hostPath, err)
		}
	})
}

// TestPathLinkToSymlinkLoopOnNestedDirfd matches wasi-testsuite path_link: a scratch
// directory opened as a non-preopen dirfd, single-segment guest paths, symlink loop.
func TestPathLinkToSymlinkLoopOnNestedDirfd(t *testing.T) {
	const (
		preopenFd   int32 = 3
		scratchOff  int32 = 100
		nestedFdPtr int32 = 200
		symlinkOff  int32 = 300
		linkOff     int32 = 400
	)

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	s := New(func() []byte { return buf }, NewModuleConfig().WithHostDirectoryPreopen("/data", root))

	const scratch = "scratch"
	copy(buf[scratchOff:], scratch)
	if errno := s.Xpath_create_directory(preopenFd, scratchOff, int32(len(scratch))); errno != wasiESuccess {
		t.Fatalf("path_create_directory(%q) = %d, want ESUCCESS", scratch, errno)
	}
	copy(buf[scratchOff:], scratch)
	if errno := s.Xpath_open(preopenFd, 0, scratchOff, int32(len(scratch)),
		int32(oflagDir), int64(rightsWritableDirPreopen), int64(rightFDRead), 0, nestedFdPtr); errno != wasiESuccess {
		t.Fatalf("path_open(%q) = %d, want ESUCCESS", scratch, errno)
	}
	nestedFd := int32(binary.LittleEndian.Uint32(buf[nestedFdPtr : nestedFdPtr+4]))

	copy(buf[symlinkOff:], "symlink")
	if errno := s.Xpath_symlink(symlinkOff, 7, nestedFd, symlinkOff, 7); errno != wasiESuccess {
		t.Fatalf("path_symlink(symlink loop) = %d, want ESUCCESS", errno)
	}

	copy(buf[linkOff:], "link")
	if errno := s.Xpath_link(nestedFd, 0, symlinkOff, 7, nestedFd, linkOff, 4); errno != wasiESuccess {
		t.Fatalf("path_link(symlink loop on nested dirfd) = %d, want ESUCCESS", errno)
	}

	hostLink := filepath.Join(root, scratch, "link")
	if fi, err := os.Lstat(hostLink); err != nil {
		t.Fatalf("hard link %q: %v", hostLink, err)
	} else if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("hard link %q is not a symlink inode", hostLink)
	}
}
