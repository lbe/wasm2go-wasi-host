//go:build darwin

package wasihost

import (
	"io/fs"
	"syscall"
	"time"
)

func getAtimeFromStat(fi fs.FileInfo) time.Time {
	stat := fi.Sys().(*syscall.Stat_t)
	return time.Unix(stat.Atimespec.Sec, stat.Atimespec.Nsec)
}
