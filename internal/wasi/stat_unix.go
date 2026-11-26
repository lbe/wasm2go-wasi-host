//go:build unix

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

// timespecToEpochNanos converts a syscall.Timespec to epoch nanoseconds.
func timespecToEpochNanos(ts syscall.Timespec) EpochNanos {
	return EpochNanos(ts.Sec)*1e9 + EpochNanos(ts.Nsec)
}

// statFromSyscallStatTimestamps maps syscall.Stat_t fields and platform-specific
// access/modify/change times into Stat_t.
func statFromSyscallStatTimestamps(d *syscall.Stat_t, info fs.FileInfo, atim, mtim, ctim syscall.Timespec) Stat_t {
	return Stat_t{
		Dev:   uint64(d.Dev),
		Ino:   Inode(d.Ino),
		Mode:  info.Mode(),
		Nlink: uint64(d.Nlink),
		Size:  d.Size,
		Atim:  timespecToEpochNanos(atim),
		Mtim:  timespecToEpochNanos(mtim),
		Ctim:  timespecToEpochNanos(ctim),
	}
}

func statFromFileInfo(info fs.FileInfo) Stat_t {
	if d, ok := info.Sys().(*syscall.Stat_t); ok {
		return statFromSyscallStat(d, info)
	}
	return defaultStatFromFileInfo(info)
}
