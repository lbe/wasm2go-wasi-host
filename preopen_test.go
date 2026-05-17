package wasihost

import (
	"encoding/binary"
	"testing"
)

func TestHostDirectoryPreopen(t *testing.T) {
	// A host directory can be exposed as a WASI preopened directory.
	// Construction with a host directory preopen assigns fd 3 as a directory preopen;
	// fd_prestat_get and fd_prestat_dir_name return the configured guest path,
	// and fd_fdstat_get reports a directory fd with directory/path rights.

	tmpDir := t.TempDir()
	guestPath := "/host-data"
	buf := make([]byte, 1024)
	mem := func() []byte { return buf }

	// RED PHASE: The plan calls for a new WithHostDirectoryPreopen option or similar
	// that we will implement in the next phase. For now, I'll use a hypothetical
	// constructor that encodes the acceptance criteria.
	//
	// The refactor plan likely wants to introduce a more WASI-standard "preopen"
	// concept that specifically maps to fd 3+ in a way that matches WASI expectations.

	s := New(mem, WithHostDirectoryPreopen(guestPath, tmpDir))

	t.Run("fd 3 is a preopen", func(t *testing.T) {
		const prestatPtr = 100
		errno := s.Xfd_prestat_get(3, prestatPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_prestat_get(3) = %d, want ESUCCESS", errno)
		}

		// prestat struct: tag (u32, 0 for dir) + name_len (u32)
		tag := binary.LittleEndian.Uint32(buf[prestatPtr:])
		if tag != 0 {
			t.Errorf("prestat.tag = %d, want 0 (directory)", tag)
		}

		nameLen := binary.LittleEndian.Uint32(buf[prestatPtr+4:])
		if int(nameLen) != len(guestPath) {
			t.Errorf("prestat.name_len = %d, want %d", nameLen, len(guestPath))
		}

		const namePtr = 200
		errno = s.Xfd_prestat_dir_name(3, namePtr, int32(nameLen))
		if errno != wasiESuccess {
			t.Fatalf("fd_prestat_dir_name(3) = %d, want ESUCCESS", errno)
		}

		gotPath := string(buf[namePtr : namePtr+int32(nameLen)])
		if gotPath != guestPath {
			t.Errorf("fd_prestat_dir_name = %q, want %q", gotPath, guestPath)
		}
	})

	t.Run("fd 3 has correct fdstat", func(t *testing.T) {
		const statPtr = 300
		errno := s.Xfd_fdstat_get(3, statPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_fdstat_get(3) = %d, want ESUCCESS", errno)
		}

		// fdstat: fs_filetype (u8) at offset 0
		ftype := buf[statPtr]
		if ftype != fdDir {
			t.Errorf("fdstat.fs_filetype = %d, want %d (directory)", ftype, fdDir)
		}

		// rights_base (u64) at offset 8
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8:])

		// Expected directory/path rights.
		// Exact bits depend on final implementation, but should include at least read/readdir.
		// For now, we'll check some characteristic directory rights.
		if rightsBase&rightFDFilestatGet == 0 {
			t.Error("rights_base missing rightFDFilestatGet")
		}
	})
}
