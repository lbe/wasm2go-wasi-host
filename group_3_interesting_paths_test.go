package wasihost

import (
	"encoding/binary"
	"os"
	"path"
	"path/filepath"
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
		WithHostDirectoryPreopen("/data", tmpDir),
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
	if entry, ok := s.fdEntry(fileFd); ok {
		if entry.path != expectedGuestPath {
			t.Fatalf("opened file guest path = %q, want %q", entry.path, expectedGuestPath)
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
