//go:build darwin

package sysfs

import (
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func withSyscallOflag(oflag wasi.Oflag, flag int) int {
	if oflag&wasi.O_DIRECTORY != 0 {
		flag |= syscall.O_DIRECTORY
	}
	if oflag&wasi.O_DSYNC != 0 {
		flag |= syscall.O_DSYNC
	}
	if oflag&wasi.O_NOFOLLOW != 0 {
		flag |= syscall.O_NOFOLLOW
	}
	if oflag&wasi.O_NONBLOCK != 0 {
		flag |= syscall.O_NONBLOCK
	}
	return flag
}
