//go:build darwin

package sysfs

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestFutimensDoesNotCloseFd guards against futimens wrapping the caller's fd in
// an *os.File: that wrapper's GC cleanup closes the fd, so a later, unrelated
// operation on the guest's open file fails with EBADF (or hits a reused fd).
func TestFutimensDoesNotCloseFd(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd := f.Fd()

	mtim := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC).UnixNano()
	if err := futimens(fd, mtim, mtim); err != nil {
		t.Fatalf("futimens: %v", err)
	}

	// Give any finalizer attached to a discarded wrapper a chance to run.
	for range 5 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	var st syscall.Stat_t
	if err := syscall.Fstat(int(fd), &st); err != nil {
		t.Fatalf("fd %d no longer valid after futimens + GC: %v", fd, err)
	}
	runtime.KeepAlive(f)
}
