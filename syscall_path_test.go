package wasihost

import (
	"encoding/binary"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"
)

const nestedDirfdEscapePath = "dir/nested/../../../escape_sibling/dir/nested/file"

// nestedDirfdSetup holds the common test fixtures for tests that exercise
// path_open on a non-preopen (nested) directory fd.
type nestedDirfdSetup struct {
	s        *State
	buf      []byte
	tmpDir   string
	nestedFd int32
}

// setupNestedDirfd creates a preopen at /data, creates "interesting_paths_dir"
// under it, opens that directory as a nested fd via path_open with
// O_DIRECTORY, and creates dir/nested subdirectories plus a file
// "dir/nested/file" on the host. The returned setup contains the State,
// guest memory buf, temp dir, and the nested fd number.
func setupNestedDirfd(t *testing.T) nestedDirfdSetup {
	t.Helper()

	const (
		preopenFd   int32 = 3
		pathOff1    int32 = 1000
		pathOff2    int32 = 2000
		pathOff3    int32 = 3000
		nestedFdPtr int32 = 5000
	)

	buf := make([]byte, 65536)
	tmpDir := t.TempDir()

	s := New(func() []byte { return buf },
		NewModuleConfig().WithHostDirectoryPreopen("/data", tmpDir),
	)

	// Create "interesting_paths_dir" under the preopen.
	copy(buf[pathOff1:], "interesting_paths_dir")
	assertESuccess(t, s.Xpath_create_directory(preopenFd, pathOff1, int32(len("interesting_paths_dir"))))

	// Open it as a nested dirfd.
	copy(buf[pathOff2:], "interesting_paths_dir")
	assertESuccess(t, s.Xpath_open(preopenFd, 0, pathOff2, int32(len("interesting_paths_dir")),
		int32(oflagDir), int64(rightsWritableDirPreopen), int64(rightFDRead), 0, nestedFdPtr))
	nestedFd := int32(binary.LittleEndian.Uint32(buf[nestedFdPtr : nestedFdPtr+4]))

	// Create dir/nested under the nested fd via WASI.
	copy(buf[pathOff3:], "dir")
	assertESuccess(t, s.Xpath_create_directory(nestedFd, pathOff3, int32(len("dir"))))
	copy(buf[pathOff3:], "dir/nested")
	assertESuccess(t, s.Xpath_create_directory(nestedFd, pathOff3, int32(len("dir/nested"))))

	// Create the actual file directly on the host so it exists.
	if err := os.WriteFile(filepath.Join(tmpDir, "interesting_paths_dir", "dir", "nested", "file"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	return nestedDirfdSetup{s: s, buf: buf, tmpDir: tmpDir, nestedFd: nestedFd}
}

// assertESuccess fails t with a descriptive message if errno is not ESUCCESS.
func assertESuccess(t *testing.T, errno int32) {
	t.Helper()
	if errno != wasiESuccess {
		t.Fatalf("got errno %d, want ESUCCESS (0)", errno)
	}
}

// setupEscapeSiblingTree creates escape_sibling under the preopen with
// dir/nested/file — the host target reachable via nestedDirfdEscapePath.
func setupEscapeSiblingTree(t *testing.T, s *State, buf []byte, tmpDir string) (escapeFile string) {
	t.Helper()

	const preopenFd int32 = 3
	const pathOff1 int32 = 1000

	copy(buf[pathOff1:], "escape_sibling")
	assertESuccess(t, s.Xpath_create_directory(preopenFd, pathOff1, int32(len("escape_sibling"))))

	if err := os.MkdirAll(filepath.Join(tmpDir, "escape_sibling", "dir", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	escapeFile = filepath.Join(tmpDir, "escape_sibling", "dir", "nested", "file")
	if err := os.WriteFile(escapeFile, []byte("escaped"), 0o644); err != nil {
		t.Fatal(err)
	}

	return escapeFile
}

// setupNestedDirfdWithEscapeTarget extends setupNestedDirfd with an escape_sibling
// tree containing dir/nested/file — the target reachable via nestedDirfdEscapePath.
func setupNestedDirfdWithEscapeTarget(t *testing.T) (setup nestedDirfdSetup, escapePath, escapeFile string) {
	t.Helper()

	setup = setupNestedDirfd(t)
	escapePath = nestedDirfdEscapePath
	escapeFile = setupEscapeSiblingTree(t, setup.s, setup.buf, setup.tmpDir)

	return setup, escapePath, escapeFile
}

func assertRejectNestedDirfdEscape(t *testing.T, op string, escapePath string, errno int32) {
	t.Helper()
	if errno == wasiESuccess {
		t.Fatalf("%s(nested_fd, %q) succeeded; expected EPERM (%d) or ENOTCAPABLE (%d)",
			op, escapePath, wasiEPerm, wasiENotCap)
	}
	if errno != wasiEPerm && errno != wasiENotCap {
		t.Fatalf("%s(nested_fd, %q) = %d; want EPERM (%d) or ENOTCAPABLE (%d)",
			op, escapePath, errno, wasiEPerm, wasiENotCap)
	}
}

func assertEscapeFileUnchanged(t *testing.T, escapeFile string) {
	t.Helper()
	got, err := os.ReadFile(escapeFile)
	if err != nil {
		t.Fatalf("reading escape_sibling file: %v", err)
	}
	if string(got) != "escaped" {
		t.Fatalf("escape_sibling file content = %q, want %q — escape path mutated host state", got, "escaped")
	}
}

// TestPathMutationsRejectEscapeOutsideNestedDirfdSubtree verifies that path
// mutations using resolveWritable on a non-preopen (nested) directory fd reject
// the canonical escape path with EPERM or ENOTCAPABLE and do not mutate host
// state outside the nested dirfd subtree.
func TestPathMutationsRejectEscapeOutsideNestedDirfdSubtree(t *testing.T) {
	t.Parallel()

	const (
		pathOff4  int32 = 4000
		pathOff5  int32 = 5000
		bufOff    int32 = 6000
		nreadOff  int32 = 7000
		targetOff int32 = 8000
	)

	tests := []struct {
		name string
		run  func(t *testing.T, setup nestedDirfdSetup, escapePath, escapeFile string)
	}{
		{
			name: "path_create_directory",
			run: func(t *testing.T, setup nestedDirfdSetup, escapePath, escapeFile string) {
				s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd
				copy(buf[pathOff4:], escapePath)
				errno := s.Xpath_create_directory(nestedFd, pathOff4, int32(len(escapePath)))
				assertRejectNestedDirfdEscape(t, "Xpath_create_directory", escapePath, errno)
				assertEscapeFileUnchanged(t, escapeFile)
			},
		},
		{
			name: "path_remove_directory",
			run: func(t *testing.T, setup nestedDirfdSetup, escapePath, escapeFile string) {
				s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd
				copy(buf[pathOff4:], escapePath)
				errno := s.Xpath_remove_directory(nestedFd, pathOff4, int32(len(escapePath)))
				assertRejectNestedDirfdEscape(t, "Xpath_remove_directory", escapePath, errno)
				assertEscapeFileUnchanged(t, escapeFile)
			},
		},
		{
			name: "path_rename",
			run: func(t *testing.T, setup nestedDirfdSetup, escapePath, escapeFile string) {
				s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd
				const srcPath = "dir/nested/rename_src"
				if err := os.WriteFile(filepath.Join(setup.tmpDir, "interesting_paths_dir", "dir", "nested", "rename_src"), []byte("src"), 0o644); err != nil {
					t.Fatal(err)
				}
				copy(buf[pathOff4:], srcPath)
				copy(buf[pathOff5:], escapePath)
				errno := s.Xpath_rename(nestedFd, pathOff4, int32(len(srcPath)), nestedFd, pathOff5, int32(len(escapePath)))
				assertRejectNestedDirfdEscape(t, "Xpath_rename", escapePath, errno)
				assertEscapeFileUnchanged(t, escapeFile)
				if _, err := os.Stat(filepath.Join(setup.tmpDir, "interesting_paths_dir", "dir", "nested", "rename_src")); err != nil {
					t.Fatalf("rename_src was moved despite escape rejection: %v", err)
				}
			},
		},
		{
			name: "path_readlink",
			run: func(t *testing.T, setup nestedDirfdSetup, escapePath, _ string) {
				s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd
				copy(buf[pathOff4:], escapePath)
				errno := s.Xpath_readlink(nestedFd, pathOff4, int32(len(escapePath)), bufOff, 256, nreadOff)
				assertRejectNestedDirfdEscape(t, "Xpath_readlink", escapePath, errno)
			},
		},
		{
			name: "path_symlink",
			run: func(t *testing.T, setup nestedDirfdSetup, escapePath, escapeFile string) {
				s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd
				copy(buf[targetOff:], "target")
				copy(buf[pathOff4:], escapePath)
				errno := s.Xpath_symlink(targetOff, int32(len("target")), nestedFd, pathOff4, int32(len(escapePath)))
				assertRejectNestedDirfdEscape(t, "Xpath_symlink", escapePath, errno)
				assertEscapeFileUnchanged(t, escapeFile)
			},
		},
		{
			name: "path_link",
			run: func(t *testing.T, setup nestedDirfdSetup, escapePath, escapeFile string) {
				s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd
				const oldPath = "dir/nested/file"
				copy(buf[pathOff4:], oldPath)
				copy(buf[pathOff5:], escapePath)
				errno := s.Xpath_link(nestedFd, 0, pathOff4, int32(len(oldPath)), nestedFd, pathOff5, int32(len(escapePath)))
				assertRejectNestedDirfdEscape(t, "Xpath_link", escapePath, errno)
				assertEscapeFileUnchanged(t, escapeFile)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup, escapePath, escapeFile := setupNestedDirfdWithEscapeTarget(t)
			tt.run(t, setup, escapePath, escapeFile)
		})
	}
}

// TestPathMutationsAllowPathsWithinNestedDirfdSubtree verifies that path
// mutations on a non-preopen (nested) directory fd still succeed for relative
// paths that remain within the nested dirfd subtree after normalization.
func TestPathMutationsAllowPathsWithinNestedDirfdSubtree(t *testing.T) {
	t.Parallel()

	const (
		pathOff4  int32 = 4000
		pathOff5  int32 = 5000
		bufOff    int32 = 6000
		nreadOff  int32 = 7000
		targetOff int32 = 8000
	)

	tests := []struct {
		name string
		run  func(t *testing.T, setup nestedDirfdSetup)
	}{
		{
			name: "path_create_directory",
			run: func(t *testing.T, setup nestedDirfdSetup) {
				s, buf, nestedFd, tmpDir := setup.s, setup.buf, setup.nestedFd, setup.tmpDir
				const allowedPath = "allowed_subdir"
				copy(buf[pathOff4:], allowedPath)
				assertESuccess(t, s.Xpath_create_directory(nestedFd, pathOff4, int32(len(allowedPath))))
				hostPath := filepath.Join(tmpDir, "interesting_paths_dir", allowedPath)
				if _, err := os.Stat(hostPath); err != nil {
					t.Fatalf("host path %q missing after in-subtree create: %v", hostPath, err)
				}
			},
		},
		{
			name: "path_remove_directory",
			run: func(t *testing.T, setup nestedDirfdSetup) {
				s, buf, nestedFd, tmpDir := setup.s, setup.buf, setup.nestedFd, setup.tmpDir
				const allowedPath = "allowed_remove_me"
				hostPath := filepath.Join(tmpDir, "interesting_paths_dir", allowedPath)
				if err := os.Mkdir(hostPath, 0o755); err != nil {
					t.Fatal(err)
				}
				copy(buf[pathOff4:], allowedPath)
				assertESuccess(t, s.Xpath_remove_directory(nestedFd, pathOff4, int32(len(allowedPath))))
				if _, err := os.Stat(hostPath); !os.IsNotExist(err) {
					t.Fatalf("host path %q still exists after in-subtree remove: %v", hostPath, err)
				}
			},
		},
		{
			name: "path_rename",
			run: func(t *testing.T, setup nestedDirfdSetup) {
				s, buf, nestedFd, tmpDir := setup.s, setup.buf, setup.nestedFd, setup.tmpDir
				const srcPath = "dir/nested/rename_src"
				const dstPath = "dir/nested/rename_dst"
				if err := os.WriteFile(filepath.Join(tmpDir, "interesting_paths_dir", "dir", "nested", "rename_src"), []byte("src"), 0o644); err != nil {
					t.Fatal(err)
				}
				copy(buf[pathOff4:], srcPath)
				copy(buf[pathOff5:], dstPath)
				assertESuccess(t, s.Xpath_rename(nestedFd, pathOff4, int32(len(srcPath)), nestedFd, pathOff5, int32(len(dstPath))))
				if _, err := os.Stat(filepath.Join(tmpDir, "interesting_paths_dir", "dir", "nested", "rename_dst")); err != nil {
					t.Fatalf("rename_dst missing after in-subtree rename: %v", err)
				}
			},
		},
		{
			name: "path_readlink",
			run: func(t *testing.T, setup nestedDirfdSetup) {
				s, buf, nestedFd, tmpDir := setup.s, setup.buf, setup.nestedFd, setup.tmpDir
				const linkPath = "allowed_readlink"
				const targetPath = "dir/nested/file"
				hostLink := filepath.Join(tmpDir, "interesting_paths_dir", linkPath)
				if err := os.Symlink("dir/nested/file", hostLink); err != nil {
					t.Fatal(err)
				}
				copy(buf[pathOff4:], linkPath)
				assertESuccess(t, s.Xpath_readlink(nestedFd, pathOff4, int32(len(linkPath)), bufOff, 256, nreadOff))
				nread := int32(binary.LittleEndian.Uint32(buf[nreadOff : nreadOff+4]))
				got := string(buf[bufOff : bufOff+nread])
				if got != targetPath {
					t.Fatalf("readlink = %q, want %q", got, targetPath)
				}
			},
		},
		{
			name: "path_symlink",
			run: func(t *testing.T, setup nestedDirfdSetup) {
				s, buf, nestedFd, tmpDir := setup.s, setup.buf, setup.nestedFd, setup.tmpDir
				const linkPath = "allowed_symlink"
				copy(buf[targetOff:], "dir/nested/file")
				copy(buf[pathOff4:], linkPath)
				assertESuccess(t, s.Xpath_symlink(targetOff, int32(len("dir/nested/file")), nestedFd, pathOff4, int32(len(linkPath))))
				hostLink := filepath.Join(tmpDir, "interesting_paths_dir", linkPath)
				if _, err := os.Lstat(hostLink); err != nil {
					t.Fatalf("symlink %q missing after in-subtree path_symlink: %v", hostLink, err)
				}
			},
		},
		{
			name: "path_link",
			run: func(t *testing.T, setup nestedDirfdSetup) {
				s, buf, nestedFd, tmpDir := setup.s, setup.buf, setup.nestedFd, setup.tmpDir
				const oldPath = "dir/nested/file"
				const newPath = "allowed_hardlink"
				copy(buf[pathOff4:], oldPath)
				copy(buf[pathOff5:], newPath)
				assertESuccess(t, s.Xpath_link(nestedFd, 0, pathOff4, int32(len(oldPath)), nestedFd, pathOff5, int32(len(newPath))))
				hostLink := filepath.Join(tmpDir, "interesting_paths_dir", newPath)
				if _, err := os.Stat(hostLink); err != nil {
					t.Fatalf("hard link %q missing after in-subtree path_link: %v", hostLink, err)
				}
			},
		},
		{
			name: "path_unlink_file",
			run: func(t *testing.T, setup nestedDirfdSetup) {
				s, buf, nestedFd, tmpDir := setup.s, setup.buf, setup.nestedFd, setup.tmpDir
				const allowedPath = "dir/nested/unlink_me"
				hostPath := filepath.Join(tmpDir, "interesting_paths_dir", "dir", "nested", "unlink_me")
				if err := os.WriteFile(hostPath, []byte("bye"), 0o644); err != nil {
					t.Fatal(err)
				}
				copy(buf[pathOff4:], allowedPath)
				assertESuccess(t, s.Xpath_unlink_file(nestedFd, pathOff4, int32(len(allowedPath))))
				if _, err := os.Stat(hostPath); !os.IsNotExist(err) {
					t.Fatalf("host file %q still exists after in-subtree unlink: %v", hostPath, err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupNestedDirfd(t)
			tt.run(t, setup)
		})
	}
}

// TestPathOpenRejectsEscapeOutsideNestedDirfdSubtree verifies that path_open on
// a non-preopen (nested) directory fd rejects relative paths whose normalized
// form escapes outside the dirfd's guest subtree. A path like
// "dir/nested/../../../escape_sibling/dir/nested/file" on a nested fd opened
// on /data/interesting_pathsDir would normalize to
// /data/escape_sibling/dir/nested/file, which is outside the nested fd's
// subtree and must be rejected with EPERM or ENOTCAPABLE.
func TestPathOpenRejectsEscapeOutsideNestedDirfdSubtree(t *testing.T) {
	t.Parallel()

	const (
		pathOff4  int32 = 4000
		fileFdPtr int32 = 6000
	)

	setup, escapePath, escapeFile := setupNestedDirfdWithEscapeTarget(t)
	s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd

	// path_open with escape path on nested fd must return EPERM or ENOTCAPABLE.
	// nestedDirfdEscapePath normalizes to /data/escape_sibling/dir/nested/file,
	// which is outside the nested fd's subtree.
	copy(buf[pathOff4:], escapePath)
	errno := s.Xpath_open(nestedFd, 0, pathOff4, int32(len(escapePath)), 0, int64(rightFDRead), 0, 0, fileFdPtr)
	assertRejectNestedDirfdEscape(t, "Xpath_open", escapePath, errno)
	assertEscapeFileUnchanged(t, escapeFile)
}

// TestPathUnlinkRejectsEscapeOutsideNestedDirfdSubtree verifies that
// path_unlink_file on a non-preopen (nested) directory fd rejects relative
// paths whose normalized form escapes outside the dirfd's guest subtree.
func TestPathUnlinkRejectsEscapeOutsideNestedDirfdSubtree(t *testing.T) {
	t.Parallel()

	const pathOff4 int32 = 4000

	setup, escapePath, escapeFile := setupNestedDirfdWithEscapeTarget(t)
	s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd

	copy(buf[pathOff4:], escapePath)
	errno := s.Xpath_unlink_file(nestedFd, pathOff4, int32(len(escapePath)))
	assertRejectNestedDirfdEscape(t, "Xpath_unlink_file", escapePath, errno)
	assertEscapeFileUnchanged(t, escapeFile)
}

// TestPathOpenRejectsGuestAbsolutePathOnNestedDirfd verifies that path_open
// on a non-preopen directory fd rejects guest-absolute paths (starting with /)
// with EPERM or ENOTCAPABLE rather than ENOENT, even when the file exists and
// can be opened via a relative path from that dirfd.
func TestPathOpenRejectsGuestAbsolutePathOnNestedDirfd(t *testing.T) {
	t.Parallel()

	const (
		pathOff4  int32 = 4000
		fileFdPtr int32 = 6000
	)

	setup := setupNestedDirfd(t)
	s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd

	// path_open with absolute path "/dir/nested/file" on nested fd
	// must return EPERM or ENOTCAPABLE, NOT ENOENT.
	absPath := "/dir/nested/file"
	copy(buf[pathOff4:], absPath)
	errno := s.Xpath_open(nestedFd, 0, pathOff4, int32(len(absPath)), 0, int64(rightFDRead), 0, 0, fileFdPtr)
	if errno == wasiENoEnt {
		t.Fatalf("Xpath_open(nested_fd, %q) = ENOENT (%d); expected EPERM (%d) or ENOTCAPABLE (%d) — absolute path on non-preopen dirfd must not fall through to ENOENT",
			absPath, wasiENoEnt, wasiEPerm, wasiENotCap)
	}
	if errno != wasiEPerm && errno != wasiENotCap {
		t.Fatalf("Xpath_open(nested_fd, %q) = %d; want EPERM (%d) or ENOTCAPABLE (%d)",
			absPath, errno, wasiEPerm, wasiENotCap)
	}

	// path_open with relative path "dir/nested/file" on nested fd must succeed.
	relPath := "dir/nested/file"
	copy(buf[pathOff4:], relPath)
	errno = s.Xpath_open(nestedFd, 0, pathOff4, int32(len(relPath)), 0, int64(rightFDRead), 0, 0, fileFdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(nested_fd, %q) = %d, want ESUCCESS (0)", relPath, errno)
	}
}

// TestPathOpenNormalizesInPreopenRelativePath verifies that mount-relative
// paths containing . and .. segments are normalized within the preopen before
// escape checks and host path joins. A path like
// "dir/.//nested/../../dir/nested/../nested///./file" must resolve to
// "dir/nested/file" relative to the nested dirfd and open successfully.
func TestPathOpenNormalizesInPreopenRelativePath(t *testing.T) {
	t.Parallel()

	const (
		pathOff4  int32 = 4000
		fileFdPtr int32 = 6000
	)

	setup := setupNestedDirfd(t)
	s, buf, tmpDir, nestedFd := setup.s, setup.buf, setup.tmpDir, setup.nestedFd

	// Open with a path full of . and .. segments that normalizes to dir/nested/file.
	messyPath := "dir/.//nested/../../dir/nested/../nested///./file"
	copy(buf[pathOff4:], messyPath)
	errno := s.Xpath_open(nestedFd, 0, pathOff4, int32(len(messyPath)), 0, int64(rightFDRead), 0, 0, fileFdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(nested_fd, %q) = %d, want ESUCCESS (0); path should normalize to dir/nested/file within preopen",
			messyPath, errno)
	}
	fileFd := int32(binary.LittleEndian.Uint32(buf[fileFdPtr : fileFdPtr+4]))
	if fileFd <= 2 {
		t.Fatalf("Xpath_open(nested_fd, %q) returned fd %d, want fd > 2", messyPath, fileFd)
	}

	// Verify the host path resolved to the normalized path.
	expectedGuestPath := path.Clean("/data/interesting_paths_dir/dir/nested/file")
	if fe, ok := s.lookupFD(fileFd); ok {
		if fe.GuestPath != expectedGuestPath {
			t.Fatalf("opened file guest path = %q, want %q", fe.GuestPath, expectedGuestPath)
		}
	} else {
		t.Fatalf("fd %d not found in fd table", fileFd)
	}

	// Verify the file was opened on the host at the normalized path.
	hostPath := filepath.Join(tmpDir, "interesting_paths_dir", "dir", "nested", "file")
	if _, err := os.Stat(hostPath); err != nil {
		t.Fatalf("host file at normalized path %q does not exist: %v", hostPath, err)
	}
}

// TestPathOpenRejectsTrailingNulInPath verifies that path_open on a nested
// directory fd rejects a relative path whose buffer contains a trailing NUL
// byte. The path bytes are "dir/nested/file" followed by \x00, and pathLen
// includes the NUL byte. The host file exists and can be opened without the
// trailing NUL, but the NUL byte must be rejected with EINVAL or ILSEQ.
func TestPathOpenRejectsTrailingNulInPath(t *testing.T) {
	t.Parallel()

	const (
		pathOff4  int32 = 4000
		fileFdPtr int32 = 6000
	)

	setup := setupNestedDirfd(t)
	s, buf, nestedFd := setup.s, setup.buf, setup.nestedFd

	// Build the path bytes: "dir/nested/file" followed by a trailing NUL.
	rawPath := "dir/nested/file\x00"
	copy(buf[pathOff4:], rawPath)
	pathLen := int32(len(rawPath)) // includes the NUL byte

	// path_open with a trailing NUL in the path buffer must return EINVAL
	// or ILSEQ, not ESUCCESS.
	errno := s.Xpath_open(nestedFd, 0, pathOff4, pathLen, 0, int64(rightFDRead), 0, 0, fileFdPtr)
	if errno == wasiESuccess {
		t.Fatalf("Xpath_open(nested_fd, path_with_trailing_nul) succeeded; expected EINVAL (%d) or ILSEQ (%d) — trailing NUL byte in path buffer must be rejected",
			wasiEInval, wasiEIlseq)
	}
	if errno != wasiEInval && errno != wasiEIlseq {
		t.Fatalf("Xpath_open(nested_fd, path_with_trailing_nul) = %d; want EINVAL (%d) or ILSEQ (%d)",
			errno, wasiEInval, wasiEIlseq)
	}
}

// TestPathOpenTrailingSlashSemantics verifies that path_open on a nested
// directory fd correctly handles trailing slashes in paths. A file path with a
// trailing slash must return ENOTDIR or ENOENT, while a directory path with a
// trailing slash must succeed and open the directory.
func TestPathOpenTrailingSlashSemantics(t *testing.T) {
	t.Parallel()

	const (
		pathOff4  int32 = 4000
		fileFdPtr int32 = 6000
		dirFdPtr  int32 = 7000
	)

	setup := setupNestedDirfd(t)
	s, buf, tmpDir, nestedFd := setup.s, setup.buf, setup.tmpDir, setup.nestedFd

	// Create a directory with a trailing slash to verify it succeeds.
	trailingSlashDir := "dir/nested/"
	copy(buf[pathOff4:], trailingSlashDir)
	errno := s.Xpath_open(nestedFd, 0, pathOff4, int32(len(trailingSlashDir)), int32(oflagDir), int64(rightsWritableDirPreopen), int64(rightFDRead), 0, dirFdPtr)
	if errno != wasiESuccess {
		// The test is written to FAIL if the implementation is missing.
		// Currently, the implementation does not handle trailing slashes
		// correctly, so this assertion will fail.
		t.Fatalf("Xpath_open(nested_fd, %q) = %d, want ESUCCESS (0); trailing slash on directory should succeed after normalization", trailingSlashDir, errno)
	}
	dirFd := int32(binary.LittleEndian.Uint32(buf[dirFdPtr : dirFdPtr+4]))
	if dirFd <= 2 {
		t.Fatalf("Xpath_open(nested_fd, %q) returned fd %d, want fd > 2", trailingSlashDir, dirFd)
	}

	// Create a file path with a trailing slash to verify it fails with ENOTDIR or ENOENT.
	trailingSlashFile := "dir/nested/file/"
	copy(buf[pathOff4:], trailingSlashFile)
	errno = s.Xpath_open(nestedFd, 0, pathOff4, int32(len(trailingSlashFile)), 0, int64(rightFDRead), 0, 0, fileFdPtr)
	if errno == wasiESuccess {
		// The test is written to FAIL if the implementation is missing.
		fd := int32(binary.LittleEndian.Uint32(buf[fileFdPtr : fileFdPtr+4]))
		t.Fatalf("Xpath_open(nested_fd, %q) succeeded (fd=%d); expected ENOTDIR (%d) or ENOENT (%d) — file path with trailing slash should fail because it resolves to a directory, not a file", trailingSlashFile, fd, wasiENotDir, wasiENoEnt)
	}
	if errno != wasiENotDir && errno != wasiENoEnt {
		// The test is written to FAIL if the implementation is missing.
		t.Fatalf("Xpath_open(nested_fd, %q) = %d; want ENOTDIR (%d) or ENOENT (%d)", trailingSlashFile, errno, wasiENotDir, wasiENoEnt)
	}

	// Verify the directory was actually opened on the host at the normalized path.
	hostDir := filepath.Join(tmpDir, "interesting_paths_dir", "dir", "nested")
	if _, err := os.Stat(hostDir); err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("host directory at normalized path %q does not exist: %v", hostDir, err)
		}
		t.Fatalf("host directory at normalized path %q: %v", hostDir, err)
	}
}

func assertMatchingFilestats(t *testing.T, buf []byte, offA, offB int32) {
	t.Helper()
	for _, tc := range []struct {
		name string
		off  int
		size int
	}{
		{"dev", filestatDevOff, 8},
		{"ino", filestatInoOff, 8},
		{"size", filestatSizeOff, 8},
	} {
		gotA := binary.LittleEndian.Uint64(buf[offA+int32(tc.off) : offA+int32(tc.off+tc.size)])
		gotB := binary.LittleEndian.Uint64(buf[offB+int32(tc.off) : offB+int32(tc.off+tc.size)])
		if gotA != gotB {
			t.Errorf("filestat %s = %d vs %d, want equal", tc.name, gotA, gotB)
		}
	}
	if gotA := buf[offA+filestatTypeOff]; gotA != buf[offB+filestatTypeOff] {
		t.Errorf("filestat filetype = %d vs %d, want equal", gotA, buf[offB+filestatTypeOff])
	}
	nlinkA := binary.LittleEndian.Uint64(buf[offA+filestatNlinkOff : offA+filestatNlinkOff+8])
	nlinkB := binary.LittleEndian.Uint64(buf[offB+filestatNlinkOff : offB+filestatNlinkOff+8])
	if nlinkA != 0 && nlinkB != 0 && nlinkA != nlinkB {
		t.Errorf("filestat nlink = %d vs %d, want equal", nlinkA, nlinkB)
	}
}

func assertMatchingFdstats(t *testing.T, buf []byte, offA, offB int32) {
	t.Helper()
	if buf[offA] != buf[offB] {
		t.Errorf("fdstat fs_filetype = %d vs %d, want equal", buf[offA], buf[offB])
	}
	flagsA := binary.LittleEndian.Uint16(buf[offA+2 : offA+4])
	flagsB := binary.LittleEndian.Uint16(buf[offB+2 : offB+4])
	if flagsA != flagsB {
		t.Errorf("fdstat fs_flags = %d vs %d, want equal", flagsA, flagsB)
	}
	rbA := binary.LittleEndian.Uint64(buf[offA+8 : offA+16])
	rbB := binary.LittleEndian.Uint64(buf[offB+8 : offB+16])
	if rbA != rbB {
		t.Errorf("fdstat fs_rights_base = 0x%x vs 0x%x, want equal", rbA, rbB)
	}
	riA := binary.LittleEndian.Uint64(buf[offA+16 : offA+24])
	riB := binary.LittleEndian.Uint64(buf[offB+16 : offB+24])
	if riA != riB {
		t.Errorf("fdstat fs_rights_inheriting = 0x%x vs 0x%x, want equal", riA, riB)
	}
}

func TestPathLinkSyscall(t *testing.T) {
	const (
		pathOff1 = 100
		pathOff2 = 200
		fdPtr    = 400
		statOff1 = 500
		statOff2 = 600
		fdstat1  = 700
		fdstat2  = 800
	)

	t.Run("hard link success same dir and subdir", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		if errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatalf("Xpath_link(file, link) = %d, want ESUCCESS", errno)
		}
		origInfo, err1 := os.Stat(filepath.Join(hostDir, "file"))
		linkInfo, err2 := os.Stat(filepath.Join(hostDir, "link"))
		if err1 != nil || err2 != nil {
			t.Fatalf("stat: %v, %v", err1, err2)
		}
		if !os.SameFile(origInfo, linkInfo) {
			t.Error("hard link is not the same file as original")
		}

		openRights := int64(rightFDRead | rightFDWrite | rightFDFilestatGet)
		if errno := s.Xpath_open(dirfd, 0, fileOff, fileLen, 0, openRights, 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open(file) = %d", errno)
		}
		fdOrig := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xpath_open(dirfd, 0, linkOff, linkLen, 0, openRights, 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open(link) = %d", errno)
		}
		fdLink := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xfd_filestat_get(fdOrig, statOff1); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get(orig) = %d", errno)
		}
		if errno := s.Xfd_filestat_get(fdLink, statOff2); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get(link) = %d", errno)
		}
		assertMatchingFilestats(t, buf, statOff1, statOff2)
		if errno := s.Xfd_fdstat_get(fdOrig, fdstat1); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get(orig) = %d", errno)
		}
		if errno := s.Xfd_fdstat_get(fdLink, fdstat2); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get(link) = %d", errno)
		}
		assertMatchingFdstats(t, buf, fdstat1, fdstat2)
		s.Xfd_close(fdOrig)
		s.Xfd_close(fdLink)

		const pathOffSubdir = 300
		subdirFD := openSubdirViaPathOpen(t, s, buf, hostDir, pathOffSubdir, fdPtr)
		if errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, subdirFD, linkOff, linkLen); errno != wasiESuccess {
			t.Fatalf("Xpath_link(file, subdir/link) = %d, want ESUCCESS", errno)
		}
		subLinkInfo, err := os.Stat(filepath.Join(hostDir, "subdir", "link"))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(origInfo, subLinkInfo) {
			t.Error("subdir hard link is not the same file as original")
		}
		if errno := s.Xpath_open(subdirFD, 0, linkOff, linkLen, 0, openRights, 0, 0, fdPtr); errno != wasiESuccess {
			t.Fatalf("Xpath_open(subdir/link) = %d", errno)
		}
		fdSubLink := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
		if errno := s.Xfd_filestat_get(fdSubLink, statOff2); errno != wasiESuccess {
			t.Fatalf("Xfd_filestat_get(subdir link) = %d", errno)
		}
		assertMatchingFilestats(t, buf, statOff1, statOff2)
		if errno := s.Xfd_fdstat_get(fdSubLink, fdstat2); errno != wasiESuccess {
			t.Fatalf("Xfd_fdstat_get(subdir link) = %d", errno)
		}
		assertMatchingFdstats(t, buf, fdstat1, fdstat2)
		s.Xfd_close(fdSubLink)
		s.Xfd_close(subdirFD)

		subLinkPathOff, subLinkPathLen := writePath(buf, pathOffSubdir, "subdir/link")
		if errno := s.Xpath_unlink_file(dirfd, subLinkPathOff, subLinkPathLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
		if errno := s.Xpath_unlink_file(dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
		if errno := s.Xpath_unlink_file(dirfd, fileOff, fileLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
		if err := os.Remove(filepath.Join(hostDir, "subdir")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Xpath_link returns EEXIST when link already exists", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create a file to link to.
		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create a link to the file.
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_link(file, link) = %d, want ESUCCESS", errno)
		}

		// Try to create the same link again - should return EEXIST.
		errno = s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiEExist {
			t.Errorf("Xpath_link(file, link) second call = %d, want EEXIST (%d)", errno, wasiEExist)
		}
	})

	t.Run("Xpath_link returns EEXIST when new path is an existing directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		if errno := s.Xpath_create_directory(dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatalf("Xpath_create_directory(link) = %d, want ESUCCESS", errno)
		}
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiEExist {
			t.Errorf("Xpath_link(file, existing dir link) = %d, want EEXIST (%d)", errno, wasiEExist)
		}
		if errno := s.Xpath_remove_directory(dirfd, linkOff, linkLen); errno != wasiESuccess {
			t.Fatal(errno)
		}
	})

	t.Run("Xpath_link returns EEXIST for self-link", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create a file.
		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Try to link the file to itself - should return EEXIST.
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, fileOff, fileLen)
		if errno != wasiEExist {
			t.Errorf("Xpath_link(file, file) = %d, want EEXIST (%d)", errno, wasiEExist)
		}
	})

	t.Run("Xpath_link returns EPERM or EACCES when linking to a directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create a directory to link to.
		if err := os.Mkdir(filepath.Join(hostDir, "dir"), 0755); err != nil {
			t.Fatal(err)
		}

		// Try to create a hard link to the directory - should return EPERM or EACCES.
		dirOff, dirLen := writePath(buf, pathOff1, "dir")
		linkOff, linkLen := writePath(buf, pathOff2, "link")
		errno := s.Xpath_link(dirfd, 0, dirOff, dirLen, dirfd, linkOff, linkLen)
		if errno != wasiEPerm && errno != wasiEAcces {
			t.Errorf("Xpath_link(dir, link) = %d, want EPERM (%d) or EACCES (%d)", errno, wasiEPerm, wasiEAcces)
		}
	})

	t.Run("Xpath_link returns ENOENT when trailing slash on new path for a non-directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create a file.
		if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Try to link to a path with trailing slash - should return ENOENT.
		fileOff, fileLen := writePath(buf, pathOff1, "file")
		linkOff, linkLen := writePath(buf, pathOff2, "link/")
		errno := s.Xpath_link(dirfd, 0, fileOff, fileLen, dirfd, linkOff, linkLen)
		if errno != wasiENoEnt {
			t.Errorf("Xpath_link(file, link/) = %d, want ENOENT (%d)", errno, wasiENoEnt)
		}
	})

	// Cycle 3 behavior: path_link hard-links symlink inodes without following and rejects SYMLINK_FOLLOW lookup on symlink paths
	t.Run("Xpath_link returns ESUCCESS for symlink hard-link and rejects SYMLINK_FOLLOW", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Skip if NO_DANGLING_FILESYSTEM is set (parity with wasi-testsuite)
		if os.Getenv("NO_DANGLING_FILESYSTEM") != "" {
			t.Skip("NO_DANGLING_FILESYSTEM set; parity with wasi-testsuite symlink tests")
		}

		const (
			targetOff  = 100
			symlinkOff = 200
			linkOff    = 300
		)

		t.Run("hard-link to symlink succeeds", func(t *testing.T) {
			// Create a symlink to a target
			copy(buf[targetOff:], "target")
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
			}

			// Xpath_link(dirfd, 0, \"symlink\", dirfd, \"link\") should return ESUCCESS
			// when linking a symlink to a target (hard link to symlink)
			copy(buf[linkOff:], "link")
			errno = s.Xpath_link(dirfd, 0, symlinkOff, 7, dirfd, linkOff, 4)
			if errno != wasiESuccess {
				t.Errorf("Xpath_link(symlink, link) = %d, want ESUCCESS", errno)
			}

			// Cleanup
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if errno := s.Xpath_unlink_file(dirfd, linkOff, 4); errno != wasiESuccess {
				t.Fatal(errno)
			}
		})

		t.Run("hard-link to symlink loop succeeds", func(t *testing.T) {
			// Create a symlink that points to itself (loop)
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(symlinkOff, 7, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(symlink, symlink) = %d, want ESUCCESS", errno)
			}

			// Xpath_link(dirfd, 0, \"symlink\", dirfd, \"link\") should return ESUCCESS
			// for symlink loop (linking symlink to itself)
			copy(buf[linkOff:], "link")
			errno = s.Xpath_link(dirfd, 0, symlinkOff, 7, dirfd, linkOff, 4)
			if errno != wasiESuccess {
				t.Errorf("Xpath_link(symlink, link) = %d, want ESUCCESS", errno)
			}

			// Cleanup
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if errno := s.Xpath_unlink_file(dirfd, linkOff, 4); errno != wasiESuccess {
				t.Fatal(errno)
			}
		})

		t.Run("linking file to existing symlink returns EEXIST", func(t *testing.T) {
			// Create a symlink to a target
			copy(buf[targetOff:], "target")
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
			}

			// Create a file to link
			if err := os.WriteFile(filepath.Join(hostDir, "file"), []byte("data"), 0644); err != nil {
				t.Fatal(err)
			}
			copy(buf[targetOff:], "file")

			// Xpath_link(dirfd, 0, \"file\", dirfd, \"symlink\") should return EEXIST
			// when linking a file to a symlink (symlink already exists)
			errno = s.Xpath_link(dirfd, 0, targetOff, 4, dirfd, symlinkOff, 7)
			if errno != wasiEExist {
				t.Errorf("Xpath_link(file, symlink) = %d, want EEXIST (%d)", errno, wasiEExist)
			}

			// Cleanup
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if err := os.Remove(filepath.Join(hostDir, "file")); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("SYMLINK_FOLLOW flag on symlink path returns EINVAL or ENOENT", func(t *testing.T) {
			// Create a symlink to a target
			copy(buf[targetOff:], "target")
			copy(buf[symlinkOff:], "symlink")
			errno := s.Xpath_symlink(targetOff, 6, dirfd, symlinkOff, 7)
			if errno != wasiESuccess {
				t.Fatalf("Xpath_symlink(target, symlink) = %d, want ESUCCESS", errno)
			}

			// Xpath_link(dirfd, wasiLookupSymlinkFollow, \"symlink\", dirfd, \"link\") should
			// return EINVAL or ENOENT (not ESUCCESS) when using SYMLINK_FOLLOW flag
			copy(buf[linkOff:], "link")
			errno = s.Xpath_link(dirfd, wasiLookupSymlinkFollow, symlinkOff, 7, dirfd, linkOff, 4)
			if errno == wasiESuccess {
				t.Errorf("Xpath_link(symlink, link) with SYMLINK_FOLLOW = ESUCCESS, want EINVAL (%d) or ENOENT (%d)", wasiEInval, wasiENoEnt)
			} else if errno != wasiEInval && errno != wasiENoEnt {
				t.Errorf("Xpath_link(symlink, link) with SYMLINKFollow = %d, want EINVAL (%d) or ENOENT (%d)", errno, wasiEInval, wasiENoEnt)
			}

			// Cleanup (link may not exist when Xpath_link correctly rejected SYMLINK_FOLLOW)
			if errno := s.Xpath_unlink_file(dirfd, symlinkOff, 7); errno != wasiESuccess {
				t.Fatal(errno)
			}
			if errno := s.Xpath_unlink_file(dirfd, linkOff, 4); errno != wasiESuccess && errno != wasiENoEnt {
				t.Fatal(errno)
			}
		})
	})
}

func TestPathRenameSyscall(t *testing.T) {
	const (
		pathOff1 = 100
		pathOff2 = 200
	)

	t.Run("moving a directory: source gone, target openable", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create a source directory
		srcDir := filepath.Join(hostDir, "src_dir")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create a target directory path
		srcOff, srcLen := writePath(buf, pathOff1, "src_dir")
		targetOff, targetLen := writePath(buf, pathOff2, "target_dir")

		// Xpath_rename should move the directory
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_rename(src, target) = %d, want ESUCCESS", errno)
		}

		// Verify source directory is gone
		if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
			t.Errorf("Source directory still exists after rename")
		}

		// Verify target directory exists and is openable
		targetPath := filepath.Join(hostDir, "target_dir")
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			t.Errorf("Target directory does not exist after rename")
		}

		// Cleanup
		if err := os.RemoveAll(targetPath); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("moving a file: source gone, target openable", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create a source file
		srcFile := filepath.Join(hostDir, "src_file")
		if err := os.WriteFile(srcFile, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create a target file path
		srcOff, srcLen := writePath(buf, pathOff1, "src_file")
		targetOff, targetLen := writePath(buf, pathOff2, "target_file")

		// Xpath_rename should move the file
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_rename(src, target) = %d, want ESUCCESS", errno)
		}

		// Verify source file is gone
		if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
			t.Errorf("Source file still exists after rename")
		}

		// Verify target file exists and is openable
		targetPath := filepath.Join(hostDir, "target_file")
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			t.Errorf("Target file does not exist after rename")
		}

		// Verify we can read the file content
		content, err := os.ReadFile(targetPath)
		if err != nil {
			t.Errorf("Cannot read target file: %v", err)
		} else if string(content) != "data" {
			t.Errorf("Target file content mismatch")
		}

		// Cleanup
		if err := os.Remove(targetPath); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("atomic replace: existing target replaced", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create source file
		srcFile := filepath.Join(hostDir, "src")
		if err := os.WriteFile(srcFile, []byte("src_data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create target file (will be replaced)
		targetFile := filepath.Join(hostDir, "target")
		if err := os.WriteFile(targetFile, []byte("old_data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "src")
		targetOff, targetLen := writePath(buf, pathOff2, "target")

		// Xpath_rename should replace the target file atomically
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_rename(src, target) = %d, want ESUCCESS", errno)
		}

		// Verify source file is gone
		if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
			t.Errorf("Source file still exists after rename")
		}

		// Verify only target exists (not both)
		if _, err := os.Stat(srcFile); os.IsNotExist(err) {
			// Source is gone, good
		} else {
			t.Errorf("Source file should not exist")
		}
		if _, err := os.Stat(targetFile); os.IsNotExist(err) {
			t.Errorf("Target file does not exist after rename")
		}

		// Verify we can read the file content (should be src_data)
		content, err := os.ReadFile(targetFile)
		if err != nil {
			t.Errorf("Cannot read target file: %v", err)
		} else if string(content) != "src_data" {
			t.Errorf("Target file content should be from source, got: %s", string(content))
		}

		// Cleanup
		if err := os.Remove(targetFile); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("empty directory rename over existing empty directory", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create empty source directory
		srcDir := filepath.Join(hostDir, "empty_src")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create empty target directory (existing)
		targetDir := filepath.Join(hostDir, "empty_target")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "empty_src")
		targetOff, targetLen := writePath(buf, pathOff2, "empty_target")

		// The host reports POSIX semantics on every OS: renaming an empty
		// directory over an existing empty directory succeeds.
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)
		if errno != wasiESuccess {
			t.Fatalf("Xpath_rename on empty directory (over existing) should succeed on darwin/linux, got %d", errno)
		}

		// Verify source directory is gone
		if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
			t.Errorf("Source directory still exists after rename")
		}

		// Verify target directory exists
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			t.Errorf("Target directory does not exist after rename")
		}

		// Cleanup only if successful (or on non-Windows)
		if runtime.GOOS != "windows" || errno == wasiESuccess {
			if err := os.RemoveAll(targetDir); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("source directory, target directory with file inside: should return ENOTEMPTY or EACCES", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create source directory
		srcDir := filepath.Join(hostDir, "src_dir")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create target directory with a file inside
		targetDir := filepath.Join(hostDir, "target_dir")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}
		targetFile := filepath.Join(targetDir, "file_inside")
		if err := os.WriteFile(targetFile, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "src_dir")
		targetOff, targetLen := writePath(buf, pathOff2, "target_dir")

		// Xpath_rename should fail because target directory is not empty
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)

		if runtime.GOOS == "windows" {
			// On Windows, renaming a directory over a non-empty directory should fail with EACCES
			if errno == wasiESuccess {
				t.Errorf("Xpath_rename on non-empty directory should fail on Windows, got ESUCCESS")
			}
		} else {
			// On Unix, should return ENOTEMPTY
			if errno != wasiENotEmpty {
				t.Fatalf("Xpath_rename on non-empty directory should return ENOTEMPTY on Unix, got %d", errno)
			}
		}
	})

	t.Run("source file, target directory: should return EISDIR or EACCES", func(t *testing.T) {
		t.Parallel()
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		// Create source file
		srcFile := filepath.Join(hostDir, "src_file")
		if err := os.WriteFile(srcFile, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create target directory
		targetDir := filepath.Join(hostDir, "target_dir")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Prepare paths
		srcOff, srcLen := writePath(buf, pathOff1, "src_file")
		targetOff, targetLen := writePath(buf, pathOff2, "target_dir")

		// Xpath_rename should fail because target is a directory, not a file
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, targetOff, targetLen)

		if runtime.GOOS == "windows" {
			// On Windows, renaming a file into a directory should fail with EACCES
			if errno == wasiESuccess {
				t.Errorf("Xpath_rename(file into dir) should fail on Windows, got ESUCCESS")
			}
		} else {
			// On Unix, should return EISDIR
			if errno != wasiEIsdir {
				t.Fatalf("Xpath_rename(file into dir) should return EISDIR on Unix, got %d", errno)
			}
		}
	})

	t.Run("source directory to target/file returns ENOTDIR on unix", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("path_rename.rs ENOTDIR case is unix-only in this plan")
		}
		s, buf := newTestState(nil)
		hostDir := setupWritableMount(t, s, buf)

		srcDir := filepath.Join(hostDir, "source")
		if err := os.Mkdir(srcDir, 0755); err != nil {
			t.Fatal(err)
		}
		targetDir := filepath.Join(hostDir, "target")
		if err := os.Mkdir(targetDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, "file_inside"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		srcOff, srcLen := writePath(buf, pathOff1, "source")
		newOff, newLen := writePath(buf, pathOff2, "target/file_inside")
		errno := s.Xpath_rename(dirfd, srcOff, srcLen, dirfd, newOff, newLen)
		if errno != wasiENotDir {
			t.Fatalf("Xpath_rename(source, target/file_inside) = %d, want ENOTDIR (%d)", errno, wasiENotDir)
		}
	})

}

// TestPathOpenTruncationUsesPathFilestatSetSizeRight verifies O_TRUNC requires
// PATH_FILESTAT_SET_SIZE in the directory preopen rights_base, not
// FD_FILESTAT_SET_SIZE in rights_inheriting (mirrors truncation_rights.rs).
func TestPathOpenTruncationUsesPathFilestatSetSizeRight(t *testing.T) {
	const (
		pathOff int32 = 500
		fdPtr   int32 = 600
		statOff int32 = 700
		statPtr int32 = 800
	)

	s, buf := newTestState(nil)
	hostDir := setupWritableMount(t, s, buf)
	baseMask := wasiTestsuiteDirectoryBaseRightsMask()
	inheritingMask := wasiTestsuiteDirectoryInheritingRightsMask()
	setupMountAtDirfd(t, s, "tmp", hostDir, true, baseMask, inheritingMask)

	if errno := s.Xfd_fdstat_get(dirfd, statPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(preopen) = %d, want ESUCCESS", errno)
	}
	gotBase := binary.LittleEndian.Uint64(buf[statPtr+8 : statPtr+16])
	// truncation_rights.rs skips when PATH_FILESTAT_SET_SIZE is absent from base.
	if gotBase&rightPathFilestatSetSize == 0 {
		t.Skip("preopen rights_base lacks PATH_FILESTAT_SET_SIZE; cannot assert truncation gating")
	}

	const fname = "file"
	if err := os.WriteFile(filepath.Join(hostDir, fname), nil, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", fname, err)
	}
	pathOffWritten, pathLen := writePath(buf, pathOff, fname)

	openRights := int64(0)
	assertTruncSucceedsSizeZero := func(label string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(hostDir, fname), []byte("content"), 0o644); err != nil {
			t.Fatalf("%s: WriteFile failed: %v", label, err)
		}
		errno := s.Xpath_open(dirfd, 0, pathOffWritten, pathLen, int32(oflagTrunc),
			openRights, 0, 0, fdPtr)
		if errno != wasiESuccess {
			t.Fatalf("%s: Xpath_open(O_TRUNC) = %d, want ESUCCESS", label, errno)
		}
		if errno := s.Xpath_filestat_get(dirfd, 0, pathOffWritten, pathLen, statOff); errno != wasiESuccess {
			t.Fatalf("%s: Xpath_filestat_get = %d, want ESUCCESS", label, errno)
		}
		size := binary.LittleEndian.Uint64(buf[statOff+filestatSizeOff : statOff+filestatSizeOff+8])
		if size != 0 {
			t.Errorf("%s: path_filestat_get size after O_TRUNC = %d, want 0", label, size)
		}
	}

	assertTruncSucceedsSizeZero("baseline")

	// O_TRUNC still succeeds after FD_FILESTAT_SET_SIZE is dropped from inheriting.
	gotInh := binary.LittleEndian.Uint64(buf[statPtr+16 : statPtr+24])
	newInh := gotInh &^ rightFDFilestatSetSize
	if errno := s.Xfd_fdstat_set_rights(dirfd, int64(gotBase), int64(newInh)); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_set_rights(drop FD_FILESTAT_SET_SIZE from inheriting) = %d, want ESUCCESS", errno)
	}
	assertTruncSucceedsSizeZero("without FD_FILESTAT_SET_SIZE in inheriting")

	// O_TRUNC fails once PATH_FILESTAT_SET_SIZE is dropped from base.
	newBase := gotBase &^ rightPathFilestatSetSize
	if errno := s.Xfd_fdstat_set_rights(dirfd, int64(newBase), int64(newInh)); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_set_rights(drop PATH_FILESTAT_SET_SIZE from base) = %d, want ESUCCESS", errno)
	}
	if err := os.WriteFile(filepath.Join(hostDir, fname), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	errno := s.Xpath_open(dirfd, 0, pathOffWritten, pathLen, int32(oflagTrunc),
		openRights, 0, 0, fdPtr)
	if errno != wasiEPerm && errno != wasiENotCap {
		t.Fatalf("Xpath_open(O_TRUNC) without PATH_FILESTAT_SET_SIZE in base = %d, want EPERM(%d) or ENOTCAPABLE(%d)",
			errno, wasiEPerm, wasiENotCap)
	}
}

// openSubdirViaPathOpen creates "subdir" under tmpDir, opens it via path_open
// with O_DIRECTORY and rightsWritableDirPreopen, and returns the resulting fd.
// It writes the path name into buf at pathOff and stores the fd at fdPtr.
func openSubdirViaPathOpen(t *testing.T, s *State, buf []byte, tmpDir string, pathOff, fdPtr int32) int32 {
	t.Helper()
	if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	copy(buf[pathOff:], "subdir")
	errno := s.Xpath_open(dirfd, 0, pathOff, 6, int32(oflagDir), int64(rightsWritableDirPreopen), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(subdir) = %d, want ESUCCESS", errno)
	}
	return int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))
}

// TestPositionAndSizeOpsOnDirectoryFDsRejected verifies that fd_seek,
// fd_tell, fd_allocate, and fd_filestat_set_size on directory file
// descriptors return EISDIR, EBADF, ENOTCAP, or EINVAL rather than ESUCCESS.
func TestPositionAndSizeOpsOnDirectoryFDsRejected(t *testing.T) {
	t.Parallel()
	const (
		resultPtr = 500
		fdPtr     = 1000
		pathOff   = 2000
	)

	cases := []struct {
		name    string
		syscall func(s *State, subdirFD int32) int32
	}{
		{
			name: "fd_seek SEEK_SET on writable preopen dir fd",
			syscall: func(s *State, _ int32) int32 {
				return s.Xfd_seek(dirfd, 0, 0, resultPtr)
			},
		},
		{
			name: "fd_seek SEEK_CUR on writable preopen dir fd",
			syscall: func(s *State, _ int32) int32 {
				return s.Xfd_seek(dirfd, 0, 1, resultPtr)
			},
		},
		{
			name: "fd_seek SEEK_END on writable preopen dir fd",
			syscall: func(s *State, _ int32) int32 {
				return s.Xfd_seek(dirfd, 0, 2, resultPtr)
			},
		},
		{
			name: "fd_tell on writable preopen dir fd",
			syscall: func(s *State, _ int32) int32 {
				return s.Xfd_tell(dirfd, resultPtr)
			},
		},
		{
			name: "fd_allocate on writable preopen dir fd",
			syscall: func(s *State, _ int32) int32 {
				return s.Xfd_allocate(dirfd, 0, 1024)
			},
		},
		{
			name: "fd_filestat_set_size on writable preopen dir fd",
			syscall: func(s *State, _ int32) int32 {
				return s.Xfd_filestat_set_size(dirfd, 1024)
			},
		},
		{
			name: "fd_seek SEEK_SET on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, subdirFD int32) int32 {
				return s.Xfd_seek(subdirFD, 0, 0, resultPtr)
			},
		},
		{
			name: "fd_seek SEEK_CUR on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, subdirFD int32) int32 {
				return s.Xfd_seek(subdirFD, 0, 1, resultPtr)
			},
		},
		{
			name: "fd_seek SEEK_END on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, subdirFD int32) int32 {
				return s.Xfd_seek(subdirFD, 0, 2, resultPtr)
			},
		},
		{
			name: "fd_tell on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, subdirFD int32) int32 {
				return s.Xfd_tell(subdirFD, resultPtr)
			},
		},
		{
			name: "fd_allocate on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, subdirFD int32) int32 {
				return s.Xfd_allocate(subdirFD, 0, 1024)
			},
		},
		{
			name: "fd_filestat_set_size on directory fd opened via path_open O_DIRECTORY",
			syscall: func(s *State, subdirFD int32) int32 {
				return s.Xfd_filestat_set_size(subdirFD, 1024)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, buf, tmpDir := newWMState(t)
			subdirFD := openSubdirViaPathOpen(t, s, buf, tmpDir, pathOff, fdPtr)

			errno := tc.syscall(s, subdirFD)
			if errno == wasiESuccess {
				t.Errorf("%s = ESUCCESS, want one of EISDIR (%d), EBADF (%d), ENOTCAP (%d), EINVAL (%d)",
					tc.name, wasiEIsdir, wasiEBadf, wasiENotCap, wasiEInval)
			}
		})
	}
}

// TestDirectoryFdOpenedViaPathOpenOmitsFDSeekInFdstat verifies that a directory
// fd opened via path_open with O_DIRECTORY and FD_SEEK requested succeeds, but
// fd_fdstat_get on the new fd reports DIRECTORY type with FD_SEEK absent from
// rights_base, and fd_seek still fails with EISDIR.
func TestDirectoryFdOpenedViaPathOpenOmitsFDSeekInFdstat(t *testing.T) {
	t.Parallel()
	const (
		pathOff   = 1000
		fdPtr     = 2000
		fdstatPtr = 3000
		resultPtr = 4000
	)

	s, buf, tmpDir := newWMState(t)

	if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Open directory with O_DIRECTORY and request FD_SEEK.
	copy(buf[pathOff:], "subdir")
	errno := s.Xpath_open(dirfd, 0, pathOff, 6, int32(oflagDir), int64(rightFDSeek), 0, 0, fdPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpath_open(subdir, O_DIRECTORY, FD_SEEK) = %d, want ESUCCESS", errno)
	}
	fd := int32(binary.LittleEndian.Uint32(buf[fdPtr : fdPtr+4]))

	// fd_fdstat_get must succeed.
	if errno := s.Xfd_fdstat_get(fd, fdstatPtr); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_get(dir fd) = %d, want ESUCCESS", errno)
	}

	// fs_filetype must be DIRECTORY.
	if ft := buf[fdstatPtr]; ft != fdDir {
		t.Errorf("fd_fdstat_get.fs_filetype = %d, want DIRECTORY (%d)", ft, fdDir)
	}

	// rights_base must NOT contain FD_SEEK.
	rightsBase := binary.LittleEndian.Uint64(buf[fdstatPtr+8 : fdstatPtr+16])
	if rightsBase&uint64(rightFDSeek) != 0 {
		t.Errorf("rights_base has FD_SEEK set (0x%x); expected it to be absent", rightsBase)
	}

	// fd_seek must still fail with EISDIR (cycle 4 behavior).
	if errno := s.Xfd_seek(fd, 0, 0, resultPtr); errno != wasiEIsdir {
		t.Errorf("fd_seek on directory fd = %d, want EISDIR (%d)", errno, wasiEIsdir)
	}
}

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
			subdirFD := openSubdirViaPathOpen(t, s, buf, tmpDir, pathOff, fdPtr)

			errno := s.Xfd_readdir(dirfd, rdBufPtr, rdBufLen, 0, usedPtr)
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
