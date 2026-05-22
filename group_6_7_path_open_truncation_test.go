package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestPathOpenTruncationUsesPathFilestatSetSizeRight verifies O_TRUNC requires
// PATH_FILESTAT_SET_SIZE in the directory preopen rights_base, not
// FD_FILESTAT_SET_SIZE in rights_inheriting (mirrors truncation_rights.rs).
func TestPathOpenTruncationUsesPathFilestatSetSizeRight(t *testing.T) {
	const (
		pathOff int32 = 500
		fdPtr   int32 = 600
		statOff int32 = 700
		statPtr int32 = 800
	)

	s, buf := newTestState()
	hostDir := t.TempDir()
	baseMask := wasiTestsuiteDirectoryBaseRightsMask()
	inheritingMask := wasiTestsuiteDirectoryInheritingRightsMask()
	setupMountAtDirfd(t, s, mountEntry{guestPath: "tmp", writable: true, hostRoot: hostDir}, baseMask, inheritingMask)

	if errno := s.Xfd_fdstat_get(dirfd, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(preopen) = %d, want ESUCCESS", errno)
	}
	gotBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	// truncation_rights.rs skips when PATH_FILESTAT_SET_SIZE is absent from base.
	if gotBase&rightPathFilestatSetSize == 0 {
		t.Skip("preopen rights_base lacks PATH_FILESTAT_SET_SIZE; cannot assert truncation gating")
	}

	const fname = "file"
	if err := os.WriteFile(filepath.Join(hostDir, fname), nil, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", fname, err)
	}
	pathOffWritten, pathLen := writePath(buf, pathOff, fname)

	openRights := int64(0)
	assertTruncSucceedsSizeZero := func(label string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(hostDir, fname), []byte("content"), 0o644); err != nil {
			t.Fatalf("%s: WriteFile failed: %v", label, err)
		}
		errno := s.Xpath_open(dirfd, 0, pathOffWritten, pathLen, int32(oflagTrunc),
			openRights, 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("%s: Xpath_open(O_TRUNC) = %d, want ESUCCESS", label, errno)
		}
		if errno := s.Xpath_filestat_get(dirfd, 0, pathOffWritten, pathLen, statOff); errno != wasiESuccess {
			t.Fatalf("%s: Xpath_filestat_get = %d, want ESUCCESS", label, errno)
		}
		size := binary.LittleEndian.Uint64(buf[statOff+filestatSizeOff : statOff+filestatSizeOff+8])
		if size != 0 {
			t.Errorf("%s: path_filestat_get size after O_TRUNC = %d, want 0", label, size)
		}
	}

	assertTruncSucceedsSizeZero("baseline")

	// O_TRUNC still succeeds after FD_FILESTAT_SET_SIZE is dropped from inheriting.
	gotInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	newInh := gotInh &^ rightFDFilestatSetSize
	if errno := s.Xfd_fdstat_set_rights(dirfd, int64(gotBase), int64(newInh)); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_set_rights(drop FD_FILESTAT_SET_SIZE from inheriting) = %d, want ESUCCESS", errno)
	}
	assertTruncSucceedsSizeZero("without FD_FILESTAT_SET_SIZE in inheriting")

	// O_TRUNC fails once PATH_FILESTAT_SET_SIZE is dropped from base.
	newBase := gotBase &^ rightPathFilestatSetSize
	if errno := s.Xfd_fdstat_set_rights(dirfd, int64(newBase), int64(newInh)); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_set_rights(drop PATH_FILESTAT_SET_SIZE from base) = %d, want ESUCCESS", errno)
	}
	if err := os.WriteFile(filepath.Join(hostDir, fname), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	errno := s.Xpath_open(dirfd, 0, pathOffWritten, pathLen, int32(oflagTrunc),
		openRights, 0, 0, fdPtr)
	if errno != wasiEPerm && errno != wasiENotCap {
		t.Fatalf("Xpath_open(O_TRUNC) without PATH_FILESTAT_SET_SIZE in base = %d, want EPERM(%d) or ENOTCAPABLE(%d)",
			errno, wasiEPerm, wasiENotCap)
	}
}
