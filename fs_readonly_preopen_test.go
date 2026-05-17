package wasihost

import (
	"encoding/binary"
	"testing"
	"testing/fstest"
)

func TestReadOnlyFSPreopen(t *testing.T) {
	// Acceptance criteria:
	// Constructing State with a read-only fs.FS preopen allows opening and reading
	// existing files but reports no write/path-mutation rights and returns a
	// read-only/capability error for create, truncate, rename, unlink, mkdir,
	// link, symlink, and writable path_open attempts.

	guestPath := "/data"
	fileName := "hello.txt"
	fileContent := "hello world"
	readOnlyFS := fstest.MapFS{
		fileName: {Data: []byte(fileContent)},
	}

	buf := make([]byte, 1024)
	mem := func() []byte { return buf }

	// API expectation: WithReadOnlyFS
	s := New(mem, WithReadOnlyFS(guestPath, readOnlyFS))

	t.Run("open and read existing file succeeds", func(t *testing.T) {
		const fdPtr = 100
		pathPtr := 200
		copy(buf[pathPtr:], fileName)

		// path_open(dirfd=3, lookupflags=0, path, path_len, oflags=0, rights_base=read, rights_inheriting=0, fdflags=0, fd_ptr)
		errno := s.Xpath_open(3, 0, int32(pathPtr), int32(len(fileName)), 0, int64(rightFDRead), 0, 0, int32(fdPtr))
		if errno != wasiESuccess {
			t.Fatalf("path_open(%q) = %d, want ESUCCESS", fileName, errno)
		}

		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr:]))

		// fd_read
		const iovsPtr = 300
		const nreadPtr = 400
		const readBufPtr = 500
		binary.LittleEndian.PutUint32(buf[iovsPtr:], uint32(readBufPtr))
		binary.LittleEndian.PutUint32(buf[iovsPtr+4:], uint32(len(fileContent)))

		errno = s.Xfd_read(fd, iovsPtr, 1, nreadPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_read = %d, want ESUCCESS", errno)
		}

		nread := binary.LittleEndian.Uint32(buf[nreadPtr:])
		if int(nread) != len(fileContent) {
			t.Errorf("nread = %d, want %d", nread, len(fileContent))
		}
		if string(buf[readBufPtr:readBufPtr+int(nread)]) != fileContent {
			t.Errorf("read content = %q, want %q", string(buf[readBufPtr:readBufPtr+int(nread)]), fileContent)
		}
	})

	t.Run("fd 3 (preopen) reports no write or path-mutation rights", func(t *testing.T) {
		const statPtr = 600
		errno := s.Xfd_fdstat_get(3, statPtr)
		if errno != wasiESuccess {
			t.Fatalf("fd_fdstat_get(3) = %d, want ESUCCESS", errno)
		}

		// rights_base (u64) at offset 8
		rightsBase := binary.LittleEndian.Uint64(buf[statPtr+8:])

		// Characteristic write/mutation rights that SHOULD be missing
		mutationRights := []struct {
			name uint64
			desc string
		}{
			{rightFDWrite, "rightFDWrite"},
		}

		for _, r := range mutationRights {
			if rightsBase&r.name != 0 {
				t.Errorf("rights_base HAS %s, want it missing for read-only FS", r.desc)
			}
		}
	})

	t.Run("mutation attempts return EROFS or NOTCAPABLE", func(t *testing.T) {
		pathPtr := 700
		copy(buf[pathPtr:], "newdir")

		// path_create_directory
		errno := s.Xpath_create_directory(3, int32(pathPtr), 6)
		if errno != wasiEROFS && errno != wasiENotCap {
			t.Errorf("path_create_directory = %d, want EROFS or ENOTCAP", errno)
		}

		// path_unlink_file
		copy(buf[pathPtr:], fileName)
		errno = s.Xpath_unlink_file(3, int32(pathPtr), int32(len(fileName)))
		if errno != wasiEROFS && errno != wasiENotCap {
			t.Errorf("path_unlink_file = %d, want EROFS or ENOTCAP", errno)
		}

		// path_open with oflagCreat on existing file
		const fdPtr = 800
		copy(buf[pathPtr:], fileName)
		// oflagCreat = 1
		errno = s.Xpath_open(3, 0, int32(pathPtr), int32(len(fileName)), 1, int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
		if errno != wasiEROFS && errno != wasiENotCap {
			t.Errorf("path_open(O_CREAT) on %q = %d, want EROFS or ENOTCAP", fileName, errno)
		}
	})
}
