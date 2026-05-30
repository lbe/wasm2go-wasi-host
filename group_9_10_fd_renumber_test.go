package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
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

	prevDestFE, _ := s.lookupFD(fdTo)
	var prevDestFile wasi.File
	if prevDestFE != nil {
		prevDestFile = prevDestFE.File
	}

	// Call Xfd_renumber(fd_from, fd_to) should return ESUCCESS.
	if errno := s.Xfd_renumber(fdFrom, fdTo); errno != wasiESuccess {
		t.Fatalf("Xfd_renumber(fdFrom=%d, fdTo=%d) = %d, want ESUCCESS", fdFrom, fdTo, errno)
	}

	if prevDestFile != nil {
		if _, errno := prevDestFile.Read(make([]byte, 1)); errno == 0 {
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

func TestFdRenumberOverPreopen(t *testing.T) {
	const (
		pathOff = 2000
		fdPtr   = 1000
	)
	s, buf, tmpDir := newWMState(t)
	// Mimic open_scratch_directory: path_open(".") on preopen fd 3.
	copy(buf[pathOff:], ".")
	errno := s.Xpath_open(3, 0, pathOff, 1, int32(oflagDir), int64(rightFDRead|rightFDReaddir), int64(rightFDRead), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(.) = %d", errno)
	}
	baseDirFd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
	// Mimic create_tmp_dir: mkdir + path_open subdir with directory rights.
	const dirName = "overwrite_preopen_dir.cleanup"
	copy(buf[pathOff:], dirName)
	if errno = s.Xpath_create_directory(baseDirFd, pathOff, int32(len(dirName))); errno != wasiESuccess {
		t.Fatalf("Xpath_create_directory = %d", errno)
	}
	errno = s.Xpath_open(baseDirFd, 0, pathOff, int32(len(dirName)), int32(oflagDir),
		int64(rightsWritableDirPreopen), int64(rightFDRead|rightFDWrite|rightFDReaddir), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(subdir) = %d", errno)
	}
	dirFd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
	_ = tmpDir
	if errno := s.Xfd_renumber(dirFd, 3); errno != wasiESuccess {
		t.Fatalf("Xfd_renumber(dirFd, preopen) = %d, want ESUCCESS", errno)
	}
}

// TestFdRenumberToHighEmptySlot verifies fd_renumber can target an unallocated
// fd >= 64; FileTable grows via InsertAt rather than rejecting high slots.
func TestFdRenumberToHighEmptySlot(t *testing.T) {
	t.Parallel()
	s, _ := newTestState(nil)
	const (
		fdFrom int32 = 5
		fdTo   int32 = 100
	)
	insertStubFD(s, fdFrom, wrapStubFSFile(stubFSFile{}), rightFDRead)
	if errno := s.Xfd_renumber(fdFrom, fdTo); errno != wasiESuccess {
		t.Fatalf("Xfd_renumber(%d, %d) = %d, want ESUCCESS", fdFrom, fdTo, errno)
	}
	if _, ok := s.lookupFD(fdFrom); ok {
		t.Errorf("fd %d still open after renumber", fdFrom)
	}
	if _, ok := s.lookupFD(fdTo); !ok {
		t.Errorf("fd %d not open after renumber to empty high slot", fdTo)
	}
}
