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

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// OpenOSFile opens a file by path and wraps it as a wasi.File.
func OpenOSFile(path string, flag wasi.Oflag, perm fs.FileMode) (wasi.File, wasi.Errno) {
	f, errno := openFile(path, flag, perm)
	if errno != 0 {
		return nil, errno
	}
	return newOsFile(path, flag, perm, f), 0
}

// OpenFSFile opens a file from an fs.FS and wraps it as a wasi.File.
func OpenFSFile(fsys fs.FS, path string, flag wasi.Oflag, perm fs.FileMode) (wasi.File, wasi.Errno) {
	if flag&wasi.O_DIRECTORY != 0 && flag&(wasi.O_WRONLY|wasi.O_RDWR) != 0 {
		return nil, wasi.EISDIR
	}
	f, err := fsys.Open(path)
	if errno := wasi.UnwrapOSError(err); errno != 0 {
		return nil, errno
	}
	return &fsFile{fs: fsys, name: path, file: f}, 0
}

// cachedStat holds lazily-computed stat metadata shared between fsFile and osFile.
type cachedStat struct {
	dev   uint64
	ino   wasi.Inode
	isDir bool
}

// fsFile wraps an fs.File as a wasi.File.
type fsFile struct {
	wasi.UnimplementedFile

	fs   fs.FS
	name string
	file fs.File

	reopenDir bool
	closed    bool
	cachedSt  *cachedStat
}

func (f *fsFile) cachedStat() (dev uint64, ino wasi.Inode, isDir bool, errno wasi.Errno) {
	if f.cachedSt == nil {
		if _, errno = f.Stat(); errno != 0 {
			return
		}
	}
	return f.cachedSt.dev, f.cachedSt.ino, f.cachedSt.isDir, 0
}

func (f *fsFile) Dev() (uint64, wasi.Errno) {
	dev, _, _, errno := f.cachedStat()
	return dev, errno
}

func (f *fsFile) Ino() (wasi.Inode, wasi.Errno) {
	_, ino, _, errno := f.cachedStat()
	return ino, errno
}

func (f *fsFile) IsDir() (bool, wasi.Errno) {
	_, _, isDir, errno := f.cachedStat()
	return isDir, errno
}

func (f *fsFile) Stat() (wasi.Stat_t, wasi.Errno) {
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

func (f *fsFile) Read(buf []byte) (n int, errno wasi.Errno) {
	if n, errno = read(f.file, buf); errno != 0 {
		errno = fileError(f, f.closed, errno)
	}
	return
}

func (f *fsFile) Pread(buf []byte, off int64) (n int, errno wasi.Errno) {
	if ra, ok := f.file.(io.ReaderAt); ok {
		if n, errno = pread(ra, buf, off); errno != 0 {
			errno = fileError(f, f.closed, errno)
		}
		return
	}
	if rs, ok := f.file.(io.ReadSeeker); ok {
		currentOffset, err := rs.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, fileError(f, f.closed, wasi.UnwrapOSError(err))
		}
		defer func() { _, _ = rs.Seek(currentOffset, io.SeekStart) }()
		if off != currentOffset {
			if _, err = rs.Seek(off, io.SeekStart); err != nil {
				return 0, fileError(f, f.closed, wasi.UnwrapOSError(err))
			}
		}
		n, err = rs.Read(buf)
		if errno = wasi.UnwrapOSError(err); errno != 0 {
			errno = fileError(f, f.closed, errno)
		}
	} else {
		errno = wasi.ENOSYS
	}
	return
}

func (f *fsFile) Seek(offset int64, whence int) (newOffset int64, errno wasi.Errno) {
	if offset == 0 && whence == io.SeekStart {
		if dir, e := f.IsDir(); e == 0 && dir {
			f.reopenDir = true
			return 0, 0
		}
	}
	if s, ok := f.file.(io.Seeker); ok {
		if newOffset, errno = seek(s, offset, whence); errno != 0 {
			errno = fileError(f, f.closed, errno)
		}
	} else {
		errno = wasi.ENOSYS
	}
	return
}

func (f *fsFile) Readdir(n int) (dirents []wasi.Dirent, errno wasi.Errno) {
	if f.closed {
		errno = wasi.EBADF
		return
	}
	if f.reopenDir {
		f.reopenDir = false
		if errno = adjustReaddirErr(f, f.closed, f.rewindDir()); errno != 0 {
			return
		}
	}
	if of, ok := f.file.(readdirFile); ok {
		if dirents, errno = readdir(of, "", n); errno != 0 {
			errno = adjustReaddirErr(f, f.closed, errno)
		}
		return
	}
	if rdf, ok := f.file.(fs.ReadDirFile); ok {
		entries, e := rdf.ReadDir(n)
		if errno = adjustReaddirErr(f, f.closed, e); errno != 0 {
			return
		}
		dirents = make([]wasi.Dirent, 0, len(entries))
		for _, e := range entries {
			dirents = append(dirents, wasi.Dirent{Name: e.Name(), Type: e.Type()})
		}
	} else {
		errno = wasi.EBADF
	}
	return
}

func (f *fsFile) Write(buf []byte) (n int, errno wasi.Errno) {
	if w, ok := f.file.(io.Writer); ok {
		if n, errno = write(w, buf); errno != 0 {
			errno = fileError(f, f.closed, errno)
		}
	} else {
		errno = wasi.ENOSYS
	}
	return
}

func (f *fsFile) Pwrite(buf []byte, off int64) (n int, errno wasi.Errno) {
	if wa, ok := f.file.(io.WriterAt); ok {
		if n, errno = pwrite(wa, buf, off); errno != 0 {
			errno = fileError(f, f.closed, errno)
		}
	} else {
		errno = wasi.ENOSYS
	}
	return
}

func (f *fsFile) Close() wasi.Errno {
	if f.closed {
		return 0
	}
	f.closed = true
	return f.close()
}

func (f *fsFile) close() wasi.Errno {
	return wasi.UnwrapOSError(f.file.Close())
}

func (f *fsFile) rewindDir() wasi.Errno {
	file, err := f.fs.Open(f.name)
	if err != nil {
		return wasi.UnwrapOSError(err)
	}
	fi, err := file.Stat()
	if err != nil {
		return wasi.UnwrapOSError(err)
	}
	if !fi.IsDir() {
		return wasi.ENOTDIR
	}
	_ = f.file.Close()
	f.file = file
	return 0
}

// readdirFile allows masking the Readdir function on os.File.
type readdirFile interface {
	Readdir(n int) ([]fs.FileInfo, error)
}

func readdir(f readdirFile, path string, n int) (dirents []wasi.Dirent, errno wasi.Errno) {
	fis, e := f.Readdir(n)
	if errno = wasi.UnwrapOSError(e); errno != 0 {
		return
	}
	dirents = make([]wasi.Dirent, 0, len(fis))
	var ino wasi.Inode
	for fi := range fis {
		t := fis[fi]
		if ino, errno = inoFromFileInfo(path, t); errno != 0 {
			return
		}
		dirents = append(dirents, wasi.Dirent{Name: t.Name(), Ino: ino, Type: t.Mode().Type()})
	}
	return
}

func read(r io.Reader, buf []byte) (n int, errno wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := r.Read(buf)
	return n, wasi.UnwrapOSError(err)
}

func pread(ra io.ReaderAt, buf []byte, off int64) (n int, errno wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := ra.ReadAt(buf, off)
	return n, wasi.UnwrapOSError(err)
}

func seek(s io.Seeker, offset int64, whence int) (int64, wasi.Errno) {
	if uint(whence) > io.SeekEnd {
		return 0, wasi.EINVAL
	}
	newOffset, err := s.Seek(offset, whence)
	return newOffset, wasi.UnwrapOSError(err)
}

func write(w io.Writer, buf []byte) (n int, errno wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := w.Write(buf)
	return n, wasi.UnwrapOSError(err)
}

func pwrite(w io.WriterAt, buf []byte, off int64) (n int, errno wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	n, err := w.WriteAt(buf, off)
	return n, wasi.UnwrapOSError(err)
}

func fileError(f wasi.File, isClosed bool, errno wasi.Errno) wasi.Errno {
	if vErrno := validate(f, isClosed, true, false); vErrno != 0 {
		return vErrno
	}
	return errno
}

func dirError(f wasi.File, isClosed bool, errno wasi.Errno) wasi.Errno {
	if vErrno := validate(f, isClosed, false, true); vErrno != 0 {
		return vErrno
	}
	return errno
}

func validate(f wasi.File, isClosed, wantFile, wantDir bool) wasi.Errno {
	if isClosed {
		return wasi.EBADF
	}
	isDir, errno := f.IsDir()
	if errno != 0 {
		return errno
	}
	if wantFile && isDir {
		return wasi.EISDIR
	} else if wantDir && !isDir {
		return wasi.ENOTDIR
	}
	return 0
}

func adjustReaddirErr(f wasi.File, isClosed bool, err error) wasi.Errno {
	if err == io.EOF {
		return 0
	} else if errno := wasi.UnwrapOSError(err); errno != 0 {
		errno = dirError(f, isClosed, errno)
		switch errno {
		case wasi.EINVAL:
			return wasi.EBADF
		case wasi.ENOTDIR:
			return wasi.EBADF
		}
		return errno
	}
	return 0
}
