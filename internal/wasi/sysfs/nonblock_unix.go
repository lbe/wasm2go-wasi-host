//go:build unix

package sysfs

import (
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func setNonblock(fd uintptr, enable bool) wasi.Errno {
	return wasi.UnwrapOSError(syscall.SetNonblock(int(fd), enable))
}
