//go:build unix

package wasihost

import (
	"io/fs"
	"syscall"
)

// hostDevIno is the host device and inode number of a file.
type hostDevIno struct {
	Dev uint64
	Ino uint64
}

// hostStatOf returns the host device and inode numbers recorded in fi.
func hostStatOf(fi fs.FileInfo) hostDevIno {
	st := fi.Sys().(*syscall.Stat_t)
	return hostDevIno{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}
}
