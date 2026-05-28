//go:build unix

package sysfs

import (
	"io/fs"
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func inoFromFileInfo(_ string, info fs.FileInfo) (wasi.Inode, wasi.Errno) {
	if s, ok := info.Sys().(*syscall.Stat_t); ok {
		return s.Ino, 0
	}
	return 0, 0
}
