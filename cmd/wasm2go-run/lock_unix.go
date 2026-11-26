//go:build unix

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile acquires an exclusive advisory lock on f, blocking until it is available.
func lockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

// unlockFile releases the advisory lock held on f.
func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
