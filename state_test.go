package wasihost

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestInitializationAndOSErrorMapping(t *testing.T) {
	mem := func() []byte { return make([]byte, 65536) }

	// Test New returns non-nil
	s := New(mem)
	if s == nil {
		t.Errorf("New(mem) returned nil, want non-nil *State")
	}

	// Test Options don't panic
	options := []Option{
		WithArgs("arg1"),
		WithEnv("VAR=VAL"),
		WithMount("/tmp", os.DirFS("/tmp")),
		WithWritableMount("/data", "/tmp/data", os.DirFS("/tmp/data")),
		WithStdin(os.Stdin),
		WithStdout(os.Stdout),
		WithStderr(os.Stderr),
		WithTracing(),
		WithOwnerAssertion(),
	}

	for i, opt := range options {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Option %d panicked: %v", i, r)
				}
			}()
			New(mem, opt)
		}()
	}

	// Test ExitError.Error() is non-empty
	exitErr := ExitError{Code: 1}
	if exitErr.Error() == "" {
		t.Errorf("ExitError.Error() returned empty string, want non-empty")
	}

	// Test mapOSError
	tests := []struct {
		err  error
		want int32
	}{
		{os.ErrNotExist, int32(wasiENoEnt)},
		{os.ErrExist, int32(wasiEExist)},
		{syscall.ENOTEMPTY, int32(wasiENotEmpty)},
		{syscall.ENOTDIR, int32(wasiENotDir)},
		{syscall.EISDIR, int32(wasiEIsdir)},
		{syscall.EACCES, int32(wasiEAcces)},
		{syscall.EPERM, int32(wasiEPerm)},
		{syscall.EROFS, int32(wasiEROFS)},
		{syscall.EXDEV, int32(wasiEXdev)},
		{syscall.EINVAL, int32(wasiEInval)},
		{errors.New("unknown"), int32(wasiEIo)},
	}

	for _, tt := range tests {
		got := mapOSError(tt.err)
		if got != tt.want {
			t.Errorf("mapOSError(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}
