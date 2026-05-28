package wasi

// File is a file interface backed by host functions needed for ABI
// including WASI.
//
// Implementations should embed UnimplementedFile for forward compatibility. Any
// unsupported method or parameter should return ENOSYS.
//
// # Errors
//
// All methods that can return an error return an Errno, which is zero
// on success.
type File interface {
	// Dev returns the device ID (Stat_t.Dev) of this file.
	Dev() (uint64, Errno)

	// Ino returns the serial number (Stat_t.Ino) of this file.
	Ino() (Inode, Errno)

	// IsDir returns true if this file is a directory.
	IsDir() (bool, Errno)

	// IsAppend returns true if the file was opened with O_APPEND.
	IsAppend() bool

	// SetAppend toggles the append mode (O_APPEND) of this file.
	SetAppend(enable bool) Errno

	// Stat is similar to syscall.Fstat.
	Stat() (Stat_t, Errno)

	// Read attempts to read all bytes in the file into buf.
	Read(buf []byte) (n int, errno Errno)

	// Pread attempts to read all bytes in the file into buf at offset off.
	Pread(buf []byte, off int64) (n int, errno Errno)

	// Seek sets the offset for the next Read or Write.
	Seek(offset int64, whence int) (newOffset int64, errno Errno)

	// Readdir reads directory entries.
	Readdir(n int) (dirents []Dirent, errno Errno)

	// Write attempts to write all bytes in buf to the file.
	Write(buf []byte) (n int, errno Errno)

	// Pwrite attempts to write all bytes in buf at offset off.
	Pwrite(buf []byte, off int64) (n int, errno Errno)

	// Truncate truncates a file to a specified length.
	Truncate(size int64) Errno

	// Sync synchronizes changes to the file.
	Sync() Errno

	// Datasync synchronizes the data of a file.
	Datasync() Errno

	// Utimens sets file access and modification times of this file.
	Utimens(atim, mtim int64) Errno

	// Close closes the underlying file.
	Close() Errno
}

// Pflag are bit flags used for polling.
type Pflag uint32

const (
	// POLLIN is a read event.
	POLLIN Pflag = 1 << iota
	// POLLOUT is a write event.
	POLLOUT
)

// Pollable is implemented by custom readers that support polling for
// readiness.
type Pollable interface {
	Poll(flag Pflag, timeoutMillis int32) (ready bool, errno Errno)
}

// PollableFile is a File that additionally supports polling for readiness
// and non-blocking mode.
type PollableFile interface {
	File
	Pollable

	// IsNonblock returns true if the file was opened with O_NONBLOCK.
	IsNonblock() bool

	// SetNonblock toggles the non-blocking mode (O_NONBLOCK) of this file.
	SetNonblock(enable bool) Errno
}
