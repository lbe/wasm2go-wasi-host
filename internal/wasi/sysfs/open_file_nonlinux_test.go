//go:build !linux

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"os"
	"os/exec"
	"testing"
)

// TestDirFSOpenFileNoFollowRejectsSymlink is implemented on Linux; elsewhere
// assert the sysfs package cross-builds for GOOS=linux (open_file_unix.go).
func TestDirFSOpenFileNoFollowRejectsSymlink(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GOOS=linux go build: %v\n%s", err, out)
	}
}
