package wasihost

import (
	"testing"
)

// TestXsockShutdownNotSocket verifies that Xsock_shutdown on standard I/O
// file descriptors (stdin, stdout, stderr) returns ENOTSOCK (57), not
// ENOSYS (52). Standard I/O fds are character devices, not sockets, so
// sock_shutdown is not a valid operation on them.
func TestXsockShutdownNotSocket(t *testing.T) {
	s, _ := newTestState(nil)

	for _, fd := range []int32{StdinFD, StdoutFD, StderrFD} {
		got := s.Xsock_shutdown(fd, 0)
		if got != wasiENotSock {
			t.Errorf("Xsock_shutdown(%d, 0) = %d, want ENOTSOCK (57)", fd, got)
		}
	}
}

// TestXsockShutdownInvalidFd verifies that Xsock_shutdown on invalid or
// closed file descriptors returns EBADF (8), not ENOSYS (52) or ENOTSOCK (57).
func TestXsockShutdownInvalidFd(t *testing.T) {
	t.Run("out of range fd returns EBADF", func(t *testing.T) {
		s, _ := newTestState(nil)
		if got := s.Xsock_shutdown(999, 0); got != wasiEBadf {
			t.Errorf("Xsock_shutdown(999, 0) = %d, want EBADF (%d)", got, wasiEBadf)
		}
	})

	t.Run("negative fd returns EBADF", func(t *testing.T) {
		s, _ := newTestState(nil)
		if got := s.Xsock_shutdown(-1, 0); got != wasiEBadf {
			t.Errorf("Xsock_shutdown(-1, 0) = %d, want EBADF (%d)", got, wasiEBadf)
		}
	})

	t.Run("closed fd returns EBADF", func(t *testing.T) {
		s, _ := newTestState(nil)
		insertStubFD(s, 10, wrapStubFSFile(stubFSFile{}), rightFDRead)
		if errno := s.Xfd_close(10); errno != wasiESuccess {
			t.Fatalf("Xfd_close(10) = %d, want ESUCCESS", errno)
		}
		if got := s.Xsock_shutdown(10, 0); got != wasiEBadf {
			t.Errorf("Xsock_shutdown(closed fd 10, 0) = %d, want EBADF (%d)", got, wasiEBadf)
		}
	})
}

// TestXsockOtherRemainENOSYS verifies that sock_accept, sock_recv, sock_send
// return ENOSYS (52) for invalid/unused fds, and sock_shutdown returns EBADF (8).
func TestXsockOtherRemainENOSYS(t *testing.T) {
	t.Run("Xsock_accept returns ENOSYS", func(t *testing.T) {
		s, _ := newTestState(nil)
		if got := s.Xsock_accept(5, 0, 0); got != wasiENoSys {
			t.Errorf("Xsock_accept(5, 0, 0) = %d, want ENOSYS (%d)", got, wasiENoSys)
		}
	})

	t.Run("Xsock_recv returns ENOSYS", func(t *testing.T) {
		s, _ := newTestState(nil)
		if got := s.Xsock_recv(5, 0, 0, 0, 0, 0); got != wasiENoSys {
			t.Errorf("Xsock_recv(5, 0, 0, 0, 0, 0) = %d, want ENOSYS (%d)", got, wasiENoSys)
		}
	})

	t.Run("Xsock_send returns ENOSYS", func(t *testing.T) {
		s, _ := newTestState(nil)
		if got := s.Xsock_send(5, 0, 0, 0, 0); got != wasiENoSys {
			t.Errorf("Xsock_send(5, 0, 0, 0, 0) = %d, want ENOSYS (%d)", got, wasiENoSys)
		}
	})

	t.Run("Xsock_shutdown returns EBADF", func(t *testing.T) {
		s, _ := newTestState(nil)
		if got := s.Xsock_shutdown(5, 0); got != wasiEBadf {
			t.Errorf("Xsock_shutdown(5, 0) = %d, want EBADF (%d)", got, wasiEBadf)
		}
	})
}
