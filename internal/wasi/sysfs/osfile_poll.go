package sysfs

import "github.com/lbe/wasm2go-wasi-host/internal/wasi"

func (f *osFile) IsNonblock() bool {
	return f.flag&wasi.O_NONBLOCK == wasi.O_NONBLOCK
}

func (f *osFile) SetNonblock(enable bool) (errno wasi.Errno) {
	if enable {
		f.flag |= wasi.O_NONBLOCK
	} else {
		f.flag &= ^wasi.O_NONBLOCK
	}
	if errno = setNonblock(f.fd, enable); errno != 0 {
		return fileError(f, f.closed, errno)
	}
	return 0
}

func (f *osFile) Poll(flag wasi.Pflag, timeoutMillis int32) (ready bool, errno wasi.Errno) {
	if f.closed {
		return false, wasi.EBADF
	}
	return poll(f.fd, flag, timeoutMillis)
}

// preadNonblock reads at off via preadFd; falls back to readFd when pread is unsupported (e.g. pipes on Darwin).
func (f *osFile) preadNonblock(buf []byte, off int64) (n int, errno wasi.Errno) {
	n, errno = preadFd(f.fd, buf, off)
	if errno != 0 {
		n, errno = readFd(f.fd, buf)
	}
	return
}
