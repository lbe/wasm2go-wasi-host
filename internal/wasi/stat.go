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

// Inode is the file serial number, or zero if unknown.
type Inode = uint64

// EpochNanos is a timestamp in epoch nanoseconds, or zero if unknown.
type EpochNanos = int64

// Stat_t is similar to syscall.Stat_t, except available on all operating
// systems, including Windows.
type Stat_t struct {
	Dev   uint64
	Ino   Inode
	Mode  fs.FileMode
	Nlink uint64
	Size  int64
	Atim  EpochNanos
	Mtim  EpochNanos
	Ctim  EpochNanos
}

// NewStat_t fills a new Stat_t from info, including OS-specific details from
// fs.FileInfo Sys(). When Sys is already a *Stat_t, it is returned as-is.
func NewStat_t(info fs.FileInfo) Stat_t {
	if st, ok := info.Sys().(*Stat_t); ok {
		return *st
	}
	return statFromFileInfo(info)
}

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

func defaultStatFromFileInfo(info fs.FileInfo) Stat_t {
	mtim := EpochNanos(info.ModTime().UnixNano())
	return Stat_t{
		Mode:  info.Mode(),
		Nlink: 1,
		Size:  info.Size(),
		Atim:  mtim,
		Mtim:  mtim,
		Ctim:  mtim,
	}
}
