package wasihost

import (
	"encoding/binary"
	"testing"
)

// TestParseReaddirDirents verifies that the shared parseReaddirDirents helper
// in helpers_test.go correctly parses a synthetic WASI dirent buffer into
// ReaddirDirent structs. This test will fail to compile until the helper and
// the ReaddirDirent type are extracted from the duplicated parseEntries
// closures in filestat_ino_test.go.
func TestParseReaddirDirents(t *testing.T) {
	// Build a synthetic dirent buffer with two entries:
	//   1. "."   (dIno=100, dType=fdDir, dNamlen=1)
	//   2. "foo" (dIno=200, dType=fdFile, dNamlen=3)
	const bufPtr int32 = 0

	buf := make([]byte, 128)

	// Entry 1: "."
	off := bufPtr
	binary.LittleEndian.PutUint64(buf[off:off+8], 24+1)              // dNext: points past this entry
	binary.LittleEndian.PutUint64(buf[off+8:off+16], 100)            // dIno
	binary.LittleEndian.PutUint32(buf[off+16:off+20], 1)             // dNamlen
	binary.LittleEndian.PutUint32(buf[off+20:off+24], uint32(fdDir)) // dType
	copy(buf[off+24:off+25], ".")

	// Entry 2: "foo"
	off2 := bufPtr + 24 + 1                                             // 24 header + 1 name byte
	binary.LittleEndian.PutUint64(buf[off2:off2+8], 24+3)               // dNext
	binary.LittleEndian.PutUint64(buf[off2+8:off2+16], 200)             // dIno
	binary.LittleEndian.PutUint32(buf[off2+16:off2+20], 3)              // dNamlen
	binary.LittleEndian.PutUint32(buf[off2+20:off2+24], uint32(fdFile)) // dType
	copy(buf[off2+24:off2+27], "foo")

	bufUsed := uint32((off2 - bufPtr) + 24 + 3) // total bytes consumed

	entries := parseReaddirDirents(buf, bufUsed, bufPtr)

	if len(entries) != 2 {
		t.Fatalf("parseReaddirDirents returned %d entries, want 2", len(entries))
	}

	// Verify entry 0: "."
	if entries[0].Name != "." {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, ".")
	}
	if entries[0].Ino != 100 {
		t.Errorf("entries[0].Ino = %d, want 100", entries[0].Ino)
	}
	if entries[0].Type != fdDir {
		t.Errorf("entries[0].Type = %d, want fdDir (%d)", entries[0].Type, fdDir)
	}
	if entries[0].Namlen != 1 {
		t.Errorf("entries[0].Namlen = %d, want 1", entries[0].Namlen)
	}

	// Verify entry 1: "foo"
	if entries[1].Name != "foo" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "foo")
	}
	if entries[1].Ino != 200 {
		t.Errorf("entries[1].Ino = %d, want 200", entries[1].Ino)
	}
	if entries[1].Type != fdFile {
		t.Errorf("entries[1].Type = %d, want fdFile (%d)", entries[1].Type, fdFile)
	}
	if entries[1].Namlen != 3 {
		t.Errorf("entries[1].Namlen = %d, want 3", entries[1].Namlen)
	}
}
