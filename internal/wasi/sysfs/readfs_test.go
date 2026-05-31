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

func TestReadFSOpenFileWithWriteFlagReturnsEROFS(t *testing.T) {
	dir := t.TempDir()
	WriteTestFiles(t, dir)

	inner := DirFS{Path: dir}
	fs := ReadFS{FS: inner}

	// O_RDWR should be denied with EROFS.
	_, errno := fs.OpenFile("animals.txt", wasi.O_RDWR, 0)
	if errno != wasi.EROFS {
		t.Errorf("ReadFS.OpenFile(O_RDWR): got errno %d (%s), want EROFS (%d)",
			errno, errno.Error(), wasi.EROFS)
	}
}

func TestReadFSMkdirReturnsEROFS(t *testing.T) {
	dir := t.TempDir()
	WriteTestFiles(t, dir)

	inner := DirFS{Path: dir}
	fs := ReadFS{FS: inner}

	errno := fs.Mkdir("newdir", 0o755)
	if errno != wasi.EROFS {
		t.Errorf("ReadFS.Mkdir: got errno %d (%s), want EROFS (%d)",
			errno, errno.Error(), wasi.EROFS)
	}

	// Verify the directory was NOT created.
	if _, err := os.Stat(filepath.Join(dir, "newdir")); !os.IsNotExist(err) {
		t.Errorf("ReadFS.Mkdir: directory should not have been created")
	}
}
