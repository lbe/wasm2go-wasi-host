package wasihost

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestPollOneoffFdReadStdin(t *testing.T) {
	// Acceptance criteria:
	// - fd_read on fd=0 alone — nevents==1, errno SUCCESS, type FD_READ, userdata preserved.
	// - Clock 1ns + fd_read fd=0 — nevents==2, one FD_READ and one CLOCK with respective userdata.
	// - go test -run TestPollOneoffFdReadStdin -count=1 should pass.

	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 2000
		subSize    = 48
		evtSize    = 32
	)

	// Create a buffer and state with stdin set to an empty reader.
	buf := make([]byte, 65536)
	s := New(func() []byte { return buf }, WithStdin(strings.NewReader("")))

	t.Run("fd_read on fd=0 alone", func(t *testing.T) {
		userdata := uint64(111)
		fd := uint32(0)

		// Write subscription at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 1) // tag: fd_read
		binary.LittleEndian.PutUint32(buf[inPtr+16:], fd)

		// Call Xpoll_oneoff
		errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Verify nevents
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		// Verify output memory bounds
		if outPtr < 0 || outPtr+evtSize > int32(len(buf)) {
			t.Fatalf("outPtr out of bounds")
		}

		// Verify error (uint16 at outPtr+8)
		errCode := binary.LittleEndian.Uint16(buf[outPtr+8 : outPtr+10])
		if errCode != 0 {
			t.Errorf("event errno = %d, want 0 (SUCCESS)", errCode)
		}

		// Verify event type byte at outPtr+10 should be 1 (EVENTTYPE_FD_READ)
		eventTypeByte := buf[outPtr+10]
		if eventTypeByte != 1 {
			t.Errorf("eventType byte = %d, want 1", eventTypeByte)
		}

		// Verify userdata at outPtr matches subscription userdata
		outUserdata := binary.LittleEndian.Uint64(buf[outPtr : outPtr+8])
		if outUserdata != userdata {
			t.Errorf("userdata = %d, want %d", outUserdata, userdata)
		}
	})

	// Test case: clock 1ns + fd_read fd=0
	t.Run("clock 1ns + fd_read fd=0", func(t *testing.T) {
		// Reset buffer
		for i := range buf {
			buf[i] = 0
		}

		userdataClock := uint64(222)
		userdataFdRead := uint64(333)
		timeout := uint64(1) // 1 nanosecond

		// Subscription 0: clock, 1ns
		binary.LittleEndian.PutUint64(buf[inPtr:], userdataClock)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 0)        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0)       // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], timeout) // timeout

		// Subscription 1: fd_read on fd=0
		binary.LittleEndian.PutUint64(buf[inPtr+subSize:], userdataFdRead)
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+8:], 1)  // tag: fd_read
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+16:], 0) // fd=0

		// Call Xpoll_oneoff with 2 subscriptions
		errno := s.Xpoll_oneoff(inPtr, outPtr, 2, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Verify nevents == 2
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 2 {
			t.Errorf("nevents = %d, want 2", nevents)
		}

		// Event order: fd_read is emitted first (before any clock sleep),
		// then clock. Per plan rule: fd events first, then clocks.
		// Verify event 0 (fd_read) at outPtr
		outUserdata0 := binary.LittleEndian.Uint64(buf[outPtr:])
		if outUserdata0 != userdataFdRead {
			t.Errorf("event 0 userdata = 0x%x, want 0x%x", outUserdata0, userdataFdRead)
		}
		eventType0 := buf[outPtr+10]
		if eventType0 != 1 {
			t.Errorf("event 0 type = %d, want 1 (FD_READ)", eventType0)
		}

		// Verify event 1 (clock) at outPtr + evtSize
		outUserdata1 := binary.LittleEndian.Uint64(buf[outPtr+evtSize:])
		if outUserdata1 != userdataClock {
			t.Errorf("event 1 userdata = 0x%x, want 0x%x", outUserdata1, userdataClock)
		}
		eventType1 := buf[outPtr+evtSize+10]
		if eventType1 != 0 {
			t.Errorf("event 1 type = %d, want 0 (CLOCK)", eventType1)
		}
	})
}
