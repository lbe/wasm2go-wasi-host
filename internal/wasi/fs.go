package wasi

import "io/fs"

// FS is a filesystem interface backed by host functions needed for ABI
// including WASI.
//
// Implementations should embed UnimplementedFS for forward compatibility. Any
// unsupported method or parameter should return ENOSYS.
//
// # Errors
//
// All methods that can return an error return an Errno, which is zero
// on success.
type FS interface {
	// PathFor returns the path associated with this FS, used as a resolution
	// boundary for path operations.
	PathFor() string

	// OpenFile opens a file. It should be closed via Close on File.
	OpenFile(path string, flag Oflag, perm fs.FileMode) (File, Errno)

	// Lstat gets file status without following symbolic links.
	Lstat(path string) (Stat_t, Errno)

	// Stat gets file status.
	Stat(path string) (Stat_t, Errno)

	// Mkdir makes a directory.
	Mkdir(path string, perm fs.FileMode) Errno

	// Chmod changes the mode of the file.
	Chmod(path string, perm fs.FileMode) Errno

	// Rename renames file or directory.
	Rename(from, to string) Errno

	// Rmdir removes a directory.
	Rmdir(path string) Errno

	// Unlink removes a directory entry.
	Unlink(path string) Errno

	// Link creates a "hard" link from oldPath to newPath.
	Link(oldPath, newPath string) Errno

	// Symlink creates a "soft" link from oldPath to linkName.
	Symlink(oldPath, linkName string) Errno

	// Readlink reads the contents of a symbolic link.
	Readlink(path string) (string, Errno)

	// Utimens sets file access and modification times on a path, at
	// nanosecond precision.
	Utimens(path string, atim, mtim int64) Errno
}
