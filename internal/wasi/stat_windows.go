//go:build windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"io/fs"
	"syscall"
)

// statFromFileInfo maps Windows file attribute data into Stat_t. Device,
// inode and link count are not part of Win32FileAttributeData; sysfs fills
// them from a file handle where it needs them.
func statFromFileInfo(info fs.FileInfo) Stat_t {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return defaultStatFromFileInfo(info)
	}
	return Stat_t{
		Mode:  info.Mode(),
		Nlink: 1,
		Size:  info.Size(),
		Atim:  d.LastAccessTime.Nanoseconds(),
		Mtim:  d.LastWriteTime.Nanoseconds(),
		Ctim:  d.CreationTime.Nanoseconds(),
	}
}
