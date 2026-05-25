package wasihost

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

const (
	subscriptionSize = 48
	eventSize        = 32
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

// Xpoll_oneoff implements poll_oneoff. This function processes subscriptions
// and writes output events. The memory layout is as follows:
//
//	Subscription (48 bytes):
//	  - 0:   userdata (uint64)
//	  - 8:   tag (u8): 0=clock, 1=fd_read, 2=fd_write
//	  - 9:   padding (7 bytes)
//	  - 16:  fd (uint32, for fd_read/fd_write) or clock.id
//	  - 20:  padding (4 bytes)
//	  - 24:  timeout (uint64, for clock events)
//	  - 32:  precision (uint64, for clock events)
//	  - 40:  flags (uint16, for clock events)
//	  - 42:  padding (6 bytes)
//
//	Output Event (32 bytes):
//	  - 0:   userdata (uint64)
//	  - 8:   error (uint16)
//	  - 10:  event_type (uint8) - stored as single byte per WASI spec
//	  - 11:  padding (uint8)
//	  - 12:  event-specific data:
//	    * For clock: timeout (uint64)
//	    * For fd_read/fd_write: fd (uint16)
//	  - 14:  padding (uint16 for fd events, or part of clock padding)
//	  - 16:  padding (uint64 for clock events to fill 32 bytes)
//	  - 24:  padding (uint64)
//
// Note: The event_type is stored as a uint8 at offset +10, not as a uint32
// at offset +12. This is a common pitfall and was a source of bugs.
//
// Clock subscriptions (event type 0) are handled by sleeping for the shortest
// requested timeout nanoseconds. fd_read and fd_write subscriptions (event
// types 1 and 2) validate fd existence but do not model actual I/O readiness.
// A real readiness model would require async I/O infrastructure beyond this
// synchronous host's scope.
func (s *State) Xpoll_oneoff(inPtr int32, outPtr int32, nsubscriptions int32, neventsPtr int32) int32 {
	// Define offsets for subscription structure
	// The WASI spec layout (used by the Rust wasi 0.11.0 crate and the wasm binary):
	//   Offset  Size  Field
	//   0       8     userdata (u64)
	//   8       1     tag (u8) - event type: 0=clock, 1=fd_read, 2=fd_write
	//   9       7     padding
	//   16      4     clock.id / fd_read.file_descriptor (u32)
	//   20      4     padding
	//   24      8     clock.timeout (u64)
	//   32      8     clock.precision (u64)
	//   40      2     clock.flags (u16)
	//   42      6     padding
	const (
		subOffsetUserdata = 0
		subOffsetTag      = 8  // tag (u8) - event type: 0=clock, 1=fd_read, 2=fd_write
		subOffsetFD       = 16 // file_descriptor (u32) for fd_read/fd_write, or clock.id
		subOffsetTimeout  = 24 // timeout (u64) for clock subscriptions
	)

	// Event type constants (used with u8 tag at subOffsetTag)
	const (
		evtTypeClock   = 0
		evtTypeFdRead  = 1
		evtTypeFdWrite = 2
	)

	s.assertSingleOwner()
	mem := s.mem()

	// ---- Pass 1: emit all ready fd_read / fd_write events immediately (no sleep) ----
	// Also collect clock subscription data for a second pass.

	eventCount := 0
	currentOutPtr := outPtr
	fdWriteEmitted := false

	// Collect clock subscription info: timeout, userdata, subOffset per index.
	type clockSub struct {
		subOffset int32
		userdata  uint64
		timeout   int64
	}
	var clockSubs []clockSub
	var minClockTimeout int64 = -1

	for idx := int32(0); idx < nsubscriptions; idx++ {
		subOffset := inPtr + idx*subscriptionSize
		eventType := int32(mem[subOffset+subOffsetTag])
		userdata := binary.LittleEndian.Uint64(mem[subOffset+subOffsetUserdata:])

		switch eventType {
		case evtTypeFdRead, evtTypeFdWrite:
			fd := int32(binary.LittleEndian.Uint32(mem[subOffset+subOffsetFD:]))
			var errno uint32 = 0
			if fd < 0 || fd >= int32(len(s.fds)) {
				errno = uint32(wasiEBadf)
			}
			// Write event immediately
			writePollOneoffEvent(mem, currentOutPtr, userdata, uint16(errno), uint8(eventType), fd)
			currentOutPtr += eventSize
			eventCount++
			if eventType == evtTypeFdWrite {
				fdWriteEmitted = true
			}

		case evtTypeClock:
			timeout := int64(binary.LittleEndian.Uint64(mem[subOffset+subOffsetTimeout:]))
			clockSubs = append(clockSubs, clockSub{subOffset: subOffset, userdata: userdata, timeout: timeout})
			if minClockTimeout < 0 || timeout < minClockTimeout {
				minClockTimeout = timeout
			}
		}
	}

	// ---- Pass 2: handle clock subscriptions ----
	// Rule: if FD_WRITE was emitted, skip clocks with timeout > 0 (guest drains writable in a loop;
	// clock fires on a later poll_oneoff). FD_READ does NOT suppress clocks.

	if fdWriteEmitted {
		// Emit only clocks with timeout == 0 (poll without blocking).
		// Skip clocks with timeout > 0 — they will fire on a later call.
		for _, cs := range clockSubs {
			if cs.timeout == 0 {
				writePollOneoffEvent(mem, currentOutPtr, cs.userdata, 0, uint8(evtTypeClock), int32(cs.timeout))
				currentOutPtr += eventSize
				eventCount++
			}
		}
	} else {
		// Clock-only or fd_read+clock case.
		// Sleep for the minimum positive timeout among clock subscriptions that will fire.
		if minClockTimeout > 0 {
			time.Sleep(time.Duration(minClockTimeout))
		}
		// Emit clocks whose timeout equals minClockTimeout.
		// (If minClockTimeout == 0 or < 0, no sleep is needed; 0-timeout clocks are always ready.)
		for _, cs := range clockSubs {
			if minClockTimeout >= 0 && cs.timeout == minClockTimeout {
				writePollOneoffEvent(mem, currentOutPtr, cs.userdata, 0, uint8(evtTypeClock), int32(cs.timeout))
				currentOutPtr += eventSize
				eventCount++
			}
		}
	}

	// Write the number of events that actually fired.
	binary.LittleEndian.PutUint32(mem[neventsPtr:], uint32(eventCount))
	return wasiESuccess
}

// writePollOneoffEvent writes a single poll_oneoff output event to mem at evtOffset.
// For clock events, fdOrTimeout is the timeout value (written at evtOffset+12 as uint64).
// For fd_read/fd_write events, fdOrTimeout is the file descriptor (written as uint16).
func writePollOneoffEvent(mem []byte, evtOffset int32, userdata uint64, errno uint16, eventType uint8, fdOrTimeout int32) {
	const (
		evtOffsetUserdata      = 0
		evtOffsetErrno         = 8
		evtOffsetEventTypeByte = 10
		evtOffsetPadding1      = 11
		evtOffsetEventData     = 12
		evtOffsetPadding2      = 20
		evtOffsetPadding3      = 24
	)

	binary.LittleEndian.PutUint64(mem[evtOffset+evtOffsetUserdata:], userdata)
	binary.LittleEndian.PutUint16(mem[evtOffset+evtOffsetErrno:], errno)
	mem[evtOffset+evtOffsetEventTypeByte] = eventType
	mem[evtOffset+evtOffsetPadding1] = 0

	switch eventType {
	case 0: // clock: store timeout as uint64
		binary.LittleEndian.PutUint64(mem[evtOffset+evtOffsetEventData:], uint64(fdOrTimeout))
		binary.LittleEndian.PutUint64(mem[evtOffset+evtOffsetPadding2:], 0)
	case 1, 2: // fd_read/fd_write: store fd as uint16
		binary.LittleEndian.PutUint16(mem[evtOffset+evtOffsetEventData:], uint16(fdOrTimeout))
		binary.LittleEndian.PutUint16(mem[evtOffset+evtOffsetPadding2:], 0)
	}
	// Zero out the remaining bytes (from +24 to +32)
	binary.LittleEndian.PutUint64(mem[evtOffset+evtOffsetPadding3:], 0)
}

// Xcall_host_function implements the env.call_host_function import used
// by zeroperl-style wasm2go modules as a host-callback bridge. This host
// does not support guest-initiated host callbacks; it always returns 0.
func (s *State) Xcall_host_function(v0, v1, v2 int32) int32 { return 0 }

// Xsched_yield implements sched_yield. This host is synchronous;
// yielding calls the [runtime.Gosched] seam and returns ESUCCESS.
func (s *State) Xsched_yield() int32 {
	schedYield()
	return wasiESuccess
}

// Xproc_raise implements proc_raise. Always returns ENOSYS. Raising a
// signal inside a WASM guest has no meaningful host mapping.
func (s *State) Xproc_raise(signal int32) int32 { return wasiENoSys }

// Xsock_accept, Xsock_recv, Xsock_send, and Xsock_shutdown implement the
// WASI socket functions. Sockets are not supported in this host: accept,
// recv, and send return ENOSYS; shutdown returns EBADF for invalid/unused
// fds and ENOTSOCK for valid non-socket fds.
func (s *State) Xsock_accept(fd, flags, resultPtr int32) int32 { return wasiENoSys }

func (s *State) Xsock_recv(fd, iovsPtr, iovsLen, riFlags, nreadPtr, roFlagsPtr int32) int32 {
	return wasiENoSys
}

func (s *State) Xsock_send(fd, iovsPtr, iovsLen, siFlags, nsentPtr int32) int32 { return wasiENoSys }

func (s *State) Xsock_shutdown(fd, how int32) int32 {
	if fd < 0 || int(fd) >= len(s.fds) {
		return wasiEBadf
	}
	entry := s.fds[fd]
	if entry.isUnused() {
		return wasiEBadf
	}
	// WASI snapshot-preview1 socket file types are 10 (SOCKET_STREAM)
	// and 11 (SOCKET_DGRAM). If the fd is valid but not a socket, return
	// ENOTSOCK. No socket fds are ever created in this host, so all
	// valid fds reach this path.
	return wasiENotSock
}

// Xfd_filestat_set_size implements fd_filestat_set_size. Returns EISDIR
// for directory fds. For osFile-backed fds, truncates the file to size bytes
// via (*os.File).Truncate when FD_FILESTAT_SET_SIZE is set in rights_base;
// otherwise returns ENOTCAPABLE. For fs.FS-backed fds, returns ESUCCESS
// without mutation (embedded files are read-only by construction).
