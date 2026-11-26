//go:build !unix && !windows

package sysfs

import (
	"syscall"
	"time"
)

// LinkNoFollow is not implemented on this platform.
func LinkNoFollow(string, string) error {
	return syscall.ENOSYS
}

// UtimesNoFollow is not implemented on this platform.
func UtimesNoFollow(string, time.Time, time.Time) error {
	return syscall.ENOSYS
}
