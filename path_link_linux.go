//go:build linux

package wasihost

import "golang.org/x/sys/unix"

// linkSymlinkInode creates a hard link to the symlink inode at oldPath. On Linux,
// linkat with no flags does not dereference a symlink oldpath (see linkat(2)).
func linkSymlinkInode(oldPath, newPath string) error {
	return unix.Linkat(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, 0)
}
