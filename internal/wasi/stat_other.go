//go:build !unix && !windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import "io/fs"

// statFromFileInfo uses only the portable fs.FileInfo fields on platforms
// without a syscall stat mapping.
func statFromFileInfo(info fs.FileInfo) Stat_t {
	return defaultStatFromFileInfo(info)
}
