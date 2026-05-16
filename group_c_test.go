package wasihost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilestatMutations(t *testing.T) {
	// Use a fixed past time far enough from "now" to distinguish clearly
	// (any real implementation would set mtime to this; stub won't)
	targetMtim := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	targetMtimNs := targetMtim.UnixNano()

	t.Run("Xfd_filestat_set_size truncates osFile to 5 bytes", func(t *testing.T) {
		s, _ := newTestState()
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

	t.Run("Xfd_filestat_set_size on FSFileWrap returns ESUCCESS with no mutation", func(t *testing.T) {
		s, _ := newTestState()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "ro.txt"), []byte("HELLO"), 0644); err != nil {
			t.Fatal(err)
		}
		fsys := os.DirFS(dir)
		f, err := fsys.Open("ro.txt")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		for len(s.fds) <= 5 {
			s.fds = append(s.fds, fdEntry{})
		}
		s.fds[5] = fdEntry{fdType: 4, file: &FSFileWrap{File: f}}

		errno := s.Xfd_filestat_set_size(5, 99)
		if errno != wasiESuccess {
			t.Errorf("got errno %d, want ESUCCESS", errno)
		}
		// Verify no mutation
		data, err := os.ReadFile(filepath.Join(dir, "ro.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "HELLO" {
			t.Errorf("file was mutated: got %q, want %q", data, "HELLO")
		}
	})

	t.Run("Xfd_filestat_set_times fstFlags=2 updates mtime on osFile", func(t *testing.T) {
		s, _ := newTestState()
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		errno := s.Xfd_filestat_set_times(5, 0, targetMtimNs, 2)
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
		s, _ := newTestState()
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

	t.Run("Xpath_filestat_set_times fstFlags=2 updates mtime by path", func(t *testing.T) {
		s, buf := newTestState()
		hostDir := setupWritableMount(t, s, buf)
		fname := "testfile.txt"
		if err := os.WriteFile(filepath.Join(hostDir, fname), []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
		pathOff, pathLen := writePath(buf, 500, fname)

		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, targetMtimNs, 2)
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
		s, buf := newTestState()
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

	t.Run("Xfd_filestat_set_times fstFlags=MTIM_NOW updates mtime to current", func(t *testing.T) {
		s, _ := newTestState()
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		// Set to a past time first to ensure NOW change is visible
		past := time.Now().Add(-1 * time.Hour)
		if err := os.Chtimes(filePath, past, past); err != nil {
			t.Fatal(err)
		}

		// 8 is MTIM_NOW
		errno := s.Xfd_filestat_set_times(5, 0, 0, 8)
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

	t.Run("Xfd_filestat_set_times fstFlags=ATIM_NOW updates atime but not mtime", func(t *testing.T) {
		s, _ := newTestState()
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

		// 4 is ATIM_NOW
		errno := s.Xfd_filestat_set_times(5, 0, 0, 4)
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
		s, buf := newTestState()
		_ = setupWritableMount(t, s, buf)
		pathOff, pathLen := writePath(buf, 500, "/nonexistent/path/deleted.txt")
		// fstMtim = 2
		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, targetMtimNs, 2)
		if errno == wasiESuccess {
			t.Error("Xpath_filestat_set_times on missing path returned ESUCCESS, want error")
		}
	})

	t.Run("Xpath_filestat_set_times fstFlags=MTIM_NOW updates mtime to current", func(t *testing.T) {
		s, buf := newTestState()
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

		// 8 is MTIM_NOW
		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, 0, 0, 8)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_set_times fstFlags=8 returned %d, want ESUCCESS", errno)
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

	t.Run("Xpath_filestat_set_times fstFlags=ATIM updates atime by path", func(t *testing.T) {
		s, buf := newTestState()
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

		// 1 is ATIM
		errno := s.Xpath_filestat_set_times(3, 0, pathOff, pathLen, int64(targetAtimNs), 0, 1)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_filestat_set_times fstFlags=1 returned %d, want ESUCCESS", errno)
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
}
