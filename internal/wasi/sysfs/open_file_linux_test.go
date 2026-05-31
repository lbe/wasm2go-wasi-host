//go:build linux

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

// TestDirFSOpenFileNoFollowRejectsSymlink verifies path_open-style opens pass
// O_NOFOLLOW to the host: opening a symlink must fail, not return the target fd.
func TestDirFSOpenFileNoFollowRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(target): %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("target", link); err != nil {
		t.Fatalf("Symlink(link -> target): %v", err)
	}

	dfs := DirFS{Path: dir}
	_, errno := dfs.OpenFile("link", wasi.O_RDONLY|wasi.O_NOFOLLOW, 0)
	if errno != wasi.ELOOP {
		t.Fatalf("OpenFile(link, O_NOFOLLOW): errno=%d (%s), want ELOOP (%d)",
			errno, errno.Error(), wasi.ELOOP)
	}
}
