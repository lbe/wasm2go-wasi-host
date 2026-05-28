package wasi

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInodeTypeAlias(t *testing.T) {
	var i Inode = 42
	if uint64(i) != 42 {
		t.Fatalf("Inode should be uint64 alias, got %T", i)
	}
}

func TestEpochNanosTypeAlias(t *testing.T) {
	var e EpochNanos = 1000000000
	if int64(e) != 1000000000 {
		t.Fatalf("EpochNanos should be int64 alias, got %T", e)
	}
}

func TestStat_tFields(t *testing.T) {
	now := EpochNanos(time.Now().UnixNano())
	st := Stat_t{
		Dev:   1,
		Ino:   Inode(123),
		Mode:  fs.ModeDir | 0o755,
		Nlink: 2,
		Size:  4096,
		Atim:  now,
		Mtim:  now,
		Ctim:  now,
	}

	if st.Dev != 1 {
		t.Errorf("Dev: got %d, want 1", st.Dev)
	}
	if st.Ino != 123 {
		t.Errorf("Ino: got %d, want 123", st.Ino)
	}
	if st.Mode != (fs.ModeDir | 0o755) {
		t.Errorf("Mode: got %v, want %v", st.Mode, fs.ModeDir|0o755)
	}
	if st.Nlink != 2 {
		t.Errorf("Nlink: got %d, want 2", st.Nlink)
	}
	if st.Size != 4096 {
		t.Errorf("Size: got %d, want 4096", st.Size)
	}
	if st.Atim != now {
		t.Errorf("Atim: got %d, want %d", st.Atim, now)
	}
	if st.Mtim != now {
		t.Errorf("Mtim: got %d, want %d", st.Mtim, now)
	}
	if st.Ctim != now {
		t.Errorf("Ctim: got %d, want %d", st.Ctim, now)
	}
}

func TestNewStat_tFromFileInfo(t *testing.T) {
	// Create a real temp file to get an os.FileInfo
	dir := t.TempDir()
	file := filepath.Join(dir, "testfile")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("os.Stat failed: %v", err)
	}

	st := NewStat_t(info)

	// Basic sanity checks on the returned Stat_t
	if st.Size != 5 {
		t.Errorf("Size: got %d, want 5", st.Size)
	}
	if st.Nlink == 0 {
		t.Error("Nlink should be at least 1")
	}
	if st.Mode&fs.ModeType != 0 {
		t.Errorf("Mode type bits: got %v, want regular file (0)", st.Mode&fs.ModeType)
	}
	if st.Mtim == 0 {
		t.Error("Mtim should not be zero for a real file")
	}
	if st.Atim == 0 {
		t.Error("Atim should not be zero for a real file on BSD/macOS")
	}
	if st.Ctim == 0 {
		t.Error("Ctim should not be zero for a real file on BSD/macOS")
	}
}

func TestNewStat_tDefaultFallback(t *testing.T) {
	// Use a mock fs.FileInfo with nil Sys() to trigger defaultStatFromFileInfo
	info := &mockFileInfo{
		name:    "mock",
		size:    100,
		mode:    0o644,
		modTime: time.Now(),
	}

	st := NewStat_t(info)

	if st.Size != 100 {
		t.Errorf("Size: got %d, want 100", st.Size)
	}
	if st.Nlink != 1 {
		t.Errorf("Nlink: got %d, want 1 (default)", st.Nlink)
	}
	if st.Dev != 0 {
		t.Errorf("Dev: got %d, want 0 (default)", st.Dev)
	}
	if st.Ino != 0 {
		t.Errorf("Ino: got %d, want 0 (default)", st.Ino)
	}
	// All timestamps should equal ModTime when Sys() is nil
	if st.Atim != st.Mtim {
		t.Errorf("Atim (%d) should equal Mtim (%d) in default fallback", st.Atim, st.Mtim)
	}
	if st.Ctim != st.Mtim {
		t.Errorf("Ctim (%d) should equal Mtim (%d) in default fallback", st.Ctim, st.Mtim)
	}
}

// mockFileInfo implements fs.FileInfo with a nil Sys() return.
type mockFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() fs.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return m.modTime }
func (m *mockFileInfo) IsDir() bool        { return m.mode.IsDir() }
func (m *mockFileInfo) Sys() interface{}   { return nil }
