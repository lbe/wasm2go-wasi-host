//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile acquires an exclusive lock on the first byte of f, blocking until it is available.
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{})
}

// unlockFile releases the lock taken by lockFile on f.
func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
