package sysfs

import (
	"io/fs"
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func lstat(path string) (wasi.Stat_t, wasi.Errno) {
	info, err := os.Lstat(path)
	if err != nil {
		return wasi.Stat_t{}, wasi.UnwrapOSError(err)
	}
	return wasi.NewStat_t(info), 0
}

func stat(path string) (wasi.Stat_t, wasi.Errno) {
	info, err := os.Stat(path)
	if err != nil {
		return wasi.Stat_t{}, wasi.UnwrapOSError(err)
	}
	return wasi.NewStat_t(info), 0
}

func statFile(f fs.File) (wasi.Stat_t, wasi.Errno) {
	info, err := f.Stat()
	if err != nil {
		return wasi.Stat_t{}, wasi.UnwrapOSError(err)
	}
	return wasi.NewStat_t(info), 0
}
