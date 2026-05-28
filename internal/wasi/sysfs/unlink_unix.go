//go:build unix

package sysfs

import (
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func unlink(name string) (errno wasi.Errno) {
	err := syscall.Unlink(name)
	if errno = wasi.UnwrapOSError(err); errno == wasi.EPERM {
		errno = wasi.EISDIR
	}
	return errno
}
