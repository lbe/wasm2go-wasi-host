//go:build !darwin && !linux

package wasihost

import "os"

func renamePath(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
