//go:build linux

package wasi

import (
	"io/fs"
	"syscall"
)

func statFromSyscallStat(d *syscall.Stat_t, info fs.FileInfo) Stat_t {
	return statFromSyscallStatTimestamps(d, info, d.Atim, d.Mtim, d.Ctim)
}
