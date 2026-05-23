//go:build linux

package wasihost

import "golang.org/x/sys/unix"

// linkSymlinkInode creates a hard link to the symlink inode at oldPath (without
// following it), using Linux O_PATH + linkat(AT_EMPTY_PATH).
func linkSymlinkInode(oldPath, newPath string) error {
	fd, err := unix.Openat(unix.AT_FDCWD, oldPath, unix.O_PATH, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Linkat(fd, "", unix.AT_FDCWD, newPath, unix.AT_EMPTY_PATH)
}
