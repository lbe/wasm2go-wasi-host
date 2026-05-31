package wasi_test

import (
	"bytes"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/testfixture"
)

// TestFSContextDualPreopenReadFSAndAdaptFS verifies FSContext fd-table operations
// with sysfs.ReadFS and sysfs.AdaptFS preopens backed by testfixture.MapFS.
func TestFSContextDualPreopenReadFSAndAdaptFS(t *testing.T) {
	mapFS := testfixture.MapFS{}
	readFS := sysfs.ReadFS{FS: mapFS}
	adaptFS := sysfs.AdaptFS{FS: mapFS}

	var buf bytes.Buffer
	fsc, err := wasi.InitFSContext(nil, &buf, &buf, []wasi.FS{readFS, adaptFS}, []string{"/ro", "/embed"})
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}

	readPreopen, ok := fsc.LookupFile(3)
	if !ok || !readPreopen.IsPreopen {
		t.Fatal("LookupFile(3): ReadFS preopen missing")
	}
	if readPreopen.Name != "/ro" {
		t.Fatalf("LookupFile(3): Name = %q, want %q", readPreopen.Name, "/ro")
	}

	adaptPreopen, ok := fsc.LookupFile(4)
	if !ok || !adaptPreopen.IsPreopen {
		t.Fatal("LookupFile(4): AdaptFS preopen missing")
	}
	if adaptPreopen.Name != "/embed" {
		t.Fatalf("LookupFile(4): Name = %q, want %q", adaptPreopen.Name, "/embed")
	}

	readFd, errno := fsc.OpenFile(readFS, "animals.txt", wasi.O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("OpenFile(readFS, animals.txt): errno %d", errno)
	}
	if readFd != 5 {
		t.Fatalf("OpenFile(readFS, animals.txt): got fd %d, want 5", readFd)
	}

	adaptFd, errno := fsc.OpenFile(adaptFS, "sub/test.txt", wasi.O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("OpenFile(adaptFS, sub/test.txt): errno %d", errno)
	}
	if adaptFd != 6 {
		t.Fatalf("OpenFile(adaptFS, sub/test.txt): got fd %d, want 6", adaptFd)
	}

	wantSub, err := mapFS.ReadFile("sub/test.txt")
	if err != nil {
		t.Fatalf("fixture ReadFile(sub/test.txt): %v", err)
	}

	feAdapt, ok := fsc.LookupFile(adaptFd)
	if !ok {
		t.Fatalf("LookupFile(%d): missing", adaptFd)
	}
	buf6 := make([]byte, 64)
	n, errno := feAdapt.File.Pread(buf6, 0)
	if errno != 0 {
		t.Fatalf("Pread(sub/test.txt): errno %d", errno)
	}
	if got, want := string(buf6[:n]), string(wantSub); got != want {
		t.Errorf("Pread(sub/test.txt): got %q, want %q", got, want)
	}

	if errno := fsc.CloseFile(adaptFd); errno != 0 {
		t.Fatalf("CloseFile(%d): errno %d", adaptFd, errno)
	}
	if _, found := fsc.LookupFile(adaptFd); found {
		t.Fatalf("LookupFile(%d) after CloseFile: should be absent", adaptFd)
	}

	if errno := fsc.Renumber(3, 7); errno != 0 {
		t.Fatalf("Renumber(3, 7): errno %d", errno)
	}
	if _, found := fsc.LookupFile(3); found {
		t.Fatal("LookupFile(3) after Renumber: should be absent")
	}
	renumbered, ok := fsc.LookupFile(7)
	if !ok || !renumbered.IsPreopen || renumbered.Name != "/ro" {
		t.Fatalf("LookupFile(7): ReadFS preopen missing after Renumber, got ok=%v name=%q", ok, renumbered.Name)
	}
	if _, ok := fsc.LookupFile(4); !ok {
		t.Fatal("LookupFile(4): AdaptFS preopen should remain after Renumber(3→7)")
	}

	if errno := fsc.Renumber(4, 3); errno != wasi.EBADF {
		t.Errorf("Renumber(4, 3) onto invalidated slot: got errno %d, want EBADF (%d)", errno, wasi.EBADF)
	}

	if err := fsc.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
}
