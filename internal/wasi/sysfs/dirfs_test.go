// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func TestDirFSRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()

	// Create a legitimate subdirectory and file inside the root.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legit.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a file outside the root to ensure it is NOT opened.
	outsidePath := filepath.Join(filepath.Dir(dir), "outside")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile outside: %v", err)
	}
	defer os.Remove(outsidePath)

	fs := DirFS{Path: dir}

	// OpenFile("../outside", O_RDONLY, 0) must return non-zero errno.
	_, errno := fs.OpenFile("../outside", wasi.O_RDONLY, 0)
	if errno == 0 {
		t.Errorf("OpenFile(\"../outside\", O_RDONLY, 0): got errno 0, want non-zero (EPERM or ENOENT)")
	}

	// Verify the outside file was not read indirectly (noop on most OSes,
	// but confirms no panic and no side effect).
	info, err := os.Stat(outsidePath)
	if err != nil {
		t.Fatalf("os.Stat outside: %v", err)
	}
	if info.Size() != 6 {
		t.Errorf("outside file size changed: got %d, want 6", info.Size())
	}

	// OpenFile("sub/../../outside", O_RDONLY, 0) must return non-zero errno.
	_, errno = fs.OpenFile("sub/../../outside", wasi.O_RDONLY, 0)
	if errno == 0 {
		t.Errorf("OpenFile(\"sub/../../outside\", O_RDONLY, 0): got errno 0, want non-zero")
	}

	// OpenFile("legit.txt", O_CREAT|O_WRONLY, 0o644) inside root must still succeed.
	f, errno := fs.OpenFile("legit.txt", wasi.O_CREAT|wasi.O_WRONLY, 0o644)
	if errno != 0 {
		t.Errorf("OpenFile(\"legit.txt\", O_CREAT|O_WRONLY, 0o644): got errno %d (%s), want 0",
			errno, errno.Error())
	} else {
		if closeErrno := f.Close(); closeErrno != 0 {
			t.Errorf("File.Close: got errno %d (%s), want 0", closeErrno, closeErrno.Error())
		}
	}

	// Mkdir("../escaped", perm) must return non-zero errno.
	errno = fs.Mkdir("../escaped", 0o755)
	if errno == 0 {
		t.Errorf("Mkdir(\"../escaped\", 0o755): got errno 0, want non-zero")
	}

	// Verify no escaped directory was created outside root.
	escapedPath := filepath.Join(filepath.Dir(dir), "escaped")
	if _, err := os.Stat(escapedPath); !os.IsNotExist(err) {
		t.Errorf("escaped directory should NOT exist outside root")
	}

	// Create file "a" inside root for rename test.
	aPath := filepath.Join(dir, "a")
	if err := os.WriteFile(aPath, []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile a: %v", err)
	}

	// Rename("a", "../b") must return non-zero errno.
	errno = fs.Rename("a", "../b")
	if errno == 0 {
		t.Errorf("Rename(\"a\", \"../b\"): got errno 0, want non-zero")
	}

	// Verify "a" still exists inside root (rename did not happen).
	if _, err := os.Stat(aPath); err != nil {
		t.Errorf("original file \"a\" should still exist in root: %v", err)
	}

	// Verify "b" was NOT created outside root.
	bPath := filepath.Join(filepath.Dir(dir), "b")
	if _, err := os.Stat(bPath); !os.IsNotExist(err) {
		t.Errorf("file \"b\" should NOT exist outside root")
	}
}

func TestDirFSCreatesAndWritesRealFile(t *testing.T) {
	dir := t.TempDir()
	WriteTestFiles(t, dir)

	fs := DirFS{Path: dir}

	f, errno := fs.OpenFile("newfile.txt", wasi.O_CREAT|wasi.O_WRONLY, 0o644)
	if errno != 0 {
		t.Fatalf("DirFS.OpenFile(O_CREAT|O_WRONLY): got errno %d (%s), want 0",
			errno, errno.Error())
	}

	content := []byte("hello from DirFS\n")
	n, errno := f.Write(content)
	if errno != 0 {
		t.Fatalf("File.Write: got errno %d (%s), want 0", errno, errno.Error())
	}
	if n != len(content) {
		t.Fatalf("File.Write: got n=%d, want %d", n, len(content))
	}

	if errno = f.Close(); errno != 0 {
		t.Fatalf("File.Close: got errno %d (%s), want 0", errno, errno.Error())
	}

	// Verify the file exists on disk with the correct content.
	got, err := os.ReadFile(filepath.Join(dir, "newfile.txt"))
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("file content: got %q, want %q", string(got), string(content))
	}
}

// Rmdir on a non-empty directory must report ENOTEMPTY. syscall.ENOTEMPTY also
// matches fs.ErrExist, so a mapper that checks sentinels first reports EEXIST.
func TestDirFSRmdirNonEmptyReturnsENOTEMPTY(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if errno := (DirFS{Path: dir}).Rmdir("a"); errno != wasi.ENOTEMPTY {
		t.Fatalf("Rmdir(non-empty) = %v (%d), want ENOTEMPTY", errno, errno)
	}
}

// Unlink on a directory must report EISDIR on every Unix host. Darwin's
// unlink(2) returns EPERM, which also matches fs.ErrPermission; the EPERM to
// EISDIR rewrite only works if the mapper keeps EPERM distinct from EACCES.
func TestDirFSUnlinkDirectoryReturnsEISDIR(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if errno := (DirFS{Path: dir}).Unlink("d"); errno != wasi.EISDIR {
		t.Fatalf("Unlink(directory) = %v (%d), want EISDIR", errno, errno)
	}
}
