//go:build darwin || linux

package wasihost

import (
	"errors"
	"os"
)

func renamePath(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	if !isEmptyDirectory(oldPath) || !isEmptyDirectory(newPath) {
		return err
	}
	if err := os.Remove(newPath); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

func isEmptyDirectory(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return false
	}
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}
