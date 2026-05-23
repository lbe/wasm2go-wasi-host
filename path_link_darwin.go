//go:build darwin

package wasihost

import "golang.org/x/sys/unix"

// linkSymlinkInode creates a hard link to the symlink inode at oldPath. On Darwin,
// linkat with no flags links the symlink itself when oldPath names a symbolic link.
func linkSymlinkInode(oldPath, newPath string) error {
	return unix.Linkat(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, 0)
}
