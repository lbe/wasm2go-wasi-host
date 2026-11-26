//go:build !unix && !windows

package main

import "os"

// lockFile is a no-op on platforms without a supported file locking primitive.
func lockFile(f *os.File) error {
	return nil
}

// unlockFile is a no-op on platforms without a supported file locking primitive.
func unlockFile(f *os.File) error {
	return nil
}
