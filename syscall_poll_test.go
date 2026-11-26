package wasihost

import (
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func TestPollOneoffEventLayout(t *testing.T) {
	const (
		inPtr      = 100
		outPtr     = 200
		neventsPtr = 300
		subSize    = 48
		evtSize    = 32
	)

	s, buf := newTestState(nil)

	t.Run("clock event", func(t *testing.T) {
		// Setup a clock subscription
		userdata := uint64(12345)
		timeout := uint64(1) // 1 nanosecond timeout

		// Write subscription at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 0)        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0)       // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], timeout) // timeout

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
			t.Errorf("error code = %d, want 0", errCode)
		}

		// Verify event type byte at outPtr+10 should be 0 (EVENTTYPE_CLOCK)
		eventTypeByte := buf[outPtr+10]
		if eventTypeByte != 0 {
			t.Errorf("eventType byte = %d, want 0", eventTypeByte)
		}

		// Verify userdata at outPtr matches subscription userdata
		outUserdata := binary.LittleEndian.Uint64(buf[outPtr : outPtr+8])
		if outUserdata != userdata {
			t.Errorf("userdata = %d, want %d", outUserdata, userdata)
		}

		// Additional check: event type should NOT be stored as uint32 at outPtr+12
		storedEventType := binary.LittleEndian.Uint32(buf[outPtr+12 : outPtr+16])
		if storedEventType == 0 {
			t.Errorf("event type should not be stored as uint32 at outPtr+12, but found 0")
		}
	})

	t.Run("fd_read event", func(t *testing.T) {
		// Setup an fd_read subscription on fd 0
		userdata := uint64(67890)
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
			t.Errorf("error code = %d, want 0", errCode)
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

		// Additional check: event type should NOT be stored as uint32 at outPtr+12
		storedEventType := binary.LittleEndian.Uint32(buf[outPtr+12 : outPtr+16])
		if storedEventType == 1 {
			t.Errorf("event type should not be stored as uint32 at outPtr+12, but found 1")
		}
	})

	t.Run("fd_write event", func(t *testing.T) {
		// Setup an fd_write subscription on fd 1
		userdata := uint64(13579)
		fd := uint32(1)

		// Write subscription at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 2) // tag: fd_write
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
			t.Errorf("error code = %d, want 0", errCode)
		}

		// Verify event type byte at outPtr+10 should be 2 (EVENTTYPE_FD_WRITE)
		eventTypeByte := buf[outPtr+10]
		if eventTypeByte != 2 {
			t.Errorf("eventType byte = %d, want 2", eventTypeByte)
		}

		// Verify userdata at outPtr matches subscription userdata
		outUserdata := binary.LittleEndian.Uint64(buf[outPtr : outPtr+8])
		if outUserdata != userdata {
			t.Errorf("userdata = %d, want %d", outUserdata, userdata)
		}

		// Additional check: event type should NOT be stored as uint32 at outPtr+12
		storedEventType := binary.LittleEndian.Uint32(buf[outPtr+12 : outPtr+16])
		if storedEventType == 2 {
			t.Errorf("event type should not be stored as uint32 at outPtr+12, but found 2")
		}
	})
}

func TestPollOneoffNeventsPacked(t *testing.T) {
	t.Parallel()
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 2000
		subSize    = 48
		evtSize    = 32
	)

	s, buf := newTestState(nil)

	t.Run("one clock subscription", func(t *testing.T) {
		// Setup a clock subscription
		userdata := uint64(12345)
		timeout := uint64(1) // 1 nanosecond timeout

		// Write subscription at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 0)        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0)       // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], timeout) // timeout

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

		// Verify only one event is written: outPtr..outPtr+31 should be modified,
		// and the region outPtr+32..outPtr+63 should remain unchanged (zero).
		// Since buffer is initially zero, we expect outPtr+32 to be zero.
		// If the implementation incorrectly writes a second event, this will be non-zero.
		if outPtr+32 < int32(len(buf)) {
			// Check the userdata at outPtr+32 is zero (indicating no second event)
			secondUserdata := binary.LittleEndian.Uint64(buf[outPtr+32:])
			if secondUserdata != 0 {
				t.Errorf("unexpected data at outPtr+32: got 0x%x, want 0 (no second event)", secondUserdata)
			}
		}
	})

	t.Run("two clocks with different timeouts", func(t *testing.T) {
		// Subscription 0: clock, 1ns timeout, userdata=0x1111111111111111
		userdata0 := uint64(0x1111111111111111)
		timeout0 := uint64(1)
		// Subscription 1: clock, 1000ns timeout, userdata=0x2222222222222222
		userdata1 := uint64(0x2222222222222222)
		timeout1 := uint64(1000)

		// Write subscription 0 at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata0)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 0)         // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0)        // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], timeout0) // timeout

		// Write subscription 1 at inPtr + subSize
		binary.LittleEndian.PutUint64(buf[inPtr+subSize:], userdata1)
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+8:], 0)         // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+subSize+16:], 0)        // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+subSize+24:], timeout1) // timeout

		// Call Xpoll_oneoff
		errno := s.Xpoll_oneoff(inPtr, outPtr, 2, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Verify nevents should be 1 (only the 1ns timeout fired)
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		// Verify the event at outPtr has userdata0
		outUserdata := binary.LittleEndian.Uint64(buf[outPtr:])
		if outUserdata != userdata0 {
			t.Errorf("event userdata = 0x%x, want 0x%x", outUserdata, userdata0)
		}

		// Verify no second event was written: outPtr+32 should be zero
		if outPtr+32 < int32(len(buf)) {
			secondUserdata := binary.LittleEndian.Uint64(buf[outPtr+32:])
			if secondUserdata != 0 {
				t.Errorf("unexpected data at outPtr+32: got 0x%x, want 0 (no second event)", secondUserdata)
			}
		}
	})

	t.Run("two clocks both 1ns with distinct userdata", func(t *testing.T) {
		// Both subscriptions have 1ns timeout, so both should fire.
		userdata0 := uint64(0x3333333333333333)
		userdata1 := uint64(0x4444444444444444)
		timeout := uint64(1)

		// Subscription 0
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata0)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 0)        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0)       // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], timeout) // timeout

		// Subscription 1
		binary.LittleEndian.PutUint64(buf[inPtr+subSize:], userdata1)
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+8:], 0)        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+subSize+16:], 0)       // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+subSize+24:], timeout) // timeout

		// Call Xpoll_oneoff
		errno := s.Xpoll_oneoff(inPtr, outPtr, 2, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Verify nevents == 2
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 2 {
			t.Errorf("nevents = %d, want 2", nevents)
		}

		// Verify event 0 at outPtr
		outUserdata0 := binary.LittleEndian.Uint64(buf[outPtr:])
		if outUserdata0 != userdata0 {
			t.Errorf("event 0 userdata = 0x%x, want 0x%x", outUserdata0, userdata0)
		}

		// Verify event 1 at outPtr + evtSize
		outUserdata1 := binary.LittleEndian.Uint64(buf[outPtr+evtSize:])
		if outUserdata1 != userdata1 {
			t.Errorf("event 1 userdata = 0x%x, want 0x%x", outUserdata1, userdata1)
		}

		// Verify that no extra events beyond the first nevents are written.
		// The memory after outPtr+2*evtSize should remain zero.
		if outPtr+2*evtSize < int32(len(buf)) {
			thirdUserdata := binary.LittleEndian.Uint64(buf[outPtr+2*evtSize:])
			if thirdUserdata != 0 {
				t.Errorf("unexpected data at outPtr+64: got 0x%x, want 0 (no third event)", thirdUserdata)
			}
		}
	})
}

func TestPollOneoffClock(t *testing.T) {
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 300
		subSize    = 48
		evtSize    = 32
	)

	s, buf := newTestState(nil)

	t.Run("two clocks with different timeouts respect minimum timeout", func(t *testing.T) {
		// Subscription 0: clock, 1ms timeout, userdata=0x1111111111111111
		userdata0 := uint64(0x1111111111111111)
		timeout0 := uint64(1000000) // 1ms in nanoseconds
		// Subscription 1: clock, 5ms timeout, userdata=0x2222222222222222
		userdata1 := uint64(0x2222222222222222)
		timeout1 := uint64(5000000) // 5ms in nanoseconds

		// Write subscription 0 at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata0)
		buf[inPtr+8] = 0                                        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0)        // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], timeout0) // timeout

		// Write subscription 1 at inPtr + subSize
		binary.LittleEndian.PutUint64(buf[inPtr+subSize:], userdata1)
		buf[inPtr+subSize+8] = 0                                        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+subSize+16:], 0)        // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+subSize+24:], timeout1) // timeout

		// Measure time before and after Xpoll_oneoff
		start := time.Now()
		errno := s.Xpoll_oneoff(inPtr, outPtr, 2, neventsPtr)
		elapsed := time.Since(start)

		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Verify nevents should be 1 (only the 1ms timeout fired)
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		// Verify the event at outPtr has userdata0
		outUserdata := binary.LittleEndian.Uint64(buf[outPtr:])
		if outUserdata != userdata0 {
			t.Errorf("event userdata = 0x%x, want 0x%x", outUserdata, userdata0)
		}

		// CRITICAL CHECK: Verify that the function actually slept for at least the minimum timeout
		minTimeout := 1 * time.Millisecond
		if elapsed < minTimeout {
			t.Errorf("Xpoll_oneoff returned too early: elapsed = %v, want at least %v", elapsed, minTimeout)
		}
	})
}

func TestPollOneoffFdWriteStdio(t *testing.T) {
	t.Parallel()
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 300
		subSize    = 48
		evtSize    = 32
	)

	s, buf := newTestState(nil)

	// Test case: fd_write on stdout (fd=1) and stderr (fd=2) with a concurrent clock timeout
	// The fd_write events should be ready immediately and should be returned before the clock
	// times out. No clock event should be returned.

	t.Run("fd_write on stdout and stderr alone", func(t *testing.T) {
		// Subscription 0: fd_write on fd=1 (stdout), userdata=0xAAAA
		userdata1 := uint64(0xAAAA)
		fd1 := uint32(1)

		// Write subscription at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata1)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 2) // tag: fd_write
		binary.LittleEndian.PutUint32(buf[inPtr+16:], fd1)

		// Call Xpoll_oneoff
		errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Verify nevents == 1
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		// Verify the event is FD_WRITE type
		evtType := buf[outPtr+10]
		if evtType != 2 {
			t.Errorf("event type = %d, want 2 (FD_WRITE)", evtType)
		}
	})

	t.Run("fd_write on stderr alone", func(t *testing.T) {
		// Subscription 0: fd_write on fd=2 (stderr), userdata=0xBBBB
		userdata2 := uint64(0xBBBB)
		fd2 := uint32(2)

		// Write subscription at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata2)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 2) // tag: fd_write
		binary.LittleEndian.PutUint32(buf[inPtr+16:], fd2)

		// Call Xpoll_oneoff
		errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Verify nevents == 1
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		// Verify the event is FD_WRITE type
		evtType := buf[outPtr+10]
		if evtType != 2 {
			t.Errorf("event type = %d, want 2 (FD_WRITE)", evtType)
		}
	})

	t.Run("combined: fd_write on stdout and stderr with clock timeout", func(t *testing.T) {
		// Subscription 0: fd_write on fd=1 (stdout), userdata=0xAAAA
		userdata1 := uint64(0xAAAA)
		fd1 := uint32(1)

		// Subscription 1: fd_write on fd=2 (stderr), userdata=0xBBBB
		userdata2 := uint64(0xBBBB)
		fd2 := uint32(2)

		// Subscription 2: clock with 200_000_000 ns timeout, userdata=0xCCCC
		userdataClock := uint64(0xCCCC)
		timeout := uint64(200_000_000)

		// Write subscription 0 (fd_write stdout) at inPtr
		binary.LittleEndian.PutUint64(buf[inPtr:], userdata1)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 2) // tag: fd_write
		binary.LittleEndian.PutUint32(buf[inPtr+16:], fd1)

		// Write subscription 1 (fd_write stderr) at inPtr + subSize
		binary.LittleEndian.PutUint64(buf[inPtr+subSize:], userdata2)
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+8:], 2) // tag: fd_write
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+16:], fd2)

		// Write subscription 2 (clock) at inPtr + 2*subSize
		binary.LittleEndian.PutUint64(buf[inPtr+2*subSize:], userdataClock)
		binary.LittleEndian.PutUint32(buf[inPtr+2*subSize+8:], 0)        // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+2*subSize+16:], 0)       // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+2*subSize+24:], timeout) // timeout

		// Call Xpoll_oneoff and measure wall time
		start := time.Now()
		errno := s.Xpoll_oneoff(inPtr, outPtr, 3, neventsPtr)
		elapsed := time.Since(start)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		// Wall time must be < 50ms (no sleep for the 200ms clock when fd_write is ready)
		if elapsed >= 50*time.Millisecond {
			t.Errorf("Xpoll_oneoff took %v, want < 50ms (slept for clock despite fd_write)", elapsed)
		}

		// Verify subscription 2 tag byte is unchanged (no write-back corruption)
		tag2 := buf[inPtr+2*subSize+8]
		if tag2 != 0 {
			t.Errorf("subscription 2 tag byte = %d, want 0 (clock) after poll", tag2)
		}

		// Verify nevents == 2 (both fd_write events, no clock event)
		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 2 {
			t.Errorf("nevents = %d, want 2", nevents)
		}

		// Verify both events are FD_WRITE type
		evtType1 := buf[outPtr+10]
		if evtType1 != 2 {
			t.Errorf("event 1 type = %d, want 2 (FD_WRITE)", evtType1)
		}

		evtType2 := buf[outPtr+evtSize+10]
		if evtType2 != 2 {
			t.Errorf("event 2 type = %d, want 2 (FD_WRITE)", evtType2)
		}

		// Verify no clock event is present (the third event should not be written)
		if outPtr+2*evtSize < int32(len(buf)) {
			thirdUserdata := binary.LittleEndian.Uint64(buf[outPtr+2*evtSize:])
			if thirdUserdata != 0 {
				t.Errorf("unexpected data at outPtr+64: got 0x%x, want 0 (no third event)", thirdUserdata)
			}
		}
	})
}

func TestPollOneoffInvalidFd(t *testing.T) {
	t.Parallel()
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 2000
		subSize    = 48
		evtSize    = 32
	)

	s, buf := newTestState(nil)

	t.Run("fd_read fd=99 alone", func(t *testing.T) {
		userdata := uint64(0xAAAA)
		invalidFd := uint32(99)

		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 1) // tag: fd_read
		binary.LittleEndian.PutUint32(buf[inPtr+16:], invalidFd)

		errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		errCode := binary.LittleEndian.Uint16(buf[outPtr+8 : outPtr+10])
		if errCode != uint16(wasiEBadf) {
			t.Errorf("event errno = %d, want EBADF (%d)", errCode, wasiEBadf)
		}

		evtType := buf[outPtr+10]
		if evtType != 1 {
			t.Errorf("event type = %d, want 1 (FD_READ)", evtType)
		}

		outUserdata := binary.LittleEndian.Uint64(buf[outPtr : outPtr+8])
		if outUserdata != userdata {
			t.Errorf("userdata = 0x%x, want 0x%x", outUserdata, userdata)
		}
	})

	t.Run("clock 1ns + fd_read fd=99", func(t *testing.T) {
		for i := range buf {
			buf[i] = 0
		}

		userdataClock := uint64(0xBBBB)
		userdataFdRead := uint64(0xCCCC)
		invalidFd := uint32(99)

		binary.LittleEndian.PutUint64(buf[inPtr:], userdataClock)
		binary.LittleEndian.PutUint32(buf[inPtr+8:], 0)  // tag: clock
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0) // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], 1) // timeout: 1ns

		binary.LittleEndian.PutUint64(buf[inPtr+subSize:], userdataFdRead)
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+8:], 1) // tag: fd_read
		binary.LittleEndian.PutUint32(buf[inPtr+subSize+16:], invalidFd)

		errno := s.Xpoll_oneoff(inPtr, outPtr, 2, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 2 {
			t.Errorf("nevents = %d, want 2", nevents)
		}

		// Event 0: fd_read with EBADF (emitted first per plan rule 1)
		outUserdata0 := binary.LittleEndian.Uint64(buf[outPtr:])
		if outUserdata0 != userdataFdRead {
			t.Errorf("event 0 userdata = 0x%x, want 0x%x", outUserdata0, userdataFdRead)
		}
		errCode0 := binary.LittleEndian.Uint16(buf[outPtr+8 : outPtr+10])
		if errCode0 != uint16(wasiEBadf) {
			t.Errorf("event 0 errno = %d, want EBADF (%d)", errCode0, wasiEBadf)
		}
		evtType0 := buf[outPtr+10]
		if evtType0 != 1 {
			t.Errorf("event 0 type = %d, want 1 (FD_READ)", evtType0)
		}

		// Event 1: CLOCK SUCCESS
		outUserdata1 := binary.LittleEndian.Uint64(buf[outPtr+evtSize:])
		if outUserdata1 != userdataClock {
			t.Errorf("event 1 userdata = 0x%x, want 0x%x", outUserdata1, userdataClock)
		}
		errCode1 := binary.LittleEndian.Uint16(buf[outPtr+evtSize+8 : outPtr+evtSize+10])
		if errCode1 != 0 {
			t.Errorf("event 1 errno = %d, want 0 (SUCCESS)", errCode1)
		}
		evtType1 := buf[outPtr+evtSize+10]
		if evtType1 != 0 {
			t.Errorf("event 1 type = %d, want 0 (CLOCK)", evtType1)
		}
	})
}

func TestPollOneoffTagU8(t *testing.T) {
	t.Parallel()
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 2000
	)

	s, buf := newTestState(nil)

	t.Run("fd_read with tag byte at +8 and non-zero padding at +9..11", func(t *testing.T) {
		userdata := uint64(0xDEADBEEF)
		fd := uint32(0)

		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)

		// Write tag as u8 at offset 8, then fill padding bytes 9-11 with non-zero.
		buf[inPtr+8] = 1     // tag: fd_read (u8)
		buf[inPtr+9] = 0xFF  // padding
		buf[inPtr+10] = 0xFF // padding
		buf[inPtr+11] = 0xFF // padding

		binary.LittleEndian.PutUint32(buf[inPtr+16:], fd)

		errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		evtType := buf[outPtr+10]
		if evtType != 1 {
			t.Errorf("event type = %d, want 1 (FD_READ)", evtType)
		}
	})

	t.Run("fd_write with tag byte at +8 and non-zero padding at +9..11", func(t *testing.T) {
		userdata := uint64(0xCAFEBABE)
		fd := uint32(1)

		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
		buf[inPtr+8] = 2 // tag: fd_write (u8)
		buf[inPtr+9] = 0xAA
		buf[inPtr+10] = 0xBB
		buf[inPtr+11] = 0xCC
		binary.LittleEndian.PutUint32(buf[inPtr+16:], fd)

		errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		evtType := buf[outPtr+10]
		if evtType != 2 {
			t.Errorf("event type = %d, want 2 (FD_WRITE)", evtType)
		}
	})

	t.Run("clock with tag byte at +8 and non-zero padding at +9..11", func(t *testing.T) {
		userdata := uint64(0x12345678)

		binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
		buf[inPtr+8] = 0 // tag: clock (u8)
		buf[inPtr+9] = 0x11
		buf[inPtr+10] = 0x22
		buf[inPtr+11] = 0x33
		binary.LittleEndian.PutUint64(buf[inPtr+16:], 0) // clock.id = 0
		binary.LittleEndian.PutUint64(buf[inPtr+24:], 1) // timeout: 1ns

		errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
		if errno != wasiESuccess {
			t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
		}

		nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
		if nevents != 1 {
			t.Errorf("nevents = %d, want 1", nevents)
		}

		evtType := buf[outPtr+10]
		if evtType != 0 {
			t.Errorf("event type = %d, want 0 (CLOCK)", evtType)
		}
	})
}

// notReadyPollableReader implements wasi.Pollable and always reports not-ready
// for POLLIN while recording Poll invocations.
type notReadyPollableReader struct {
	pollCalls int
}

func (r *notReadyPollableReader) Read(p []byte) (int, error) {
	return 0, nil
}

func (r *notReadyPollableReader) Poll(flag wasi.Pflag, timeoutMillis int32) (ready bool, errno wasi.Errno) {
	r.pollCalls++
	if flag == wasi.POLLIN {
		return false, 0
	}
	return false, wasi.ENOTSUP
}

func TestPollOneoffFdReadConsultsPollableBeforeEmitting(t *testing.T) {
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 2000
	)

	reader := &notReadyPollableReader{}
	buf := make([]byte, 65536)
	s := New(func() []byte { return buf }, NewModuleConfig().WithStdin(reader))

	userdata := uint64(4242)
	binary.LittleEndian.PutUint64(buf[inPtr:], userdata)
	binary.LittleEndian.PutUint32(buf[inPtr+8:], 1) // tag: fd_read
	binary.LittleEndian.PutUint32(buf[inPtr+16:], 0)

	errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpoll_oneoff returned %d, want ESUCCESS", errno)
	}

	if reader.pollCalls == 0 {
		t.Fatal("Xpoll_oneoff fd_read did not call Poll on stdin Pollable")
	}

	nevents := binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
	if nevents != 0 {
		t.Errorf("nevents = %d, want 0 when Poll reports not-ready", nevents)
	}
}

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
	s := New(func() []byte { return buf }, NewModuleConfig().WithStdin(strings.NewReader("")))

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

func pollOneoffFdRead(t *testing.T, s *State, buf []byte, fd uint32) uint32 {
	t.Helper()
	const (
		inPtr      = 100
		outPtr     = 1000
		neventsPtr = 2000
	)
	binary.LittleEndian.PutUint64(buf[inPtr:], 0xfeed)
	binary.LittleEndian.PutUint32(buf[inPtr+8:], 1) // tag: fd_read
	binary.LittleEndian.PutUint32(buf[inPtr+16:], fd)
	errno := s.Xpoll_oneoff(inPtr, outPtr, 1, neventsPtr)
	if errno != wasiESuccess {
		t.Fatalf("Xpoll_oneoff(fd=%d) = %d, want ESUCCESS", fd, errno)
	}
	return binary.LittleEndian.Uint32(buf[neventsPtr : neventsPtr+4])
}

// TestPollOneoffFdReadNonblockHostPipe verifies poll_oneoff fd_read and fd_read
// stay consistent with fd_fdstat_set_flags NONBLOCK on a host-backed pipe.
func TestPollOneoffFdReadNonblockHostPipe(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		// Windows has no /dev/fd; FreeBSD's exposes only fds 0-2 without fdescfs.
		t.Skip("needs /dev/fd/N for arbitrary fds")
	}
	const (
		iovsOff  = 3000
		dataOff  = 4000
		nreadOff = 5000
	)

	s, buf := newTestState(nil)
	readFD, writeEnd := allocHostPipeReaderFD(t, s)
	defer writeEnd.Close()

	if errno := s.Xfd_fdstat_set_flags(readFD, fdFlagsNonBlock); errno != wasiESuccess {
		t.Fatalf("Xfd_fdstat_set_flags(NONBLOCK) = %d, want ESUCCESS", errno)
	}

	if nevents := pollOneoffFdRead(t, s, buf, uint32(readFD)); nevents != 0 {
		t.Fatalf("poll_oneoff empty nonblocking pipe: nevents = %d, want 0", nevents)
	}

	if _, err := writeEnd.Write([]byte("x")); err != nil {
		t.Fatalf("Write pipe: %v", err)
	}

	if nevents := pollOneoffFdRead(t, s, buf, uint32(readFD)); nevents != 1 {
		t.Fatalf("poll_oneoff after data: nevents = %d, want 1", nevents)
	}

	binary.LittleEndian.PutUint32(buf[iovsOff:], uint32(dataOff))
	binary.LittleEndian.PutUint32(buf[iovsOff+4:], 1)
	if errno := s.Xfd_read(readFD, iovsOff, 1, nreadOff); errno != wasiESuccess {
		t.Fatalf("Xfd_read drain = %d, want ESUCCESS", errno)
	}
	if got := binary.LittleEndian.Uint32(buf[nreadOff : nreadOff+4]); got != 1 {
		t.Fatalf("Xfd_read drain nread = %d, want 1", got)
	}

	if errno := s.Xfd_read(readFD, iovsOff, 1, nreadOff); errno != wasiEAgain {
		t.Fatalf("Xfd_read empty nonblocking pipe = %d, want EAGAIN (%d)", errno, wasiEAgain)
	}
}
