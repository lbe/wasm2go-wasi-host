package wasihost

import "runtime"

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
	wasiENotSock  int32 = 57
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
	rightFDDatasync           uint64 = 1 << 0  // __WASI_RIGHT_FD_DATASYNC
	rightFDRead               uint64 = 1 << 1  // __WASI_RIGHT_FD_READ
	rightFDSeek               uint64 = 1 << 2  // __WASI_RIGHT_FD_SEEK
	rightFDFdstatSetFlags     uint64 = 1 << 3  // __WASI_RIGHT_FD_FDSTAT_SET_FLAGS
	rightFDSync               uint64 = 1 << 4  // __WASI_RIGHT_FD_SYNC
	rightFDTell               uint64 = 1 << 5  // __WASI_RIGHT_FD_TELL
	rightFDWrite              uint64 = 1 << 6  // __WASI_RIGHT_FD_WRITE
	rightFDAdvise             uint64 = 1 << 7  // __WASI_RIGHT_FD_ADVISE
	rightFDAllocate           uint64 = 1 << 8  // __WASI_RIGHT_FD_ALLOCATE
	rightPathCreateDirectory  uint64 = 1 << 9  // __WASI_RIGHT_PATH_CREATE_DIRECTORY
	rightPathCreateFile       uint64 = 1 << 10 // __WASI_RIGHT_PATH_CREATE_FILE
	rightPathLinkSource       uint64 = 1 << 11 // __WASI_RIGHT_PATH_LINK_SOURCE
	rightPathLinkTarget       uint64 = 1 << 12 // __WASI_RIGHT_PATH_LINK_TARGET
	rightPathOpen             uint64 = 1 << 13 // __WASI_RIGHT_PATH_OPEN
	rightFDReaddir            uint64 = 1 << 14 // __WASI_RIGHT_FD_READDIR
	rightPathReadlink         uint64 = 1 << 15 // __WASI_RIGHT_PATH_READLINK
	rightPathRenameSource     uint64 = 1 << 16 // __WASI_RIGHT_PATH_RENAME_SOURCE
	rightPathRenameTarget     uint64 = 1 << 17 // __WASI_RIGHT_PATH_RENAME_TARGET
	rightPathFilestatGet      uint64 = 1 << 18 // __WASI_RIGHT_PATH_FILESTAT_GET
	rightPathFilestatSetSize  uint64 = 1 << 19 // __WASI_RIGHT_PATH_FILESTAT_SET_SIZE
	rightPathFilestatSetTimes uint64 = 1 << 20 // __WASI_RIGHT_PATH_FILESTAT_SET_TIMES
	rightFDFilestatGet        uint64 = 1 << 21 // __WASI_RIGHT_FD_FILESTAT_GET
	rightFDFilestatSetSize    uint64 = 1 << 22 // __WASI_RIGHT_FD_FILESTAT_SET_SIZE
	rightFDFilestatSetTimes   uint64 = 1 << 23 // __WASI_RIGHT_FD_FILESTAT_SET_TIMES
	rightPathSymlink          uint64 = 1 << 24 // __WASI_RIGHT_PATH_SYMLINK
	rightPathRemoveDirectory  uint64 = 1 << 25 // __WASI_RIGHT_PATH_REMOVE_DIRECTORY
	rightPathUnlinkFile       uint64 = 1 << 26 // __WASI_RIGHT_PATH_UNLINK_FILE
	rightPollFDReadwrite      uint64 = 1 << 27 // __WASI_RIGHT_POLL_FD_READWRITE

	// rightsDirectoryInherited is every __wasi_rights_t bit from 9 through 26 (path and fd
	// capabilities in that range per snapshot-preview1). Used as rights_inheriting for writable dir preopens.
	rightsDirectoryInherited uint64 = rightPathCreateDirectory | rightPathCreateFile | rightPathLinkSource | rightPathLinkTarget |
		rightPathOpen | rightFDReaddir | rightPathReadlink |
		rightPathRenameSource | rightPathRenameTarget |
		rightPathFilestatGet | rightPathFilestatSetSize | rightPathFilestatSetTimes |
		rightFDFilestatGet | rightFDFilestatSetSize | rightFDFilestatSetTimes |
		rightPathSymlink | rightPathRemoveDirectory | rightPathUnlinkFile
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
	// Writable directory preopen fdstat masks (path_open_preopen.rs directory_base_rights /
	// directory_inheriting_rights). Base is path bits 9–26 except FD_FILESTAT_SET_SIZE (bit 22),
	// including PATH_FILESTAT_SET_SIZE (bit 19). Inheriting adds fd_* superset for path_open clamping.
	rightsWritableDirPreopenBase       = rightsDirectoryInherited&^rightFDFilestatSetSize | rightPathFilestatSetSize
	rightsWritableDirPreopenInheriting = (rightsDirectoryInherited &^ rightPathFilestatSetSize) |
		rightFDRead | rightFDSeek | rightFDFdstatSetFlags | rightFDWrite |
		rightFDDatasync | rightFDSync | rightFDTell | rightFDAdvise | rightFDAllocate |
		rightFDFilestatSetSize | rightPollFDReadwrite
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

// WASI snapshot-preview1 filestat layout (64 bytes).
const (
	filestatDevOff   = 0
	filestatInoOff   = 8
	filestatTypeOff  = 16
	filestatNlinkOff = 24
	filestatSizeOff  = 32
	filestatAtimOff  = 40
	filestatMtimOff  = 48
	filestatCtimOff  = 56
	filestatSize     = 64
)

// writeFilestat writes a 64-byte WASI filestat struct at bufPtr in mem.
// fdType is a WASI snapshot-preview1 filetype tag (same values as fdstat.fs_filetype),
// e.g. fdDir, fdFile, fdSymlink.

const (
	StdinFD  = 0
	StdoutFD = 1
	StderrFD = 2
)
