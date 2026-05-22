// Package wasihost implements a standalone, general-purpose WASI
// snapshot-preview1 host for binaries compiled with wasm2go. It covers
// all 40 WASI preview1 functions plus the env.call_host_function stub
// required by zeroperl-style wasm2go modules.
//
// # Usage
//
// Construct a [State] with [New], passing a callback that returns the guest's
// linear memory slice. The callback is re-invoked on every syscall so that
// memory growth events are handled transparently:
//
//	state := wasihost.New(
//	    func() []byte { return *mod.Xmemory().Slice() },
//	    wasihost.WithStdin(stdinReader),
//	    wasihost.WithStdout(stdoutWriter),
//	    wasihost.WithHostDirectoryPreopen("/", hostDir),
//	)
//	mod := zeroperl.New(state, state)
//
// # Concurrency
//
// State is not safe for concurrent use. Each wasm2go Module instance must
// have its own State, and both must be used from a single goroutine.
//
// # Memory layout
//
// All multi-byte integers written into guest memory are little-endian, per
// the WASI snapshot-preview1 specification.
package wasihost

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// WASI snapshot-preview1 errno values. Names and numeric values follow
// the specification exactly.
const (
	wasiESuccess  int32 = 0
	wasiEAcces    int32 = 2
	wasiEBadf     int32 = 8
	wasiEExist    int32 = 20
	wasiEInval    int32 = 28
	wasiEIo       int32 = 29
	wasiEIsdir    int32 = 31
	wasiELoop     int32 = 32 // __WASI_ERRNO_LOOP (e.g. O_NOFOLLOW-style open on symlink)
	wasiENoEnt    int32 = 44
	wasiENoSys    int32 = 52
	wasiENotDir   int32 = 54
	wasiENotEmpty int32 = 55
	wasiENotSup   int32 = 56
	wasiEPerm     int32 = 63
	wasiEROFS     int32 = 66
	wasiEXdev     int32 = 75
	wasiENotCap   int32 = 76
	wasiEIlseq    int32 = 84 // __WASI_ERRNO_ILSEQ: illegal byte sequence

	// WASI file descriptor type tags, written into fdstat and filestat structs.
	fdCharDev byte = 2 // character device (stdin, stdout, stderr, /dev/null)
	fdDir     byte = 3 // directory (__WASI_FILETYPE_DIRECTORY)
	fdFile    byte = 4 // regular file (__WASI_FILETYPE_REGULAR_FILE)
	fdSymlink byte = 7 // symbolic link (__WASI_FILETYPE_SYMBOLIC_LINK)

	// WASI snapshot-preview1 __wasi_rights_t (bit positions per spec).
	rightFDRead             uint64 = 1 << 1  // __WASI_RIGHT_FD_READ
	rightFDSeek             uint64 = 1 << 2  // __WASI_RIGHT_FD_SEEK
	rightFDFdstatSetFlags   uint64 = 1 << 3  // __WASI_RIGHT_FD_FDSTAT_SET_FLAGS
	rightFDWrite            uint64 = 1 << 6  // __WASI_RIGHT_FD_WRITE
	rightPathOpen           uint64 = 1 << 13 // __WASI_RIGHT_PATH_OPEN
	rightFDReaddir          uint64 = 1 << 14 // __WASI_RIGHT_FD_READDIR
	rightPathReadlink       uint64 = 1 << 15 // __WASI_RIGHT_PATH_READLINK
	rightPathFilestatGet    uint64 = 1 << 18 // __WASI_RIGHT_PATH_FILESTAT_GET
	rightFDFilestatGet      uint64 = 1 << 21 // __WASI_RIGHT_FD_FILESTAT_GET
	rightFDFilestatSetSize  uint64 = 1 << 22 // __WASI_RIGHT_FD_FILESTAT_SET_SIZE
	rightFDFilestatSetTimes uint64 = 1 << 23 // __WASI_RIGHT_FD_FILESTAT_SET_TIMES

	// rightsDirectoryInherited is every path_* capability in bits 9-26 of __wasi_rights_t
	// (path_create_directory through path_unlink_file per snapshot-preview1), i.e. (1<<27)-(1<<9).
	// Excludes fd_poll and fd_sock at 27-28. Used as rights_inheriting for writable dir preopens.
	rightsDirectoryInherited uint64 = (1 << 27) - (1 << 9)
	// WASI filestat_set_times flags.
	// Note: the wasi-testsuite Rust tests were compiled with the wasi 0.11.0
	// crate where ATIM_NOW is bit 1 and MTIM is bit 2 (swapped vs the final
	// snapshot-preview1 spec). We match the guest layout so that e2e tests
	// pass with the precompiled wasm binaries.
	fstAtim    int32 = 1 << 0
	fstAtimNow int32 = 1 << 1
	fstMtim    int32 = 1 << 2
	fstMtimNow int32 = 1 << 3

	// __wasi_lookupflags_t bit: follow symlinks when resolving a path (e.g.
	// path_open, path_filestat_get). Matches __WASI_LOOKUPFLAGS_SYMLINK_FOLLOW.
	wasiLookupSymlinkFollow int32 = 1 << 0
)

// Rights bundles for preopens, files, and character devices (composed from the
// __wasi_rights_t constants above; values match snapshot-preview1).
var (
	// Writable host-directory preopen: FD read/seek/fdstat-set/write plus full
	// directory path capability set for children (rightsDirectoryInherited).
	rightsWritableDirPreopen = rightFDRead | rightFDSeek | rightFDFdstatSetFlags | rightFDWrite | rightsDirectoryInherited
	// Read-only fs.FS preopen: navigation and metadata without write or path mutation.
	rightsReadOnlyDirPreopen = rightFDRead | rightFDSeek | rightFDFdstatSetFlags | rightPathOpen | rightFDReaddir | rightPathReadlink | rightPathFilestatGet | rightFDFilestatGet
	// Regular file: FD I/O, fdstat, and filestat get/set size/times (no path_* bits).
	rightsRegularFile = rightFDRead | rightFDSeek | rightFDFdstatSetFlags | rightFDWrite | rightFDFilestatGet | rightFDFilestatSetSize | rightFDFilestatSetTimes
	// Alias for tests and callers that request the full writable-directory mask via path_open.
	rightsRegular = rightsWritableDirPreopen
	rightsCharDev = rightFDRead | rightFDWrite | rightFDFdstatSetFlags | rightFDFilestatGet

	// WASI fd_flags bitmasks.
	fdFlagsAppend   int32 = 1 << 0
	fdFlagsDSync    int32 = 1 << 1
	fdFlagsNonBlock int32 = 1 << 2
	fdFlagsRSync    int32 = 1 << 3
	fdFlagsSync     int32 = 1 << 4

	// WASI path_open oflags bits. Bit positions match the WASI specification.
	oflagCreat uint32 = 1 << 0 // O_CREAT: create file if it does not exist
	oflagDir   uint32 = 1 << 1 // __WASI_OFLAGS_DIRECTORY: path must be a directory (ENOTDIR if it is a non-directory file)
	oflagExcl  uint32 = 1 << 2 // O_EXCL: fail if file already exists
	oflagTrunc uint32 = 1 << 3 // O_TRUNC: truncate file to zero length on open

	// schedYield is a seam for testing sched_yield.
	schedYield func() = runtime.Gosched
)

// State is a WASI snapshot-preview1 host for a single wasm2go Module
// instance. It implements all X-prefixed method interfaces generated by
// wasm2go for the wasi_snapshot_preview1 and env import modules.
//
// Construct State with [New]; do not create it directly.
// State is not safe for concurrent use.
type State struct {
	mem         func() []byte
	fds         []fdEntry
	preopens    []fdEntry
	mounts      []mountEntry
	env         []string
	args        []string
	startTime   time.Time
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	trace       bool
	assertOwner bool
	ownerMu     sync.Mutex
	ownerGID    uint64
}

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

// fdEntry is one slot in the WASI file-descriptor table. Slots 0-2 are
// stdin, stdout, and stderr (fdCharDev). Slots 3 through 3+len(mounts)-1
// are preopen directory entries. Higher slots are allocated dynamically
// by path_open and freed by fd_close.
type fdEntry struct {
	file             fsFile
	path             string
	fdType           byte
	offset           int64
	mount            int
	preopen          bool
	dirFile          fs.ReadDirFile
	readdirSnapshot  []fs.DirEntry
	rightsBase       uint64
	rightsInheriting uint64
	fdFlags          uint16
}

func (e fdEntry) isUnused() bool {
	return e.file == nil && e.fdType == 0
}

// errnoIfFDRightsMissing returns wasiENotCap if rightsBase does not include
// every bit set in required; otherwise it returns 0.
func errnoIfFDRightsMissing(rightsBase, required uint64) int32 {
	if (rightsBase & required) != required {
		return wasiENotCap
	}
	return wasiESuccess
}

// errnoIfHostPathNotADirectory is the path_open O_DIRECTORY pre-check for a
// resolved writable host path. When the path exists and is not a directory,
// it returns wasiENotDir. Stat errors (including ENOENT) return 0 so the
// subsequent OpenFile (or overlay fallback) can map errors as usual.
func errnoIfHostPathNotADirectory(hostPath string) int32 {
	fi, err := os.Stat(hostPath)
	if err == nil && !fi.IsDir() {
		return wasiENotDir
	}
	return 0
}

// fdTypeFromInfo returns fdDir when fi is non-nil and represents a directory;
// otherwise it returns fdFile.
func fdTypeFromInfo(fi fs.FileInfo) byte {
	if fi != nil && fi.IsDir() {
		return fdDir
	}
	return fdFile
}

// errnoIfContradictoryFstFlags returns wasiEInval when fstFlags contains both
// ATIM and ATIM_NOW or both MTIM and MTIM_NOW; otherwise it returns
// wasiESuccess.
func errnoIfContradictoryFstFlags(fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstAtimNow) == fstAtim|fstAtimNow {
		return wasiEInval
	}
	if fstFlags&(fstMtim|fstMtimNow) == fstMtim|fstMtimNow {
		return wasiEInval
	}
	return wasiESuccess
}

// errnoForDirectoryFDOp returns EISDIR when the fd entry refers to a
// directory, because byte-oriented I/O (fd_read, fd_pread, fd_write,
// fd_pwrite) and position/size operations (fd_seek, fd_tell,
// fd_allocate, fd_filestat_set_size) are not defined on directories.
// Returns 0 for non-directory entries.
func errnoForDirectoryFDOp(entry fdEntry) int32 {
	if entry.fdType == fdDir {
		return wasiEIsdir
	}
	return 0
}

// mountEntry maps a guest path prefix to a host filesystem. If writable
// is true and hostRoot is set, write operations (os.Create, os.Remove,
// os.Mkdir, os.Rename, etc.) are applied to the host filesystem rooted at
// hostRoot. root is used for read-through lookups.
type mountEntry struct {
	guestPath string
	root      fs.FS
	hostRoot  string
	writable  bool
	readonly  bool // explicitly read-only via WithReadOnlyFS
}

// Option is a functional option for configuring a [State] at construction
// time. Options are applied in the order they are passed to [New].
type Option func(*State)

// ExitError is the panic value raised by proc_exit. Embedding applications
// must recover it at the wasm eval boundary to obtain the guest exit code:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        if e, ok := r.(wasihost.ExitError); ok {
//	            // e.Code is the guest exit status
//	        }
//	    }
//	}()
type ExitError struct{ Code int32 }

// Error implements the error interface. Returns a string of the form
// "exit status N" where N is the guest exit code.
func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// New creates a WASI snapshot-preview1 host state. mem is called on every
// syscall invocation to obtain the current guest linear memory slice; it is
// safe to call after memory growth events because wasm2go re-slices on grow.
//
// The returned *State satisfies the X-prefixed method interfaces generated
// by wasm2go. Pass it as both import arguments to the generated module's
// New constructor:
//
//	state := wasihost.New(func() []byte { return *mod.Xmemory().Slice() }, opts...)
//	mod   := zeroperl.New(state, state)
//
// If mem is nil, any method that accesses guest memory will panic. A nil
// mem is only safe for tests that do not exercise memory-reading methods.
func New(mem func() []byte, opts ...Option) *State {
	s := &State{mem: mem, startTime: time.Now()}
	for _, opt := range opts {
		opt(s)
	}
	// Initialize fd table
	s.fds = make([]fdEntry, 3+len(s.mounts), 8+len(s.mounts))
	s.fds[0] = fdEntry{fdType: fdCharDev, path: "stdin", rightsBase: rightsCharDev, rightsInheriting: 0}
	s.fds[1] = fdEntry{fdType: fdCharDev, path: "stdout", rightsBase: rightsCharDev, rightsInheriting: 0}
	s.fds[2] = fdEntry{fdType: fdCharDev, path: "stderr", rightsBase: rightsCharDev, rightsInheriting: 0}

	for i := range s.mounts {
		rights := rightsWritableDirPreopen
		if s.mounts[i].readonly {
			rights = rightsReadOnlyDirPreopen
		}
		s.fds[3+i] = fdEntry{
			path:             s.mounts[i].guestPath,
			fdType:           fdDir,
			mount:            i,
			preopen:          true,
			rightsBase:       rights,
			rightsInheriting: rights,
		}
	}
	s.preopens = s.fds[3 : 3+len(s.mounts)]

	return s
}

// WithArgs sets the argv for the WASI process, used by args_get and
// args_sizes_get. Defaults to no arguments. Multiple calls append.
func WithArgs(args ...string) Option { return func(s *State) { s.args = append(s.args, args...) } }

// WithEnv sets the full environment variable list as "KEY=VALUE" strings,
// used by environ_get and environ_sizes_get. Callers are responsible for
// including all required entries such as PERL5LIB. Multiple calls append.
func WithEnv(env ...string) Option { return func(s *State) { s.env = append(s.env, env...) } }

// WithReadOnlyFS adds a read-only guest filesystem mount that is explicitly
// marked as read-only in WASI preopens, reporting no write or mutation rights.
func WithReadOnlyFS(guestPath string, root fs.FS) Option {
	return func(s *State) {
		s.mounts = append(s.mounts, mountEntry{guestPath: guestPath, root: root, writable: false, readonly: true})
	}
}

// WithHostDirectoryPreopen adds a host directory as a WASI preopened directory.
func WithHostDirectoryPreopen(guestPath, hostPath string) Option {
	return func(s *State) {
		s.mounts = append(s.mounts, mountEntry{guestPath: guestPath, hostRoot: hostPath, root: os.DirFS(hostPath), writable: true})
	}
}

// WithStdin sets the io.Reader for guest fd 0. Defaults to a reader that
// returns (0, io.EOF) immediately. Defaulting to os.Stdin is deliberately
// avoided: this host is designed for embedded server use where the process
// stdin is unrelated to the guest.
func WithStdin(r io.Reader) Option { return func(s *State) { s.stdin = r } }

// WithStdout sets the io.Writer for guest fd 1. Defaults to io.Discard.
// Defaulting to os.Stdout is deliberately avoided; accidental stdout
// leakage is a correctness hazard in embedded server use.
func WithStdout(w io.Writer) Option { return func(s *State) { s.stdout = w } }

// WithStderr sets the io.Writer for guest fd 2. Defaults to io.Discard.
// See [WithStdout] for rationale.
func WithStderr(w io.Writer) Option { return func(s *State) { s.stderr = w } }

// WithTracing enables per-syscall trace logging to os.Stdout. Off by
// default. In the embedding application, activate via the
// EXIFTOOL_WASI_TRACE=1 environment variable.
func WithTracing() Option { return func(s *State) { s.trace = true } }

// WithOwnerAssertion enables a runtime goroutine-ownership assertion. When
// enabled, any WASI call from a goroutine other than the first caller
// panics with a descriptive message. Off by default. In the embedding
// application, activate via the EXIFTOOL_WASI_ASSERT_OWNER=1 environment
// variable.
func WithOwnerAssertion() Option { return func(s *State) { s.assertOwner = true } }

// Mem returns the current guest linear memory slice by invoking the mem
// callback passed to [New]. The returned slice may be replaced after any
// memory growth event; callers must not retain it across calls.
func (s *State) Mem() []byte { return s.mem() }

// allocFD returns the index of the first empty fd-table slot above the
// preopen range. It extends the table by one slot if no free slot exists.
func (s *State) allocFD() int32 {
	s.assertSingleOwner()
	start := 3 + len(s.preopens)
	for i := start; i < len(s.fds); i++ {
		if s.fds[i].isUnused() {
			return int32(i)
		}
	}
	idx := int32(len(s.fds))
	s.fds = append(s.fds, fdEntry{})
	return idx
}

// storeOpenedFD records an opened file in the fd table, computes stored rights
// via [pathOpenStoredRights], and writes the allocated fd number to guest
// memory at fdPtr. If fdType is fdDir and file implements [fs.ReadDirFile],
// entry.dirFile is set so directory iteration works.
func (s *State) storeOpenedFD(file fsFile, guestPath string, fdType byte, parentInh uint64, fdRightsBase, fdRightsInheriting int64, fdFlags int32, fdPtr int32) int32 {
	rb, ri := pathOpenStoredRights(parentInh, fdType, fdRightsBase, fdRightsInheriting)
	fd := s.allocFD()
	entry := fdEntry{
		file:             file,
		path:             guestPath,
		fdType:           fdType,
		rightsBase:       rb,
		rightsInheriting: ri,
		fdFlags:          uint16(fdFlags),
	}
	if fdType == fdDir {
		if df, ok := file.(fs.ReadDirFile); ok {
			entry.dirFile = df
		}
	}
	s.fds[fd] = entry
	binary.LittleEndian.PutUint32(s.mem()[fdPtr:], uint32(fd))
	return wasiESuccess
}

// logTrace writes a formatted line to os.Stdout when tracing is enabled.
func (s *State) logTrace(format string, args ...interface{}) {
	if s.trace {
		fmt.Printf(format+"\n", args...)
	}
}

// assertSingleOwner panics if the calling goroutine is not the goroutine
// that first called a method on this State. Has no effect when
// WithOwnerAssertion was not passed to New.
func (s *State) assertSingleOwner() {
	if !s.assertOwner {
		return
	}
	gid := currentGID()
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	if s.ownerGID == 0 {
		s.ownerGID = gid
		return
	}
	if s.ownerGID != gid {
		panic(fmt.Sprintf("wasiState single-owner invariant violated: owner goroutine=%d current goroutine=%d", s.ownerGID, gid))
	}
}

// currentGID returns the numeric ID of the calling goroutine by parsing
// the output of runtime.Stack. There is no public Go API for goroutine IDs;
// this approach is intentionally fragile by design - it is only used for
// debugging assertions, not for correctness logic.
func currentGID() uint64 {
	var b [64]byte
	n := runtime.Stack(b[:], false)
	line := string(b[:n])
	const prefix = "goroutine "
	if !strings.HasPrefix(line, prefix) {
		panic("unable to parse goroutine id: missing prefix")
	}
	line = line[len(prefix):]
	end := strings.IndexByte(line, ' ')
	if end < 0 {
		panic("unable to parse goroutine id: missing delimiter")
	}
	id, err := strconv.ParseUint(line[:end], 10, 64)
	if err != nil {
		panic(fmt.Sprintf("unable to parse goroutine id: %v", err))
	}
	return id
}

// wasiDirInfo is a sentinel fs.FileInfo returned by DirEntriesFile.Stat.
// It satisfies the fs.FileInfo interface with directory-type metadata and
// zero values for fields that are not meaningful for virtual directory listings.
type wasiDirInfo struct{}

func (wasiDirInfo) Name() string       { return "." }
func (wasiDirInfo) Size() int64        { return 0 }
func (wasiDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (wasiDirInfo) ModTime() time.Time { return time.Time{} }
func (wasiDirInfo) IsDir() bool        { return true }
func (wasiDirInfo) Sys() any           { return nil }

// synthDirEntry is a minimal fs.DirEntry for synthetic . and .. entries.
type synthDirEntry struct{ name string }

func (e synthDirEntry) Name() string               { return e.name }
func (e synthDirEntry) IsDir() bool                { return true }
func (e synthDirEntry) Type() fs.FileMode          { return fs.ModeDir }
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
func (d *DirEntriesFile) Close() error               { return nil }
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

// Xenviron_sizes_get implements environ_sizes_get. Writes the count of
// configured environment strings and their total buffer size (including
// null terminators) to the respective pointers in guest memory.
func (s *State) Xenviron_sizes_get(countPtr, bufSizePtr int32) int32 {
	writeStringTableSizes(s.mem(), countPtr, bufSizePtr, s.env)
	return wasiESuccess
}

// Xenviron_get implements environ_get. Writes a pointer array at envPtr
// and the corresponding null-terminated "KEY=VALUE" strings packed at
// envBufPtr into guest memory.
func (s *State) Xenviron_get(envPtr, envBufPtr int32) int32 {
	writeStringTable(s.mem(), envPtr, envBufPtr, s.env)
	return wasiESuccess
}

// Xfd_prestat_get implements fd_prestat_get. Returns the prestat struct
// for the preopen directory at fd. Returns EBADF if fd is not a valid,
// in-use preopen.
func (s *State) Xfd_prestat_get(fd, prestatPtr int32) int32 {
	entry, ok := s.preopenEntryByFD(fd)
	if !ok {
		return wasiEBadf
	}
	mem := s.mem()
	pathLen := uint32(len(entry.path))
	binary.LittleEndian.PutUint32(mem[prestatPtr:], 0)
	binary.LittleEndian.PutUint32(mem[prestatPtr+4:], pathLen)
	return wasiESuccess
}

// Xfd_prestat_dir_name implements fd_prestat_dir_name. Writes the guest
// path string for the preopen directory at fd into guest memory. Returns
// EBADF if fd is not a valid, in-use preopen, or EINVAL when pathLen is
// smaller than the preopen path length.
func (s *State) Xfd_prestat_dir_name(fd, pathPtr, pathLen int32) int32 {
	entry, ok := s.preopenEntryByFD(fd)
	if !ok {
		return wasiEBadf
	}
	name := entry.path
	if int(pathLen) < len(name) {
		return wasiEInval
	}
	mem := s.mem()
	copy(mem[pathPtr:], name)
	return wasiESuccess
}

// Xfd_fdstat_get implements fd_fdstat_get. Writes a 24-byte fdstat struct
// at statPtr. fds 0-2 are reported as character devices; all others use
// the type recorded in the fd table.
func (s *State) Xfd_fdstat_get(fd, statPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	writeFdstat(s.mem(), statPtr, entry.fdType, entry.fdFlags, entry.rightsBase, entry.rightsInheriting)
	return wasiESuccess
}

// writeFdstat writes a 24-byte WASI fdstat struct at statPtr in mem.
// Layout: fdtype(2) + flags(2) + padding(4) + rights_base(8) + rights_inheriting(8).
func writeFdstat(mem []byte, statPtr int32, fdType byte, fdFlags uint16, rightsBase, rightsInheriting uint64) {
	var buf [24]byte
	binary.LittleEndian.PutUint16(buf[0:], uint16(fdType))
	binary.LittleEndian.PutUint16(buf[2:], fdFlags)
	binary.LittleEndian.PutUint32(buf[4:], 0)
	binary.LittleEndian.PutUint64(buf[8:], rightsBase)
	binary.LittleEndian.PutUint64(buf[16:], rightsInheriting)
	copy(mem[statPtr:], buf[:])
}

// statDevIno extracts dev and ino from an fs.FileInfo's underlying syscall.Stat_t.
// Returns zero values if the underlying type is not *syscall.Stat_t.
func statDevIno(fi fs.FileInfo) (dev uint64, ino uint64) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev), st.Ino
	}
	return 0, 0
}

// writeFilestat writes a 64-byte WASI filestat struct at bufPtr in mem.
// Layout: dev(8) + ino(8) + filetype(8) + nlink(8) + size(8) + atim(8) + mtim(8) + ctim(8).
// fdType is a WASI snapshot-preview1 filetype tag (same values as fdstat.fs_filetype),
// e.g. fdDir, fdFile, fdSymlink.
// atim, mtim, and ctim are all set to mtimeNs; this host does not track
// separate access times.
func writeFilestat(mem []byte, bufPtr int32, fdType byte, size int64, mtimeNs int64, dev uint64, ino uint64) {
	var buf [64]byte
	binary.LittleEndian.PutUint64(buf[0:], dev)
	binary.LittleEndian.PutUint64(buf[8:], ino)
	binary.LittleEndian.PutUint64(buf[16:], uint64(fdType))
	binary.LittleEndian.PutUint64(buf[24:], 1)
	binary.LittleEndian.PutUint64(buf[32:], uint64(size))
	binary.LittleEndian.PutUint64(buf[40:], uint64(mtimeNs))
	binary.LittleEndian.PutUint64(buf[48:], uint64(mtimeNs))
	binary.LittleEndian.PutUint64(buf[56:], uint64(mtimeNs))
	copy(mem[bufPtr:], buf[:])
}

// Xfd_renumber implements fd_renumber. Copies the fd-table slot at fd to
// the slot at to and clears the source slot. Returns EBADF if either
// index is out of range.
func (s *State) Xfd_renumber(fd, to int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) || to < 0 || int(to) >= len(s.fds) {
		return wasiEBadf
	}
	s.fds[to] = s.fds[fd]
	s.fds[fd] = fdEntry{}
	return wasiESuccess
}

// Xproc_exit implements proc_exit. Panics with [ExitError] so that the
// embedding application's eval boundary can recover it and obtain the
// guest exit code.
func (s *State) Xproc_exit(code int32) {
	panic(ExitError{Code: code})
}

// Xrandom_get implements random_get. Fills the guest memory region
// [bufPtr, bufPtr+bufLen) with cryptographically random bytes.
func (s *State) Xrandom_get(bufPtr, bufLen int32) int32 {
	mem := s.mem()
	rand.Read(mem[bufPtr : bufPtr+bufLen])
	return wasiESuccess
}

// Xclock_time_get implements clock_time_get. Writes the current time as
// a uint64 nanosecond value to resultPtr.
//
// clockID 0 (CLOCK_REALTIME): wall-clock time via time.Now().UnixNano().
// clockID 1 (CLOCK_MONOTONIC): nanoseconds elapsed since State construction.
// Any other clockID: returns ENOSYS.
func (s *State) Xclock_time_get(clockID int32, precision int64, resultPtr int32) int32 {
	mem := s.mem()
	switch clockID {
	case 0: // realtime
		binary.LittleEndian.PutUint64(mem[resultPtr:], uint64(time.Now().UnixNano()))
		return wasiESuccess
	case 1: // monotonic
		var t int64
		if s.startTime.IsZero() {
			t = time.Now().UnixNano()
		} else {
			t = time.Since(s.startTime).Nanoseconds()
		}
		binary.LittleEndian.PutUint64(mem[resultPtr:], uint64(t))
		return wasiESuccess
	default:
		return wasiENoSys
	}
}

// Xclock_res_get implements clock_res_get. Writes 1 (nanosecond
// resolution) for clockID 0 and 1. Returns ENOSYS for any other clockID.
func (s *State) Xclock_res_get(clockID int32, resultPtr int32) int32 {
	switch clockID {
	case 0, 1:
		binary.LittleEndian.PutUint64(s.mem()[resultPtr:], 1)
		return wasiESuccess
	default:
		return wasiENoSys
	}
}

// Xargs_sizes_get implements args_sizes_get. Writes the argument count
// and total buffer size (including null terminators) to guest memory.
func (s *State) Xargs_sizes_get(argcPtr, argvSizePtr int32) int32 {
	writeStringTableSizes(s.mem(), argcPtr, argvSizePtr, s.args)
	return wasiESuccess
}

// Xargs_get implements args_get. Writes a pointer array at argvPtr and
// the corresponding null-terminated argument strings packed at argvBufPtr
// into guest memory. Uses the same layout as environ_get.
func (s *State) Xargs_get(argvPtr, argvBufPtr int32) int32 {
	writeStringTable(s.mem(), argvPtr, argvBufPtr, s.args)
	return wasiESuccess
}

// readBytes reads length bytes from guest memory starting at ptr.
// Returns nil if ptr or length is zero.
func (s *State) readBytes(ptr, length int32) []byte {
	if ptr == 0 || length == 0 {
		return nil
	}
	return s.mem()[ptr : ptr+length]
}

// resolvePath resolves a guest-absolute path to the best-matching mount
// and a mount-relative path string using longest-prefix matching.
// Returns (nil, "") if no mount covers the path.
func (s *State) resolvePath(guestPath string) (*mountEntry, string) {
	var best *mountEntry
	bestLen := -1
	bestRel := ""
	for i := range s.mounts {
		m := &s.mounts[i]
		mp := path.Clean("/" + m.guestPath)
		if mp == "." {
			mp = "/"
		}

		clean := path.Clean("/" + guestPath)
		if clean == "." {
			clean = "/"
		}

		if (clean == mp || strings.HasPrefix(clean, mp+"/")) && len(mp) > bestLen {
			rel := strings.TrimPrefix(clean, mp)
			rel = strings.TrimPrefix(rel, "/")
			best = m
			bestLen = len(mp)
			bestRel = rel
		}

		raw := "/" + strings.TrimPrefix(guestPath, "/")
		if (strings.HasPrefix(raw, mp+"/") || raw == mp) && len(mp) > bestLen {
			rel := strings.TrimPrefix(raw, mp)
			rel = strings.TrimPrefix(rel, "/")
			best = m
			bestLen = len(mp)
			bestRel = rel
		}
	}
	return best, bestRel
}

// mountGuestPath returns the normalized guest-absolute path for a
// mount-relative path by joining the mount's guest path with relPath
// and applying path.Clean. For example, mount.guestPath="/data" and
// relPath="dir/../file" yields "/data/file".
func mountGuestPath(m *mountEntry, relPath string) string {
	return path.Clean("/" + m.guestPath + "/" + relPath)
}

// preopenMountRelEscapes reports whether a mount-relative guest path
// lexically escapes upward past the preopen root after normalization
// (for example ".." or "../segment").
func preopenMountRelEscapes(rel string) bool {
	relLex := strings.TrimLeft(rel, "/")
	cleanLex := path.Clean(relLex)
	return cleanLex == ".." || strings.HasPrefix(cleanLex, "../")
}

// preopenDirfdLexicallyEscapes reports whether dirfd refers to a directory
// preopen and mountRel would lexically escape that preopen's root (see
// preopenMountRelEscapes). Matches the guard used before host-backed path
// operations in resolveWritable, Xpath_open, and Xpath_filestat_get.
func (s *State) preopenDirfdLexicallyEscapes(dirfd int32, mountRel string) bool {
	entry, ok := s.fdEntry(dirfd)
	return ok && entry.preopen && preopenMountRelEscapes(mountRel)
}

// fdEntry returns the fdEntry for dirfd if it is in bounds.
func (s *State) fdEntry(dirfd int32) (fdEntry, bool) {
	if dirfd < 0 || int(dirfd) >= len(s.fds) {
		return fdEntry{}, false
	}
	return s.fds[dirfd], true
}

// isNonPreopenDirfd reports whether dirfd refers to an open directory
// that was not a preopen (i.e. it was opened via path_open).
func (s *State) isNonPreopenDirfd(dirfd int32) bool {
	entry, ok := s.fdEntry(dirfd)
	return ok && !entry.preopen && entry.fdType == fdDir
}

// guestAbsPathForFDEntry returns the guest-absolute path to store in an fd
// entry for guestPath opened via dirfd. Absolute paths are returned unchanged;
// relative paths are joined against the dirfd entry's stored guest-absolute
// path (preopen or nested directory).
func (s *State) guestAbsPathForFDEntry(dirfd int32, guestPath string) string {
	if strings.HasPrefix(guestPath, "/") {
		return guestPath
	}
	if entry, ok := s.fdEntry(dirfd); ok && entry.path != "" {
		return path.Join(entry.path, guestPath)
	}
	return guestPath
}

// nonPreopenDirfdResolvedPathEscapes reports whether resolving relPath
// through the given mount produces a guest-absolute path that falls
// outside the subtree of a non-preopen directory fd. This prevents
// path_open from accessing paths above the dirfd's resolved directory
// using ".." segments.
func (s *State) nonPreopenDirfdResolvedPathEscapes(dirfd int32, mount *mountEntry, relPath string) bool {
	if !s.isNonPreopenDirfd(dirfd) {
		return false
	}
	resolvedGuest := mountGuestPath(mount, relPath)
	dirEntry, ok := s.fdEntry(dirfd)
	if !ok || dirEntry.path == "" {
		return false
	}
	prefix := dirEntry.path
	return resolvedGuest != prefix && !strings.HasPrefix(resolvedGuest, prefix+"/")
}

// preopenEntryByFD returns the fdEntry for preopen fd if it is valid and
// in use. The ok bool is false when fd is not a preopen or the slot is unused.
func (s *State) preopenEntryByFD(fd int32) (fdEntry, bool) {
	idx := fd - 3
	if idx < 0 || idx >= int32(len(s.preopens)) {
		return fdEntry{}, false
	}
	entry := s.preopens[idx]
	if entry.isUnused() {
		return fdEntry{}, false
	}
	return entry, true
}

// joinWritableHostPathForLookup joins hostRoot with a mount-relative path for a
// host directory preopen. When symlink following is not requested and the final
// path component exists and is a symlink, it returns ELOOP (WASI snapshot-preview1),
// matching O_NOFOLLOW-style path open behavior. When symlink following is
// requested, it runs writableHostSymlinkFollowConfinementErrno so resolution
// cannot escape hostRoot.
func joinWritableHostPathForLookup(hostRoot, relPath string, lookupFlags int32) (hostPath string, errno int32) {
	hostPath = filepath.Join(hostRoot, filepath.FromSlash(relPath))
	if lookupFlags&wasiLookupSymlinkFollow == 0 {
		// Open without SYMLINK_FOLLOW must not traverse the final symlink; Lstat
		// distinguishes a present symlink from a missing path (Lstat error).
		fi, err := os.Lstat(hostPath)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return hostPath, wasiELoop
		}
		return hostPath, wasiESuccess
	}
	return hostPath, writableHostSymlinkFollowConfinementErrno(hostRoot, hostPath)
}

// statHostPathOrOverlay runs os.Lstat or os.Stat on hostPath (depending on
// followFinalSymlink), and if that fails tries fs.Stat on overlay at relPath.
// Used for writable mounts where some paths exist only in the overlay fs.FS.
func statHostPathOrOverlay(hostPath string, overlay fs.FS, relPath string, followFinalSymlink bool) (fs.FileInfo, error) {
	var fi fs.FileInfo
	var err error
	if followFinalSymlink {
		fi, err = os.Stat(hostPath)
	} else {
		fi, err = os.Lstat(hostPath)
	}
	if err != nil {
		return fs.Stat(overlay, relPath)
	}
	return fi, nil
}

// filestatFdTypeFromInfo maps os.FileInfo / fs.FileInfo to a WASI preview1
// filetype for the filestat struct.
func filestatFdTypeFromInfo(fi fs.FileInfo) byte {
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return fdSymlink
	case fi.IsDir():
		return fdDir
	default:
		return fdFile
	}
}

// writableHostSymlinkFollowConfinementErrno checks that resolving hostPath with
// symlink awareness stays inside hostRoot (after filepath.Clean / EvalSymlinks).
// It returns ESUCCESS when the resolved path is confined to the preopen,
// ENOTCAPABLE when symlink steps would reach outside the root, and ENOENT when
// symlink resolution fails (non-ErrNotExist errors) or the root cannot be resolved.
//
// When the final path component is missing-as with O_CREAT while following
// symlinks-EvalSymlinks returns ErrNotExist for the full path. The implementation
// then walks up with filepath.Dir, re-running EvalSymlinks on each parent, until
// it finds an existing prefix. That preserves confinement checks for symlink
// chains that point outside the preopen followed by a non-existent trailing name.
func writableHostSymlinkFollowConfinementErrno(hostRoot, hostPath string) int32 {
	p := filepath.Clean(hostPath)
	var resolved string
	for {
		var err error
		resolved, err = filepath.EvalSymlinks(p)
		if err == nil {
			break
		}
		if errors.Is(err, fs.ErrNotExist) {
			parent := filepath.Dir(p)
			if parent == p {
				return wasiESuccess
			}
			p = parent
			continue
		}
		return wasiENoEnt
	}
	rootReal, err := filepath.EvalSymlinks(hostRoot)
	if err != nil {
		return wasiENoEnt
	}
	rootAbs, err := filepath.Abs(rootReal)
	if err != nil {
		return wasiEIo
	}
	resAbs, err := filepath.Abs(resolved)
	if err != nil {
		return wasiEIo
	}
	rel, err := filepath.Rel(rootAbs, resAbs)
	if err != nil {
		return wasiENotCap
	}
	if rel == "." {
		return wasiESuccess
	}
	sep := string(filepath.Separator)
	if rel == ".." || strings.HasPrefix(rel, ".."+sep) {
		return wasiENotCap
	}
	return wasiESuccess
}

// resolveDirfdPath resolves a WASI (dirfd, pathPtr, pathLen) triple to a
// mountEntry and a mount-relative path string.
//
// Absolute guest paths bypass dirfd and are resolved via resolvePath.
// For relative paths, preopen fds resolve directly against their mount.
// Non-preopen directory fds join the relative path against the absolute
// guest path stored in the fd entry, enabling correct nested path_open
// resolution during directory recursion.
// Non-directory fds (including regular files) yield a nil mount.
func (s *State) resolveDirfdPath(dirfd, pathPtr, pathLen int32) (*mountEntry, string) {
	pathBytes := s.readBytes(pathPtr, pathLen)
	guestPath := string(pathBytes)
	if strings.HasPrefix(guestPath, "/") {
		if s.isNonPreopenDirfd(dirfd) {
			return nil, ""
		}
		return s.resolvePath(guestPath)
	}
	if entry, ok := s.fdEntry(dirfd); ok {
		if entry.preopen && entry.mount >= 0 && entry.mount < len(s.mounts) {
			return &s.mounts[entry.mount], guestPath
		}
		if entry.fdType == fdDir && entry.path != "" {
			full := path.Join(entry.path, guestPath)
			return s.resolvePath(full)
		}
	}
	return nil, ""
}

// resolveWritable resolves a (dirfd, path) pair to a host path for mutation
// and other host-backed operations. Directory preopens reject mount-relative
// paths that lexically escape the preopen root with ENOTCAPABLE (see
// preopenDirfdLexicallyEscapes) before joining hostRoot.
func (s *State) resolveWritable(dirfd, pathPtr, pathLen int32) (string, int32) {
	m, rel := s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if m == nil {
		if dirfd < 0 || int(dirfd) >= len(s.fds) {
			return "", wasiEBadf
		}
		return "", wasiEROFS
	}

	if s.preopenDirfdLexicallyEscapes(dirfd, rel) {
		return "", wasiENotCap
	}

	if !m.writable || m.hostRoot == "" {
		return "", wasiEROFS
	}

	return filepath.Join(m.hostRoot, filepath.FromSlash(rel)), wasiESuccess
}

// Xpath_create_directory implements path_create_directory. Creates a
// directory at the resolved host path via os.Mkdir. Returns EROFS if the
// mount is read-only, EEXIST if the directory already exists, ENOENT if
// the parent does not exist.
func (s *State) Xpath_create_directory(dirfd, pathPtr, pathLen int32) int32 {
	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	if err := os.Mkdir(path, 0755); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_remove_directory implements path_remove_directory. Removes an
// empty directory at the resolved host path. Returns EROFS if the mount
// is read-only, ENOTDIR if the target is a file, ENOTEMPTY if the
// directory is not empty.
func (s *State) Xpath_remove_directory(dirfd, pathPtr, pathLen int32) int32 {
	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return mapOSError(err)
	}
	if !fi.IsDir() {
		return wasiENotDir
	}
	if err := os.Remove(path); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_unlink_file implements path_unlink_file. Removes a file at the
// resolved host path. Returns EROFS if the mount is read-only, EISDIR if
// the target is a directory, ENOENT if the file does not exist.
func (s *State) Xpath_unlink_file(dirfd, pathPtr, pathLen int32) int32 {
	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return mapOSError(err)
	}
	if fi.IsDir() {
		return wasiEIsdir
	}
	if err := os.Remove(path); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_readlink implements path_readlink. Reads the target of a symbolic
// link at the resolved host path into guest memory at bufPtr, capped at
// bufLen bytes, and writes the actual byte count to nreadPtr. Returns
// EROFS if the mount is read-only (embedded fs.FS mounts have no symlinks).
func (s *State) Xpath_readlink(dirfd, pathPtr, pathLen, bufPtr, bufLen, nreadPtr int32) int32 {
	path, errno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if errno != wasiESuccess {
		return errno
	}
	target, err := os.Readlink(path)
	if err != nil {
		return mapOSError(err)
	}
	mem := s.mem()
	n := copy(mem[bufPtr:bufPtr+bufLen], target)
	binary.LittleEndian.PutUint32(mem[nreadPtr:], uint32(n))
	return wasiESuccess
}

// Xpath_symlink implements path_symlink. Creates a symbolic link at the
// resolved host path for newPath pointing to the raw string oldPath.
// Returns EROFS if the mount is read-only, EEXIST if the link path already
// exists.
func (s *State) Xpath_symlink(oldPathPtr, oldPathLen, dirfd, newPathPtr, newPathLen int32) int32 {
	target := string(s.readBytes(oldPathPtr, oldPathLen))
	path, errno := s.resolveWritable(dirfd, newPathPtr, newPathLen)
	if errno != wasiESuccess {
		return errno
	}
	if err := os.Symlink(target, path); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpath_link implements path_link. Creates a hard link from the resolved
// old path to the resolved new path via os.Link. Returns EROFS if either
// mount is read-only.
func (s *State) Xpath_link(oldDirfd, oldFlags, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen int32) int32 {
	oldPath, oldErr := s.resolveWritable(oldDirfd, oldPathPtr, oldPathLen)
	if oldErr != wasiESuccess {
		return oldErr
	}
	newPath, newErr := s.resolveWritable(newDirfd, newPathPtr, newPathLen)
	if newErr != wasiESuccess {
		return newErr
	}
	if err := os.Link(oldPath, newPath); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xfd_close implements fd_close. Closes the file associated with fd and
// clears the fd-table slot. Returns EBADF if fd is invalid.
func (s *State) Xfd_close(fd int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	if entry.file != nil {
		entry.file.Close()
	}
	s.fds[fd] = fdEntry{}
	return wasiESuccess
}

// Xfd_read implements fd_read. For fd 0 (stdin), reads from the
// io.Reader configured by [WithStdin]. For other fds, reads via ReadAt
// at the current fd offset when available, otherwise via Read. Returns
// EISDIR for directory fds, ENOTCAPABLE when FD_READ is not set in the
// fd's rights_base.
func (s *State) Xfd_read(fd int32, iovsPtr int32, iovsCount int32, nreadPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	mem := s.mem()
	if fd == 0 {
		if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDRead); errno != 0 {
			return errno
		}
		var total uint32
		for i := int32(0); i < iovsCount; i++ {
			off := iovsPtr + i*8
			bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
			bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
			if bufLen == 0 {
				continue
			}
			var n int
			var err error
			if s.stdin != nil {
				n, err = s.stdin.Read(mem[bufPtr : bufPtr+bufLen])
			} else {
				n, err = 0, io.EOF
			}
			total += uint32(n)
			if err != nil {
				binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
				if err != io.EOF {
					return wasiEIo
				}
				return wasiESuccess
			}
			if n < int(bufLen) {
				break
			}
		}
		binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
		return wasiESuccess
	}
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDRead); errno != 0 {
		return errno
	}
	var total uint32
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		var n int
		var err error
		if ra, ok := entry.file.(interface {
			ReadAt([]byte, int64) (int, error)
		}); ok {
			n, err = ra.ReadAt(mem[bufPtr:bufPtr+bufLen], entry.offset)
		} else {
			n, err = entry.file.Read(mem[bufPtr : bufPtr+bufLen])
		}
		total += uint32(n)
		entry.offset += int64(n)
		if err != nil {
			if err == io.EOF {
				break
			}
			s.fds[fd] = entry
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return wasiEIo
		}
		if n < int(bufLen) {
			break
		}
	}
	s.fds[fd] = entry
	binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
	return wasiESuccess
}

// Xfd_write implements fd_write. For fd 1 and 2 (stdout/stderr), writes
// to the io.Writers configured by [WithStdout] and [WithStderr]. For
// other fds, uses WriteAt with the current fd offset; only osFile-backed
// entries support writes. Returns EISDIR for directory fds, ENOTCAPABLE
// when FD_WRITE is not set in the fd's rights_base.
func (s *State) Xfd_write(fd int32, iovsPtr int32, iovsCount int32, nwrittenPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	mem := s.mem()
	if fd == 1 || fd == 2 {
		if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDWrite); errno != 0 {
			return errno
		}
		var total uint32
		for i := int32(0); i < iovsCount; i++ {
			off := iovsPtr + i*8
			bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
			bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
			data := mem[bufPtr : bufPtr+bufLen]
			var n int
			var err error
			if fd == 1 {
				if s.stdout != nil {
					n, err = s.stdout.Write(data)
				}
			} else {
				if s.stderr != nil {
					n, err = s.stderr.Write(data)
				}
			}
			total += uint32(n)
			if err != nil {
				binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
				return wasiEIo
			}
		}
		binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
		return wasiESuccess
	}
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDWrite); errno != 0 {
		return errno
	}
	var total uint32
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		wa, ok := entry.file.(interface {
			WriteAt([]byte, int64) (int, error)
		})
		if !ok {
			break
		}
		writeOff := entry.offset
		if entry.fdFlags&uint16(fdFlagsAppend) != 0 {
			if fi, err := entry.file.Stat(); err == nil {
				writeOff = fi.Size()
			}
		}
		n, err := wa.WriteAt(mem[bufPtr:bufPtr+bufLen], writeOff)
		entry.offset += int64(n)
		total += uint32(n)
		if err != nil {
			s.fds[fd] = entry
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
			return wasiEIo
		}
	}
	s.fds[fd] = entry
	binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
	return wasiESuccess
}

// Xfd_seek implements fd_seek. Delegates to the underlying io.Seeker and
// updates the WASI-level fd offset so that subsequent fd_write (via
// WriteAt) and fd_tell return consistent positions. Returns EISDIR for
// directory fds, EINVAL if the file does not implement io.Seeker.
func (s *State) Xfd_seek(fd int32, offset int64, whence, newOffsetPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil || entry.preopen {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	sk, ok := entry.file.(io.Seeker)
	if !ok {
		return wasiEInval
	}
	n, err := sk.Seek(offset, int(whence))
	if err != nil {
		return wasiEIo
	}
	entry.offset = n
	s.fds[fd] = entry
	binary.LittleEndian.PutUint64(s.mem()[newOffsetPtr:], uint64(n))
	return wasiESuccess
}

// Xfd_readdir implements fd_readdir. Writes WASI dirent structs into
// guest memory starting from the entry at cookie. Each dirent is
// 24 + len(name) bytes. For preopen fds backed by fs.ReadDirFS, the
// directory listing is loaded on first call and cached in the fd entry.
func (s *State) Xfd_readdir(fd int32, bufPtr int32, bufLen int32, cookie int64, bufUsedPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := &s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	mem := s.mem()

	// cookie=0 invalidates the per-fd listing snapshot so the next call
	// re-reads from the host. cookie>0 uses the warm snapshot without
	// touching the host (stable across host directory mutations).
	if cookie == 0 {
		entry.readdirSnapshot = nil
		entry.dirFile = nil
		if _, ok := entry.file.(*DirEntriesFile); ok {
			entry.file = nil
		}
	}

	// If a snapshot already exists (from a prior cookie=0 call), use it
	// instead of re-reading from the host.
	if entry.readdirSnapshot == nil {
		// Cache listing on first call for preopens or any directory fd.
		if entry.dirFile == nil {
			if entry.preopen {
				if entry.mount < 0 || entry.mount >= len(s.mounts) {
					return wasiEBadf
				}
				if entry.file == nil {
					if d, ok := s.mounts[entry.mount].root.(fs.ReadDirFS); ok {
						entries, err := d.ReadDir(".")
						if err != nil {
							return wasiEIo
						}
						entry.file = &DirEntriesFile{Entries: entries}
					}
				}
			}
			if entry.file == nil {
				return wasiEBadf
			}
			var df fs.ReadDirFile
			switch f := entry.file.(type) {
			case fs.ReadDirFile:
				df = f
			case *FSFileWrap:
				df, _ = f.File.(fs.ReadDirFile)
			}
			if df == nil {
				return wasiENotDir
			}
			entry.dirFile = df
		}

		// Seek back to the start before reading: ReadDir(-1) returns all
		// entries from the current file offset. A non-zero offset would
		// produce a truncated listing.
		if seeker, ok := entry.file.(io.Seeker); ok {
			// os.File (writable host paths) always supports seeking;
			// a Seek error on those is a genuine fault.  FSFileWrap
			// may fail Seek when the underlying fs.FS doesn't support
			// it — that is benign for ReadDir.
			if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr != nil {
				if _, isWrap := entry.file.(*FSFileWrap); !isWrap {
					return wasiEIo
				}
			}
		}
		rawEntries, err := entry.dirFile.ReadDir(-1)
		if err != nil && err != io.EOF {
			return wasiEIo
		}

		// Restore entries to the file if it's our own DirEntriesFile adapter,
		// so that subsequent calls with cookies can still access them.
		if de, ok := entry.file.(*DirEntriesFile); ok {
			de.idx = 0
		}

		// Prepend synthetic . and .. entries and store the full listing
		// as the per-fd snapshot.
		entries := make([]fs.DirEntry, 0, 2+len(rawEntries))
		entries = append(entries, synthDirEntry{"."}, synthDirEntry{".."})
		entries = append(entries, rawEntries...)
		entry.readdirSnapshot = entries
	}

	entries := entry.readdirSnapshot

	// Pre-compute inodes for synthetic . and .. entries.
	var selfIno, parentIno uint64
	if m := entry.mount; m >= 0 && m < len(s.mounts) && s.mounts[m].writable && s.mounts[m].hostRoot != "" {
		if entry.preopen {
			if fi, err := os.Stat(s.mounts[m].hostRoot); err == nil {
				_, selfIno = statDevIno(fi)
			}
			if fi, err := os.Stat(filepath.Dir(s.mounts[m].hostRoot)); err == nil {
				_, parentIno = statDevIno(fi)
			}
		} else {
			if fi, err := entry.file.Stat(); err == nil {
				_, selfIno = statDevIno(fi)
			}
			parentPath := filepath.Dir(filepath.Join(s.mounts[m].hostRoot, entry.path))
			if fi, err := os.Stat(parentPath); err == nil {
				_, parentIno = statDevIno(fi)
			}
		}
	}

	if int(cookie) >= len(entries) {
		binary.LittleEndian.PutUint32(mem[bufUsedPtr:], 0)
		return wasiESuccess
	}
	var bufUsed uint32
	var i int
	for i = int(cookie); i < len(entries); i++ {
		name := entries[i].Name()
		var ftype byte
		if entries[i].IsDir() {
			ftype = fdDir
		} else {
			ftype = fdFile
		}
		nameLen := uint32(len(name))
		entryLen := uint32(24 + nameLen)
		if bufUsed+entryLen > uint32(bufLen) {
			break
		}
		off := bufPtr + int32(bufUsed)
		binary.LittleEndian.PutUint64(mem[off:], uint64(i+1))
		var ino uint64
		switch i {
		case 0:
			// Synthetic "." entry: use the directory's own inode.
			ino = selfIno
		case 1:
			// Synthetic ".." entry: use the parent directory inode.
			ino = parentIno
		default:
			// Real entries: extract ino from DirEntry.Info().Sys() when available.
			if info, err := entries[i].Info(); err == nil {
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					ino = st.Ino
				}
			}
		}
		binary.LittleEndian.PutUint64(mem[off+8:], ino)
		binary.LittleEndian.PutUint32(mem[off+16:], nameLen)
		binary.LittleEndian.PutUint32(mem[off+20:], uint32(ftype))
		copy(mem[off+24:], name)
		bufUsed += entryLen
	}
	// WASI cookie-based resume: when more entries remain but the last one did
	// not fit in buf, report bufLen in bufUsed. The caller detects this and
	// resumes via the cookie (otherwise it would think the directory is done).
	if i < len(entries) && bufUsed > 0 && bufUsed < uint32(bufLen) {
		bufUsed = uint32(bufLen)
	}
	binary.LittleEndian.PutUint32(mem[bufUsedPtr:], bufUsed)
	return wasiESuccess
}

// Xfd_filestat_get implements fd_filestat_get. Writes a 64-byte filestat
// struct for the open fd. For preopen directory fds, stats the mount root
// via fs.Stat.
func (s *State) Xfd_filestat_get(fd, bufPtr int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.preopen {
		if entry.mount < 0 || entry.mount >= len(s.mounts) {
			return wasiEBadf
		}
		mnt := s.mounts[entry.mount]
		if mnt.writable && mnt.hostRoot != "" {
			hostFi, err := os.Stat(mnt.hostRoot)
			if err != nil {
				return wasiEIo
			}
			dev, ino := statDevIno(hostFi)
			writeFilestat(s.mem(), bufPtr, fdDir, hostFi.Size(), hostFi.ModTime().UnixNano(), dev, ino)
		} else {
			fi, err := fs.Stat(mnt.root, ".")
			if err != nil {
				return wasiEIo
			}
			writeFilestat(s.mem(), bufPtr, fdDir, fi.Size(), fi.ModTime().UnixNano(), 0, 0)
		}
		return wasiESuccess
	}
	if entry.file == nil {
		return wasiEBadf
	}
	fi, err := entry.file.Stat()
	if err != nil {
		return wasiEIo
	}
	dev, ino := statDevIno(fi)
	writeFilestat(s.mem(), bufPtr, entry.fdType, fi.Size(), fi.ModTime().UnixNano(), dev, ino)
	return wasiESuccess
}

// Xpath_filestat_get implements path_filestat_get. Resolves the path and
// writes a 64-byte filestat struct. On writable host-backed mounts, when
// SYMLINK_FOLLOW is absent the final component is examined with Lstat-like
// semantics (a symlink is reported as filetype symbolic link); when it is
// set, symlinks in the final component are followed (Stat). Both paths try
// the host filesystem first, then fall back to fs.Stat on the overlay for
// embedded-only files. Read-only mounts use fs.Stat on the mount root only.
// Writable host directory preopens return ENOTCAPABLE for mount-relative paths
// that lexically escape the preopen (same check as path_open and resolveWritable).
func (s *State) Xpath_filestat_get(dirfd, flags, pathPtr, pathLen, bufPtr int32) int32 {
	mount, relPath := s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if mount == nil {
		return wasiENoEnt
	}

	var fi fs.FileInfo
	var err error
	if mount.writable && mount.hostRoot != "" {
		if s.preopenDirfdLexicallyEscapes(dirfd, relPath) {
			return wasiENotCap
		}
		if flags&wasiLookupSymlinkFollow == 0 {
			hostPath := filepath.Join(mount.hostRoot, filepath.FromSlash(relPath))
			fi, err = statHostPathOrOverlay(hostPath, mount.root, relPath, false)
		} else {
			hostPath, errno := joinWritableHostPathForLookup(mount.hostRoot, relPath, flags)
			if errno != wasiESuccess {
				return errno
			}
			fi, err = statHostPathOrOverlay(hostPath, mount.root, relPath, true)
		}
		if err != nil {
			return wasiENoEnt
		}
	} else {
		fi, err = fs.Stat(mount.root, relPath)
		if err != nil {
			return wasiENoEnt
		}
	}
	dev, ino := statDevIno(fi)
	writeFilestat(s.mem(), bufPtr, filestatFdTypeFromInfo(fi), fi.Size(), fi.ModTime().UnixNano(), dev, ino)
	return wasiESuccess
}

// fileRightsForOpen returns rights_base and rights_inheriting for a regular file
// opened via path_open. rights_inheriting is always parentInh.
// req is rights_base already intersected with parentInh; rights_base is then
// the intersection of rightsRegularFile, parentInh, and the effective read/write
// intent from req (WASI preview1: read-only and write-only opens collapse to a single bit).
func fileRightsForOpen(parentInh, req uint64) (base uint64, inheriting uint64) {
	inheriting = parentInh
	maxFile := rightsRegularFile & parentInh
	effective := req
	switch req & (rightFDRead | rightFDWrite) {
	case rightFDRead | rightFDWrite:
		effective = rightsRegularFile
	case rightFDRead:
		effective = rightFDRead
	case rightFDWrite:
		effective = rightFDWrite
	}
	return effective & maxFile, inheriting
}

// pathOpenStoredRights returns the rights_base and rights_inheriting actually
// stored for an fd created by path_open. fdRightsBase and fdRightsInheriting are
// clamped to parentInh so bits the parent cannot pass on are dropped without
// failing the open. Regular files are further reduced via fileRightsForOpen.
// Directories have FD_SEEK stripped because seek/tell are not defined on them.
func pathOpenStoredRights(parentInh uint64, openedType byte, fdRightsBase, fdRightsInheriting int64) (base uint64, inheriting uint64) {
	base = uint64(fdRightsBase) & parentInh
	inheriting = uint64(fdRightsInheriting) & parentInh
	switch openedType {
	case fdFile:
		base, inheriting = fileRightsForOpen(parentInh, base)
	case fdDir:
		base &^= rightFDSeek
	}
	return base, inheriting
}

// Xpath_open implements path_open. Resolves the guest path and allocates
// a new fd. For writable mounts, opens via os.OpenFile using oflags and
// fdRightsBase to determine OS open flags; falls back to the overlay
// fs.FS for read-only opens that do not create or truncate. When O_DIRECTORY
// is set on a writable host-backed path, an existing non-directory returns
// ENOTDIR before open. Trailing slashes in the guest path are preserved on
// the host path so the OS returns ENOTDIR when the final component is a file.
// The special path "/dev/null" is handled as a character-device fd without
// mount resolution. Absolute guest paths stored on directory fds enable
// correct nested resolution during directory recursion. fd_rights_base and
// fd_rights_inheriting are clamped to the directory fd's rights_inheriting
// when recording the new fd (bits outside that mask are dropped). Other
// checks still return ENOTCAPABLE, e.g. write on a read-only mount or sandbox
// escape on a preopen directory.
//
// FS open errors (overlay fallback after a missing host file, embedded fs.FS
// mounts, and read-only preopens) are passed through [mapOSError], so well-known
// errors such as permission denied map to the appropriate WASI errno rather than
// a single ENOENT for every failure.
func (s *State) Xpath_open(dirfd int32, lookupFlags int32, pathPtr int32, pathLen int32, oflags int32, fdRightsBase int64, fdRightsInheriting int64, fdFlags int32, fdPtr int32) int32 {
	s.assertSingleOwner()
	pathBytes := s.readBytes(pathPtr, pathLen)
	for _, b := range pathBytes {
		if b == 0 {
			return wasiEInval
		}
	}
	guestPath := string(pathBytes)
	mem := s.mem()
	if guestPath == "/dev/null" {
		fd := s.allocFD()
		s.fds[fd] = fdEntry{fdType: fdCharDev, path: "/dev/null", rightsBase: rightsCharDev, rightsInheriting: 0}
		binary.LittleEndian.PutUint32(mem[fdPtr:], uint32(fd))
		return wasiESuccess
	}
	mount, relPath := s.resolveDirfdPath(dirfd, pathPtr, pathLen)
	if mount == nil {
		if entry, ok := s.fdEntry(dirfd); ok && entry.fdType == fdFile && !strings.HasPrefix(guestPath, "/") {
			return wasiENotDir
		}
		if s.isNonPreopenDirfd(dirfd) && strings.HasPrefix(guestPath, "/") {
			return wasiEPerm
		}
		return wasiENoEnt
	}
	parentInh := uint64(0)
	if entry, ok := s.fdEntry(dirfd); ok {
		parentInh = entry.rightsInheriting
	}

	if mount.readonly {
		if (uint32(oflags)&(oflagCreat|oflagTrunc)) != 0 || (uint64(fdRightsBase)&rightFDWrite) != 0 {
			return wasiENotCap
		}
	}

	if s.preopenDirfdLexicallyEscapes(dirfd, relPath) {
		return wasiENotCap
	}
	if s.nonPreopenDirfdResolvedPathEscapes(dirfd, mount, relPath) {
		return wasiENotCap
	}
	var f fs.File
	var err error

	if mount.writable && mount.hostRoot != "" {
		hostPath, errno := joinWritableHostPathForLookup(mount.hostRoot, relPath, lookupFlags)
		if errno != wasiESuccess {
			return errno
		}
		wantDirectory := uint32(oflags)&oflagDir != 0
		if wantDirectory {
			if errno := errnoIfHostPathNotADirectory(hostPath); errno != 0 {
				return errno
			}
		}
		osFlags := os.O_RDONLY
		if (uint64(fdRightsBase)&rightFDWrite) != 0 || (uint32(oflags)&(oflagCreat|oflagTrunc|oflagExcl)) != 0 {
			osFlags = os.O_RDWR
		}
		if uint32(oflags)&oflagCreat != 0 {
			osFlags |= os.O_CREATE
		}
		if uint32(oflags)&oflagTrunc != 0 {
			osFlags |= os.O_TRUNC
		}
		if uint32(oflags)&oflagExcl != 0 {
			osFlags |= os.O_EXCL
		}
		if wantDirectory {
			osFlags = os.O_RDONLY
		}
		// If the guest path ends with a slash, the final component must be a
		// directory. Preserve the trailing slash in the host path so the OS
		// returns ENOTDIR for non-directories.
		if strings.HasSuffix(guestPath, "/") {
			hostPath += string(filepath.Separator)
		}
		hostFile, osErr := os.OpenFile(hostPath, osFlags, 0o666)
		if osErr != nil {
			if uint32(oflags)&(oflagCreat|oflagTrunc|oflagExcl) == 0 &&
				errors.Is(osErr, os.ErrNotExist) {
				f, err = mount.root.Open(relPath)
				if err != nil {
					// Preserve overlay errno (mapOSError); do not collapse to ENOENT.
					return mapOSError(err)
				}
				fi, _ := f.Stat()
				return s.storeOpenedFD(&FSFileWrap{File: f}, mountGuestPath(mount, relPath), fdTypeFromInfo(fi), parentInh, fdRightsBase, fdRightsInheriting, fdFlags, fdPtr)
			}
			return mapOSError(osErr)
		}
		fi, _ := hostFile.Stat()
		return s.storeOpenedFD(&osFile{File: hostFile}, mountGuestPath(mount, relPath), fdTypeFromInfo(fi), parentInh, fdRightsBase, fdRightsInheriting, fdFlags, fdPtr)
	}
	f, err = mount.root.Open(relPath)
	if err != nil {
		// mount.root ([fs.FS]): preserve errno via [mapOSError], same as host opens.
		return mapOSError(err)
	}
	fi, _ := f.Stat()
	return s.storeOpenedFD(&FSFileWrap{File: f}, s.guestAbsPathForFDEntry(dirfd, guestPath), fdTypeFromInfo(fi), parentInh, fdRightsBase, fdRightsInheriting, fdFlags, fdPtr)
}

// Xpath_rename implements path_rename. Resolves both old and new paths
// and calls os.Rename. Returns EROFS if either mount is read-only or if
// the resolved paths are not beneath a writable preopen root.
func (s *State) Xpath_rename(oldDirfd, oldPathPtr, oldPathLen, newDirfd, newPathPtr, newPathLen int32) int32 {
	oldMount, _ := s.resolveDirfdPath(oldDirfd, oldPathPtr, oldPathLen)
	newMount, _ := s.resolveDirfdPath(newDirfd, newPathPtr, newPathLen)
	if oldMount == nil || newMount == nil {
		return wasiENoEnt
	}

	oldPath, oldErr := s.resolveWritable(oldDirfd, oldPathPtr, oldPathLen)
	if oldErr != wasiESuccess {
		return oldErr
	}
	newPath, newErr := s.resolveWritable(newDirfd, newPathPtr, newPathLen)
	if newErr != wasiESuccess {
		return newErr
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// Xpoll_oneoff implements poll_oneoff. Clock subscriptions (event type 0)
// are handled by sleeping for the shortest requested timeout nanoseconds.
// fd_read and fd_write subscriptions (event types 1 and 2) validate fd
// existence but do not model actual I/O readiness. A real readiness model
// would require async I/O infrastructure beyond this synchronous host's
// scope.
func (s *State) Xpoll_oneoff(inPtr int32, outPtr int32, nsubscriptions int32, neventsPtr int32) int32 {
	s.assertSingleOwner()
	mem := s.mem()
	var minTimeout int64 = -1
	for i := int32(0); i < nsubscriptions; i++ {
		subOff := inPtr + i*48
		userdata := binary.LittleEndian.Uint64(mem[subOff:])
		eventType := binary.LittleEndian.Uint32(mem[subOff+40:])
		var errno uint32 = 0
		switch eventType {
		case 0: // clock
			timeout := int64(binary.LittleEndian.Uint64(mem[subOff+8+8:]))
			if timeout > 0 && (minTimeout < 0 || timeout < minTimeout) {
				minTimeout = timeout
			}
		case 1: // fd_read
			fd := int32(binary.LittleEndian.Uint32(mem[subOff+8:]))
			if fd < 0 || fd >= int32(len(s.fds)) {
				errno = uint32(wasiEBadf)
			}
		case 2: // fd_write
			fd := int32(binary.LittleEndian.Uint32(mem[subOff+8:]))
			if fd < 0 || fd >= int32(len(s.fds)) {
				errno = uint32(wasiEBadf)
			}
		}
		evOff := outPtr + i*32
		binary.LittleEndian.PutUint64(mem[evOff:], userdata)
		binary.LittleEndian.PutUint16(mem[evOff+8:], uint16(errno))
		binary.LittleEndian.PutUint16(mem[evOff+10:], 0)
		binary.LittleEndian.PutUint32(mem[evOff+12:], eventType)
		binary.LittleEndian.PutUint64(mem[evOff+16:], 0)
		binary.LittleEndian.PutUint64(mem[evOff+24:], 0)
	}
	if minTimeout > 0 {
		time.Sleep(time.Duration(minTimeout))
	}
	binary.LittleEndian.PutUint32(mem[neventsPtr:], uint32(nsubscriptions))
	return wasiESuccess
}

// Xcall_host_function implements the env.call_host_function import used
// by zeroperl-style wasm2go modules as a host-callback bridge. This host
// does not support guest-initiated host callbacks; it always returns 0.
func (s *State) Xcall_host_function(v0, v1, v2 int32) int32 { return 0 }

// writeStringTableSizes writes the item count and total buffer size
// (sum of len(item)+1 for each item) to countPtr and bufSizePtr in mem.
// Shared by environ_sizes_get and args_sizes_get.
func writeStringTableSizes(mem []byte, countPtr, bufSizePtr int32, items []string) {
	binary.LittleEndian.PutUint32(mem[countPtr:], uint32(len(items)))
	var total uint32
	for _, s := range items {
		total += uint32(len(s)) + 1
	}
	binary.LittleEndian.PutUint32(mem[bufSizePtr:], total)
}

// writeStringTable writes a pointer array at ptrBase and null-terminated
// strings packed at bufBase into mem. Each pointer is a uint32 offset into
// mem pointing to the start of the corresponding string. Shared by
// environ_get and args_get.
func writeStringTable(mem []byte, ptrBase, bufBase int32, items []string) {
	bufOff := uint32(bufBase)
	for i, s := range items {
		binary.LittleEndian.PutUint32(mem[ptrBase+int32(i*4):], bufOff)
		n := copy(mem[bufOff:], s)
		mem[bufOff+uint32(n)] = 0
		bufOff += uint32(n) + 1
	}
}

// Xfd_pread implements fd_pread. Reads from fd at the given offset
// without updating the fd's WASI-level offset (entry.offset). Requires
// the underlying file to implement ReadAt; if it does not, no bytes are
// read and ENOTSUP is returned. Returns EINVAL for fds 0-2 (positioned
// reads on stdio are not defined by WASI). Returns EISDIR for directory
// fds, ENOTCAPABLE when FD_READ is not set in the fd's rights_base.
func (s *State) Xfd_pread(fd, iovsPtr, iovsCount int32, offset int64, nreadPtr int32) int32 {
	if fd <= 2 {
		return wasiEInval
	}
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDRead); errno != 0 {
		return errno
	}
	mem := s.mem()
	var total uint32
	curOff := offset
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		ra, ok := entry.file.(interface {
			ReadAt([]byte, int64) (int, error)
		})
		if !ok {
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return wasiENotSup
		}
		n, err := ra.ReadAt(mem[bufPtr:bufPtr+bufLen], curOff)
		total += uint32(n)
		curOff += int64(n)
		if err != nil {
			if err == io.EOF {
				break
			}
			binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
			return wasiEIo
		}
	}
	binary.LittleEndian.PutUint32(mem[nreadPtr:], total)
	return wasiESuccess
}

// Xfd_pwrite implements fd_pwrite. Writes to fd at the given offset
// without updating the fd's WASI-level offset (entry.offset). Requires
// the underlying file to implement WriteAt; if it does not, no bytes are
// written and ENOTSUP is returned. If WriteAt returns an error, fd_pwrite
// returns EIO and preserves the partial byte count in guest memory.
// Returns EINVAL for fds 0-2 (positioned writes on stdio are not
// defined by WASI). Returns EISDIR for directory fds, ENOTCAPABLE when
// FD_WRITE is not set in the fd's rights_base.
func (s *State) Xfd_pwrite(fd, iovsPtr, iovsCount int32, offset int64, nwrittenPtr int32) int32 {
	if fd <= 2 {
		return wasiEInval
	}
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDWrite); errno != 0 {
		return errno
	}

	mem := s.mem()
	var total uint32
	curOff := offset
	for i := int32(0); i < iovsCount; i++ {
		off := iovsPtr + i*8
		bufPtr := int32(binary.LittleEndian.Uint32(mem[off:]))
		bufLen := int32(binary.LittleEndian.Uint32(mem[off+4:]))
		if bufLen == 0 {
			continue
		}
		wa, ok := entry.file.(interface {
			WriteAt([]byte, int64) (int, error)
		})
		if !ok {
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
			return wasiENotSup
		}

		n, err := wa.WriteAt(mem[bufPtr:bufPtr+bufLen], curOff)
		total += uint32(n)
		curOff += int64(n)
		if err != nil {
			binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
			return wasiEIo
		}
	}
	binary.LittleEndian.PutUint32(mem[nwrittenPtr:], total)
	return wasiESuccess
}

// Xfd_tell implements fd_tell. Returns the WASI-level fd offset
// (entry.offset) rather than the kernel file position. This is necessary
// because fd_write uses WriteAt, which does not advance the kernel
// position; reading it back via Seek(0, SeekCurrent) would return a
// stale value. Returns EISDIR for directory fds.
func (s *State) Xfd_tell(fd, offsetPtr int32) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	binary.LittleEndian.PutUint64(s.mem()[offsetPtr:], uint64(entry.offset))
	return wasiESuccess
}

// Xsched_yield implements sched_yield. This host is synchronous;
// yielding calls the [runtime.Gosched] seam and returns ESUCCESS.
func (s *State) Xsched_yield() int32 {
	schedYield()
	return wasiESuccess
}

// Xfd_datasync implements fd_datasync. Validates that fd is a valid
// open file descriptor index. For sync-capable fds (those whose underlying
// file implements a Sync() error method, such as osFile), invokes host Sync
// and maps any error. For other files, returns ESUCCESS without mutation.
func (s *State) Xfd_datasync(fd int32) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil && entry.fdType == 0 {
		return wasiEBadf
	}
	if syncer, ok := entry.file.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			return mapOSError(err)
		}
	}
	return wasiESuccess
}

// Xfd_sync implements fd_sync. Always returns ESUCCESS.
func (s *State) Xfd_sync(fd int32) int32 {
	return wasiESuccess
}

// Xfd_fdstat_set_flags implements fd_fdstat_set_flags.
// Supported flags: APPEND, DSYNC, NONBLOCK, RSYNC, SYNC.
func (s *State) Xfd_fdstat_set_flags(fd, flags int32) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := &s.fds[fd]
	if entry.file == nil && entry.fdType == 0 {
		return wasiEBadf
	}
	// WASI only allows setting these 5 flags.
	if (flags & ^(fdFlagsAppend | fdFlagsDSync | fdFlagsNonBlock | fdFlagsRSync | fdFlagsSync)) != 0 {
		return wasiEInval
	}
	entry.fdFlags = uint16(flags)
	return wasiESuccess
}

// Xfd_advise implements fd_advise (POSIX posix_fadvise). Always returns
// ESUCCESS. There is no portable Go equivalent for this hint; it is
// silently ignored.
func (s *State) Xfd_advise(fd int32, offset, length int64, advice int32) int32 { return wasiESuccess }

// Xfd_allocate implements fd_allocate (fallocate). Returns EISDIR for
// directory fds. For regular files, disk-space pre-reservation is advisory;
// this is an intentional no-op.
func (s *State) Xfd_allocate(fd int32, offset, length int64) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	return wasiESuccess
}

func (s *State) Xfd_fdstat_set_rights(fd int32, base, inheriting int64) int32 {
	s.assertSingleOwner()
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := &s.fds[fd]
	if entry.file == nil && entry.fdType == 0 {
		return wasiEBadf
	}
	uBase := uint64(base)
	uInheriting := uint64(inheriting)
	if (uBase & ^entry.rightsBase) != 0 || (uInheriting & ^entry.rightsInheriting) != 0 {
		return wasiENotCap
	}
	entry.rightsBase = uBase
	entry.rightsInheriting = uInheriting
	return wasiESuccess
}

// Xproc_raise implements proc_raise. Always returns ENOSYS. Raising a
// signal inside a WASM guest has no meaningful host mapping.
func (s *State) Xproc_raise(signal int32) int32 { return wasiENoSys }

// Xsock_accept, Xsock_recv, Xsock_send, and Xsock_shutdown implement the
// WASI socket functions. All return ENOSYS; sockets are not supported in
// this host.
func (s *State) Xsock_accept(fd, flags, resultPtr int32) int32 { return wasiENoSys }
func (s *State) Xsock_recv(fd, iovsPtr, iovsLen, riFlags, nreadPtr, roFlagsPtr int32) int32 {
	return wasiENoSys
}
func (s *State) Xsock_send(fd, iovsPtr, iovsLen, siFlags, nsentPtr int32) int32 { return wasiENoSys }
func (s *State) Xsock_shutdown(fd, how int32) int32                             { return wasiENoSys }

// Xfd_filestat_set_size implements fd_filestat_set_size. Returns EISDIR
// for directory fds. For osFile-backed fds, truncates the file to size bytes
// via (*os.File).Truncate when FD_FILESTAT_SET_SIZE is set in rights_base;
// otherwise returns ENOTCAPABLE. For fs.FS-backed fds, returns ESUCCESS
// without mutation (embedded files are read-only by construction).
func (s *State) Xfd_filestat_set_size(fd int32, size int64) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	if errno := errnoForDirectoryFDOp(entry); errno != 0 {
		return errno
	}
	if of, ok := entry.file.(*osFile); ok {
		if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDFilestatSetSize); errno != 0 {
			return errno
		}
		if err := of.Truncate(size); err != nil {
			return mapOSError(err)
		}
	}
	return wasiESuccess
}

// Xfd_filestat_set_times implements fd_filestat_set_times.
//
// For osFile-backed fds, calls os.Chtimes with the specified nanosecond
// values. Honors ATIM, MTIM, ATIM_NOW, and MTIM_NOW flags. Rejects
// contradictory flag combinations (ATIM together with ATIM_NOW, or MTIM
// together with MTIM_NOW) with EINVAL. For fs.FS-backed fds, returns
// ESUCCESS without mutation.
func (s *State) Xfd_filestat_set_times(fd int32, atim, mtim int64, fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstMtim|fstAtimNow|fstMtimNow) == 0 {
		return wasiESuccess
	}
	if errno := errnoIfContradictoryFstFlags(fstFlags); errno != 0 {
		return errno
	}
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.file == nil {
		return wasiEBadf
	}
	of, ok := entry.file.(*osFile)
	if !ok {
		return wasiESuccess
	}

	fi, err := of.Stat()
	if err != nil {
		return mapOSError(err)
	}

	targetAtim, targetMtim := computeTargetTimes(fi, atim, mtim, fstFlags)

	if err := os.Chtimes(of.Name(), targetAtim, targetMtim); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

func computeTargetTimes(fi fs.FileInfo, atim, mtim int64, fstFlags int32) (time.Time, time.Time) {
	targetAtim := getAtimeFromStat(fi)
	if fstFlags&fstAtimNow != 0 {
		targetAtim = time.Now()
	} else if fstFlags&fstAtim != 0 {
		targetAtim = time.Unix(0, atim)
	}

	targetMtim := fi.ModTime()
	if fstFlags&fstMtimNow != 0 {
		targetMtim = time.Now()
	} else if fstFlags&fstMtim != 0 {
		targetMtim = time.Unix(0, mtim)
	}
	return targetAtim, targetMtim
}

// Xpath_filestat_set_times implements path_filestat_set_times.
//
// ATIM (bit 0), ATIM_NOW (bit 1), MTIM (bit 2), and MTIM_NOW (bit 3) flags
// are acted upon. Rejects contradictory flag combinations (ATIM together with
// ATIM_NOW, or MTIM together with MTIM_NOW) with EINVAL. Resolves paths with
// resolveWritable (including ENOTCAPABLE when a directory preopen path
// lexically escapes the mount) and calls os.Chtimes. Returns EROFS when the
// path is read-only or cannot be resolved to a writable host path.
func (s *State) Xpath_filestat_set_times(dirfd, flags, pathPtr, pathLen int32, atim, mtim int64, fstFlags int32) int32 {
	if fstFlags&(fstAtim|fstMtim|fstAtimNow|fstMtimNow) == 0 {
		return wasiESuccess
	}
	if errno := errnoIfContradictoryFstFlags(fstFlags); errno != 0 {
		return errno
	}
	primary, werrno := s.resolveWritable(dirfd, pathPtr, pathLen)
	if werrno != wasiESuccess {
		return werrno
	}

	fi, err := os.Stat(primary)
	if err != nil {
		return mapOSError(err)
	}

	targetAtim, targetMtim := computeTargetTimes(fi, atim, mtim, fstFlags)

	if err := os.Chtimes(primary, targetAtim, targetMtim); err != nil {
		return mapOSError(err)
	}
	return wasiESuccess
}

// mapOSError returns the closest WASI snapshot-preview1 errno for a host
// error from os/syscall (including *os.PathError and wrapped errors).
// Errors from [fs.FS] Open are supported as well: the standard library uses
// the same sentinels for fs and os (e.g. [fs.ErrNotExist] == [os.ErrNotExist],
// [fs.ErrPermission] == [os.ErrPermission]), so [errors.Is] matches both.
//
// Mappings use [errors.Is] against well-known errors:
//
//   - [os.ErrNotExist] → ENOENT (44)
//   - [syscall.ENOTEMPTY] → ENOTEMPTY (55)
//   - [os.ErrExist] → EEXIST (20)
//   - [syscall.ENOTDIR] → ENOTDIR (54)
//   - [syscall.EISDIR] → EISDIR (31)
//   - [syscall.EACCES] → EACCES (2)
//   - [syscall.EPERM] → EPERM (63)
//   - [os.ErrPermission] → EACCES (2)
//   - [syscall.EROFS] → EROFS (66)
//   - [syscall.EXDEV] → EXDEV (75)
//   - [syscall.EINVAL] → EINVAL (28)
//
// Any other error maps to EIO (29).
func mapOSError(err error) int32 {
	if errors.Is(err, os.ErrNotExist) {
		return wasiENoEnt
	}
	if errors.Is(err, syscall.ENOTEMPTY) {
		return wasiENotEmpty
	}
	if errors.Is(err, os.ErrExist) {
		return wasiEExist
	}
	if errors.Is(err, syscall.ENOTDIR) {
		return wasiENotDir
	}
	if errors.Is(err, syscall.EISDIR) {
		return wasiEIsdir
	}
	if errors.Is(err, syscall.EACCES) {
		return wasiEAcces
	}
	if errors.Is(err, syscall.EPERM) {
		return wasiEPerm
	}
	if errors.Is(err, os.ErrPermission) {
		return wasiEAcces
	}
	if errors.Is(err, syscall.EROFS) {
		return wasiEROFS
	}
	if errors.Is(err, syscall.EXDEV) {
		return wasiEXdev
	}
	if errors.Is(err, syscall.EINVAL) {
		return wasiEInval
	}
	return wasiEIo
}
