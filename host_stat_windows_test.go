//go:build windows

package wasihost

import "io/fs"

// hostDevIno is the host device and inode number of a file.
type hostDevIno struct {
	Dev uint64
	Ino uint64
}

// hostStatOf returns zeros on Windows: os.Stat does not expose a volume serial
// or file index, and tests that need them skip when the inode is zero.
func hostStatOf(fs.FileInfo) hostDevIno {
	return hostDevIno{}
}
