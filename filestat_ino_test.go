package wasihost

import (
	"encoding/binary"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"testing/fstest"
)

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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", contDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", mutDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem, WithReadOnlyFS("/ro", fstest.MapFS{
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

	s := New(mem, WithReadOnlyFS("/data", mapFS))

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
// errThrowSeeker implements fs.ReadDirFile and io.Seeker; Seek returns io.ErrClosedPipe.
type errThrowSeeker struct {
	fs.File
}

func (errThrowSeeker) Read([]byte) (int, error)             { return 0, io.EOF }
func (errThrowSeeker) Stat() (fs.FileInfo, error)           { return nil, nil }
func (errThrowSeeker) Close() error                         { return nil }
func (errThrowSeeker) ReadDir(_ int) ([]fs.DirEntry, error) { return nil, nil }
func (errThrowSeeker) Seek(_ int64, _ int) (int64, error)   { return 0, io.ErrClosedPipe }

// TestFdReaddirParentDino verifies that synthetic ".." on writable host paths
// gets the parent directory inode, not the directory's own inode.
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

	parentInfo, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("os.Stat(parent) failed: %v", err)
	}
	parentIno := parentInfo.Sys().(*syscall.Stat_t).Ino

	subInfo, err := os.Stat(subDir)
	if err != nil {
		t.Fatalf("os.Stat(sub) failed: %v", err)
	}
	subIno := subInfo.Sys().(*syscall.Stat_t).Ino

	if parentIno == 0 || subIno == 0 {
		t.Skip("host reports st_ino == 0")
	}
	if parentIno == subIno {
		t.Fatalf("test requires parent and subdirectory to have distinct inodes")
	}

	buf := make([]byte, 65536)
	mem := func() []byte { return buf }
	s := New(mem, WithHostDirectoryPreopen("/data", subDir))

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
	if dotDotIno != parentIno {
		t.Errorf("entry[1].Ino = %d, want %d (parent directory ino)", dotDotIno, parentIno)
	}

	// Ensure they're distinct (the bug was using the same ino for both)
	if dotIno == dotDotIno {
		t.Error(". and .. have the same dIno; expected distinct inodes")
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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem, WithHostDirectoryPreopen("/data", tmpDir))

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
		s := New(mem)

		errno := s.Xfd_readdir(999, rdBufPtr, rdBufLen, 0, usedPtr)
		if errno != wasiEBadf {
			t.Fatalf("Xfd_readdir(999) = %d, want EBADF (%d)", errno, wasiEBadf)
		}
	})

	// --- Subtest 4: seek error returns EIO ---
	t.Run("seek error returns EIO", func(t *testing.T) {
		buf := make([]byte, 65536)
		mem := func() []byte { return buf }
		s := New(mem)

		// Override fd 3 with our seek-error file.
		// Grow fds slice to make room.
		for len(s.fds) < 4 {
			s.fds = append(s.fds, fdEntry{})
		}
		s.fds[3] = fdEntry{
			file:   &errThrowSeeker{},
			fdType: 3,
		}

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
