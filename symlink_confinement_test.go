package wasihost

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func acceptableSymlinkConfinementErr(errno int32) bool {
	return errno == wasiENotCap || errno == wasiENoEnt
}

func TestWritableHostPreopenRejectsSymlinkEscapeToOutsidePath(t *testing.T) {
	// A symlink fully inside the guest-visible tree must not grant access to paths
	// outside the preopened host root. Operations that would follow the symlink
	// into ../secret must return ENOTCAPABLE or ENOENT; the outside file must stay
	// untouched, and ordinary files under the root remain reachable.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	legitPath := filepath.Join(root, "legit.txt")
	if err := os.WriteFile(legitPath, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkName := filepath.Join(root, "leak")
	if err := os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, WithHostDirectoryPreopen("/data", root))

	const (
		dirfd        int32 = 3
		fdPtr        int32 = 80
		pathLeakOff  int32 = 100
		pathLegitOff       = 250
		statPtr      int32 = 400
		offOld       int32 = 500
		offNew       int32 = 600
	)

	linkGuest := "leak"
	copy(buf[pathLeakOff:], linkGuest)

	errno := s.Xpath_open(dirfd, wasiLookupSymlinkFollow, pathLeakOff, int32(len(linkGuest)), 0,
		int64(rightFDRead|rightFDWrite), 0, 0, fdPtr)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_open(write through symlink %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, errno, wasiENotCap, wasiENoEnt)
	}

	errno = s.Xpath_filestat_get(dirfd, wasiLookupSymlinkFollow, pathLeakOff, int32(len(linkGuest)), statPtr)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_filestat_get(follow symlink %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, errno, wasiENotCap, wasiENoEnt)
	}

	copy(buf[offOld:], linkGuest)
	newName := "renamed_leak"
	copy(buf[offNew:], newName)

	errno = s.Xpath_rename(dirfd, offOld, int32(len(linkGuest)), dirfd, offNew, int32(len(newName)))
	// path_rename does not follow symlinks; it renames the symlink inode within the preopen.
	if errno != wasiESuccess {
		t.Fatalf("path_rename(symlink %q) = %d, want ESUCCESS", linkGuest, errno)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != "top secret" {
		t.Fatalf("outside file was created or modified: got %q", gotSecret)
	}

	legitGuest := "legit.txt"
	copy(buf[pathLegitOff:], legitGuest)
	errno = s.Xpath_open(dirfd, 0, pathLegitOff, int32(len(legitGuest)), 0, int64(rightFDRead), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("path_open(%q) = %d, want ESUCCESS (legitimate path should still work)", legitGuest, errno)
	}
}

func TestPathOpenWithoutSymlinkFollowReturnsLoopForEscapeSymlinkWithoutTouchingOutsideFile(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(outsideDir, "secret.txt")
	secretContent := []byte("outside payload")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}

	legitPath := filepath.Join(root, "legit.txt")
	if err := os.WriteFile(legitPath, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	confinedLink := filepath.Join(root, "to_legit")
	if err := os.Symlink("legit.txt", confinedLink); err != nil {
		t.Fatal(err)
	}

	escapeLink := filepath.Join(root, "to_outside")
	if err := os.Symlink("../outside/secret.txt", escapeLink); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, WithHostDirectoryPreopen("/data", root))

	const (
		dirfd           int32 = 3
		fdPtrConfined   int32 = 70
		fdPtrEscape     int32 = 80
		pathConfinedOff int32 = 100
		pathEscapeOff   int32 = 200
	)

	confinedGuest := "to_legit"
	copy(buf[pathConfinedOff:], confinedGuest)
	errno := s.Xpath_open(dirfd, wasiLookupSymlinkFollow, pathConfinedOff, int32(len(confinedGuest)),
		0, int64(rightFDRead), 0, 0, fdPtrConfined)
	if errno != wasiESuccess {
		t.Fatalf("path_open(follow confined symlink %q) = %d, want ESUCCESS (%d)",
			confinedGuest, errno, wasiESuccess)
	}

	escapeGuest := "to_outside"
	copy(buf[pathEscapeOff:], escapeGuest)
	const fdSentinel uint32 = 0xfeedd00f
	binary.LittleEndian.PutUint32(buf[fdPtrEscape:], fdSentinel)

	errno = s.Xpath_open(dirfd, 0, pathEscapeOff, int32(len(escapeGuest)), 0,
		int64(rightFDRead), 0, 0, fdPtrEscape)
	if errno != wasiELoop {
		t.Fatalf("path_open(no-follow escape symlink %q) = %d, want ELOOP (%d)",
			escapeGuest, errno, wasiELoop)
	}
	if got := binary.LittleEndian.Uint32(buf[fdPtrEscape:]); got != fdSentinel {
		t.Fatalf("fd slot at fdPtr was written (%d) though open should fail; want sentinel %d",
			got, fdSentinel)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(secretContent) {
		t.Fatalf("outside file was read or modified: got %q, want %q", gotSecret, secretContent)
	}
}

const wasiFiletypeSymbolicLink uint64 = 7 // __WASI_FILETYPE_SYMBOLIC_LINK (preview1)

func TestPathFilestatGetWithoutSymlinkFollowReflectsSymlinkInodeNotEscapedTarget(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const outsideSize = 50_000
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, bytes.Repeat([]byte("Z"), outsideSize), 0o644); err != nil {
		t.Fatal(err)
	}

	escapeLink := filepath.Join(root, "link")
	if err := os.Symlink("../outside/secret.txt", escapeLink); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, WithHostDirectoryPreopen("/data", root))

	const (
		dirfd     int32 = 3
		pathOff   int32 = 120
		statOff   int32 = 400
		linkGuest       = "link"
	)

	copy(buf[pathOff:], linkGuest)
	errno := s.Xpath_filestat_get(dirfd, wasiLookupSymlinkFollow, pathOff, int32(len(linkGuest)), statOff)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_filestat_get(follow escape symlink %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			linkGuest, errno, wasiENotCap, wasiENoEnt)
	}

	errno = s.Xpath_filestat_get(dirfd, 0, pathOff, int32(len(linkGuest)), statOff)
	if errno != wasiESuccess {
		t.Fatalf("path_filestat_get(no-follow on symlink %q) = %d, want ESUCCESS (%d)",
			linkGuest, errno, wasiESuccess)
	}

	filetype := binary.LittleEndian.Uint64(buf[statOff+16 : statOff+24])
	if filetype != wasiFiletypeSymbolicLink {
		t.Fatalf("filestat filetype = %d, want symbolic link (%d)", filetype, wasiFiletypeSymbolicLink)
	}
	stSize := int64(binary.LittleEndian.Uint64(buf[statOff+32 : statOff+40]))
	if stSize == outsideSize {
		t.Fatalf("filestat size matches outside target (%d); expected symlink inode metadata, not the escaped file", outsideSize)
	}
}
