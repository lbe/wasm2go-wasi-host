package sysfs

import (
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/testfixture"
)

// TestReadFSWrapsWithTestfixtureMapFSAndDeniesAllWrites verifies that ReadFS
// correctly wraps the testfixture.MapFS and denies all write operations.
func TestReadFSWrapsWithTestfixtureMapFSAndDeniesAllWrites(t *testing.T) {
	mapFS := testfixture.MapFS{}
	wantAnimals, err := mapFS.ReadFile("animals.txt")
	if err != nil {
		t.Fatalf("fixture ReadFile(animals.txt): %v", err)
	}

	readFS := ReadFS{FS: mapFS}

	// OpenFile on animals.txt with O_RDONLY should succeed
	file, errno := readFS.OpenFile("animals.txt", wasi.O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("ReadFS.OpenFile(O_RDONLY): unexpected errno %d", errno)
	}
	defer file.Close()

	// Read should return expected content
	buf := make([]byte, 1024)
	n, errno := file.Read(buf)
	if errno != 0 {
		t.Fatalf("Read: unexpected errno %d", errno)
	}

	if string(buf[:n]) != string(wantAnimals) {
		t.Errorf("Read content = %q, want %q", string(buf[:n]), string(wantAnimals))
	}

	// OpenFile with O_RDWR should fail with EROFS
	_, errno = readFS.OpenFile("animals.txt", wasi.O_RDWR, 0)
	if errno != wasi.EROFS {
		t.Errorf("ReadFS.OpenFile(O_RDWR): got errno %d, want EROFS (%d)", errno, wasi.EROFS)
	}

	// Mkdir should fail with EROFS
	errno = readFS.Mkdir("newdir", 0755)
	if errno != wasi.EROFS {
		t.Errorf("ReadFS.Mkdir: got errno %d, want EROFS (%d)", errno, wasi.EROFS)
	}
}

// TestAdaptFSWrapsTestfixtureMapFSAndDeniesMutations verifies that AdaptFS
// delegates Open/Read to testfixture.MapFS and returns ENOSYS for mutations.
func TestAdaptFSWrapsTestfixtureMapFSAndDeniesMutations(t *testing.T) {
	mapFS := testfixture.MapFS{}
	wantDirDash, err := mapFS.ReadFile("dir/-")
	if err != nil {
		t.Fatalf("fixture ReadFile(dir/-): %v", err)
	}

	adaptFS := AdaptFS{FS: mapFS}

	// dir/- exercises Tier 2 edge-case path names through AdaptFS.
	file, errno := adaptFS.OpenFile("dir/-", wasi.O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("AdaptFS.OpenFile(dir/-): unexpected errno %d", errno)
	}
	defer file.Close()

	buf := make([]byte, 64)
	n, errno := file.Read(buf)
	if errno != 0 {
		t.Fatalf("Read(dir/-): unexpected errno %d", errno)
	}
	if got, want := string(buf[:n]), string(wantDirDash); got != want {
		t.Errorf("Read(dir/-): got %q, want %q", got, want)
	}

	errno = adaptFS.Mkdir("newdir", 0755)
	if errno != wasi.ENOSYS {
		t.Errorf("AdaptFS.Mkdir: got errno %d, want ENOSYS (%d)", errno, wasi.ENOSYS)
	}
}
