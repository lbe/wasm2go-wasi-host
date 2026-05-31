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
)

// WriteTestFiles writes a test fixture to the given directory on the real
// filesystem, creating subdirectories as needed.
func WriteTestFiles(t *testing.T, dir string) {
	t.Helper()
	fixture := map[string][]byte{
		"animals.txt":  []byte("dog\ncat\nbird\nfish\ncow\n"),
		"sub/test.txt": []byte("hello\n"),
		"dir/a.dat":    []byte("aaaaaaaaaaaaaaaaaaaa"),
		"dir/ab.dat":   []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		"empty.txt":    {},
	}
	for name, content := range fixture {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
}
