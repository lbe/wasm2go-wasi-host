package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestPathFilestatSymlinkFollowSemantics verifies that path_filestat_get and
// path_filestat_set_times distinguish a symlink from its target based on the
// LOOKUPFLAGS_SYMLINK_FOLLOW flag.
func TestPathFilestatSymlinkFollowSemantics(t *testing.T) {
	s, buf := newTestState()
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

	// Create symlink "symlink" -> "file".
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
