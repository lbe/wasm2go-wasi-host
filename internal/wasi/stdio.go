package wasi

import (
	"io"
	"io/fs"
)

// StdinFile wraps an io.Reader as a wasi.File for FdStdin.
// When the reader also implements Pollable, Poll is forwarded to it.
type StdinFile struct {
	noopStdinFile
	io.Reader
}

// Read reads from the underlying reader, converting io.EOF to Errno 0.
func (f *StdinFile) Read(buf []byte) (int, Errno) {
	n, err := f.Reader.Read(buf)
	return n, UnwrapOSError(err)
}

// Poll checks if the underlying reader is ready for reading.
// If the reader implements Pollable, readiness is delegated to it;
// otherwise POLLIN is always ready and other flags return ENOTSUP.
func (f *StdinFile) Poll(flag Pflag, timeoutMillis int32) (ready bool, errno Errno) {
	if p, ok := f.Reader.(Pollable); ok {
		return p.Poll(flag, timeoutMillis)
	}
	if flag != POLLIN {
		return false, ENOTSUP
	}
	return true, 0
}

// writerFile wraps an io.Writer as a wasi.File for FdStdout/FdStderr.
type writerFile struct {
	noopStdoutFile
	w io.Writer
}

// Write writes to the underlying writer.
func (f *writerFile) Write(buf []byte) (int, Errno) {
	n, err := f.w.Write(buf)
	return n, UnwrapOSError(err)
}

// noopStdinFile is the default stdin file; Read returns 0 bytes and Poll
// always reports ready for POLLIN.
type noopStdinFile struct {
	noopStdioFile
}

// Read is a no-op; returns 0 bytes read.
func (noopStdinFile) Read([]byte) (int, Errno) {
	return 0, 0
}

// Poll implements the same method as documented on Pollable
func (noopStdinFile) Poll(flag Pflag, timeoutMillis int32) (ready bool, errno Errno) {
	if flag != POLLIN {
		return false, ENOTSUP
	}
	return true, 0
}

// noopStdoutFile is the default stdout/stderr file; Write silently
// discards all data and reports success.
type noopStdoutFile struct {
	noopStdioFile
}

// Write silently discards all data and reports zero bytes written.
func (noopStdoutFile) Write([]byte) (int, Errno) {
	return 0, 0
}

// noopStdioFile provides default implementations for stdio files.
// Stat returns modeDevice, IsDir returns false, Close returns zero.
// Other methods are inherited from UnimplementedFile (returning ENOSYS).
type noopStdioFile struct {
	UnimplementedFile
}

const modeDevice = fs.ModeDevice | 0o640

// Stat returns a Stat_t with device mode and Nlink=1.
func (noopStdioFile) Stat() (Stat_t, Errno) {
	return Stat_t{Mode: modeDevice, Nlink: 1}, 0
}

// IsDir returns false; stdio streams are character devices, not directories.
func (noopStdioFile) IsDir() (bool, Errno) {
	return false, 0
}

// Truncate is a no-op for stdio streams; returns success silently.
func (noopStdioFile) Truncate(int64) Errno { return 0 }
