package wasi

import (
	"io"
	"io/fs"
)

const (
	FdStdin int32 = iota
	FdStdout
	FdStderr
)

// FileEntry represents an open file in a file system, keyed by its
// file descriptor in a FileTable.
type FileEntry struct {
	// Name is the path of the file relative to its filesystem root.
	Name string

	// IsPreopen indicates this entry was opened during initialization
	// (e.g. stdio streams or pre-opened directories).
	IsPreopen bool

	// FS is the filesystem this entry belongs to.
	FS FS

	// File is the underlying open file handle.
	File File

	// Offset is the current file offset for read/write operations.
	Offset int64

	// RightsBase is the set of rights available on this file descriptor.
	RightsBase uint64

	// RightsInheriting is the set of rights inherited by child file descriptors.
	RightsInheriting uint64

	// GuestPath is the guest-absolute path for path_open resolution on non-preopen fds.
	GuestPath string

	// FdFlags holds WASI fd_flags set at path_open (append, sync, etc.).
	FdFlags uint16

	// Overlay is an optional embedded fs.FS fallback for writable host preopens.
	Overlay fs.FS

	direntCache *DirentCache
}

// DirentCache gets or creates a DirentCache for this file entry.
// It returns ENOTDIR if the underlying file is not a directory.
// The cache is synthesized once and reused on subsequent calls.
func (f *FileEntry) DirentCache() (*DirentCache, Errno) {
	if f.direntCache != nil {
		return f.direntCache, 0
	}
	isDir, errno := f.File.IsDir()
	if errno != 0 {
		return nil, errno
	}
	if !isDir {
		return nil, ENOTDIR
	}
	// Generate the dotEntries only once.
	dotIno, errno := f.File.Ino()
	if errno != 0 {
		return nil, errno
	}
	dotEntries := [2]Dirent{}
	dotEntries[0] = Dirent{Name: ".", Ino: dotIno, Type: fs.ModeDir}
	dotEntries[1] = Dirent{Name: "..", Ino: 0, Type: fs.ModeDir}
	// Normalize readPath: preopens and "" or "/" read from "." via fs.ReadDir fallback.
	readPath := f.Name
	if f.IsPreopen || readPath == "" || readPath == "/" {
		readPath = "."
	}
	f.direntCache = &DirentCache{f: f.File, fs: extractFS(f.FS), readPath: readPath, dotEntries: dotEntries[:]}
	return f.direntCache, 0
}

// FileTable is a dense, growable mapping from file descriptors (int32)
// to their corresponding FileEntry pointers.
type FileTable = Table[int32, *FileEntry]

// FSContext manages a table of open file descriptors, providing
// open, close, renumber, and lookup operations.
type FSContext struct {
	openedFiles FileTable
	invalidated map[int32]bool
}

// InitFSContext creates an FSContext with:
//   - stdin at fd 0 (custom reader or no-op)
//   - stdout at fd 1 (custom writer or no-op)
//   - stderr at fd 2 (custom writer or no-op)
//   - one pre-opened filesystem at fd 3 per entry in fsList
func InitFSContext(
	stdin io.Reader,
	stdout, stderr io.Writer,
	fsList []FS, guestPaths []string,
) (*FSContext, error) {
	c := &FSContext{invalidated: make(map[int32]bool)}

	// Insert stdio entries.
	inFile := &FileEntry{
		Name: "stdin", IsPreopen: true, File: &noopStdinFile{},
		RightsBase: rightsCharDev, RightsInheriting: 0,
	}
	if stdin != nil {
		inFile.File = &StdinFile{Reader: stdin}
	}
	c.openedFiles.InsertAt(inFile, FdStdin)

	outFile := &FileEntry{
		Name: "stdout", IsPreopen: true, File: &noopStdoutFile{},
		RightsBase: rightsCharDev, RightsInheriting: 0,
	}
	if stdout != nil {
		outFile.File = &writerFile{w: stdout}
	}
	c.openedFiles.InsertAt(outFile, FdStdout)

	errFile := &FileEntry{
		Name: "stderr", IsPreopen: true, File: &noopStdoutFile{},
		RightsBase: rightsCharDev, RightsInheriting: 0,
	}
	if stderr != nil {
		errFile.File = &writerFile{w: stderr}
	}
	c.openedFiles.InsertAt(errFile, FdStderr)

	for i, f := range fsList {
		guestPath := guestPaths[i]
		if StripPrefixesAndTrailingSlash(guestPath) == "" {
			guestPath = "/"
		}
		c.openedFiles.Insert(&FileEntry{
			FS:        f,
			Name:      guestPath,
			IsPreopen: true,
			File:      &lazyDir{fs: f},
		})
	}
	return c, nil
}

// LookupFile returns the FileEntry for the given file descriptor and true,
// or nil and false if the descriptor is not open.
func (c *FSContext) LookupFile(fd int32) (*FileEntry, bool) {
	if c.invalidated[fd] {
		return nil, false
	}
	return c.openedFiles.Lookup(fd)
}

// OpenFile opens a path on fsys with the given flags and permissions,
// inserts it into the fd table, and returns the assigned file descriptor.
func (c *FSContext) OpenFile(fsys FS, path string, flag Oflag, perm fs.FileMode) (int32, Errno) {
	f, errno := fsys.OpenFile(path, flag, perm)
	if errno != 0 {
		return 0, errno
	}
	fe := &FileEntry{FS: fsys, File: f}
	if path == "/" || path == "." {
		fe.Name = ""
	} else {
		fe.Name = path
	}
	if newFD, ok := c.openedFiles.Insert(fe); !ok {
		return 0, EBADF
	} else {
		delete(c.invalidated, newFD)
		return newFD, 0
	}
}

// InsertFileAt stores fe at the exact file descriptor.
func (c *FSContext) InsertFileAt(fe *FileEntry, fd int32) Errno {
	if !c.openedFiles.InsertAt(fe, fd) {
		return EBADF
	}
	delete(c.invalidated, fd)
	return 0
}

// AllocFile inserts fe at the first free descriptor slot and returns it.
func (c *FSContext) AllocFile(fe *FileEntry) (int32, Errno) {
	fd, ok := c.openedFiles.Insert(fe)
	if !ok {
		return 0, EBADF
	}
	delete(c.invalidated, fd)
	return fd, 0
}

// CloseFile closes the file at fd and removes it from the table.
// It returns EBADF if fd is not open.
func (c *FSContext) CloseFile(fd int32) Errno {
	f, ok := c.openedFiles.Lookup(fd)
	if !ok {
		return EBADF
	}
	if errno := f.File.Close(); errno != 0 {
		return errno
	}
	c.openedFiles.Delete(fd)
	delete(c.invalidated, fd)
	return 0
}

// Renumber moves the file descriptor from to to.
// If to is already open, its file is closed first (including preopens).
// Returns EBADF for invalid fds.
func (c *FSContext) Renumber(from, to int32) Errno {
	if c.invalidated[to] {
		return EBADF
	}
	fromFile, ok := c.openedFiles.Lookup(from)
	if !ok || to < 0 {
		return EBADF
	}

	if toFile, ok := c.openedFiles.Lookup(to); ok {
		_ = toFile.File.Close()
	}

	c.openedFiles.Delete(from)
	if !c.openedFiles.InsertAt(fromFile, to) {
		return EBADF
	}
	c.invalidated[from] = true
	delete(c.invalidated, to)
	return 0
}

// Close closes all open files and resets the fd table.
// It implements io.Closer.
func (c *FSContext) Close() error {
	var err error
	c.openedFiles.Range(func(fd int32, entry *FileEntry) bool {
		if errno := entry.File.Close(); errno != 0 {
			err = errno
		}
		return true
	})
	c.openedFiles = FileTable{}
	return err
}

// StripPrefixesAndTrailingSlash removes leading "." and "/" segments and
// trailing slashes from path. A result of "." coerces to the empty string
// because the current directory is handled by the guest.
func StripPrefixesAndTrailingSlash(path string) string {
	pathLen := len(path)
	for ; pathLen > 0 && path[pathLen-1] == '/'; pathLen-- {
	}

	pathI := 0
loop:
	for pathI < pathLen {
		switch path[pathI] {
		case '/':
			pathI++
		case '.':
			nextI := pathI + 1
			switch {
			case nextI < pathLen && path[nextI] == '/':
				pathI = nextI + 1
			case nextI == pathLen:
				pathI = nextI
			default:
				break loop
			}
		default:
			break loop
		}
	}
	return path[pathI:pathLen]
}
