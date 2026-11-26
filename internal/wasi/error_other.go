//go:build !windows

package wasi

import "syscall"

// mapPlatformErrno has no platform-specific codes outside Windows; the
// portable syscall table handles them.
func mapPlatformErrno(syscall.Errno) (Errno, bool) {
	return 0, false
}
