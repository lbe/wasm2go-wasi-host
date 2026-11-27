//go:build unix

package sysfs

import (
	"time"

	"golang.org/x/sys/unix"
)

// LinkNoFollow creates newPath as a hard link to oldPath without following a
// symlink at oldPath, so the link refers to the symlink itself. linkat with no
// flags does not dereference a symlink oldpath on Linux, macOS or FreeBSD.
func LinkNoFollow(oldPath, newPath string) error {
	return unix.Linkat(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, 0)
}

// UtimesNoFollow sets the access and modification times of path without
// following a symlink at path.
func UtimesNoFollow(path string, atim, mtim time.Time) error {
	ts := []unix.Timespec{
		unix.NsecToTimespec(atim.UnixNano()),
		unix.NsecToTimespec(mtim.UnixNano()),
	}
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW)
}
