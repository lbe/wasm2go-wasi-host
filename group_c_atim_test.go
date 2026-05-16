package wasihost

import (
	"os"
	"testing"
	"time"
)

func TestFdFilestatSetTimesAtim(t *testing.T) {
	s, _ := newTestState()
	filePath := setupOsFileFd(t, s, 5, []byte("data"))

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
	errno := s.Xfd_filestat_set_times(5, targetAtimNs, 0, 1)
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

	t.Run("Xfd_filestat_set_times fstFlags=2 updates mtime on osFile", func(t *testing.T) {
		s, _ := newTestState()
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		// fstMtim = 2
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

	t.Run("Xfd_filestat_set_times fstFlags=MTIM_NOW updates mtime to current", func(t *testing.T) {
		s, _ := newTestState()
		filePath := setupOsFileFd(t, s, 5, []byte("data"))

		past := time.Now().Add(-1 * time.Hour)
		if err := os.Chtimes(filePath, past, past); err != nil {
			t.Fatal(err)
		}

		// fstMtimNow = 8
		errno := s.Xfd_filestat_set_times(5, 0, 0, 8)
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
