// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"io"
	"io/fs"
	"os"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

const (
	nonBlockingFileReadSupported  = true
	nonBlockingFileWriteSupported = true
)

func newOsFile(path string, flag wasi.Oflag, perm fs.FileMode, f *os.File) wasi.File {
	return &osFile{path: path, flag: flag, perm: perm, reopenDir: true, file: f, fd: f.Fd()}
}

// osFile adapts an *os.File to wasi.File with cached Dev/Ino/IsDir.
type osFile struct {
	path string
	flag wasi.Oflag
	perm fs.FileMode
	file *os.File
	fd   uintptr

	reopenDir bool
	closed    bool
	cachedSt  *cachedStat
}

func (f *osFile) cachedStat() (dev uint64, ino wasi.Inode, isDir bool, errno wasi.Errno) {
	if f.cachedSt == nil {
		if _, errno = f.Stat(); errno != 0 {
			return
		}
	}
	return f.cachedSt.dev, f.cachedSt.ino, f.cachedSt.isDir, 0
}

func (f *osFile) Dev() (uint64, wasi.Errno) {
	dev, _, _, errno := f.cachedStat()
	return dev, errno
}

func (f *osFile) Ino() (wasi.Inode, wasi.Errno) {
	_, ino, _, errno := f.cachedStat()
	return ino, errno
}

func (f *osFile) IsDir() (bool, wasi.Errno) {
	_, _, isDir, errno := f.cachedStat()
	return isDir, errno
}

func (f *osFile) IsAppend() bool {
	return f.flag&wasi.O_APPEND == wasi.O_APPEND
}

func (f *osFile) SetAppend(enable bool) (errno wasi.Errno) {
	if enable {
		f.flag |= wasi.O_APPEND
	} else {
		f.flag &= ^wasi.O_APPEND
	}
	return fileError(f, f.closed, f.reopen())
}

func (f *osFile) reopen() (errno wasi.Errno) {
	var (
		isDir  bool
		offset int64
		err    error
	)

	isDir, errno = f.IsDir()
	if errno != 0 {
		return errno
	}

	if !isDir {
		offset, err = f.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return wasi.UnwrapOSError(err)
		}
	}

	flag := f.flag &^ (wasi.O_CREAT | wasi.O_TRUNC)
	file, errno := openFile(f.path, flag, f.perm)
	if errno != 0 {
		return errno
	}
	errno = f.checkSameFile(file)
	if errno != 0 {
		return errno
	}

	if !isDir {
		_, err = file.Seek(offset, io.SeekStart)
		if err != nil {
			_ = file.Close()
			return wasi.UnwrapOSError(err)
		}
	}

	_ = f.file.Close()
	f.file = file
	f.fd = file.Fd()
	return 0
}

func (f *osFile) checkSameFile(osf *os.File) wasi.Errno {
	fi1, err := f.file.Stat()
	if err != nil {
		return wasi.UnwrapOSError(err)
	}
	fi2, err := osf.Stat()
	if err != nil {
		return wasi.UnwrapOSError(err)
	}
	if os.SameFile(fi1, fi2) {
		return 0
	}
	return wasi.ENOENT
}

func (f *osFile) Stat() (wasi.Stat_t, wasi.Errno) {
	if f.closed {
		return wasi.Stat_t{}, wasi.EBADF
	}
	st, errno := statFile(f.file)
	switch errno {
	case 0:
		f.cachedSt = &cachedStat{dev: st.Dev, ino: st.Ino, isDir: st.Mode&fs.ModeDir == fs.ModeDir}
	case wasi.EIO:
		errno = wasi.EBADF
	}
	return st, errno
}

func (f *osFile) Read(buf []byte) (n int, errno wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	if nonBlockingFileReadSupported && f.IsNonblock() {
		n, errno = readFd(f.fd, buf)
	} else {
		n, errno = read(f.file, buf)
	}
	if errno != 0 {
		errno = fileError(f, f.closed, errno)
	}
	return
}

func (f *osFile) Pread(buf []byte, off int64) (n int, errno wasi.Errno) {
	if nonBlockingFileReadSupported && f.IsNonblock() {
		n, errno = f.preadNonblock(buf, off)
	} else {
		n, errno = pread(f.file, buf, off)
	}
	if errno != 0 {
		errno = fileError(f, f.closed, errno)
	}
	return
}

func (f *osFile) Seek(offset int64, whence int) (newOffset int64, errno wasi.Errno) {
	if newOffset, errno = seek(f.file, offset, whence); errno != 0 {
		errno = fileError(f, f.closed, errno)
		if errno == wasi.EISDIR && offset == 0 && whence == io.SeekStart {
			errno = 0
			f.reopenDir = true
		}
	}
	return
}

func (f *osFile) Readdir(n int) (dirents []wasi.Dirent, errno wasi.Errno) {
	if f.reopenDir {
		f.reopenDir = false
		if errno = adjustReaddirErr(f, f.closed, f.reopen()); errno != 0 {
			return
		}
	}
	if dirents, errno = readdir(f.file, f.path, n); errno != 0 {
		errno = adjustReaddirErr(f, f.closed, errno)
	}
	return
}

func (f *osFile) Write(buf []byte) (n int, errno wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	if nonBlockingFileWriteSupported && f.IsNonblock() {
		n, errno = writeFd(f.fd, buf)
	} else if n, errno = write(f.file, buf); errno != 0 {
		errno = fileError(f, f.closed, errno)
	}
	return
}

func (f *osFile) Pwrite(buf []byte, off int64) (n int, errno wasi.Errno) {
	if n, errno = pwriteFd(f.fd, buf, off); errno != 0 {
		errno = fileError(f, f.closed, errno)
	}
	return
}

func (f *osFile) Truncate(size int64) (errno wasi.Errno) {
	if size < 0 {
		return wasi.EINVAL
	}
	if errno = wasi.UnwrapOSError(f.file.Truncate(size)); errno != 0 {
		errno = fileError(f, f.closed, errno)
	}
	return
}

func (f *osFile) Sync() wasi.Errno {
	return fsync(f.file)
}

func (f *osFile) Datasync() wasi.Errno {
	return datasync(f.file)
}

func (f *osFile) Utimens(atim, mtim int64) wasi.Errno {
	if f.closed {
		return wasi.EBADF
	}
	// Path-based utimens preserves nanosecond timestamp precision.
	if f.path != "" {
		return utimens(f.path, atim, mtim)
	}
	err := futimens(f.fd, atim, mtim)
	return wasi.UnwrapOSError(err)
}

func (f *osFile) Close() wasi.Errno {
	if f.closed {
		return 0
	}
	f.closed = true
	return f.close()
}

func (f *osFile) close() wasi.Errno {
	return wasi.UnwrapOSError(f.file.Close())
}

// UnderlyingOSFile returns the wrapped *os.File when f is an os-backed sysfs file.
func UnderlyingOSFile(f wasi.File) (*os.File, bool) {
	o, ok := f.(*osFile)
	if !ok || o == nil {
		return nil, false
	}
	return o.file, true
}
