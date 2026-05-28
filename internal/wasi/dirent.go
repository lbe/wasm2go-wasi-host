package wasi

import (
	"io"
	"io/fs"
	"reflect"
)

// DirentCache is a caching abstraction of File.Readdir that synthesizes
// dot (".") and dotdot (".") entries and supports a sliding-window read
// pattern for incremental directory listing.
type DirentCache struct {
	// f is the underlying file
	f File

	// fs is the underlying filesystem used for reading directory entries.
	// This is extracted from the FileEntry's FS field via reflection when
	// the FS wraps an fs.FS (e.g., osDirFS in tests).
	fs fs.FS

	// readPath is the path to read from when falling back to fs.ReadDir.
	// It is derived from FileEntry.Name and normalized ("" or "/" becomes ".").
	readPath string

	// dotEntries are the "." and ".." entries added when the directory is
	// initialized.
	dotEntries []Dirent

	// dirents are the potentially unread directory entries.
	dirents []Dirent

	// countRead is the total count of dirents read since last rewind.
	countRead uint64

	// eof is true when the underlying file is at EOF.
	eof bool
}

// extractFS attempts to extract an fs.FS from a wasi.FS using reflection.
// This handles the common case where the wasi.FS wraps an fs.FS (e.g.,
// osDirFS in tests, or sysfs.DirFS).
func extractFS(wfs FS) fs.FS {
	v := reflect.ValueOf(wfs)
	// Dereference pointer to get the underlying struct.
	//nolint:govet // reflect.Ptr is clearer than the literal 22
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	field := v.FieldByName("FS")
	if field.IsValid() && field.CanInterface() {
		if fsys, ok := field.Interface().(fs.FS); ok {
			return fsys
		}
	}
	return nil
}

// readFileDir reads up to n directory entries from fsys at path,
// returning them as wasi.Dirents. If n <= 0, all entries are read.
func readFileDir(fsys fs.FS, path string, n int) ([]Dirent, error) {
	entries, err := fs.ReadDir(fsys, path)
	if err != nil {
		return nil, err
	}
	if n > 0 && n < len(entries) {
		entries = entries[:n]
	}
	dirents := make([]Dirent, 0, len(entries))
	for _, e := range entries {
		dirents = append(dirents, Dirent{Name: e.Name(), Type: e.Type()})
	}
	return dirents, nil
}

// Read returns up to n dirents starting at position pos.
// When pos is 0, the directory is rewound. Implements a sliding window
// over directory entries, always returning dot (".") and dotdot ("..")
// entries first.
func (d *DirentCache) Read(pos uint64, n uint32) (dirents []Dirent, errno Errno) {
	switch {
	case pos > d.countRead:
		return nil, ENOENT
	case pos == 0 && d.dirents != nil:
		if _, errno = d.f.Seek(0, io.SeekStart); errno != 0 {
			return
		}
		d.dirents = nil
		d.countRead = 0
	}

	if n == 0 {
		return
	}

	if d.dirents == nil {
		d.dirents = d.dotEntries
		d.countRead = 2
		d.eof = false

		if countToRead := int(n - 2); countToRead > 0 {
			var newDirents []Dirent
			var err error
			// Try File.Readdir first, then fall back to fs.ReadDir.
			newDirents, errno = d.f.Readdir(countToRead)
			if errno == ENOSYS && d.fs != nil {
				// Fall back to reading from the underlying fs.FS.
				// Use d.readPath which is the directory path from FileEntry.Name.
				newDirents, err = readFileDir(d.fs, d.readPath, countToRead)
				if err != nil {
					errno = UnwrapOSError(err)
				} else {
					errno = 0
				}
			}
			if errno != 0 {
				return
			} else if countRead := len(newDirents); countRead > 0 {
				d.eof = countRead < countToRead
				d.dirents = append(d.dirents, newDirents...)
				d.countRead += uint64(countRead)
			}
		}

		return d.cachedDirents(n), 0
	}

	cacheStart := d.countRead - uint64(len(d.dirents))
	if pos < cacheStart {
		errno = ENOENT
		return
	} else if posInCache := pos - cacheStart; posInCache != 0 {
		if uint64(len(d.dirents)) == posInCache {
			d.dirents = d.dirents[:0]
		} else {
			d.dirents = d.dirents[posInCache:]
		}
	}

	if countToRead := int(n) - len(d.dirents); countToRead > 0 && !d.eof {
		var newDirents []Dirent
		var err error
		newDirents, errno = d.f.Readdir(countToRead)
		if errno == ENOSYS && d.fs != nil {
			// Use d.readPath which is the directory path from FileEntry.Name.
			newDirents, err = readFileDir(d.fs, d.readPath, countToRead)
			if err != nil {
				errno = UnwrapOSError(err)
			} else {
				errno = 0
			}
		}
		if errno != 0 {
			return
		} else if countRead := len(newDirents); countRead > 0 {
			d.eof = countRead < countToRead
			d.dirents = append(d.dirents, newDirents...)
			d.countRead += uint64(countRead)
		}
	}

	return d.cachedDirents(n), 0
}

// cachedDirents returns up to n dirents from the cache.
func (d *DirentCache) cachedDirents(n uint32) []Dirent {
	direntCount := uint32(len(d.dirents))
	switch {
	case direntCount == 0:
		return nil
	case direntCount > n:
		return d.dirents[:n]
	}
	return d.dirents
}
