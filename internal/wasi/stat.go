// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import "io/fs"

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
