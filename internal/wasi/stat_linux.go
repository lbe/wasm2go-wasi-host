//go:build linux

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

func statFromSyscallStat(d *syscall.Stat_t, info fs.FileInfo) Stat_t {
	return statFromSyscallStatTimestamps(d, info, d.Atim, d.Mtim, d.Ctim)
}
