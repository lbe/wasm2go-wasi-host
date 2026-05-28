//go:build !(linux || darwin || windows)

package sysfs

import "github.com/lbe/wasm2go-wasi-host/internal/wasi"

func poll(uintptr, wasi.Pflag, int32) (bool, wasi.Errno) {
	return false, wasi.ENOSYS
}
