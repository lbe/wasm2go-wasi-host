package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestPathFilestatOpenFdFlagsAndSetTimes verifies path_open records requested
// fd_flags on a newly created file and path_filestat_set_times updates mtim
// observable via path_filestat_get (mirrors path_filestat.rs lines 12–60).
func TestPathFilestatOpenFdFlagsAndSetTimes(t *testing.T) {
	const (
		pathOff        int32 = 500
		fdPtr          int32 = 600
		fdStatOff      int32 = 700
		statOff        int32 = 800
		fdstatFlagsOff int32 = 2 // fdstat fs_flags field
	)

	s, buf := newTestState()
	_ = setupWritableMount(t, s, buf)

	const fname = "file"
	pathOffWritten, pathLen := writePath(buf, pathOff, fname)

	openRights := int64(rightFDRead | rightFDWrite | rightPathFilestatGet)
	requestedFlags := fdFlagsAppend | fdFlagsSync

	errno := s.Xpath_open(dirfd, 0, pathOffWritten, pathLen, int32(oflagCreat),
		openRights, 0, requestedFlags, fdPtr)
	syncRequested := true
	// Some platforms reject FDFLAGS_SYNC on path_open; retry with APPEND only.
	if errno == wasiENotSup || errno == wasiEInval {
		syncRequested = false
		requestedFlags = fdFlagsAppend
		errno = s.Xpath_open(dirfd, 0, pathOffWritten, pathLen, int32(oflagCreat),
			openRights, 0, requestedFlags, fdPtr)
	}
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(%q) = %d, want ESUCCESS", fname, errno)
	}

	fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
	if fd <= 2 {
		t.Fatalf("path_open returned fd %d, want fd > 2", fd)
	}

	// fd_fdstat_get: verify path_open honored requested fd_flags.
	if errno = s.Xfd_fdstat_get(fd, fdStatOff); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get = %d, want ESUCCESS", errno)
	}
	gotFdFlags := int32(binary.LittleEndian.Uint16(buf[fdStatOff+fdstatFlagsOff : fdStatOff+fdstatFlagsOff+2]))
	if gotFdFlags&fdFlagsAppend == 0 {
		t.Errorf("fd_fdstat_get fd_flags = %#x, want FDFLAGS_APPEND set", gotFdFlags)
	}
	if syncRequested && gotFdFlags&fdFlagsSync == 0 {
		t.Errorf("fd_fdstat_get fd_flags = %#x, want FDFLAGS_SYNC set when open with SYNC succeeded", gotFdFlags)
	}

	// path_filestat_get: baseline size and mtim for newly created file.
	if errno = s.Xpath_filestat_get(dirfd, 0, pathOffWritten, pathLen, statOff); errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get = %d, want ESUCCESS", errno)
	}
	size := binary.LittleEndian.Uint64(buf[statOff+filestatSizeOff : statOff+filestatSizeOff+8])
	if size != 0 {
		t.Errorf("path_filestat_get size = %d, want 0 for newly created file", size)
	}
	baselineMtim := int64(binary.LittleEndian.Uint64(buf[statOff+filestatMtimOff : statOff+filestatMtimOff+8]))
	if baselineMtim == 0 {
		t.Fatal("path_filestat_get mtim = 0, want non-zero baseline")
	}

	targetMtim := baselineMtim - 100
	errno = s.Xpath_filestat_set_times(dirfd, 0, pathOffWritten, pathLen, 0, targetMtim, fstMtim)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_set_times = %d, want ESUCCESS", errno)
	}

	if errno = s.Xpath_filestat_get(dirfd, 0, pathOffWritten, pathLen, statOff); errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get after set_times = %d, want ESUCCESS", errno)
	}
	gotMtim := int64(binary.LittleEndian.Uint64(buf[statOff+filestatMtimOff : statOff+filestatMtimOff+8]))
	if gotMtim != targetMtim {
		t.Errorf("path_filestat_get mtim after set_times = %d, want %d", gotMtim, targetMtim)
	}
}

// TestPathAndFdFilestatDevIno verifies that path_filestat_get and fd_filestat_get
// report the same non-zero dev and distinct ino values for different files in the
// same host-backed directory (mirrors stat-dev-ino.c and filestat_ino_test.go).
func TestPathAndFdFilestatDevIno(t *testing.T) {
	const (
		pathOffA  int32 = 100
		pathOffB  int32 = 200
		statOffA  int32 = 300
		statOffB  int32 = 400
		fdStatOff int32 = 500
		fdPtrA    int32 = 600
		fdPtrB    int32 = 604
	)

	s, buf := newTestState()
	hostDir := setupWritableMount(t, s, buf)

	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(hostDir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) failed: %v", name, err)
		}
	}

	hostA, err := os.Stat(filepath.Join(hostDir, "a.txt"))
	if err != nil {
		t.Fatalf("os.Stat(a.txt) failed: %v", err)
	}
	hostB, err := os.Stat(filepath.Join(hostDir, "b.txt"))
	if err != nil {
		t.Fatalf("os.Stat(b.txt) failed: %v", err)
	}
	hostStatA := hostA.Sys().(*syscall.Stat_t)
	hostStatB := hostB.Sys().(*syscall.Stat_t)
	if hostStatA.Ino == 0 || hostStatB.Ino == 0 {
		t.Skip("host filesystem reports st_ino == 0; cannot assert distinct non-zero ino")
	}
	if hostStatA.Dev == 0 || hostStatB.Dev == 0 {
		t.Skip("host filesystem reports st_dev == 0; cannot assert non-zero dev")
	}

	pathOffWrittenA, pathLenA := writePath(buf, pathOffA, "a.txt")
	pathOffWrittenB, pathLenB := writePath(buf, pathOffB, "b.txt")

	if errno := s.Xpath_filestat_get(dirfd, 0, pathOffWrittenA, pathLenA, statOffA); errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(a.txt) = %d, want ESUCCESS", errno)
	}
	if errno := s.Xpath_filestat_get(dirfd, 0, pathOffWrittenB, pathLenB, statOffB); errno != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(b.txt) = %d, want ESUCCESS", errno)
	}

	devA := binary.LittleEndian.Uint64(buf[statOffA+filestatDevOff : statOffA+filestatDevOff+8])
	inoA := binary.LittleEndian.Uint64(buf[statOffA+filestatInoOff : statOffA+filestatInoOff+8])
	devB := binary.LittleEndian.Uint64(buf[statOffB+filestatDevOff : statOffB+filestatDevOff+8])
	inoB := binary.LittleEndian.Uint64(buf[statOffB+filestatInoOff : statOffB+filestatInoOff+8])

	if devA == 0 {
		t.Errorf("path_filestat_get(a.txt).dev = 0, want non-zero (host st_dev = %d)", hostStatA.Dev)
	}
	if devB == 0 {
		t.Errorf("path_filestat_get(b.txt).dev = 0, want non-zero (host st_dev = %d)", hostStatB.Dev)
	}
	if devA != devB {
		t.Errorf("path_filestat_get dev mismatch: a.txt=%d b.txt=%d, want equal (same directory)", devA, devB)
	}
	if inoA == 0 {
		t.Errorf("path_filestat_get(a.txt).ino = 0, want non-zero (host st_ino = %d)", hostStatA.Ino)
	}
	if inoB == 0 {
		t.Errorf("path_filestat_get(b.txt).ino = 0, want non-zero (host st_ino = %d)", hostStatB.Ino)
	}
	if inoA == inoB {
		t.Errorf("path_filestat_get ino not distinct: a.txt=%d b.txt=%d, want different ino for different files", inoA, inoB)
	}
	if inoA != hostStatA.Ino {
		t.Errorf("path_filestat_get(a.txt).ino = %d, want %d (host st_ino)", inoA, hostStatA.Ino)
	}
	if inoB != hostStatB.Ino {
		t.Errorf("path_filestat_get(b.txt).ino = %d, want %d (host st_ino)", inoB, hostStatB.Ino)
	}

	openRights := int64(rightFDRead | rightPathFilestatGet)
	if errno := s.Xpath_open(dirfd, 0, pathOffWrittenA, pathLenA, 0, openRights, 0, 0, fdPtrA); errno != wasiESuccess {
		t.Fatalf("Xpath_open(a.txt) = %d, want ESUCCESS", errno)
	}
	if errno := s.Xpath_open(dirfd, 0, pathOffWrittenB, pathLenB, 0, openRights, 0, 0, fdPtrB); errno != wasiESuccess {
		t.Fatalf("Xpath_open(b.txt) = %d, want ESUCCESS", errno)
	}
	fdA := int32(binary.LittleEndian.Uint32(buf[fdPtrA : fdPtrA+4]))
	fdB := int32(binary.LittleEndian.Uint32(buf[fdPtrB : fdPtrB+4]))

	if errno := s.Xfd_filestat_get(fdA, fdStatOff); errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_get(a.txt fd) = %d, want ESUCCESS", errno)
	}
	fdDevA := binary.LittleEndian.Uint64(buf[fdStatOff+filestatDevOff : fdStatOff+filestatDevOff+8])
	fdInoA := binary.LittleEndian.Uint64(buf[fdStatOff+filestatInoOff : fdStatOff+filestatInoOff+8])
	if fdDevA != devA {
		t.Errorf("fd_filestat_get(a.txt).dev = %d, want %d (path_filestat_get dev)", fdDevA, devA)
	}
	if fdInoA != inoA {
		t.Errorf("fd_filestat_get(a.txt).ino = %d, want %d (path_filestat_get ino)", fdInoA, inoA)
	}

	fdStatOffB := fdStatOff + filestatSize
	if errno := s.Xfd_filestat_get(fdB, fdStatOffB); errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_get(b.txt fd) = %d, want ESUCCESS", errno)
	}
	fdDevB := binary.LittleEndian.Uint64(buf[fdStatOffB+filestatDevOff : fdStatOffB+filestatDevOff+8])
	fdInoB := binary.LittleEndian.Uint64(buf[fdStatOffB+filestatInoOff : fdStatOffB+filestatInoOff+8])
	if fdDevB != devB {
		t.Errorf("fd_filestat_get(b.txt).dev = %d, want %d (path_filestat_get dev)", fdDevB, devB)
	}
	if fdInoB != inoB {
		t.Errorf("fd_filestat_get(b.txt).ino = %d, want %d (path_filestat_get ino)", fdInoB, inoB)
	}
}

// TestFdFilestatSetSizeAndTimes verifies fd_filestat_set_size and
// fd_filestat_set_times on a file opened via path_open with CREAT update
// size and mtime without changing atime observable via fd_filestat_get.
func TestFdFilestatSetSizeAndTimes(t *testing.T) {
	const (
		pathOff int32 = 500
		fdPtr   int32 = 600
		statOff int32 = 700
	)

	s, buf := newTestState()
	_ = setupWritableMount(t, s, buf)

	const fname = "file"
	pathOffWritten, pathLen := writePath(buf, pathOff, fname)

	openRights := int64(rightFDRead | rightFDWrite | rightFDFilestatGet |
		rightFDFilestatSetSize | rightFDFilestatSetTimes)

	errno := s.Xpath_open(dirfd, 0, pathOffWritten, pathLen, int32(oflagCreat),
		openRights, 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(%q) = %d, want ESUCCESS", fname, errno)
	}

	fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
	if fd <= 2 {
		t.Fatalf("path_open returned fd %d, want fd > 2", fd)
	}

	if errno = s.Xfd_filestat_get(fd, statOff); errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_get = %d, want ESUCCESS", errno)
	}
	size := binary.LittleEndian.Uint64(buf[statOff+filestatSizeOff : statOff+filestatSizeOff+8])
	if size != 0 {
		t.Errorf("fd_filestat_get size = %d, want 0 for newly created file", size)
	}
	baselineAtim := int64(binary.LittleEndian.Uint64(buf[statOff+filestatAtimOff : statOff+filestatAtimOff+8]))
	if baselineAtim == 0 {
		t.Fatal("fd_filestat_get atim = 0, want non-zero baseline")
	}

	errno = s.Xfd_filestat_set_size(fd, 100)
	if errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_set_size(100) = %d, want ESUCCESS", errno)
	}

	if errno = s.Xfd_filestat_get(fd, statOff); errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_get after set_size = %d, want ESUCCESS", errno)
	}
	size = binary.LittleEndian.Uint64(buf[statOff+filestatSizeOff : statOff+filestatSizeOff+8])
	if size != 100 {
		t.Errorf("fd_filestat_get size after set_size = %d, want 100", size)
	}

	newMtim := baselineAtim - 100_000_000
	if newMtim <= 0 {
		newMtim = 1_000_000_000
	}

	errno = s.Xfd_filestat_set_times(fd, newMtim, newMtim, fstMtim)
	if errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_set_times = %d, want ESUCCESS", errno)
	}

	if errno = s.Xfd_filestat_get(fd, statOff); errno != wasiESuccess {
		t.Fatalf("Xfd_filestat_get after set_times = %d, want ESUCCESS", errno)
	}
	gotMtim := int64(binary.LittleEndian.Uint64(buf[statOff+filestatMtimOff : statOff+filestatMtimOff+8]))
	if gotMtim != newMtim {
		t.Errorf("fd_filestat_get mtim after set_times = %d, want %d", gotMtim, newMtim)
	}
	gotAtim := int64(binary.LittleEndian.Uint64(buf[statOff+filestatAtimOff : statOff+filestatAtimOff+8]))
	if gotAtim != baselineAtim {
		t.Errorf("fd_filestat_get atim after set_times = %d, want unchanged %d", gotAtim, baselineAtim)
	}
	size = binary.LittleEndian.Uint64(buf[statOff+filestatSizeOff : statOff+filestatSizeOff+8])
	if size != 100 {
		t.Errorf("fd_filestat_get size after set_times = %d, want 100", size)
	}
}
