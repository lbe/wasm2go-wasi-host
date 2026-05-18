package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
	s, _ := newTestState()
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
		s2, buf := newTestState()
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
	s, buf := newTestState()
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
