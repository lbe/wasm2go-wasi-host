package wasihost

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// fsFile is the internal read/stat/close interface satisfied by both
// [FSFileWrap] (for fs.FS-backed files) and [osFile] (for *os.File-backed
// files). It is the element type of fdEntry.file.
type fsFile interface {
	Read([]byte) (int, error)
	Stat() (fs.FileInfo, error)
	Close() error
}

// osFile wraps *os.File to satisfy the fsFile interface for fd entries
// backed by real host files on a writable mount. It also exposes ReadAt,
// WriteAt, Truncate, and Sync through the embedded *os.File, which are
// used by fd_read, fd_write, fd_filestat_set_size, and fd_datasync.
type osFile struct{ *os.File }

// wasiDirInfo is a sentinel fs.FileInfo returned by DirEntriesFile.Stat.
// It satisfies the fs.FileInfo interface with directory-type metadata and
// zero values for fields that are not meaningful for virtual directory listings.
type wasiDirInfo struct{}

func (wasiDirInfo) Name() string { return "." }

func (wasiDirInfo) Size() int64 { return 0 }

func (wasiDirInfo) Mode() fs.FileMode { return fs.ModeDir | 0o555 }

func (wasiDirInfo) ModTime() time.Time { return time.Time{} }

func (wasiDirInfo) IsDir() bool { return true }

func (wasiDirInfo) Sys() any { return nil }

// synthDirEntry is a minimal fs.DirEntry for synthetic . and .. entries.
type synthDirEntry struct{ name string }

func (e synthDirEntry) Name() string { return e.name }

func (e synthDirEntry) IsDir() bool { return true }

func (e synthDirEntry) Type() fs.FileMode { return fs.ModeDir }

func (e synthDirEntry) Info() (fs.FileInfo, error) { return wasiDirInfo{}, nil }

// DirEntriesFile adapts a []fs.DirEntry to the fs.ReadDirFile interface.
// It is used internally by fd_readdir to serve preopen directory listings
// from fs.ReadDirFS mounts. The Entries field is exported so that tests
// can construct values directly without going through path_open.
type DirEntriesFile struct {
	Entries []fs.DirEntry
	idx     int
}

func (d *DirEntriesFile) Read(_ []byte) (int, error) { return 0, io.EOF }

func (d *DirEntriesFile) Close() error { return nil }

func (d *DirEntriesFile) Stat() (fs.FileInfo, error) { return wasiDirInfo{}, nil }

func (d *DirEntriesFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.idx >= len(d.Entries) {
		return nil, io.EOF
	}
	if n <= 0 || d.idx+n > len(d.Entries) {
		n = len(d.Entries) - d.idx
	}
	out := d.Entries[d.idx : d.idx+n]
	d.idx += n
	return out, nil
}

// FSFileWrap adapts an fs.File to the fd-table file interface by adding
// an explicit Seek method. If the underlying fs.File implements io.Seeker,
// Seek delegates to it; otherwise Seek returns an error. Used for
// read-only embedded-FS file entries opened via path_open.
type FSFileWrap struct{ fs.File }

func (f *FSFileWrap) Stat() (fs.FileInfo, error) {
	return f.File.Stat()
}

func (f *FSFileWrap) Seek(offset int64, whence int) (int64, error) {
	if s, ok := f.File.(io.Seeker); ok {
		return s.Seek(offset, whence)
	}
	return 0, fmt.Errorf("seek not supported")
}
