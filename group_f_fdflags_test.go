package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestFdFlags(t *testing.T) {
	t.Parallel()

	const (
		fdPtr       = 1000
		pathOff     = 2000
		iovsOff     = 3000
		dataBuf     = 4000
		nwrittenOff = 5000
		statPtr     = 6000
		tellOff     = 7000
	)

	// fd_flags values from WASI spec
	const (
		fdFlagsAppend   int32 = 1 << 0
		fdFlagsDSync    int32 = 1 << 1
		fdFlagsNonBlock int32 = 1 << 2
		fdFlagsRSync    int32 = 1 << 3
		fdFlagsSync     int32 = 1 << 4
	)

	t.Run("FDFLAGS_APPEND causes fd_write to append regardless of offset", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "append.txt"
		initialData := []byte("HELLO")
		if err := os.WriteFile(filepath.Join(tmpDir, fname), initialData, 0644); err != nil {
			t.Fatal(err)
		}

		copy(buf[pathOff:], fname)
		// Open with FDFLAGS_APPEND
		errno := s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)),
			0, int64(rightFDWrite|rightFDRead|rightFDSeek), 0, fdFlagsAppend, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_open append = %d", errno)
		}
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// Seek to middle of file
		var newOffPtr int32 = 8000
		if errno := s.Xfd_seek(fd, 2, 0, newOffPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_seek = %d", errno)
		}

		// Write "WORLD"
		copy(buf[dataBuf:], "WORLD")
		binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
		binary.LittleEndian.PutUint32(buf[iovsOff+4:], 5)
		if errno := s.Xfd_write(fd, iovsOff, 1, nwrittenOff); errno != wasiESuccess {
			t.Fatalf("Xfd_write = %d", errno)
		}

		// Verify content is "HELLOWORLD" not "HEWORLO"
		data, err := os.ReadFile(filepath.Join(tmpDir, fname))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "HELLOWORLD" {
			t.Errorf("content = %q, want HELLOWORLD (append behavior)", string(data))
		}
	})

	t.Run("fd_fdstat_get reports stored fd flags", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "statflags.txt"
		os.WriteFile(filepath.Join(tmpDir, fname), []byte("x"), 0644)
		copy(buf[pathOff:], fname)

		// Open with APPEND and DSYNC
		requestedFlags := fdFlagsAppend | fdFlagsDSync
		s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(rightFDRead), 0, requestedFlags, fdPtr)
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		if errno := s.Xfd_fdstat_get(fd, statPtr); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get = %d", errno)
		}

		// fd_flags is uint16 at offset 2 of fdstat
		gotFlags := int32(binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4]))
		if gotFlags != requestedFlags {
			t.Errorf("got fd_flags = %d, want %d", gotFlags, requestedFlags)
		}
	})

	t.Run("fd_fdstat_set_flags updates supported flags and rejects unsupported", func(t *testing.T) {
		s, buf, tmpDir := newWMState(t)
		fname := "setflags.txt"
		os.WriteFile(filepath.Join(tmpDir, fname), []byte("x"), 0644)
		copy(buf[pathOff:], fname)
		s.Xpath_open(dirfd, 0, pathOff, int32(len(fname)), 0, int64(rightFDRead), 0, 0, fdPtr)
		fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

		// Set APPEND
		if errno := s.Xfd_fdstat_set_flags(fd, fdFlagsAppend); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_set_flags(APPEND) = %d", errno)
		}

		// Verify it was set
		s.Xfd_fdstat_get(fd, statPtr)
		gotFlags := int32(binary.LittleEndian.Uint16(buf[statPtr+2 : statPtr+4]))
		if (gotFlags & fdFlagsAppend) == 0 {
			t.Error("APPEND flag was not set")
		}

		// Try to set an invalid/unsupported flag (WASI only allows setting APPEND, DSYNC, NONBLOCK, RSYNC, SYNC)
		// We use a bit that is definitely not in the spec for fdflags.
		const unknownFlag int32 = 1 << 15
		if errno := s.Xfd_fdstat_set_flags(fd, unknownFlag); errno != wasiEInval {
			t.Errorf("Xfd_fdstat_set_flags(unknown) = %d, want EInval", errno)
		}
	})
}
