package wasihost

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

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

// Xfd_renumber implements fd_renumber. Renumbers the file descriptor
// from fd_from to fd_to. If fd_to is open, it is closed first. After this
// operation, fd_from is invalid and fd_to refers to the same open file
// description as fd_from used to.
func (s *State) Xfd_renumber(fd_from, fd_to int32) int32 {
	s.assertSingleOwner()
	// Validate fd_from and fd_to are within range.
	if fd_from < 0 || fd_from >= int32(len(s.fds)) {
		return wasiEBadf
	}
	if fd_to < 0 || fd_to >= int32(len(s.fds)) {
		return wasiEBadf
	}
	// If fd_from is unused, it's invalid.
	if s.fds[fd_from].isUnused() {
		return wasiEBadf
	}
	// If fd_from == fd_to, return error (no-op would close the fd).
	if fd_from == fd_to {
		return wasiEBadf
	}
	// If fd_to is invalidated, return EBADF.
	if s.invalidated[int(fd_to)] {
		return wasiEBadf
	}
	// If fd_to is open (not unused), close it first.
	if !s.fds[fd_to].isUnused() {
		// Close the destination fd if it's open.
		if s.fds[fd_to].file != nil {
			s.fds[fd_to].file.Close()
		}
		// Clear the destination entry.
		s.fds[fd_to] = fdEntry{}
	}
	// Copy the source entry to the destination.
	s.fds[fd_to] = s.fds[fd_from]
	// Invalidate the source entry.
	s.fds[fd_from] = fdEntry{}
	// Mark the source as invalidated so future renumber attempts to it fail.
	s.invalidated[int(fd_from)] = true
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

// Xargs_sizes_get implements args_sizes_get. Writes the argument count
// and total buffer size (including null terminators) to guest memory.
func (s *State) Xargs_sizes_get(argcPtr, argvSizePtr int32) int32 {
	writeStringTableSizes(s.mem(), argcPtr, argvSizePtr, s.args)
	return wasiESuccess
}

// Xargs_get implements args_get. Writes a pointer array at argvPtr and
// the corresponding null-terminated argument strings packed at argvBufPtr
// into guest memory. Uses the same layout as environ_get.

// Xargs_get implements args_get. Writes a pointer array at argvPtr and
// the corresponding null-terminated argument strings packed at argvBufPtr
// into guest memory. Uses the same layout as environ_get.
func (s *State) Xargs_get(argvPtr, argvBufPtr int32) int32 {
	writeStringTable(s.mem(), argvPtr, argvBufPtr, s.args)
	return wasiESuccess
}

// readBytes reads length bytes from guest memory starting at ptr.
// Returns nil if ptr or length is zero.

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

// Xcall_host_function implements the env.call_host_function import used
// by zeroperl-style wasm2go modules as a host-callback bridge. This host
// does not support guest-initiated host callbacks; it always returns 0.
func (s *State) Xcall_host_function(v0, v1, v2 int32) int32 { return 0 }

// writeStringTableSizes writes the item count and total buffer size
// (sum of len(item)+1 for each item) to countPtr and bufSizePtr in mem.
// Shared by environ_sizes_get and args_sizes_get.

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

// Xproc_raise implements proc_raise. Always returns ENOSYS. Raising a
// signal inside a WASM guest has no meaningful host mapping.
func (s *State) Xproc_raise(signal int32) int32 { return wasiENoSys }

// Xsock_accept, Xsock_recv, Xsock_send, and Xsock_shutdown implement the
// WASI socket functions. All return ENOSYS; sockets are not supported in
// this host.

// Xsock_accept, Xsock_recv, Xsock_send, and Xsock_shutdown implement the
// WASI socket functions. All return ENOSYS; sockets are not supported in
// this host.
func (s *State) Xsock_accept(fd, flags, resultPtr int32) int32 { return wasiENoSys }

func (s *State) Xsock_recv(fd, iovsPtr, iovsLen, riFlags, nreadPtr, roFlagsPtr int32) int32 {
	return wasiENoSys
}

func (s *State) Xsock_send(fd, iovsPtr, iovsLen, siFlags, nsentPtr int32) int32 { return wasiENoSys }

func (s *State) Xsock_shutdown(fd, how int32) int32 { return wasiENoSys }

// Xfd_filestat_set_size implements fd_filestat_set_size. Returns EISDIR
// for directory fds. For osFile-backed fds, truncates the file to size bytes
// via (*os.File).Truncate when FD_FILESTAT_SET_SIZE is set in rights_base;
// otherwise returns ENOTCAPABLE. For fs.FS-backed fds, returns ESUCCESS
// without mutation (embedded files are read-only by construction).
