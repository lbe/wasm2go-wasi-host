package wasihost

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPathCreateDirectoryRejectsSymlinkEscapePrefix(t *testing.T) {
	// A path mutation through an in-root symlink that escapes the preopen must not
	// create directories outside hostRoot. path_create_directory on "leak/evildir"
	// when leak -> ../secret.txt must return ENOTCAPABLE or ENOENT; the outside
	// tree must remain untouched.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	secretContent := []byte("top secret")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
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
		dirfd   int32 = 3
		pathOff int32 = 100
	)

	guestPath := "leak/evildir"
	copy(buf[pathOff:], guestPath)

	errno := s.Xpath_create_directory(dirfd, pathOff, int32(len(guestPath)))
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_create_directory(%q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			guestPath, errno, wasiENotCap, wasiENoEnt)
	}

	outsideEvil := filepath.Join(tmpDir, "evildir")
	if _, err := os.Stat(outsideEvil); err == nil {
		t.Fatalf("directory created outside preopen root at %q", outsideEvil)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(secretContent) {
		t.Fatalf("outside file was modified: got %q, want %q", gotSecret, secretContent)
	}
}

func TestPathFilestatSetTimesRejectsSymlinkEscapeFollow(t *testing.T) {
	// path_filestat_set_times with LOOKUP_SYMLINK_FOLLOW on a single-segment escape
	// symlink must not touch the outside target. leak -> ../secret.txt must return
	// ENOTCAPABLE or ENOENT; outside file content and mtimes stay unchanged.

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	secretContent := []byte("top secret")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(secretPath, past, past); err != nil {
		t.Fatal(err)
	}
	fi0, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	origMtime := fi0.ModTime()

	linkName := filepath.Join(root, "leak")
	if err = os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, WithHostDirectoryPreopen("/data", root))

	const (
		dirfd   int32 = 3
		pathOff int32 = 100
	)

	guestPath := "leak"
	copy(buf[pathOff:], guestPath)

	errno := s.Xpath_filestat_set_times(dirfd, wasiLookupSymlinkFollow, pathOff, int32(len(guestPath)), 0, 0, fstMtimNow)
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("path_filestat_set_times(follow %q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			guestPath, errno, wasiENotCap, wasiENoEnt)
	}

	gotSecret, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(secretContent) {
		t.Fatalf("outside file was modified: got %q, want %q", gotSecret, secretContent)
	}

	fi1, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.ModTime().Equal(origMtime) {
		t.Fatalf("outside file mtime changed: was %v, now %v", origMtime, fi1.ModTime())
	}
}

type symlinkEscapePrefixSetup struct {
	s             *State
	buf           []byte
	tmpDir        string
	root          string
	secretPath    string
	secretContent []byte
}

const (
	symlinkEscapeDirfd     int32 = 3
	symlinkEscapePathOff   int32 = 100
	symlinkEscapePath2Off  int32 = 200
	symlinkEscapeTargetOff int32 = 300
	symlinkEscapeBufOff    int32 = 400
	symlinkEscapeNreadOff  int32 = 500
)

func setupSymlinkEscapePrefixFixture(t *testing.T) symlinkEscapePrefixSetup {
	t.Helper()

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	secretPath := filepath.Join(tmpDir, "secret.txt")
	secretContent := []byte("top secret")
	if err := os.WriteFile(secretPath, secretContent, 0o644); err != nil {
		t.Fatal(err)
	}

	linkName := filepath.Join(root, "leak")
	if err := os.Symlink("../secret.txt", linkName); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, WithHostDirectoryPreopen("/data", root))

	return symlinkEscapePrefixSetup{
		s:             s,
		buf:           buf,
		tmpDir:        tmpDir,
		root:          root,
		secretPath:    secretPath,
		secretContent: secretContent,
	}
}

func assertSymlinkEscapeOutsideUntouched(t *testing.T, setup symlinkEscapePrefixSetup) {
	t.Helper()

	gotSecret, err := os.ReadFile(setup.secretPath)
	if err != nil {
		t.Fatalf("outside secret file: %v", err)
	}
	if string(gotSecret) != string(setup.secretContent) {
		t.Fatalf("outside file was modified: got %q, want %q", gotSecret, setup.secretContent)
	}
}

func assertAcceptableSymlinkConfinementErr(t *testing.T, op, guestPath string, errno int32) {
	t.Helper()
	if !acceptableSymlinkConfinementErr(errno) {
		t.Fatalf("%s(%q) = %d, want ENOTCAPABLE (%d) or ENOENT (%d)",
			op, guestPath, errno, wasiENotCap, wasiENoEnt)
	}
}

func TestPathMutationsRejectSymlinkEscapePrefix(t *testing.T) {
	// Multi-segment guest paths through an in-root escape symlink (leak -> ../secret.txt)
	// must not mutate the host tree outside the preopen root.

	guestPaths := []struct {
		name string
		p    string
	}{
		{name: "leak_evildir", p: "leak/evildir"},
		{name: "leak_sub", p: "leak/sub"},
	}

	tests := []struct {
		name string
		run  func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string)
	}{
		{
			name: "path_unlink_file",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_unlink_file(symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)))
				assertAcceptableSymlinkConfinementErr(t, "path_unlink_file", guestPath, errno)
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_remove_directory",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				outsideDir := filepath.Join(setup.tmpDir, "evildir")
				if err := os.Mkdir(outsideDir, 0o755); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_remove_directory(symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)))
				assertAcceptableSymlinkConfinementErr(t, "path_remove_directory", guestPath, errno)
				if _, err := os.Stat(outsideDir); err != nil {
					t.Fatalf("outside directory was removed: %v", err)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_rename",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				const srcPath = "rename_src"
				if err := os.WriteFile(filepath.Join(setup.root, srcPath), []byte("src"), 0o644); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapePathOff:], srcPath)
				copy(setup.buf[symlinkEscapePath2Off:], guestPath)
				errno := setup.s.Xpath_rename(
					symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(srcPath)),
					symlinkEscapeDirfd, symlinkEscapePath2Off, int32(len(guestPath)),
				)
				assertAcceptableSymlinkConfinementErr(t, "path_rename", guestPath, errno)
				if _, err := os.Stat(filepath.Join(setup.root, srcPath)); err != nil {
					t.Fatalf("rename_src was moved despite escape rejection: %v", err)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_symlink",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				const target = "inside_target"
				if err := os.WriteFile(filepath.Join(setup.root, target), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapeTargetOff:], target)
				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_symlink(
					symlinkEscapeTargetOff, int32(len(target)),
					symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)),
				)
				assertAcceptableSymlinkConfinementErr(t, "path_symlink", guestPath, errno)
				outsideMarker := filepath.Join(setup.tmpDir, filepath.Base(guestPath))
				if _, err := os.Lstat(outsideMarker); err == nil {
					t.Fatalf("symlink was created outside preopen root at %q", outsideMarker)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_readlink",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				copy(setup.buf[symlinkEscapePathOff:], guestPath)
				errno := setup.s.Xpath_readlink(
					symlinkEscapeDirfd, symlinkEscapePathOff, int32(len(guestPath)),
					symlinkEscapeBufOff, 256, symlinkEscapeNreadOff,
				)
				assertAcceptableSymlinkConfinementErr(t, "path_readlink", guestPath, errno)
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
		{
			name: "path_link",
			run: func(t *testing.T, setup symlinkEscapePrefixSetup, guestPath string) {
				const oldPath = "link_src"
				if err := os.WriteFile(filepath.Join(setup.root, oldPath), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}

				copy(setup.buf[symlinkEscapePathOff:], oldPath)
				copy(setup.buf[symlinkEscapePath2Off:], guestPath)
				errno := setup.s.Xpath_link(
					symlinkEscapeDirfd, 0, symlinkEscapePathOff, int32(len(oldPath)),
					symlinkEscapeDirfd, symlinkEscapePath2Off, int32(len(guestPath)),
				)
				assertAcceptableSymlinkConfinementErr(t, "path_link", guestPath, errno)
				outsideMarker := filepath.Join(setup.tmpDir, filepath.Base(guestPath))
				if _, err := os.Lstat(outsideMarker); err == nil {
					t.Fatalf("hard link was created outside preopen root at %q", outsideMarker)
				}
				assertSymlinkEscapeOutsideUntouched(t, setup)
			},
		},
	}

	for _, gp := range guestPaths {
		for _, tt := range tests {
			t.Run(gp.name+"/"+tt.name, func(t *testing.T) {
				setup := setupSymlinkEscapePrefixFixture(t)
				tt.run(t, setup, gp.p)
			})
		}
	}
}

// TestPathMutationsAllowSymlinkInTree verifies that path_create_directory succeeds for
// legitimate paths under a writable preopen when in-tree symlinks stay confined (no escape).
func TestPathMutationsAllowSymlinkInTree(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// In-tree symlink: stays under preopen root (not an escape).
	if err := os.Symlink("subdir", filepath.Join(root, "to_subdir")); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	mem := func() []byte { return buf }
	s := New(mem, WithHostDirectoryPreopen("/data", root))

	const (
		dirfd   int32 = 3
		pathOff int32 = 100
	)

	t.Run("single_segment_allowed", func(t *testing.T) {
		const guestPath = "allowed"
		copy(buf[pathOff:], guestPath)
		errno := s.Xpath_create_directory(dirfd, pathOff, int32(len(guestPath)))
		if errno != wasiESuccess {
			t.Fatalf("path_create_directory(%q) = %d, want ESUCCESS (%d)", guestPath, errno, wasiESuccess)
		}
		hostPath := filepath.Join(root, guestPath)
		if _, err := os.Stat(hostPath); err != nil {
			t.Fatalf("host directory %q missing after create: %v", hostPath, err)
		}
	})

	t.Run("multi_segment_subdir_nested", func(t *testing.T) {
		const guestPath = "subdir/nested"
		copy(buf[pathOff:], guestPath)
		errno := s.Xpath_create_directory(dirfd, pathOff, int32(len(guestPath)))
		if errno != wasiESuccess {
			t.Fatalf("path_create_directory(%q) = %d, want ESUCCESS (%d)", guestPath, errno, wasiESuccess)
		}
		hostPath := filepath.Join(root, "subdir", "nested")
		if _, err := os.Stat(hostPath); err != nil {
			t.Fatalf("host directory %q missing after create: %v", hostPath, err)
		}
	})
}

// TestPathLinkToSymlinkLoopOnNestedDirfd matches wasi-testsuite path_link: a scratch
// directory opened as a non-preopen dirfd, single-segment guest paths, symlink loop.
func TestPathLinkToSymlinkLoopOnNestedDirfd(t *testing.T) {
	const (
		preopenFd   int32 = 3
		scratchOff  int32 = 100
		nestedFdPtr int32 = 200
		symlinkOff  int32 = 300
		linkOff     int32 = 400
	)

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 2048)
	s := New(func() []byte { return buf }, WithHostDirectoryPreopen("/data", root))

	const scratch = "scratch"
	copy(buf[scratchOff:], scratch)
	if errno := s.Xpath_create_directory(preopenFd, scratchOff, int32(len(scratch))); errno != wasiESuccess {
		t.Fatalf("path_create_directory(%q) = %d, want ESUCCESS", scratch, errno)
	}
	copy(buf[scratchOff:], scratch)
	if errno := s.Xpath_open(preopenFd, 0, scratchOff, int32(len(scratch)),
		int32(oflagDir), int64(rightsWritableDirPreopen), int64(rightFDRead), 0, nestedFdPtr); errno != wasiESuccess {
		t.Fatalf("path_open(%q) = %d, want ESUCCESS", scratch, errno)
	}
	nestedFd := int32(binary.LittleEndian.Uint32(buf[nestedFdPtr : nestedFdPtr+4]))

	copy(buf[symlinkOff:], "symlink")
	if errno := s.Xpath_symlink(symlinkOff, 7, nestedFd, symlinkOff, 7); errno != wasiESuccess {
		t.Fatalf("path_symlink(symlink loop) = %d, want ESUCCESS", errno)
	}

	copy(buf[linkOff:], "link")
	if errno := s.Xpath_link(nestedFd, 0, symlinkOff, 7, nestedFd, linkOff, 4); errno != wasiESuccess {
		t.Fatalf("path_link(symlink loop on nested dirfd) = %d, want ESUCCESS", errno)
	}

	hostLink := filepath.Join(root, scratch, "link")
	if fi, err := os.Lstat(hostLink); err != nil {
		t.Fatalf("hard link %q: %v", hostLink, err)
	} else if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("hard link %q is not a symlink inode", hostLink)
	}
}
