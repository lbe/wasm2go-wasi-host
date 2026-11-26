//go:build windows

package sysfs

import (
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func datasync(f *os.File) wasi.Errno {
	return fsync(f)
}
