package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestByteIOOnDirectoryFDsRejected verifies that fd_read, fd_pread, fd_write,
// and fd_pwrite on directory file descriptors return EISDIR, EBADF, or ENOTCAP
// rather than treating the fd as a regular file.
func TestByteIOOnDirectoryFDsRejected(t *testing.T) {
	t.Parallel()
	const (
		iovsOff     = 1024
		dataBuf     = 2048
		nreadOff    = 512
		nwrittenOff = 520
		fdPtr       = 3000
		pathOff     = 4000
		rdBufPtr    = 5000
		rdBufLen    = 4096
		usedPtr     = 6000
	)

	allowed := map[int32]bool{
		wasiEIsdir:  true,
		wasiEBadf:   true,
		wasiENotCap: true,
	}

	cases := []struct {
		name       string
		syscall    func(s *State, buf []byte, subdirFD int32) int32
		resultOff  int32
		resultName string
	}{
		{
			name: "fd_read on writable preopen dir fd",
			syscall: func(s *State, buf []byte, _ int32) int32 {
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_read(dirfd, iovsOff, 1, nreadOff)
			},
			resultOff:  nreadOff,
			resultName: "nread",
		},
		{
			name: "fd_pread on writable preopen dir fd",
			syscall: func(s *State, buf []byte, _ int32) int32 {
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_pread(dirfd, iovsOff, 1, 0, nreadOff)
			},
			resultOff:  nreadOff,
			resultName: "nread",
		},
		{
			name: "fd_write on writable preopen dir fd",
			syscall: func(s *State, buf []byte, _ int32) int32 {
				copy(buf[dataBuf:], "test")
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_write(dirfd, iovsOff, 1, nwrittenOff)
			},
			resultOff:  nwrittenOff,
			resultName: "nwritten",
		},
		{
			name: "fd_pwrite on writable preopen dir fd",
			syscall: func(s *State, buf []byte, _ int32) int32 {
				copy(buf[dataBuf:], "test")
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_pwrite(dirfd, iovsOff, 1, 0, nwrittenOff)
			},
			resultOff:  nwrittenOff,
			resultName: "nwritten",
		},
		{
			name: "fd_read on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, buf []byte, subdirFD int32) int32 {
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_read(subdirFD, iovsOff, 1, nreadOff)
			},
			resultOff:  nreadOff,
			resultName: "nread",
		},
		{
			name: "fd_pread on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, buf []byte, subdirFD int32) int32 {
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_pread(subdirFD, iovsOff, 1, 0, nreadOff)
			},
			resultOff:  nreadOff,
			resultName: "nread",
		},
		{
			name: "fd_write on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, buf []byte, subdirFD int32) int32 {
				copy(buf[dataBuf:], "test")
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_write(subdirFD, iovsOff, 1, nwrittenOff)
			},
			resultOff:  nwrittenOff,
			resultName: "nwritten",
		},
		{
			name: "fd_pwrite on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, buf []byte, subdirFD int32) int32 {
				copy(buf[dataBuf:], "test")
				binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataBuf))
				binary.LittleEndian.PutUint32(buf[iovsOff+4:], 4)
				return s.Xfd_pwrite(subdirFD, iovsOff, 1, 0, nwrittenOff)
			},
			resultOff:  nwrittenOff,
			resultName: "nwritten",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, buf, tmpDir := newWMState(t)

			if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0o755); err != nil {
				t.Fatal(err)
			}

			copy(buf[pathOff:], "subdir")
			errno := s.Xpath_open(dirfd, 0, pathOff, 6, int32(oflagDir), int64(rightsWritableDirPreopen), 0, 0, fdPtr)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_open(subdir) = %d, want ESUCCESS", errno)
			}
			subdirFD := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

			errno = s.Xfd_readdir(dirfd, rdBufPtr, rdBufLen, 0, usedPtr)
			if errno != wasiESuccess {
				t.Fatalf("Xfd_readdir(preopen) = %d, want ESUCCESS", errno)
			}

			binary.LittleEndian.PutUint32(buf[tc.resultOff:], 0xDEADBEEF)

			errno = tc.syscall(s, buf, subdirFD)
			if !allowed[errno] {
				t.Errorf("%s = %d, want one of EISDIR (%d), EBADF (%d), ENOTCAP (%d)",
					tc.name, errno, wasiEIsdir, wasiEBadf, wasiENotCap)
			}
		})
	}
}
