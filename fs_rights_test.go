package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

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

// TestPathOpenClampsRightsAbsentFromPreopenInheriting reproduces the actual
// wasi-testsuite failure mode: wasi-libc compiled binaries call path_open
// requesting rights that include bits absent from the preopen's inheriting
// mask (FD_DATASYNC=1<<0, FD_SYNC=1<<4, FD_TELL=1<<5, FD_ADVISE=1<<7,
// FD_ALLOCATE=1<<8). A correct runtime succeeds and returns an fd with those
// absent bits clamped away; the fd must still be usable for the rights it
// does have.
func TestPathOpenClampsRightsAbsentFromPreopenInheriting(t *testing.T) {
	t.Parallel()

	// Bits present in every wasi-libc open request that are absent from
	// rightsWritableDirPreopen (and thus from the preopen's inheriting mask).
	const (
		rightFDDatasync uint64 = 1 << 0 // __WASI_RIGHT_FD_DATASYNC
		rightFDSync     uint64 = 1 << 4 // __WASI_RIGHT_FD_SYNC
		rightFDTell     uint64 = 1 << 5 // __WASI_RIGHT_FD_TELL
		rightFDAdvise   uint64 = 1 << 7 // __WASI_RIGHT_FD_ADVISE
		rightFDAllocate uint64 = 1 << 8 // __WASI_RIGHT_FD_ALLOCATE

		absentBits = rightFDDatasync | rightFDSync | rightFDTell | rightFDAdvise | rightFDAllocate
	)

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

	// Request rights that include bits absent from the preopen's inheriting mask.
	// This is exactly what wasi-libc does: request everything, rely on runtime to cap.
	requestedBase := uint64(rightFDRead) | uint64(rightFDWrite) | absentBits
	errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fileName)), 0,
		int64(requestedBase), int64(absentBits), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open with rights absent from preopen inheriting = %d (%#x requested), want ESUCCESS; "+
			"absent bits %#x not in rightsWritableDirPreopen; runtime must clamp, not reject",
			errno, requestedBase, absentBits)
	}

	newFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	errno = s.Xfd_fdstat_get(newFD, statOff)
	if errno != wasiESuccess {
		t.Fatalf("fd_fdstat_get on clamped fd = %d", errno)
	}
	gotBase := binary.LittleEndian.Uint64(buf[statOff+8:])

	// All absent bits must have been clamped away.
	for _, tc := range []struct {
		name string
		bit  uint64
	}{
		{"FD_DATASYNC (1<<0)", rightFDDatasync},
		{"FD_SYNC (1<<4)", rightFDSync},
		{"FD_TELL (1<<5)", rightFDTell},
		{"FD_ADVISE (1<<7)", rightFDAdvise},
		{"FD_ALLOCATE (1<<8)", rightFDAllocate},
	} {
		if gotBase&tc.bit != 0 {
			t.Errorf("fd rights_base has %s set (%#x); expected clamped away (not in preopen inheriting)",
				tc.name, gotBase)
		}
	}

	// The fd must still carry FD_READ so it is usable.
	if gotBase&uint64(rightFDRead) == 0 {
		t.Errorf("fd rights_base missing FD_READ after clamping (%#x); fd should be usable for reads", gotBase)
	}

	// Confirm the fd is actually usable: a read must succeed.
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
		WithHostDirectoryPreopen("/tmp", tmpDir),
		WithReadOnlyFS("/ro", fstest.MapFS{}),
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

	var expectedWritableDirPreopen uint64
	expectedWritableDirPreopen |= wasiRightsFDRead | wasiRightsFDSeek | wasiRightsFDFdstatSetFlags | wasiRightsFDWrite
	for shift := 9; shift <= 26; shift++ {
		expectedWritableDirPreopen |= uint64(1) << shift
	}

	errno := s.Xfd_fdstat_get(dirfd, statPtr)
	if errno != wasiESuccess {
		t.Fatalf("fd_fdstat_get(preopen dir) = %d, want ESUCCESS", errno)
	}
	gotPreBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	gotPreInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	if gotPreBase != expectedWritableDirPreopen {
		t.Errorf("preopen fd_fdstat_get rights_base = %#x, want %#x (WASI preview1 bit positions)", gotPreBase, expectedWritableDirPreopen)
	}
	if gotPreInh != expectedWritableDirPreopen {
		t.Errorf("preopen fd_fdstat_get rights_inheriting = %#x, want %#x (WASI preview1 bit positions)", gotPreInh, expectedWritableDirPreopen)
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
	if gotFileInh != expectedWritableDirPreopen {
		t.Errorf("file fd_fdstat_get rights_inheriting = %#x, want %#x", gotFileInh, expectedWritableDirPreopen)
	}
}
