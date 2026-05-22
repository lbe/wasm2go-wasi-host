package wasihost

import (
	"encoding/binary"
	"runtime"
	"testing"
)

// TestWritablePreopenRightsMatchWasiTestsuite verifies writable directory preopens
// advertise the directory base and inheriting rights masks from path_open_preopen.rs.
func TestWritablePreopenRightsMatchWasiTestsuite(t *testing.T) {
	const (
		statPtr = 9000
		fdPtr   = 9100
		pathOff = 9200
	)

	buf := make([]byte, 65536)
	tmpDir := t.TempDir()
	s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", tmpDir))

	const preopenFd = int32(3)

	if errno := s.Xfd_fdstat_get(preopenFd, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(preopen) = %d, want ESUCCESS", errno)
	}
	gotBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	gotInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])

	if gotBase != rightsWritableDirPreopenBase {
		t.Errorf("fs_rights_base = %#x, want %#x (rightsWritableDirPreopenBase)", gotBase, rightsWritableDirPreopenBase)
	}
	if gotInh != rightsWritableDirPreopenInheriting {
		t.Errorf("fs_rights_inheriting = %#x, want %#x (rightsWritableDirPreopenInheriting)", gotInh, rightsWritableDirPreopenInheriting)
	}

	const dot = "."
	copy(buf[pathOff:], dot)

	errno := s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), 0,
		int64(gotBase), int64(gotInh), 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., same rights) = %d, want ESUCCESS", errno)
	}

	errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), 0, 0, 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., empty rights) = %d, want ESUCCESS", errno)
	}

	errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), int32(oflagDir), 0, 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., O_DIRECTORY, empty rights) = %d, want ESUCCESS", errno)
	}

	errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), int32(oflagDir),
		int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(., O_DIRECTORY, FD_READ) = %d, want ESUCCESS", errno)
	}

	if runtime.GOOS != "windows" {
		errno = s.Xpath_open(preopenFd, 0, pathOff, int32(len(dot)), int32(oflagDir),
			int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
		if errno != wasiEIsdir {
			t.Fatalf("path_open(., O_DIRECTORY, FD_READ|FD_WRITE) = %d, want EISDIR (%d)", errno, wasiEIsdir)
		}
	}
}
