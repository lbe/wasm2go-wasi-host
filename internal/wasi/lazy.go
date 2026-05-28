package wasi

// lazyDir wraps a wasi.FS and lazily opens the directory "." on first use.
// It embeds DirFile for default directory method implementations.

// compile-time check to ensure lazyDir implements File.

var _ File = (*lazyDir)(nil)

type lazyDir struct {
	DirFile

	fs FS
	// f is the lazily opened directory file; nil until file() is called.
	f File
}

func (d *lazyDir) Dev() (uint64, Errno) {
	if f, ok := d.file(); !ok {
		return 0, EBADF
	} else {
		return f.Dev()
	}
}

func (d *lazyDir) Ino() (Inode, Errno) {
	if f, ok := d.file(); !ok {
		return 0, EBADF
	} else {
		return f.Ino()
	}
}

func (d *lazyDir) Seek(offset int64, whence int) (int64, Errno) {
	if f, ok := d.file(); !ok {
		return 0, EBADF
	} else {
		return f.Seek(offset, whence)
	}
}

func (d *lazyDir) Stat() (Stat_t, Errno) {
	if f, ok := d.file(); !ok {
		return Stat_t{}, EBADF
	} else {
		return f.Stat()
	}
}

func (d *lazyDir) Readdir(n int) ([]Dirent, Errno) {
	if f, ok := d.file(); !ok {
		return nil, EBADF
	} else {
		return f.Readdir(n)
	}
}

func (d *lazyDir) Sync() Errno {
	if f, ok := d.file(); !ok {
		return EBADF
	} else {
		return f.Sync()
	}
}

func (d *lazyDir) Datasync() Errno {
	if f, ok := d.file(); !ok {
		return EBADF
	} else {
		return f.Datasync()
	}
}

func (d *lazyDir) Utimens(atim, mtim int64) Errno {
	if f, ok := d.file(); !ok {
		return EBADF
	} else {
		return f.Utimens(atim, mtim)
	}
}

func (d *lazyDir) file() (File, bool) {
	if f := d.f; d.f != nil {
		return f, true
	}
	var errno Errno
	d.f, errno = d.fs.OpenFile(".", O_RDONLY, 0)
	switch errno {
	case 0:
		return d.f, true
	case ENOENT:
		return nil, false
	default:
		// Return nil, false so callers can return EBADF instead of panicking.
		return nil, false
	}
}

func (d *lazyDir) Close() Errno {
	f := d.f
	if f == nil {
		return 0
	}
	return f.Close()
}
