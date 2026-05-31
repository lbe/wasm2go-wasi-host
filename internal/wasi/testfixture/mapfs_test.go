// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package testfixture

import (
	"testing"
)

// TestMapFSContainsRequiredFiles verifies that the testfixture package
// provides a MapFS with the required test files per design-improvements-2 Tier 2:
// - animals.txt at root
// - sub/test.txt in subdirectory
// - dir/- edge name (directory with hyphen-minus name)
func TestMapFSContainsRequiredFiles(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "animals.txt", path: "animals.txt", want: "dog\ncat\nbird\nfish\ncow\n"},
		{name: "sub/test.txt", path: "sub/test.txt", want: "hello\n"},
		{name: "dir/-", path: "dir/-", want: "edge case data\n"},
	}

	fs := MapFS{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fs.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("MapFS.ReadFile(%q): %v", tt.path, err)
			}
			if string(got) != tt.want {
				t.Errorf("content = %q, want %q", string(got), tt.want)
			}
		})
	}
}
