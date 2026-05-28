//go:build !windows

package sysfs

import (
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func fsync(f *os.File) wasi.Errno {
	return wasi.UnwrapOSError(f.Sync())
}
