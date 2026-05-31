//go:build !linux && unix

package wasi

import (
	"io/fs"
	"syscall"
)

func statFromSyscallStat(d *syscall.Stat_t, info fs.FileInfo) Stat_t {
	return statFromSyscallStatTimestamps(d, info, d.Atimespec, d.Mtimespec, d.Ctimespec)
}
