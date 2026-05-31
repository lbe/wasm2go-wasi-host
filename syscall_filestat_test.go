package wasihost

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

func TestFilestatMutations(t *testing.T) {
	// Use a fixed past time far enough from "now" to distinguish clearly
	// (any real implementation would set mtime to this; stub won't)
	targetMtim := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	targetMtimNs := targetMtim.UnixNano()

	t.Run("Xfd_filestat_set_size truncates osFile to 5 bytes", func(t *testing.T) {
		s, _ := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte("ABCDEFGHIJ"))

		errno := s.Xfd_filestat_set_size(5, 5)
		if errno != wasiESuccess {
			t.Fatalf("filestat_set_size returned %d, want ESUCCESS", errno)
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 5 {
			t.Errorf("file size = %d, want 5", len(data))
		}
		fi, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() != 5 {
			t.Errorf("os.Stat size = %d, want 5", fi.Size())
		}
	})

	t.Run("Xfd_filestat_set_times fstMtim updates mtime on osFile", func(t *testing.T) {
		s, _ := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, fstMtim)
		if errno != wasiESuccess {
			t.Fatalf("filestat_set_times returned %d, want ESUCCESS", errno)
		}
		fi, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		diff := fi.ModTime().Sub(targetMtim)
		if diff < 0 {
			diff = -diff
		}
		if diff > 2*time.Second {
			t.Errorf("mtime = %v, want within 2s of %v (diff=%v)", fi.ModTime(), targetMtim, diff)
		}
	})

	t.Run("Xfd_filestat_set_times fstFlags=0 does not mutate mtime", func(t *testing.T) {
		s, _ := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte("data"))
		fi0, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		before := fi0.ModTime()

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, 0)
		if errno != wasiESuccess {
			t.Fatalf("filestat_set_times returned %d, want ESUCCESS", errno)
		}
		fi1, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		// mtime should be unchanged
		if !fi1.ModTime().Equal(before) {
			t.Errorf("mtime changed unexpectedly: before=%v after=%v", before, fi1.ModTime())
		}
	})

	t.Run("Xpath_filestat_set_times fstMtim updates mtime by path", func(t *testing.T) {
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)
		fname := "testfile.txt"
		if err := os.WriteFile(filepath.Join(hostDir, fname), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		pathOff, pathLen := writePath(buf, 500, fname)

		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, targetMtimNs, fstMtim)
		if errno != wasiESuccess {
			t.Fatalf("xpath_filestat_set_times returned %d, want ESUCCESS", errno)
		}
		fi, err := os.Stat(filepath.Join(hostDir, fname))
		if err != nil {
			t.Fatal(err)
		}
		diff := fi.ModTime().Sub(targetMtim)
		if diff < 0 {
			diff = -diff
		}
		if diff > 2*time.Second {
			t.Errorf("mtime = %v, want within 2s of %v (diff=%v)", fi.ModTime(), targetMtim, diff)
		}
	})

	t.Run("Xpath_filestat_set_times fstFlags=0 is no-op", func(t *testing.T) {
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)
		fname := "noop.txt"
		if err := os.WriteFile(filepath.Join(hostDir, fname), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		fi0, err := os.Stat(filepath.Join(hostDir, fname))
		if err != nil {
			t.Fatal(err)
		}
		before := fi0.ModTime()
		pathOff, pathLen := writePath(buf, 500, fname)

		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, targetMtimNs, 0)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_set_times fstFlags=0 = %d, want ESUCCESS", errno)
		}
		fi1, err := os.Stat(filepath.Join(hostDir, fname))
		if err != nil {
			t.Fatal(err)
		}
		if !fi1.ModTime().Equal(before) {
			t.Errorf("mtime changed: before=%v after=%v", before, fi1.ModTime())
		}
	})

	t.Run("Xfd_filestat_set_times fstMtimNow updates mtime to current", func(t *testing.T) {
		s, _ := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		// Set to a past time first to ensure NOW change is visible
		past := time.Now().Add(-1 * time.Hour)
		if err := os.Chtimes(filePath, past, past); err != nil {
			t.Fatal(err)
		}

		errno := s.Xfd_filestat_set_times(5, 0, 0, fstMtimNow)
		if errno != wasiESuccess {
			t.Fatalf("filestat_set_times returned %d, want ESUCCESS", errno)
		}

		fi, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		// mtime should be within a few seconds of "now"
		if time.Since(fi.ModTime()) > 5*time.Second {
			t.Errorf("mtime = %v, not close to now", fi.ModTime())
		}
	})

	t.Run("Xfd_filestat_set_times fstAtimNow updates atime but not mtime", func(t *testing.T) {
		s, _ := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		// Ensure we have a baseline and distinguish from "now"
		past := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
		if err := os.Chtimes(filePath, past, past); err != nil {
			t.Fatal(err)
		}

		fi0, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		mtimeBefore := fi0.ModTime()

		errno := s.Xfd_filestat_set_times(5, 0, 0, fstAtimNow)
		if errno != wasiESuccess {
			t.Fatalf("filestat_set_times returned %d, want ESUCCESS", errno)
		}

		fi1, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}

		atimeAfter := getAtimeFromStat(fi1)
		mtimeAfter := fi1.ModTime()

		// atime should be close to now
		if time.Since(atimeAfter) > 10*time.Second {
			t.Errorf("atime = %v, not close to now (since=%v)", atimeAfter, time.Since(atimeAfter))
		}

		// mtime should be unchanged
		if !mtimeAfter.Equal(mtimeBefore) {
			t.Errorf("mtime changed: before=%v after=%v", mtimeBefore, mtimeAfter)
		}
	})

	t.Run("Xpath_filestat_set_times error on nonexistent path", func(t *testing.T) {
		s, buf := newTestState(nil)
		_ = setupWritableMount(t, s, buf)
		pathOff, pathLen := writePath(buf, 500, "/nonexistent/path/deleted.txt")
		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, targetMtimNs, fstMtim)
		if errno == wasiESuccess {
			t.Error("Xpath_filestat_set_times on missing path returned ESUCCESS, want error")
		}
	})

	t.Run("Xpath_filestat_set_times fstMtimNow updates mtime to current", func(t *testing.T) {
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)
		fname := "now.txt"
		hostPath := filepath.Join(hostDir, fname)
		if err := os.WriteFile(hostPath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}

		// Set to a past time first to ensure NOW change is visible
		past := time.Now().Add(-1 * time.Hour)
		if err := os.Chtimes(hostPath, past, past); err != nil {
			t.Fatal(err)
		}

		pathOff, pathLen := writePath(buf, 500, fname)

		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, 0, fstMtimNow)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_set_times fstMtimNow returned %d, want ESUCCESS", errno)
		}

		fi, err := os.Stat(hostPath)
		if err != nil {
			t.Fatal(err)
		}
		// mtime should be within a few seconds of "now"
		if time.Since(fi.ModTime()) > 5*time.Second {
			t.Errorf("mtime = %v, not close to now", fi.ModTime())
		}
	})

	t.Run("Xpath_filestat_set_times fstAtim updates atime by path", func(t *testing.T) {
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)
		fname := "atim.txt"
		hostPath := filepath.Join(hostDir, fname)
		if err := os.WriteFile(hostPath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}

		// Set to a past time first to ensure change is visible
		past := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(hostPath, past, past); err != nil {
			t.Fatal(err)
		}

		targetAtim := time.Date(2022, 2, 2, 2, 2, 2, 0, time.UTC)
		targetAtimNs := targetAtim.UnixNano()

		pathOff, pathLen := writePath(buf, 500, fname)

		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, int64(targetAtimNs), 0, fstAtim)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_set_times fstAtim returned %d, want ESUCCESS", errno)
		}

		fi, err := os.Stat(hostPath)
		if err != nil {
			t.Fatal(err)
		}

		atimAfter := getAtimeFromStat(fi)
		mtimeAfter := fi.ModTime()

		diff := atimAfter.Sub(targetAtim)
		if diff < 0 {
			diff = -diff
		}
		// Filesystem precision might vary, but 2s is safe for most
		if diff > 2*time.Second {
			t.Errorf("atime = %v, want within 2s of %v (diff=%v)", atimAfter, targetAtim, diff)
		}

		// mtime should be unchanged (still 'past')
		if !mtimeAfter.Equal(past) {
			t.Errorf("mtime changed: got %v, want %v", mtimeAfter, past)
		}
	})

	t.Run("Xpath_filestat_set_times fstAtimNow updates atime but not mtime", func(t *testing.T) {
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)
		fname := "atim_now.txt"
		hostPath := filepath.Join(hostDir, fname)
		if err := os.WriteFile(hostPath, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}

		// Set to a past time first to ensure change is visible
		past := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
		if err := os.Chtimes(hostPath, past, past); err != nil {
			t.Fatal(err)
		}

		fi0, err := os.Stat(hostPath)
		if err != nil {
			t.Fatal(err)
		}
		mtimeBefore := fi0.ModTime()

		pathOff, pathLen := writePath(buf, 500, fname)

		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, 0, fstAtimNow)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_set_times fstAtimNow returned %d, want ESUCCESS", errno)
		}

		fi1, err := os.Stat(hostPath)
		if err != nil {
			t.Fatal(err)
		}

		atimAfter := getAtimeFromStat(fi1)
		mtimeAfter := fi1.ModTime()

		// atime should be close to now
		if time.Since(atimAfter) > 10*time.Second {
			t.Errorf("atime = %v, not close to now (since=%v)", atimAfter, time.Since(atimAfter))
		}

		// mtime should be unchanged
		if !mtimeAfter.Equal(mtimeBefore) {
			t.Errorf("mtime changed: before=%v after=%v", mtimeBefore, mtimeAfter)
		}
	})
}

func TestFdFilestatSetTimesAtim(t *testing.T) {
	s, _ := newTestState(nil)
	fd := int32(5)
	filePath := setupOsFileFd(t, s, fd, []byte("data"))

	// targetAtim is far in the past to be distinguishable
	targetAtim := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	targetAtimNs := targetAtim.UnixNano()

	// get initial mtime
	fi0, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	initialMtim := fi0.ModTime()

	// fstAtim = 1
	errno := s.Xfd_filestat_set_times(fd, targetAtimNs, 0, fstAtim)
	if errno != wasiESuccess {
		t.Fatalf("filestat_set_times returned %d, want ESUCCESS", errno)
	}

	fi1, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify ATIM
	atime := getAtimeFromStat(fi1)
	diff := atime.Sub(targetAtim)
	if diff < 0 {
		diff = -diff
	}
	if diff > 2*time.Second {
		t.Errorf("atime = %v, want within 2s of %v (diff=%v)", atime, targetAtim, diff)
	}

	// Verify MTIM remains unchanged
	if !fi1.ModTime().Equal(initialMtim) {
		t.Errorf("mtime changed unexpectedly: before=%v after=%v", initialMtim, fi1.ModTime())
	}
}

func TestFdFilestatSetTimesAtimPreservesExistingTests(t *testing.T) {
	// targetMtim is far enough from "now"
	targetMtim := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	targetMtimNs := targetMtim.UnixNano()

	t.Run("Xfd_filestat_set_times fstMtim updates mtime on osFile", func(t *testing.T) {
		s, _ := newTestState(nil)
		fd := int32(5)
		filePath := setupOsFileFd(t, s, fd, []byte("data"))

		errno := s.Xfd_filestat_set_times(fd, 0, targetMtimNs, fstMtim)
		if errno != wasiESuccess {
			t.Fatalf("filestat_set_times returned %d, want ESUCCESS", errno)
		}
		fi, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		diff := fi.ModTime().Sub(targetMtim)
		if diff < 0 {
			diff = -diff
		}
		if diff > 2*time.Second {
			t.Errorf("mtime = %v, want within 2s of %v (diff=%v)", fi.ModTime(), targetMtim, diff)
		}
	})

	t.Run("Xfd_filestat_set_times fstMtimNow updates mtime to current", func(t *testing.T) {
		s, _ := newTestState(nil)
		fd := int32(5)
		filePath := setupOsFileFd(t, s, fd, []byte("data"))

		past := time.Now().Add(-1 * time.Hour)
		if err := os.Chtimes(filePath, past, past); err != nil {
			t.Fatal(err)
		}

		errno := s.Xfd_filestat_set_times(fd, 0, 0, fstMtimNow)
		if errno != wasiESuccess {
			t.Fatalf("filestat_set_times returned %d, want ESUCCESS", errno)
		}

		fi, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(fi.ModTime()) > 5*time.Second {
			t.Errorf("mtime = %v, not close to now", fi.ModTime())
		}
	})
}

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

	s, buf := newTestState(nil)
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

	s, buf := newTestState(nil)
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

	s, buf := newTestState(nil)
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

var contradictoryFstFlagsCases = []struct {
	name     string
	fstFlags int32
	atim     int64
	mtim     int64
}{
	{
		name:     "fstMtim and fstMtimNow together",
		fstFlags: fstMtim | fstMtimNow,
		atim:     0,
		mtim:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
	},
	{
		name:     "fstAtim and fstAtimNow together",
		fstFlags: fstAtim | fstAtimNow,
		atim:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
		mtim:     0,
	},
}

// assertTimesUnchanged fails the test if the file at hostPath no longer has
// the expected atime and mtime.
func assertTimesUnchanged(t *testing.T, hostPath string, wantAtime, wantMtime time.Time) {
	t.Helper()
	fi, err := os.Stat(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := getAtimeFromStat(fi); !got.Equal(wantAtime) {
		t.Errorf("atime mutated: before=%v after=%v", wantAtime, got)
	}
	if got := fi.ModTime(); !got.Equal(wantMtime) {
		t.Errorf("mtime mutated: before=%v after=%v", wantMtime, got)
	}
}

func TestFdFilestatSetTimesRejectsContradictoryFlags(t *testing.T) {
	s, _ := newTestState(nil)
	filePath := setupOsFileFd(t, s, 5, []byte("data"))

	// Set both times to a known past value so we can detect unwanted mutation.
	past := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filePath, past, past); err != nil {
		t.Fatal(err)
	}

	fi0, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	atimeBefore := getAtimeFromStat(fi0)
	mtimeBefore := fi0.ModTime()

	for _, tc := range contradictoryFstFlagsCases {
		t.Run(tc.name, func(t *testing.T) {
			errno := s.Xfd_filestat_set_times(5, tc.atim, tc.mtim, tc.fstFlags)
			if errno != wasiEInval {
				t.Fatalf("got errno %d, want EINVAL (%d)", errno, wasiEInval)
			}
			assertTimesUnchanged(t, filePath, atimeBefore, mtimeBefore)
		})
	}

	// Also verify the same behavior when the fd was opened via path_open
	// (closes the gap from plan traceability which assumes path_open).
	t.Run("via_path_open", func(t *testing.T) {
		s2, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s2, buf)
		fname := "po_contradictory.txt"
		hostPath := filepath.Join(hostDir, fname)
		if err := os.WriteFile(hostPath, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(hostPath, past, past); err != nil {
			t.Fatal(err)
		}

		fi2, err := os.Stat(hostPath)
		if err != nil {
			t.Fatal(err)
		}
		atime2 := getAtimeFromStat(fi2)
		mtime2 := fi2.ModTime()

		const fdPtr = 900
		pathOff, pathLen := writePath(buf, 500, fname)
		errno := s2.Xpath_open(3, 0, pathOff, pathLen, 0, int64(rightFDRead|rightFDWrite|rightFDFilestatGet|rightFDFilestatSetTimes), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open = %d, want ESUCCESS", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		for _, tc := range contradictoryFstFlagsCases {
			t.Run(tc.name, func(t *testing.T) {
				errno := s2.Xfd_filestat_set_times(fd, tc.atim, tc.mtim, tc.fstFlags)
				if errno != wasiEInval {
					t.Fatalf("got errno %d, want EINVAL (%d)", errno, wasiEInval)
				}
				assertTimesUnchanged(t, hostPath, atime2, mtime2)
			})
		}
	})
}

func TestPathFilestatSetTimesRejectsContradictoryFlags(t *testing.T) {
	s, buf := newTestState(nil)
	hostDir := setupWritableMount(t, s, buf)
	fname := "contradictory.txt"
	hostPath := filepath.Join(hostDir, fname)
	if err := os.WriteFile(hostPath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	past := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(hostPath, past, past); err != nil {
		t.Fatal(err)
	}

	fi0, err := os.Stat(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	atimeBefore := getAtimeFromStat(fi0)
	mtimeBefore := fi0.ModTime()

	for _, tc := range contradictoryFstFlagsCases {
		t.Run(tc.name, func(t *testing.T) {
			pathOff, pathLen := writePath(buf, 500, fname)
			errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, tc.atim, tc.mtim, tc.fstFlags)
			if errno != wasiEInval {
				t.Fatalf("got errno %d, want EINVAL (%d)", errno, wasiEInval)
			}
			assertTimesUnchanged(t, hostPath, atimeBefore, mtimeBefore)
		})
	}
}

// TestFdFilestatSetSizeReturnsWazeroErrnoViaFileTruncate verifies
// Xfd_filestat_set_size uses fe.File.Truncate and errnoFromWasi for every file type.
func TestFdFilestatSetSizeReturnsWazeroErrnoViaFileTruncate(t *testing.T) {
	t.Parallel()

	t.Run("osFile-backed fd truncates with ESUCCESS when FD_FILESTAT_SET_SIZE is granted", func(t *testing.T) {
		s, _ := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte("ABCDEFGHIJ"))

		// Verify the file has the right to truncate
		fe := lookupFETest(s, 5)
		if fe.RightsBase&rightFDFilestatSetSize == 0 {
			t.Fatal("test setup error: fd missing FD_FILESTAT_SET_SIZE right")
		}

		errno := s.Xfd_filestat_set_size(5, 5)
		if errno != wasiESuccess {
			t.Errorf("got errno %d, want ESUCCESS", errno)
		}

		// Verify the file was actually truncated
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 5 {
			t.Errorf("file size = %d, want 5", len(data))
		}
	})

	t.Run("osFile-backed fd returns ENOTCAPABLE without FD_FILESTAT_SET_SIZE", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte("ABCDEFGHIJ"))

		// Remove the truncate right
		lookupFETest(s, 5).RightsBase &^= rightFDFilestatSetSize

		errno := s.Xfd_filestat_set_size(5, 5)
		if errno != wasiENotCap {
			t.Errorf("got errno %d, want ENOTCAPABLE (%d)", errno, wasiENotCap)
		}
	})

	t.Run("fsFile-backed fd (AdaptFS) returns ENOSYS when Truncate not implemented", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/f.txt", []byte("HELLO"), 0644); err != nil {
			t.Fatal(err)
		}

		// Use AdaptFS which wraps fs.FS
		fsys := &sysfs.AdaptFS{FS: os.DirFS(dir)}
		f, wasiErrno := fsys.OpenFile("f.txt", wasi.O_RDWR, 0)
		if wasiErrno != 0 {
			t.Fatalf("OpenFile failed: %v", wasiErrno)
		}
		insertStubFD(s, 5, f, rightFDFilestatSetSize)

		errno := s.Xfd_filestat_set_size(5, 99)
		// fsFile.Truncate returns ENOSYS when the underlying fs.File doesn't support it
		if errno != wasiENoSys {
			t.Errorf("got errno %d, want ENOSYS (%d)", errno, wasiENoSys)
		}

		// Verify no mutation occurred
		data, err := os.ReadFile(dir + "/f.txt")
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "HELLO" {
			t.Errorf("file was mutated: got %q, want %q", data, "HELLO")
		}
	})

	t.Run("ReadFS readFile fd returns EBADF for Truncate", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/ro.txt", []byte("HELLO"), 0644); err != nil {
			t.Fatal(err)
		}

		// Use ReadFS which wraps fs.FS as read-only
		fsys := &sysfs.ReadFS{FS: &sysfs.DirFS{Path: dir}}
		f, wasiErrno := fsys.OpenFile("ro.txt", wasi.O_RDONLY, 0)
		if wasiErrno != 0 {
			t.Fatalf("OpenFile failed: %v", wasiErrno)
		}
		insertStubFD(s, 5, f, rightFDFilestatSetSize)

		errno := s.Xfd_filestat_set_size(5, 99)
		// readFile.Truncate returns EBADF (via writeErr)
		if errno != wasiEBadf {
			t.Errorf("got errno %d, want EBADF (%d)", errno, wasiEBadf)
		}
	})

	t.Run("directory fd returns EISDIR", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()

		// Open a directory
		fsys := &sysfs.AdaptFS{FS: os.DirFS(dir)}
		f, wasiErrno := fsys.OpenFile(".", wasi.O_RDONLY|wasi.O_DIRECTORY, 0)
		if wasiErrno != 0 {
			t.Fatalf("OpenFile directory failed: %v", wasiErrno)
		}
		insertStubFD(s, 5, f, rightFDFilestatSetSize)

		errno := s.Xfd_filestat_set_size(5, 99)
		if errno != wasiEIsdir {
			t.Errorf("got errno %d, want EISDIR (%d)", errno, wasiEIsdir)
		}
	})

	t.Run("fd_allocate delegates to same Truncate path", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte("data"))

		// fd_allocate uses fdGrowToSize which calls Truncate
		errno := s.Xfd_allocate(5, 0, 16)
		if errno != wasiESuccess {
			t.Errorf("got errno %d, want ESUCCESS", errno)
		}
	})
}

// TestFdFilestatSetTimesReturnsWazeroErrnoViaFileUtimens verifies
// Xfd_filestat_set_times uses fe.File.Utimens and errnoFromWasi for every file type.
func TestFdFilestatSetTimesReturnsWazeroErrnoViaFileUtimens(t *testing.T) {
	t.Parallel()

	targetMtim := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	targetMtimNs := targetMtim.UnixNano()

	t.Run("osFile-backed fd updates times with ESUCCESS when FD_FILESTAT_SET_TIMES is granted", func(t *testing.T) {
		s, _ := newTestState(nil)
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		// Verify the file has the right to set times
		fe := lookupFETest(s, 5)
		if fe.RightsBase&rightFDFilestatSetTimes == 0 {
			t.Fatal("test setup error: fd missing FD_FILESTAT_SET_TIMES right")
		}

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, fstMtim)
		if errno != wasiESuccess {
			t.Errorf("got errno %d, want ESUCCESS", errno)
		}

		// Verify the mtime was actually set
		fi, err := os.Stat(filePath)
		if err != nil {
			t.Fatal(err)
		}
		diff := fi.ModTime().Sub(targetMtim)
		if diff < 0 {
			diff = -diff
		}
		if diff > 2*time.Second {
			t.Errorf("mtime = %v, want within 2s of %v (diff=%v)", fi.ModTime(), targetMtim, diff)
		}
	})

	t.Run("osFile-backed fd returns ENOTCAPABLE without FD_FILESTAT_SET_TIMES", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte("data"))

		// Remove the set times right
		lookupFETest(s, 5).RightsBase &^= rightFDFilestatSetTimes

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, fstMtim)
		if errno != wasiENotCap {
			t.Errorf("got errno %d, want ENOTCAPABLE (%d)", errno, wasiENotCap)
		}
	})

	t.Run("fsFile-backed fd (AdaptFS) returns ENOSYS when Utimens not implemented", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/f.txt", []byte("HELLO"), 0644); err != nil {
			t.Fatal(err)
		}

		// Use AdaptFS which wraps fs.FS
		fsys := &sysfs.AdaptFS{FS: os.DirFS(dir)}
		f, wasiErrno := fsys.OpenFile("f.txt", wasi.O_RDWR, 0)
		if wasiErrno != 0 {
			t.Fatalf("OpenFile failed: %v", wasiErrno)
		}
		insertStubFD(s, 5, f, rightFDFilestatSetTimes)

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, fstMtim)
		// fsFile.Utimens returns ENOSYS when the underlying fs.File doesn't support it
		if errno != wasiENoSys {
			t.Errorf("got errno %d, want ENOSYS (%d)", errno, wasiENoSys)
		}

		// Verify no mutation occurred
		data, err := os.ReadFile(dir + "/f.txt")
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "HELLO" {
			t.Errorf("file was mutated: got %q, want %q", data, "HELLO")
		}
	})

	t.Run("ReadFS readFile fd returns EBADF for Utimens", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/ro.txt", []byte("HELLO"), 0644); err != nil {
			t.Fatal(err)
		}

		// Use ReadFS which wraps fs.FS as read-only
		fsys := &sysfs.ReadFS{FS: &sysfs.DirFS{Path: dir}}
		f, wasiErrno := fsys.OpenFile("ro.txt", wasi.O_RDONLY, 0)
		if wasiErrno != 0 {
			t.Fatalf("OpenFile failed: %v", wasiErrno)
		}
		insertStubFD(s, 5, f, rightFDFilestatSetTimes)

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, fstMtim)
		// readFile.Utimens returns EBADF (via writeErr)
		if errno != wasiEBadf {
			t.Errorf("got errno %d, want EBADF (%d)", errno, wasiEBadf)
		}
	})

	t.Run("directory fd returns EISDIR", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()

		// Open a directory
		fsys := &sysfs.AdaptFS{FS: os.DirFS(dir)}
		f, wasiErrno := fsys.OpenFile(".", wasi.O_RDONLY|wasi.O_DIRECTORY, 0)
		if wasiErrno != 0 {
			t.Fatalf("OpenFile directory failed: %v", wasiErrno)
		}
		insertStubFD(s, 5, f, rightFDFilestatSetTimes)

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, fstMtim)
		if errno != wasiEIsdir {
			t.Errorf("got errno %d, want EISDIR (%d)", errno, wasiEIsdir)
		}
	})

	t.Run("fstFlags=0 returns ESUCCESS without calling Utimens", func(t *testing.T) {
		s, _ := newTestState(nil)
		_ = setupOsFileFd(t, s, 5, []byte("data"))

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, 0)
		if errno != wasiESuccess {
			t.Errorf("got errno %d, want ESUCCESS", errno)
		}
	})

	t.Run("ReadFS readFile fd returns EBADF for Utimens with fstAtimNow", func(t *testing.T) {
		s, _ := newTestState(nil)
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/ro.txt", []byte("HELLO"), 0644); err != nil {
			t.Fatal(err)
		}

		// Use ReadFS which wraps fs.FS as read-only
		fsys := &sysfs.ReadFS{FS: &sysfs.DirFS{Path: dir}}
		f, wasiErrno := fsys.OpenFile("ro.txt", wasi.O_RDONLY, 0)
		if wasiErrno != 0 {
			t.Fatalf("OpenFile failed: %v", wasiErrno)
		}
		insertStubFD(s, 5, f, rightFDFilestatSetTimes)

		errno := s.Xfd_filestat_set_times(5, 0, 0, fstAtimNow)
		// readFile.Utimens returns EBADF (via writeErr)
		if errno != wasiEBadf {
			t.Errorf("got errno %d, want EBADF (%d)", errno, wasiEBadf)
		}
	})
}

// vfsDotDotStatFS wraps DirFS and assigns a synthetic inode for Stat/Lstat("..").
type vfsDotDotStatFS struct {
	sysfs.DirFS
	dotDotIno uint64
}

func (v vfsDotDotStatFS) Stat(path string) (wasi.Stat_t, wasi.Errno) {
	if path == ".." {
		st, errno := v.DirFS.Stat(".")
		if errno != 0 {
			return st, errno
		}
		st.Ino = v.dotDotIno
		return st, 0
	}
	return v.DirFS.Stat(path)
}

func (v vfsDotDotStatFS) Lstat(path string) (wasi.Stat_t, wasi.Errno) {
	if path == ".." {
		st, errno := v.DirFS.Lstat(".")
		if errno != 0 {
			return st, errno
		}
		st.Ino = v.dotDotIno
		return st, 0
	}
	return v.DirFS.Lstat(path)
}

// TestFdReaddirHostSyntheticDots verifies that fd_readdir on a host-backed
// (writable) directory fd injects synthetic "." and ".." entries before the
// real directory entries, with correct d_ino (from host stat), d_type,
// d_namlen, and d_next cookie values.
func TestFdReaddirHostSyntheticDots(t *testing.T) {
	const (
		rdBufPtr   = 1000
		rdBufLen   = 4096
		usedPtr    = 8000
		dirPathOff = 9000
		fdPtr      = 9500
	)

	// --- Shared setup: temp dir with one file ---
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "realfile.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Get the host stat for the temp directory to know expected ino
	hostInfo, err := os.Stat(tmpDir)
	if err != nil {
		t.Fatalf("os.Stat(tempDir) failed: %v", err)
	}
	hostStat := hostInfo.Sys().(*syscall.Stat_t)
	wantIno := hostStat.Ino

	if wantIno == 0 {
		t.Skip("host filesystem reports st_ino == 0; cannot assert non-zero ino")
	}

	assertEntries := func(t *testing.T, entries []ReaddirDirent) {
		t.Helper()
		if len(entries) < 3 {
			t.Fatalf("expected at least 3 entries (. .. realfile.txt), got %d: %+v", len(entries), entries)
		}

		// Assert: entry[0] is "."
		if entries[0].Name != "." {
			t.Errorf("entry[0].Name = %q, want %q", entries[0].Name, ".")
		}
		if entries[0].Type != byte(fdDir) {
			t.Errorf("entry[0].Type = %d, want fdDir (%d)", entries[0].Type, fdDir)
		}
		if entries[0].Namlen != 1 {
			t.Errorf("entry[0].Namlen = %d, want 1", entries[0].Namlen)
		}
		if entries[0].Ino != wantIno {
			t.Errorf("entry[0].Ino = %d, want %d (host st_ino)", entries[0].Ino, wantIno)
		}
		if entries[0].Next != 1 {
			t.Errorf("entry[0].Next = %d, want 1", entries[0].Next)
		}

		// Assert: entry[1] is ".."
		if entries[1].Name != ".." {
			t.Errorf("entry[1].Name = %q, want %q", entries[1].Name, "..")
		}
		if entries[1].Type != byte(fdDir) {
			t.Errorf("entry[1].Type = %d, want fdDir (%d)", entries[1].Type, fdDir)
		}
		if entries[1].Namlen != 2 {
			t.Errorf("entry[1].Namlen = %d, want 2", entries[1].Namlen)
		}
		if entries[1].Next != 2 {
			t.Errorf("entry[1].Next = %d, want 2", entries[1].Next)
		}

		// Assert: entry[2] is the real file "realfile.txt"
		if entries[2].Name != "realfile.txt" {
			t.Errorf("entry[2].Name = %q, want %q", entries[2].Name, "realfile.txt")
		}
		if entries[2].Type != byte(fdFile) {
			t.Errorf("entry[2].Type = %d, want fdFile (%d)", entries[2].Type, fdFile)
		}
		if entries[2].Ino == 0 {
			t.Errorf("entry[2].Ino = 0, want non-zero")
		}
	}

	// --- Subtest 1: writable host preopen ---
	t.Run("writable host preopen", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		// fd 3 is the preopen
		errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_readdir = %d, want ESUCCESS", errno)
		}

		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("bufUsed = 0, expected entries")
		}

		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		assertEntries(t, entries)
	})

	// --- Subtest 2: path_open'd directory fd ---
	t.Run("path_open'd directory fd", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		// fd 3 is the preopen for "/data". Open the temp dir itself via path_open.
		// We use "." as the path relative to the preopen fd to get a new fd for the same dir.
		copy(buf[dirPathOff:], ".")
		errno := s.Xpath_open(3, 0, dirPathOff, 1, int32(oflagDir), int64(rightFDRead), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(.) = %d, want ESUCCESS", errno)
		}
		dirFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		errno = s.Xfd_readdir(dirFD, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_readdir = %d, want ESUCCESS", errno)
		}

		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("bufUsed = 0, expected entries")
		}

		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		assertEntries(t, entries)
	})
}

func TestFdReaddirCachingAndPagination(t *testing.T) {
	const (
		rdBufPtr = 1000
		rdBufLen = 4096
		usedPtr  = 8000
	)

	// Setup: temp dir with 3 files
	tmpDir := t.TempDir()
	for _, name := range []string{"f1", "f2", "f3"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("data"), 0644); err != nil {
			t.Fatalf("WriteFile(%s) failed: %v", name, err)
		}
	}

	// Dirent sizes: "." = 24+1=25, ".." = 24+2=26, "f1" = 24+2=26
	// Exactly 3 dirents fit in 25+26+26 = 77 bytes
	const exactBufLen = 77

	// --- Subtest 1: full buffer returns all 5 entries ---
	t.Run("full buffer returns all 5 entries", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_readdir = %d, want ESUCCESS", errno)
		}

		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("bufUsed = 0, expected entries")
		}

		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 5 {
			t.Fatalf("expected 5 entries, got %d: %+v", len(entries), entries)
		}
		wantNames := []string{".", "..", "f1", "f2", "f3"}
		for i, want := range wantNames {
			if entries[i].Name != want {
				t.Errorf("entry[%d].Name = %q, want %q", i, entries[i].Name, want)
			}
		}
	})

	// --- Subtest 2: pagination with exact buffer ---
	t.Run("pagination with exact buffer", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		// First call: cookie=0, bufLen=77 fits exactly 3 dirents (., .., f1)
		errno := s.Xfd_readdir(3, rdBufPtr, exactBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("first Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("first call: bufUsed = 0, expected entries")
		}
		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 3 {
			t.Fatalf("first call: expected 3 entries, got %d: %+v", len(entries), entries)
		}
		wantNames := []string{".", "..", "f1"}
		for i, want := range wantNames {
			if entries[i].Name != want {
				t.Errorf("first call: entry[%d].Name = %q, want %q", i, entries[i].Name, want)
			}
		}

		// The d_next of f1 (entry[2]) should point to f2 (cookie=3)
		f1Cookie := entries[2].Next

		// Second call: cookie = f1.d_next, should return f2 and f3
		errno = s.Xfd_readdir(3, rdBufPtr, rdBufLen, int64(f1Cookie), usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("second Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed = binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("second call: bufUsed = 0, expected entries")
		}
		entries = parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 2 {
			t.Fatalf("second call: expected 2 entries (f2, f3), got %d: %+v", len(entries), entries)
		}
		if entries[0].Name != "f2" {
			t.Errorf("second call: entry[0].Name = %q, want %q", entries[0].Name, "f2")
		}
		if entries[1].Name != "f3" {
			t.Errorf("second call: entry[1].Name = %q, want %q", entries[1].Name, "f3")
		}

		// Third call: cookie = f3.d_next, should return bufUsed=0
		f3Cookie := entries[1].Next
		errno = s.Xfd_readdir(3, rdBufPtr, rdBufLen, int64(f3Cookie), usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("third Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed = binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed != 0 {
			t.Fatalf("third call: expected bufUsed=0, got %d", bufUsed)
		}
	})

	// --- Subtest 3: cache is stable across continuation cookies ---
	t.Run("cache is stable across continuation cookies", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		// First call: read just . and .. (buf fits exactly 2 dirents: 25+26=51)
		errno := s.Xfd_readdir(3, rdBufPtr, 51, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("first Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 2 {
			t.Fatalf("first call: expected 2 entries, got %d: %+v", len(entries), entries)
		}
		if entries[0].Name != "." || entries[1].Name != ".." {
			t.Fatalf("first call: expected ., .., got %+v", entries)
		}

		// d_next of .. should be cookie=2 (pointing to f1)
		dotDotCookie := entries[1].Next

		// Now delete f2 from the host directory
		if err := os.Remove(filepath.Join(tmpDir, "f2")); err != nil {
			t.Fatalf("os.Remove(f2) failed: %v", err)
		}

		// Continue reading with the cached cookie — should still return f1, f2, f3 from cache
		errno = s.Xfd_readdir(3, rdBufPtr, rdBufLen, int64(dotDotCookie), usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("second Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed = binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("second call: bufUsed = 0, expected entries from cache")
		}
		entries = parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 3 {
			t.Fatalf("second call: expected 3 entries (f1, f2, f3) from cache, got %d: %+v", len(entries), entries)
		}
		wantNames := []string{"f1", "f2", "f3"}
		for i, want := range wantNames {
			if entries[i].Name != want {
				t.Errorf("second call: entry[%d].Name = %q, want %q (from cache)", i, entries[i].Name, want)
			}
		}
	})

	// --- Subtest 4: path_open'd osFile continuation stays stable across host mutations ---
	t.Run("path_open'd osFile continuation stays stable across host mutations", func(t *testing.T) {
		contDir := t.TempDir()
		for _, name := range []string{"f1", "f2", "f3"} {
			if err := os.WriteFile(filepath.Join(contDir, name), []byte("data"), 0644); err != nil {
				t.Fatalf("WriteFile(%s) failed: %v", name, err)
			}
		}

		const (
			rdBufPtr   = 1000
			rdBufLen   = 4096
			usedPtr    = 8000
			dirPathOff = 9000
			fdPtr      = 9500
		)

		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", contDir))

		// path_open the directory to get an osFile-backed fd
		copy(buf[dirPathOff:], ".")
		errno := s.Xpath_open(3, 0, dirPathOff, 1, int32(oflagDir), int64(rightFDRead), 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open(.) = %d, want ESUCCESS", errno)
		}
		dirFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// First call: cookie=0 with a buffer fitting only "." and ".." (25+26=51 bytes)
		errno = s.Xfd_readdir(dirFD, rdBufPtr, 51, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("first Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("first call: bufUsed = 0, expected entries")
		}
		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 2 {
			t.Fatalf("first call: expected 2 entries (. and ..), got %d: %+v", len(entries), entries)
		}
		if entries[0].Name != "." || entries[1].Name != ".." {
			t.Fatalf("first call: expected ., .., got %+v", entries)
		}
		dotDotCookie := entries[1].Next
		if dotDotCookie != 2 {
			t.Fatalf("d_next for .. = %d, want 2", dotDotCookie)
		}

		// Delete f2 from the host directory
		if err := os.Remove(filepath.Join(contDir, "f2")); err != nil {
			t.Fatalf("os.Remove(f2) failed: %v", err)
		}

		// Continuation with cookie=2 should return f1, f2, f3 FROM SNAPSHOT
		// (currently fails: implementation re-reads from host every call, so f2 is absent)
		errno = s.Xfd_readdir(dirFD, rdBufPtr, rdBufLen, int64(dotDotCookie), usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("second Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed = binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("second call: bufUsed = 0, expected entries from snapshot")
		}
		entries = parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 3 {
			t.Fatalf("second call: expected 3 entries (f1, f2, f3) from snapshot, got %d: %+v", len(entries), entries)
		}
		seen := make(map[string]bool)
		for _, e := range entries {
			seen[e.Name] = true
		}
		for _, want := range []string{"f1", "f2", "f3"} {
			if !seen[want] {
				t.Errorf("second call: missing entry %q in snapshot entries: %+v", want, entries)
			}
		}
	})

	// --- Subtest 5: cookie=0 re-reads after mutation ---
	t.Run("cookie=0 re-reads after mutation", func(t *testing.T) {
		// Use a separate temp dir so previous subtest deletions don't affect us
		mutDir := t.TempDir()
		for _, name := range []string{"f1", "f2", "f3"} {
			if err := os.WriteFile(filepath.Join(mutDir, name), []byte("data"), 0644); err != nil {
				t.Fatalf("WriteFile(%s) failed: %v", name, err)
			}
		}

		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", mutDir))

		// First call populates the cache with all 5 entries
		errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("first Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 5 {
			t.Fatalf("first call: expected 5 entries, got %d: %+v", len(entries), entries)
		}

		// Now delete f2 from the host directory
		if err := os.Remove(filepath.Join(mutDir, "f2")); err != nil {
			t.Fatalf("os.Remove(f2) failed: %v", err)
		}

		// A fresh cookie=0 call should re-read from host and return updated list:
		// ., .., f1, f3 (4 entries) — NOT the cached 5 entries
		errno = s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("second Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed = binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("second call: bufUsed = 0, expected entries")
		}
		entries = parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) != 4 {
			t.Fatalf("second call: expected 4 entries (f2 deleted), got %d: %+v", len(entries), entries)
		}
		wantNames := []string{".", "..", "f1", "f3"}
		for i, want := range wantNames {
			if entries[i].Name != want {
				t.Errorf("second call: entry[%d].Name = %q, want %q", i, entries[i].Name, want)
			}
		}
	})
}

func TestFd_filestat_get_host_ino(t *testing.T) {
	// --- Writable host preopen: ino and dev must come from host stat ---
	t.Run("writable host preopen populates ino and dev from host stat", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Get the host stat for the temp directory to know expected ino/dev
		hostInfo, err := os.Stat(tmpDir)
		if err != nil {
			t.Fatalf("os.Stat(tempDir) failed: %v", err)
		}
		hostStat := hostInfo.Sys().(*syscall.Stat_t)
		wantIno := hostStat.Ino
		wantDev := uint64(hostStat.Dev)

		if wantIno == 0 {
			t.Skip("host filesystem reports st_ino == 0; cannot assert non-zero ino")
		}
		if wantDev == 0 {
			t.Skip("host filesystem reports st_dev == 0; cannot assert non-zero dev")
		}

		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		// fd 3 is the first (only) preopen
		const bufPtr = 100
		errno := s.Xfd_filestat_get(3, bufPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_filestat_get(3) = %d, want ESUCCESS", errno)
		}

		// filestat layout: dev(8) + ino(8) + filetype(8) + nlink(8) + size(8) + atim(8) + mtim(8) + ctim(8)
		gotDev := binary.LittleEndian.Uint64(buf[bufPtr+0:])
		gotIno := binary.LittleEndian.Uint64(buf[bufPtr+8:])

		if gotIno == 0 {
			t.Errorf("filestat.ino = 0, want non-zero (host st_ino = %d)", wantIno)
		}
		if gotIno != wantIno {
			t.Errorf("filestat.ino = %d, want %d (host st_ino)", gotIno, wantIno)
		}
		if gotDev == 0 {
			t.Errorf("filestat.dev = 0, want non-zero (host st_dev = %d)", wantDev)
		}
		if gotDev != wantDev {
			t.Errorf("filestat.dev = %d, want %d (host st_dev)", gotDev, wantDev)
		}
	})

	// --- Read-only fs.FS preopen: ino and dev must be 0 ---
	t.Run("read-only fs.FS preopen returns zero ino and dev", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }

		// Use a simple in-memory FS so there is no host backing
		s := New(mem, NewModuleConfig().WithReadOnlyFS("/ro", fstest.MapFS{
			"hello.txt": &fstest.MapFile{Data: []byte("hello")},
		}))

		// fd 3 is the first (only) preopen
		const bufPtr = 100
		errno := s.Xfd_filestat_get(3, bufPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_filestat_get(3) = %d, want ESUCCESS", errno)
		}

		gotDev := binary.LittleEndian.Uint64(buf[bufPtr+0:])
		gotIno := binary.LittleEndian.Uint64(buf[bufPtr+8:])

		if gotIno != 0 {
			t.Errorf("filestat.ino = %d, want 0 for fs.FS-backed preopen", gotIno)
		}
		if gotDev != 0 {
			t.Errorf("filestat.dev = %d, want 0 for fs.FS-backed preopen", gotDev)
		}
	})
}

// TestReadonlyPathFilestatGetSymlinkFollow verifies path_filestat_get on a
// read-only ReadFS(DirFS) preopen honors LOOKUPFLAGS_SYMLINK_FOLLOW.
func TestReadonlyPathFilestatGetSymlinkFollow(t *testing.T) {
	s, buf := newTestState(nil)
	dir := t.TempDir()

	const targetName = "target.bin"
	targetContent := []byte("0123456789abcdef")
	if err := os.WriteFile(filepath.Join(dir, targetName), targetContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetName, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	fsys := &sysfs.ReadFS{FS: sysfs.DirFS{Path: dir}}
	wf, errno := fsys.OpenFile(".", wasi.O_RDONLY|wasi.O_DIRECTORY, 0)
	if errno != 0 {
		t.Fatalf("OpenFile preopen: errno %d", errno)
	}
	insertTestFileAt(s, 3, &wasi.FileEntry{
		FS:               fsys,
		Name:             "ro",
		GuestPath:        "ro",
		IsPreopen:        true,
		File:             wf,
		RightsBase:       rightsReadOnlyDirPreopen,
		RightsInheriting: rightsReadOnlyDirPreopen,
	})

	const (
		dirfd   = int32(3)
		pathOff = 100
		statOff = 200
	)
	copy(buf[pathOff:], "link")

	rc := s.Xpath_filestat_get(dirfd, 0, pathOff, 4, statOff)
	if rc != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(link, no-follow) = %d, want ESUCCESS", rc)
	}
	noFollowType := binary.LittleEndian.Uint64(buf[statOff+16 : statOff+24])
	noFollowSize := binary.LittleEndian.Uint64(buf[statOff+32 : statOff+40])
	if byte(noFollowType) != fdSymlink {
		t.Errorf("no-follow filetype = %d, want fdSymlink (%d)", noFollowType, fdSymlink)
	}

	rc = s.Xpath_filestat_get(dirfd, wasiLookupSymlinkFollow, pathOff, 4, statOff)
	if rc != wasiESuccess {
		t.Fatalf("Xpath_filestat_get(link, follow) = %d, want ESUCCESS", rc)
	}
	followType := binary.LittleEndian.Uint64(buf[statOff+16 : statOff+24])
	followSize := binary.LittleEndian.Uint64(buf[statOff+32 : statOff+40])
	if byte(followType) != fdFile {
		t.Errorf("follow filetype = %d, want fdFile (%d)", followType, fdFile)
	}
	if followSize != uint64(len(targetContent)) {
		t.Errorf("follow size = %d, want %d (target file)", followSize, len(targetContent))
	}
	if noFollowSize == followSize {
		t.Errorf("no-follow size = follow size = %d; want distinct metadata for symlink vs target", noFollowSize)
	}
}

// TestFdReaddirSyntheticDots verifies that fd_readdir on an fs.FS-backed
// directory fd injects synthetic "." and ".." entries before the real
// directory entries, with correct d_ino (0), d_type, d_namlen, and d_next
// cookie values.
func TestFdReaddirSyntheticDots(t *testing.T) {
	const (
		rdBufPtr   = 1000
		rdBufLen   = 4096
		usedPtr    = 8000
		dirPathOff = 9000
		fdPtr      = 9500
	)

	buf := make([]byte, 65536)
	mem := func() []byte { return buf }

	// Create an fs.FS with a directory containing one file.
	// We use a subdirectory "mydir" so we can open it via path_open
	// on the read-only mount, giving us a non-preopen directory fd
	// that still uses fs.FS backing.
	mapFS := fstest.MapFS{
		"mydir":           &fstest.MapFile{Mode: fs.ModeDir | 0755},
		"mydir/hello.txt": &fstest.MapFile{Data: []byte("hello")},
	}

	s := New(mem, NewModuleConfig().WithReadOnlyFS("/data", mapFS))

	// fd 3 is the preopen for "/data". Open "mydir" via path_open.
	copy(buf[dirPathOff:], "mydir")
	errno := s.Xpath_open(3, 0, dirPathOff, 5, int32(oflagDir), int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(mydir) = %d, want ESUCCESS", errno)
	}
	dirFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// Call fd_readdir with cookie=0 and a buffer large enough for all entries.
	errno = s.Xfd_readdir(dirFD, rdBufPtr, rdBufLen, 0, usedPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xfd_readdir = %d, want ESUCCESS", errno)
	}

	bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
	if bufUsed == 0 {
		t.Fatal("bufUsed = 0, expected entries")
	}
	if bufUsed > uint32(rdBufLen) {
		t.Fatalf("bufUsed = %d, want <= bufLen (%d)", bufUsed, rdBufLen)
	}

	entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)

	// Assert: at least 3 entries (., .., hello.txt).
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 entries (. .. hello.txt), got %d: %+v", len(entries), entries)
	}

	// Assert: entry[0] is "."
	if entries[0].Name != "." {
		t.Errorf("entry[0].Name = %q, want %q", entries[0].Name, ".")
	}
	if entries[0].Type != byte(fdDir) {
		t.Errorf("entry[0].Type = %d, want fdDir (%d)", entries[0].Type, fdDir)
	}
	if entries[0].Namlen != 1 {
		t.Errorf("entry[0].Namlen = %d, want 1", entries[0].Namlen)
	}
	if entries[0].Ino != 0 {
		t.Errorf("entry[0].Ino = %d, want 0", entries[0].Ino)
	}
	if entries[0].Next != 1 {
		t.Errorf("entry[0].Next = %d, want 1", entries[0].Next)
	}

	// Assert: entry[1] is ".."
	if entries[1].Name != ".." {
		t.Errorf("entry[1].Name = %q, want %q", entries[1].Name, "..")
	}
	if entries[1].Type != byte(fdDir) {
		t.Errorf("entry[1].Type = %d, want fdDir (%d)", entries[1].Type, fdDir)
	}
	if entries[1].Namlen != 2 {
		t.Errorf("entry[1].Namlen = %d, want 2", entries[1].Namlen)
	}
	if entries[1].Next != 2 {
		t.Errorf("entry[1].Next = %d, want 2", entries[1].Next)
	}

	// Assert: entry[2] is the real file "hello.txt".
	if entries[2].Name != "hello.txt" {
		t.Errorf("entry[2].Name = %q, want %q", entries[2].Name, "hello.txt")
	}
	if entries[2].Type != byte(fdFile) {
		t.Errorf("entry[2].Type = %d, want fdFile (%d)", entries[2].Type, fdFile)
	}
	if entries[2].Next != 3 {
		t.Errorf("entry[2].Next = %d, want 3", entries[2].Next)
	}
}

// TestFdReaddirEdgeCases verifies fd_readdir behavior for a buffer too small
// to hold any dirent, an exhausted cookie past the last entry, and an invalid
// file descriptor.

type errThrowSeekerWasi struct{ wasi.UnimplementedFile }

func (errThrowSeekerWasi) IsDir() (bool, wasi.Errno) { return true, 0 }
func (errThrowSeekerWasi) Readdir(int) ([]wasi.Dirent, wasi.Errno) {
	return nil, wasi.EIO
}

// TestFdReaddirParentDino verifies synthetic "." uses the open directory inode
// and ".." does not reuse it. DirFS cannot Stat/Lstat(".."), so parent d_ino is 0
// unless the mount VFS implements parent stat (see TestFdReaddirParentDotDotInodeFromVFSStatLstat).
func TestFdReaddirParentDino(t *testing.T) {
	const (
		rdBufPtr = 1000
		rdBufLen = 4096
		usedPtr  = 8000
	)

	// Create parent/subdir structure
	parentDir := t.TempDir()
	subDir := filepath.Join(parentDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Mkdir(%s) failed: %v", subDir, err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	subInfo, err := os.Stat(subDir)
	if err != nil {
		t.Fatalf("os.Stat(sub) failed: %v", err)
	}
	subIno := subInfo.Sys().(*syscall.Stat_t).Ino

	if subIno == 0 {
		t.Skip("host reports st_ino == 0")
	}

	buf := make([]byte, 65536)
	mem := func() []byte { return buf }
	s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", subDir))

	// fd 3 is the preopen for /data → subDir.
	errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xfd_readdir = %d, want ESUCCESS", errno)
	}
	bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
	if bufUsed == 0 {
		t.Fatal("bufUsed = 0, expected entries")
	}

	entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
	if len(entries) < 3 {
		t.Fatalf("expected >= 3 entries, got %d: %+v", len(entries), entries)
	}

	// "." should have the subdirectory's own inode
	if entries[0].Name != "." {
		t.Fatalf("entry[0].Name = %q, want the dot entry", entries[0].Name)
	}
	dotIno := entries[0].Ino
	if dotIno != subIno {
		t.Errorf("entry[0].Ino = %d, want %d (subdirectory ino)", dotIno, subIno)
	}

	// ".." should have the PARENT directory's inode, not the subdirectory's
	if entries[1].Name != ".." {
		t.Fatalf("entry[1].Name = %q, want parent dot-dot entry", entries[1].Name)
	}
	dotDotIno := entries[1].Ino
	if dotDotIno != 0 {
		t.Errorf("entry[1].Ino = %d, want 0 without VFS Stat/Lstat on ..", dotDotIno)
	}

	// Ensure they're distinct (the bug was using the same ino for both)
	if dotIno == dotDotIno {
		t.Error(". and .. have the same dIno; expected distinct inodes")
	}
}

// TestFdReaddirParentDotDotInodeFromVFSStatLstat verifies that synthetic ".."
// on a DirFS preopen uses fe.FS Stat/Lstat for the parent inode, not os.Stat
// on the host parent path.
func TestFdReaddirParentDotDotInodeFromVFSStatLstat(t *testing.T) {
	const (
		rdBufPtr     = 1000
		rdBufLen     = 4096
		usedPtr      = 8000
		vfsParentIno = uint64(0xC0FFEE01)
	)

	parentDir := t.TempDir()
	subDir := filepath.Join(parentDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Mkdir(%s) failed: %v", subDir, err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "a.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	parentInfo, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("os.Stat(parent) failed: %v", err)
	}
	hostParentIno := parentInfo.Sys().(*syscall.Stat_t).Ino
	if hostParentIno == 0 {
		t.Skip("host filesystem reports st_ino == 0")
	}
	if hostParentIno == vfsParentIno {
		t.Fatalf("test requires host parent ino distinct from synthetic VFS ino")
	}

	fsys := vfsDotDotStatFS{
		DirFS:     sysfs.DirFS{Path: subDir},
		dotDotIno: vfsParentIno,
	}
	if st, errno := fsys.Stat(".."); errno != 0 {
		t.Fatalf("vfsDotDotStatFS.Stat(..) = %d", errno)
	} else if st.Ino != vfsParentIno {
		t.Fatalf("vfsDotDotStatFS.Stat(..).Ino = %d, want %d", st.Ino, vfsParentIno)
	}

	s, buf := newTestState(NewModuleConfig().WithHostDirectoryPreopen("/data", subDir))
	wf, openErrno := fsys.OpenFile(".", wasi.O_RDONLY|wasi.O_DIRECTORY, 0)
	if openErrno != 0 {
		t.Fatalf("OpenFile preopen: %d", openErrno)
	}
	insertTestFileAt(s, 3, &wasi.FileEntry{
		FS:               fsys,
		Name:             "/data",
		GuestPath:        "/data",
		IsPreopen:        true,
		File:             wf,
		RightsBase:       rightsWritableDirPreopenBase,
		RightsInheriting: rightsWritableDirPreopenInheriting,
	})

	errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xfd_readdir = %d, want ESUCCESS", errno)
	}
	bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
	if bufUsed == 0 {
		t.Fatal("bufUsed = 0, expected entries")
	}

	entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries (. ..), got %d: %+v", len(entries), entries)
	}
	if entries[1].Name != ".." {
		t.Fatalf("entry[1].Name = %q, want parent dot-dot entry", entries[1].Name)
	}
	if entries[1].Ino == 0 {
		t.Fatal(".. d_ino = 0, want non-zero inode from VFS Stat/Lstat")
	}
	if entries[1].Ino != vfsParentIno {
		t.Errorf(".. d_ino = %d, want %d from VFS Stat/Lstat (host os.Stat parent ino = %d)",
			entries[1].Ino, vfsParentIno, hostParentIno)
	}
	if entries[1].Ino == hostParentIno {
		t.Errorf(".. d_ino came from host os.Stat bypass (%d), not VFS", hostParentIno)
	}

	// DirFS rejects Stat/Lstat(".."); readdirDotDotIno must not use os.Stat hostFallback.
	sPlain, bufPlain := newTestState(NewModuleConfig().WithHostDirectoryPreopen("/data", subDir))
	errno = sPlain.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
	if errno != wasiESuccess {
		t.Fatalf("plain DirFS Xfd_readdir = %d, want ESUCCESS", errno)
	}
	bufUsed = binary.LittleEndian.Uint32(bufPlain[usedPtr : usedPtr+4])
	entries = parseReaddirDirents(bufPlain, bufUsed, rdBufPtr)
	if len(entries) < 2 || entries[1].Name != ".." {
		t.Fatalf("plain DirFS entries = %+v", entries)
	}
	if entries[1].Ino == hostParentIno {
		t.Errorf("plain DirFS .. d_ino = %d from host os.Stat bypass; want 0 without VFS Stat/Lstat", hostParentIno)
	}
	if entries[1].Ino != 0 {
		t.Errorf("plain DirFS .. d_ino = %d, want 0 when VFS cannot Stat/Lstat ..", entries[1].Ino)
	}
}

func TestFdReaddirEdgeCases(t *testing.T) {
	const (
		rdBufPtr = 1000
		rdBufLen = 4096
		usedPtr  = 8000
	)

	// --- Subtest 1: bufLen=1 returns bufUsed=0 ---
	t.Run("bufLen=1 returns bufUsed=0", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "only.txt"), []byte("data"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		// fd 3 is the preopen. bufLen=1 is too small for any dirent
		// (smallest dirent is "." at 24+1=25 bytes).
		errno := s.Xfd_readdir(3, rdBufPtr, 1, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed != 0 {
			t.Fatalf("bufUsed = %d, want 0 for bufLen=1", bufUsed)
		}
	})

	// --- Subtest 2: exhausted cookie returns bufUsed=0 ---
	t.Run("exhausted cookie returns bufUsed=0", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("data"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir))

		// First call: read all entries to find the last cookie.
		errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("first Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed == 0 {
			t.Fatal("first call: bufUsed = 0, expected entries")
		}
		entries := parseReaddirDirents(buf, bufUsed, rdBufPtr)
		if len(entries) < 3 {
			t.Fatalf("expected at least 3 entries (. .. a.txt), got %d", len(entries))
		}

		// The last entry's d_next is the cookie past the end.
		lastCookie := entries[len(entries)-1].Next

		// Second call: cookie past the last entry should return bufUsed=0.
		errno = s.Xfd_readdir(3, rdBufPtr, rdBufLen, int64(lastCookie), usedPtr)
		if errno != wasiESuccess {
			t.Fatalf("second Xfd_readdir = %d, want ESUCCESS", errno)
		}
		bufUsed = binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed != 0 {
			t.Fatalf("bufUsed = %d, want 0 for exhausted cookie", bufUsed)
		}
	})

	// --- Subtest 3: invalid fd returns EBADF ---
	t.Run("invalid fd returns EBADF", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, nil)

		errno := s.Xfd_readdir(999, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiEBadf {
			t.Fatalf("Xfd_readdir(999) = %d, want EBADF (%d)", errno, wasiEBadf)
		}
	})

	// --- Subtest 4: seek error returns EIO ---
	t.Run("seek error returns EIO", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem, NewModuleConfig().WithHostDirectoryPreopen("/", t.TempDir()))

		insertTestFileAt(s, 3, &wasi.FileEntry{
			File:       &errThrowSeekerWasi{},
			IsPreopen:  true,
			Name:       "/",
			GuestPath:  "/",
			RightsBase: rightsReadOnlyDirPreopen,
		})

		errno := s.Xfd_readdir(3, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiEIo {
			t.Fatalf("Xfd_readdir = %d, want EIO (%d)", errno, wasiEIo)
		}
		bufUsed := binary.LittleEndian.Uint32(buf[usedPtr : usedPtr+4])
		if bufUsed != 0 {
			t.Fatalf("bufUsed = %d, want 0 after seek error", bufUsed)
		}
	})
}
