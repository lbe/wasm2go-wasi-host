package wasi

import "io/fs"

// UnimplementedFS is an FS that returns ENOSYS for all methods.
// Embed it in your struct to satisfy the FS interface with safe defaults
// and forward compatibility.
type UnimplementedFS struct{}

func (UnimplementedFS) OpenFile(path string, flag Oflag, perm fs.FileMode) (File, Errno) {
	return nil, ENOSYS
}

func (UnimplementedFS) Lstat(path string) (Stat_t, Errno) {
	return Stat_t{}, ENOSYS
}

func (UnimplementedFS) Stat(path string) (Stat_t, Errno) {
	return Stat_t{}, ENOSYS
}

func (UnimplementedFS) Readlink(path string) (string, Errno) {
	return "", ENOSYS
}

func (UnimplementedFS) Mkdir(path string, perm fs.FileMode) Errno {
	return ENOSYS
}

func (UnimplementedFS) Chmod(path string, perm fs.FileMode) Errno {
	return ENOSYS
}

func (UnimplementedFS) Rename(from, to string) Errno {
	return ENOSYS
}

func (UnimplementedFS) Rmdir(path string) Errno {
	return ENOSYS
}

func (UnimplementedFS) Link(_, _ string) Errno {
	return ENOSYS
}

func (UnimplementedFS) Symlink(_, _ string) Errno {
	return ENOSYS
}

func (UnimplementedFS) Unlink(path string) Errno {
	return ENOSYS
}

func (UnimplementedFS) Utimens(path string, atim, mtim int64) Errno {
	return ENOSYS
}

// UnimplementedFile is a File that returns ENOSYS for all methods,
// except no-ops (Sync, Datasync, Close) which return zero.
// Embed it in your struct to satisfy the File interface with safe defaults
// and forward compatibility.
type UnimplementedFile struct{}

func (UnimplementedFile) Dev() (uint64, Errno) {
	return 0, 0
}

func (UnimplementedFile) Ino() (Inode, Errno) {
	return 0, 0
}

func (UnimplementedFile) IsDir() (bool, Errno) {
	return false, 0
}

func (UnimplementedFile) IsAppend() bool {
	return false
}

func (UnimplementedFile) SetAppend(bool) Errno {
	return ENOSYS
}

func (UnimplementedFile) Stat() (Stat_t, Errno) {
	return Stat_t{}, ENOSYS
}

func (UnimplementedFile) Read([]byte) (int, Errno) {
	return 0, ENOSYS
}

func (UnimplementedFile) Pread([]byte, int64) (int, Errno) {
	return 0, ENOSYS
}

func (UnimplementedFile) Seek(int64, int) (int64, Errno) {
	return 0, ENOSYS
}

func (UnimplementedFile) Readdir(int) (dirents []Dirent, errno Errno) {
	return nil, ENOSYS
}

func (UnimplementedFile) Write([]byte) (int, Errno) {
	return 0, ENOSYS
}

func (UnimplementedFile) Pwrite([]byte, int64) (int, Errno) {
	return 0, ENOSYS
}

func (UnimplementedFile) Truncate(int64) Errno {
	return ENOSYS
}

func (UnimplementedFile) Sync() Errno {
	return 0
}

func (UnimplementedFile) Datasync() Errno {
	return 0
}

func (UnimplementedFile) Utimens(int64, int64) Errno {
	return ENOSYS
}

func (UnimplementedFile) Close() Errno {
	return 0
}
