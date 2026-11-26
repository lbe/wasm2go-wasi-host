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

// hostInode returns the NTFS file index of hostPath (without following a
// final symlink), which serves as the inode number, or 0 if it cannot be read.
func hostInode(hostPath string) Inode {
	p, err := syscall.UTF16PtrFromString(hostPath)
	if err != nil {
		return 0
	}
	h, err := syscall.CreateFile(p, 0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var fi syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &fi); err != nil {
		return 0
	}
	return Inode(fi.FileIndexHigh)<<32 | Inode(fi.FileIndexLow)
}
