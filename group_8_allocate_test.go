package wasihost

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// TestFdAllocate verifies Xfd_allocate (fd_allocate / fallocate) behavior.
// The function should extend the file via Truncate when offset+length exceeds
// the current size, and leave it unchanged when the range is already within
// the current allocation.
func TestFdAllocate(t *testing.T) {
	const (
		fdPtr   = 1000
		pathOff = 2000
		statPtr = 3000
	)

	s, buf, tmpDir := newWMState(t)

	// Create an empty file.
	fname := "alloc.txt"
	if err := os.WriteFile(filepath.Join(tmpDir, fname), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	// Open with write + allocate rights. rightFDAllocate is in
	// rightsWritableDirPreopenInheriting and will be preserved by
	// fileRightsForOpen when explicitly requested.
	copy(buf[pathOff:], fname)
	errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
		int32(oflagTrunc), int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open = %d", errno)
	}
	fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// getSize returns the file size from Xfd_filestat_get.
	getSize := func() int64 {
		t.Helper()
		if errno := s.Xfd_filestat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get = %d", errno)
		}
		return int64(binary.LittleEndian.Uint64(buf[statPtr+filestatSizeOff : statPtr+filestatSizeOff+8]))
	}

	// Verify initial size is 0.
	if size := getSize(); size != 0 {
		t.Fatalf("initial file size = %d, want 0", size)
	}

	// 1. allocate(0, 100) on empty file -> size = 100
	t.Run("allocate from zero extends file", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 0, 100)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(0, 100) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 100 {
			t.Errorf("size after allocate(0,100) = %d, want 100", size)
		}
	})

	// 2. allocate(10, 10) within existing size -> no growth, size stays 100
	t.Run("allocate within existing size does not grow", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 10, 10)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(10, 10) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 100 {
			t.Errorf("size after allocate(10,10) = %d, want 100", size)
		}
	})

	// 3. allocate(90, 20) -> offset+length=110 > 100, size=110
	t.Run("allocate beyond existing size grows file", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 90, 20)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(90, 20) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 110 {
			t.Errorf("size after allocate(90,20) = %d, want 110", size)
		}
	})

	// 4. allocate(100, 100) -> offset+length=200 > 110, size=200
	t.Run("allocate far beyond existing size grows file further", func(t *testing.T) {
		errno := s.Xfd_allocate(fd, 100, 100)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_allocate(100, 100) = %d, want ESUCCESS", errno)
		}
		if size := getSize(); size != 200 {
			t.Errorf("size after allocate(100,100) = %d, want 200", size)
		}
	})

	// 5. invalid offset/length returns EINVAL and does not change the file.
	t.Run("negative offset returns EINVAL", func(t *testing.T) {
		s2, buf2, tmp2 := newWMState(t)
		if err := os.WriteFile(filepath.Join(tmp2, "bad-off.txt"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		const p = 4100
		copy(buf2[p:], "bad-off.txt")
		errno := s2.Xpath_open(dirfd, 0, p, 11, int32(oflagTrunc),
			int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d", errno)
		}
		badFd := int32(binary.LittleEndian.Uint32(buf2[fdPtr : fdPtr+4]))
		errno = s2.Xfd_allocate(badFd, -1, 10)
		if errno != wasiEInval {
			t.Errorf("Xfd_allocate(-1, 10) = %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	t.Run("negative length returns EINVAL", func(t *testing.T) {
		s2, buf2, tmp2 := newWMState(t)
		if err := os.WriteFile(filepath.Join(tmp2, "bad-len.txt"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		const p = 4200
		copy(buf2[p:], "bad-len.txt")
		errno := s2.Xpath_open(dirfd, 0, p, 11, int32(oflagTrunc),
			int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d", errno)
		}
		badFd := int32(binary.LittleEndian.Uint32(buf2[fdPtr : fdPtr+4]))
		errno = s2.Xfd_allocate(badFd, 0, -1)
		if errno != wasiEInval {
			t.Errorf("Xfd_allocate(0, -1) = %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	t.Run("offset plus length overflow returns EINVAL", func(t *testing.T) {
		s2, buf2, tmp2 := newWMState(t)
		if err := os.WriteFile(filepath.Join(tmp2, "bad-ovf.txt"), []byte{}, 0644); err != nil {
			t.Fatal(err)
		}
		const p = 4300
		copy(buf2[p:], "bad-ovf.txt")
		errno := s2.Xpath_open(dirfd, 0, p, 11, int32(oflagTrunc),
			int64(rightFDWrite|rightFDAllocate), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d", errno)
		}
		badFd := int32(binary.LittleEndian.Uint32(buf2[fdPtr : fdPtr+4]))
		errno = s2.Xfd_allocate(badFd, math.MaxInt64, 1)
		if errno != wasiEInval {
			t.Errorf("Xfd_allocate(MaxInt64, 1) = %d, want EINVAL (%d)", errno, wasiEInval)
		}
	})

	// 6. fd_allocate without FD_ALLOCATE in rights_base returns ENOTCAPABLE.
	t.Run("without FD_ALLOCATE returns ENOTCAPABLE", func(t *testing.T) {
		const woFdPtr = 4000
		copy(buf[pathOff:], fname)
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
			0, int64(rightFDWrite), 0, 0, woFdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(write-only) = %d", errno)
		}
		woFd := int32(binary.LittleEndian.Uint32(buf[woFdPtr : woFdPtr+4]))
		errno = s.Xfd_fdstat_get(woFd, statPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d", errno)
		}
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
		if rightsBase&rightFDAllocate != 0 {
			t.Fatalf("rightsBase = %#x, FD_ALLOCATE must not be granted", rightsBase)
		}
		errno = s.Xfd_allocate(woFd, 0, 64)
		if errno != wasiENotCap {
			t.Errorf("Xfd_allocate without FD_ALLOCATE = %d, want ENOTCAPABLE (%d)", errno, wasiENotCap)
		}
	})

	// 7. FSFileWrap with FD_ALLOCATE returns ENOTSUP (not ENOTCAPABLE).
	t.Run("FSFileWrap returns ENOTSUP", func(t *testing.T) {
		const wrapFd = 5
		m := fstest.MapFS{"ro.txt": &fstest.MapFile{Data: []byte("hello")}}
		f, err := m.Open("ro.txt")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		s2, _ := newTestState()
		for len(s2.fds) <= wrapFd {
			s2.fds = append(s2.fds, fdEntry{})
		}
		s2.fds[wrapFd] = fdEntry{
			fdType:     fdFile,
			file:       &FSFileWrap{File: f},
			rightsBase: rightFDAllocate,
		}
		errno := s2.Xfd_allocate(wrapFd, 0, 10)
		if errno != wasiENotSup {
			t.Errorf("Xfd_allocate on FSFileWrap = %d, want ENOTSUP (%d)", errno, wasiENotSup)
		}
	})
}
