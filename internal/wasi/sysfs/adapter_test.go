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

func TestAdaptFSMkdirReturnsENOSYS(t *testing.T) {
	dir := t.TempDir()
	WriteTestFiles(t, dir)

	fs := AdaptFS{FS: os.DirFS(dir)}

	errno := fs.Mkdir("newdir", 0o755)
	if errno != wasi.ENOSYS {
		t.Errorf("AdaptFS.Mkdir: got errno %d (%s), want ENOSYS (%d)",
			errno, errno.Error(), wasi.ENOSYS)
	}

	// Verify the directory was NOT created.
	if _, err := os.Stat(filepath.Join(dir, "newdir")); !os.IsNotExist(err) {
		t.Errorf("AdaptFS.Mkdir: directory should not have been created")
	}
}

func TestAdaptFSRenameReturnsENOSYS(t *testing.T) {
	dir := t.TempDir()
	WriteTestFiles(t, dir)

	fs := AdaptFS{FS: os.DirFS(dir)}

	errno := fs.Rename("empty.txt", "renamed.txt")
	if errno != wasi.ENOSYS {
		t.Errorf("AdaptFS.Rename: got errno %d (%s), want ENOSYS (%d)",
			errno, errno.Error(), wasi.ENOSYS)
	}

	// Verify the file was NOT renamed.
	if _, err := os.Stat(filepath.Join(dir, "empty.txt")); err != nil {
		t.Errorf("AdaptFS.Rename: original file should still exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "renamed.txt")); !os.IsNotExist(err) {
		t.Errorf("AdaptFS.Rename: renamed file should not exist")
	}
}
