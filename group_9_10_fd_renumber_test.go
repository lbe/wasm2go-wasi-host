package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestFdRenumberClosesDestination covers fd_renumber: closes the destination fd,
// copies the source fd table entry, and invalidates the source fd.
func TestFdRenumberClosesDestination(t *testing.T) {
	t.Parallel()
	const (
		fdPtr   = 1000
		pathOff = 2000
		statPtr = 3000
	)

	// Create a state with a writable preopen at "/tmp" backed by a temp dir.
	s, buf, tmpDir := newWMState(t)

	// Create two files with FD_READ|FD_WRITE using path_open CREAT.
	// We'll use fd 4 and fd 5 for the source and destination.
	const (
		file1 = "file1.txt"
		file2 = "file2.txt"
	)
	copy(buf[pathOff:], file1)
	if errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(file1)), int32(oflagCreat), int64(rightFDRead|rightFDWrite), 0, 0, fdPtr); errno != wasiESuccess {
		t.Fatalf("Xpath_open(%q) = %d, want ESUCCESS", file1, errno)
	}
	fdFrom := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	copy(buf[pathOff:], file2)
	if errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(file2)), int32(oflagCreat), int64(rightFDRead|rightFDWrite), 0, 0, fdPtr); errno != wasiESuccess {
		t.Fatalf("Xpath_open(%q) = %d, want ESUCCESS", file2, errno)
	}
	fdTo := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// Record fdstat of source fd (fd_from).
	if errno := s.Xfd_fdstat_get(fdFrom, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(fdFrom) = %d", errno)
	}
	recordedType := buf[statPtr]
	recordedFlags := binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4])
	recordedRightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	recordedRightsInheriting := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])

	prevDestFile := s.fds[fdTo].file

	// Call Xfd_renumber(fd_from, fd_to) should return ESUCCESS.
	if errno := s.Xfd_renumber(fdFrom, fdTo); errno != wasiESuccess {
		t.Fatalf("Xfd_renumber(fdFrom=%d, fdTo=%d) = %d, want ESUCCESS", fdFrom, fdTo, errno)
	}

	if prevDestFile != nil {
		if _, err := prevDestFile.Read(make([]byte, 1)); err == nil {
			t.Fatal("destination open file still readable after renumber; fd_to was not closed")
		}
	}

	// Call Xfd_close(fd_from) should return EBADF (source fd invalidated).
	if errno := s.Xfd_close(fdFrom); errno != wasiEBadf {
		t.Fatalf("Xfd_close(fdFrom) = %d, want EBADF", errno)
	}

	// Call Xfd_fdstat_get(fd_to) should match the recorded fdstat.
	if errno := s.Xfd_fdstat_get(fdTo, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(fdTo) = %d", errno)
	}
	if gotType := buf[statPtr]; gotType != recordedType {
		t.Errorf("fd_to.fs_filetype = %d, want %d", gotType, recordedType)
	}
	if gotFlags := binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4]); gotFlags != recordedFlags {
		t.Errorf("fd_to.fs_flags = %d, want %d", gotFlags, recordedFlags)
	}
	if gotRightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16]); gotRightsBase != recordedRightsBase {
		t.Errorf("fd_to.fs_rights_base = 0x%x, want 0x%x", gotRightsBase, recordedRightsBase)
	}
	if gotRightsInheriting := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24]); gotRightsInheriting != recordedRightsInheriting {
		t.Errorf("fd_to.fs_rights_inheriting = 0x%x, want 0x%x", gotRightsInheriting, recordedRightsInheriting)
	}

	// Call Xfd_renumber(fd_to, fd_from) should return EBADF (destination already closed).
	if errno := s.Xfd_renumber(fdTo, fdFrom); errno != wasiEBadf {
		t.Fatalf("Xfd_renumber(fdTo, fdFrom) = %d, want EBADF", errno)
	}

	// Call Xfd_close(fd_to) should return ESUCCESS.
	if errno := s.Xfd_close(fdTo); errno != wasiESuccess {
		t.Fatalf("Xfd_close(fdTo) = %d, want ESUCCESS", errno)
	}

	// Verify host files are still unlinkable (host file system operations still work).
	// The files should still exist and be removable.
	if _, err := os.Stat(filepath.Join(tmpDir, file1)); err != nil {
		t.Fatalf("file1.txt missing after fd_renumber: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, file2)); err != nil {
		t.Fatalf("file2.txt missing after fd_renumber: %v", err)
	}
	// Clean up by removing the files.
	if err := os.Remove(filepath.Join(tmpDir, file1)); err != nil {
		t.Fatalf("failed to remove file1.txt: %v", err)
	}
	if err := os.Remove(filepath.Join(tmpDir, file2)); err != nil {
		t.Fatalf("failed to remove file2.txt: %v", err)
	}
}
