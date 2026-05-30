package wasihost

import (
	"fmt"
	"io"
	"io/fs"
	"time"
)

// Test-only adapters for readdir and read-only embedded-FS fd entries.

// wasiDirInfo is sentinel directory metadata for virtual directory listings.
type wasiDirInfo struct{}

func (wasiDirInfo) Name() string       { return "." }
func (wasiDirInfo) Size() int64        { return 0 }
func (wasiDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (wasiDirInfo) ModTime() time.Time { return time.Time{} }
func (wasiDirInfo) IsDir() bool        { return true }
func (wasiDirInfo) Sys() any           { return nil }

// DirEntriesFile adapts []fs.DirEntry to fs.ReadDirFile for preopen directory
// listings from fs.ReadDirFS mounts. Entries is exported so tests can construct
// values without path_open.
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

// FSFileWrap adapts fs.File for read-only embedded-FS fd entries. Seek
// delegates to io.Seeker when present; otherwise returns an error.
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
