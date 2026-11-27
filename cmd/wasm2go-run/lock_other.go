//go:build !unix && !windows

package main

import "os"

// lockFile does nothing on platforms without a supported file lock.
func lockFile(f *os.File) error {
	return nil
}

// unlockFile does nothing on platforms without a supported file lock.
func unlockFile(f *os.File) error {
	return nil
}
