//go:build unix

package sysfs

import (
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func rename(from, to string) wasi.Errno {
	if from == to {
		return 0
	}
	return wasi.UnwrapOSError(syscall.Rename(from, to))
}
