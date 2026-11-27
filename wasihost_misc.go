package wasihost

import (
	"encoding/binary"
	"io"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

const (
	subscriptionSize = 48
	eventSize        = 32
)

// Xenviron_sizes_get implements environ_sizes_get. Writes the count of
// configured environment strings and their total buffer size (including
// null terminators) to the respective pointers in guest memory.
func (s *State) Xenviron_sizes_get(countPtr, bufSizePtr int32) int32 {
	if s.ctx == nil {
		writeStringTableSizes(s.mem(), countPtr, bufSizePtr, nil)
		return wasiESuccess
	}
	writeStringTableSizes(s.mem(), countPtr, bufSizePtr, bytesToStrings(s.ctx.Environ()))
	return wasiESuccess
}

func (s *State) Xenviron_get(envPtr, envBufPtr int32) int32 {
	if s.ctx == nil {
		return wasiESuccess
	}
	writeStringTable(s.mem(), envPtr, envBufPtr, bytesToStrings(s.ctx.Environ()))
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
	pathLen := uint32(len(fileEntryGuestPath(entry)))
	binary.LittleEndian.PutUint32(mem[prestatPtr:], 0)
	binary.LittleEndian.PutUint32(mem[prestatPtr+4:], pathLen)
	return wasiESuccess
}

func (s *State) Xfd_prestat_dir_name(fd, pathPtr, pathLen int32) int32 {
	entry, ok := s.preopenEntryByFD(fd)
	if !ok {
		return wasiEBadf
	}
	name := fileEntryGuestPath(entry)
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
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	fdType := byte(fdCharDev)
	if fd > StderrFD {
		var errno int32
		fdType, errno = fileEntryFdType(fe)
		if errno != 0 {
			return errno
		}
	}
	writeFdstat(s.mem(), statPtr, fdType, fileEntryFdFlags(fe), fe.RightsBase, fe.RightsInheriting)
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
	if fd_from == fd_to {
		return wasiEBadf
	}
	if _, ok := s.lookupFD(fd_from); !ok {
		return wasiEBadf
	}
	if s.ctx == nil || s.ctx.FSContext() == nil {
		return wasiEBadf
	}
	werrno := s.ctx.FSContext().Renumber(fd_from, fd_to)
	if werrno != 0 {
		return errnoFromWasi(werrno)
	}
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
	if s.ctx == nil {
		return wasiESuccess
	}
	_, err := io.ReadFull(s.ctx.RandSource(), mem[bufPtr:bufPtr+bufLen])
	if err != nil {
		return wasiEIo
	}
	return wasiESuccess
}

func (s *State) Xclock_time_get(clockID int32, precision int64, resultPtr int32) int32 {
	mem := s.mem()
	if s.ctx == nil {
		return wasiENoSys
	}
	switch clockID {
	case 0:
		sec, nsec := s.ctx.Walltime()
		binary.LittleEndian.PutUint64(mem[resultPtr:], uint64(sec*1e9+int64(nsec)))
		return wasiESuccess
	case 1:
		binary.LittleEndian.PutUint64(mem[resultPtr:], uint64(s.ctx.Nanotime()))
		return wasiESuccess
	default:
		return wasiENoSys
	}
}

func (s *State) Xclock_res_get(clockID int32, resultPtr int32) int32 {
	switch clockID {
	case 0, 1:
		binary.LittleEndian.PutUint64(s.mem()[resultPtr:], 1)
		return wasiESuccess
	default:
		return wasiENoSys
	}
}

func (s *State) Xargs_sizes_get(argcPtr, argvSizePtr int32) int32 {
	if s.ctx == nil {
		writeStringTableSizes(s.mem(), argcPtr, argvSizePtr, nil)
		return wasiESuccess
	}
	writeStringTableSizes(s.mem(), argcPtr, argvSizePtr, bytesToStrings(s.ctx.Args()))
	return wasiESuccess
}

func (s *State) Xargs_get(argvPtr, argvBufPtr int32) int32 {
	if s.ctx == nil {
		return wasiESuccess
	}
	writeStringTable(s.mem(), argvPtr, argvBufPtr, bytesToStrings(s.ctx.Args()))
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
//	  - 10:  event_type (uint8), one byte per the WASI spec
//	  - 11:  padding (uint8)
//	  - 12:  event-specific data:
//	    * For clock: timeout (uint64)
//	    * For fd_read/fd_write: fd (uint16)
//	  - 14:  padding (uint16 for fd events, or part of clock padding)
//	  - 16:  padding (uint64 for clock events to fill 32 bytes)
//	  - 24:  padding (uint64)
//
// The event_type is a uint8 at offset +10, not a uint32 at offset +12.
// Getting this wrong caused bugs here before.
//
// Clock subscriptions sleep for the shortest requested timeout. fd_read and
// fd_write subscriptions call Poll with a zero timeout on PollableFile or
// Pollable fds. When Poll reports not ready, no event is emitted. Other open
// fds emit an event at once. When an fd_write event is emitted, clocks with a
// positive timeout are skipped.
func (s *State) Xpoll_oneoff(inPtr int32, outPtr int32, nsubscriptions int32, neventsPtr int32) int32 {
	const (
		subOffsetUserdata = 0
		subOffsetTag      = 8 // 0=clock, 1=fd_read, 2=fd_write
		subOffsetFD       = 16
		subOffsetTimeout  = 24
	)

	// Event type constants (used with u8 tag at subOffsetTag)
	const (
		evtTypeClock   = 0
		evtTypeFdRead  = 1
		evtTypeFdWrite = 2
	)

	s.assertSingleOwner()
	mem := s.mem()

	// Pass 1: emit every ready fd_read and fd_write event without sleeping.
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
			pflag := wasi.POLLIN
			if eventType == evtTypeFdWrite {
				pflag = wasi.POLLOUT
			}
			fe, ok := s.lookupFD(fd)
			var fePtr *wasi.FileEntry
			if ok {
				fePtr = fe
			}
			nextOut, wrote := pollOneoffProcessFdSubscription(mem, currentOutPtr, uint8(eventType), userdata, fd, fePtr, pflag)
			currentOutPtr = nextOut
			if wrote {
				eventCount++
				if eventType == evtTypeFdWrite {
					fdWriteEmitted = true
				}
			}

		case evtTypeClock:
			timeout := int64(binary.LittleEndian.Uint64(mem[subOffset+subOffsetTimeout:]))
			clockSubs = append(clockSubs, clockSub{subOffset: subOffset, userdata: userdata, timeout: timeout})
			if minClockTimeout < 0 || timeout < minClockTimeout {
				minClockTimeout = timeout
			}
		}
	}

	// Pass 2: handle clock subscriptions.
	// If an FD_WRITE event was emitted, skip clocks with a timeout above 0. The
	// guest writes in a loop, and the clock fires on a later poll_oneoff.
	// FD_READ events do not suppress clocks.

	if fdWriteEmitted {
		// Emit only clocks with timeout == 0 (poll without blocking).
		// Skip clocks with a timeout above 0. They fire on a later call.
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
			if s.ctx != nil {
				_ = s.ctx.Nanosleep(minClockTimeout)
			}
		}
		// Emit clocks whose timeout equals minClockTimeout.
		// When minClockTimeout is 0 or less, no sleep is needed. Clocks with a
		// 0 timeout are always ready.
		for _, cs := range clockSubs {
			if minClockTimeout >= 0 && cs.timeout == minClockTimeout {
				writePollOneoffEvent(mem, currentOutPtr, cs.userdata, 0, uint8(evtTypeClock), int32(cs.timeout))
				currentOutPtr += eventSize
				eventCount++
			}
		}
	}

	// Write the number of events that fired.
	binary.LittleEndian.PutUint32(mem[neventsPtr:], uint32(eventCount))
	return wasiESuccess
}

// pollOneoffProcessFdSubscription handles one fd_read or fd_write subscription.
// It consults Poll when the file is Pollable; otherwise it emits if the fd is open.
func pollOneoffProcessFdSubscription(mem []byte, outPtr int32, eventType uint8, userdata uint64, fd int32, fe *wasi.FileEntry, pflag wasi.Pflag) (nextOutPtr int32, wrote bool) {
	if fe == nil {
		writePollOneoffEvent(mem, outPtr, userdata, uint16(wasiEBadf), eventType, fd)
		return outPtr + eventSize, true
	}
	if pf, ok := fe.File.(wasi.PollableFile); ok {
		return pollOneoffEmitAfterPoll(mem, outPtr, eventType, userdata, fd, pf, pflag)
	}
	if pollable, ok := fe.File.(wasi.Pollable); ok {
		return pollOneoffEmitAfterPoll(mem, outPtr, eventType, userdata, fd, pollable, pflag)
	}
	writePollOneoffEvent(mem, outPtr, userdata, 0, eventType, fd)
	return outPtr + eventSize, true
}

func pollOneoffEmitAfterPoll(mem []byte, outPtr int32, eventType uint8, userdata uint64, fd int32, p wasi.Pollable, pflag wasi.Pflag) (nextOutPtr int32, wrote bool) {
	ready, werrno := p.Poll(pflag, 0)
	if werrno != 0 {
		writePollOneoffEvent(mem, outPtr, userdata, uint16(errnoFromWasi(werrno)), eventType, fd)
		return outPtr + eventSize, true
	}
	if ready {
		writePollOneoffEvent(mem, outPtr, userdata, 0, eventType, fd)
		return outPtr + eventSize, true
	}
	return outPtr, false
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
	// Zero bytes +24 to +32.
	binary.LittleEndian.PutUint64(mem[evtOffset+evtOffsetPadding3:], 0)
}

// Xcall_host_function implements the env.call_host_function import used
// by zeroperl-style wasm2go modules as a host-callback bridge. This host
// does not support guest-initiated host callbacks; it always returns 0.
func (s *State) Xcall_host_function(v0, v1, v2 int32) int32 { return 0 }

// Xsched_yield implements sched_yield. This host is synchronous. It calls
// ctx.Osyield, which defaults to runtime.Gosched, and returns ESUCCESS.
func (s *State) Xsched_yield() int32 {
	if s.ctx == nil {
		return wasiESuccess
	}
	_ = s.ctx.Osyield()
	return wasiESuccess
}

// Xproc_raise implements proc_raise. Always returns ENOSYS. Raising a
// signal inside a WASM guest has no host equivalent.
func (s *State) Xproc_raise(signal int32) int32 { return wasiENoSys }

// Xsock_accept, Xsock_recv, Xsock_send, and Xsock_shutdown implement the
// WASI socket functions. This host does not support sockets. accept, recv
// and send return ENOSYS. shutdown returns EBADF for an fd that is not open
// and ENOTSOCK for any open fd.
func (s *State) Xsock_accept(fd, flags, resultPtr int32) int32 { return wasiENoSys }

func (s *State) Xsock_recv(fd, iovsPtr, iovsLen, riFlags, nreadPtr, roFlagsPtr int32) int32 {
	return wasiENoSys
}

func (s *State) Xsock_send(fd, iovsPtr, iovsLen, siFlags, nsentPtr int32) int32 { return wasiENoSys }

func (s *State) Xsock_shutdown(fd, how int32) int32 {
	fe, ok := s.lookupFD(fd)
	if !ok || fe.File == nil {
		return wasiEBadf
	}
	return wasiENotSock
}
