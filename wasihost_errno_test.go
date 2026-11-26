package wasihost

import (
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// TestErrnoFromWasiSpecValues checks every internal errno against the numeric
// value in the WASI snapshot-preview1 specification. An internal errno with no
// case in errnoFromWasi silently becomes EIO (29).
func TestErrnoFromWasiSpecValues(t *testing.T) {
	tests := []struct {
		in   wasi.Errno
		want int32
	}{
		{0, 0},
		{wasi.EACCES, 2},
		{wasi.EAGAIN, 6},
		{wasi.EBADF, 8},
		{wasi.EEXIST, 20},
		{wasi.EFAULT, 21},
		{wasi.EINTR, 27},
		{wasi.EINVAL, 28},
		{wasi.EIO, 29},
		{wasi.EISDIR, 31},
		{wasi.ELOOP, 32},
		{wasi.ENAMETOOLONG, 37},
		{wasi.ENOENT, 44},
		{wasi.ENOSYS, 52},
		{wasi.ENOTDIR, 54},
		{wasi.ENOTEMPTY, 55},
		{wasi.ENOTSUP, 58},
		{wasi.ENOTSOCK, 57},
		{wasi.EPERM, 63},
		{wasi.EROFS, 69},
		{wasi.ERANGE, 68},
		{wasi.EXDEV, 75},
	}
	for _, tt := range tests {
		if got := errnoFromWasi(tt.in); got != tt.want {
			t.Errorf("errnoFromWasi(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestWasiErrnoConstantsMatchSpec pins the guest-visible errno constants to the
// WASI snapshot-preview1 numbering (wasi crate 0.11 lib_generated.rs, generated
// from the preview1 witx). A wrong value here is returned to every guest.
func TestWasiErrnoConstantsMatchSpec(t *testing.T) {
	tests := []struct {
		name string
		got  int32
		want int32
	}{
		{"SUCCESS", wasiESuccess, 0},
		{"ACCES", wasiEAcces, 2},
		{"AGAIN", wasiEAgain, 6},
		{"BADF", wasiEBadf, 8},
		{"EXIST", wasiEExist, 20},
		{"FAULT", wasiEFault, 21},
		{"ILSEQ", wasiEIlseq, 25},
		{"INTR", wasiEIntr, 27},
		{"INVAL", wasiEInval, 28},
		{"IO", wasiEIo, 29},
		{"ISDIR", wasiEIsdir, 31},
		{"LOOP", wasiELoop, 32},
		{"NAMETOOLONG", wasiENameTooLong, 37},
		{"NOENT", wasiENoEnt, 44},
		{"NOSYS", wasiENoSys, 52},
		{"NOTDIR", wasiENotDir, 54},
		{"NOTEMPTY", wasiENotEmpty, 55},
		{"NOTSOCK", wasiENotSock, 57},
		{"NOTSUP", wasiENotSup, 58},
		{"PERM", wasiEPerm, 63},
		{"RANGE", wasiERange, 68},
		{"ROFS", wasiEROFS, 69},
		{"XDEV", wasiEXdev, 75},
		{"NOTCAPABLE", wasiENotCap, 76},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("ERRNO_%s = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}
