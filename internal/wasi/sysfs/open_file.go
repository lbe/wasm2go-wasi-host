//go:build !windows

package sysfs

import (
	"io/fs"
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func openFile(path string, oflag wasi.Oflag, perm fs.FileMode) (*os.File, wasi.Errno) {
	f, err := os.OpenFile(path, toOsOpenFlag(oflag), perm)
	return f, wasi.UnwrapOSError(err)
}
