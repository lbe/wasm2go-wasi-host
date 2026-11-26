//go:build windows

package sysfs

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// LinkNoFollow creates newPath as a hard link to oldPath. On Windows, os.Link
// does not follow a symlink at oldPath.
func LinkNoFollow(oldPath, newPath string) error {
	return os.Link(oldPath, newPath)
}

// UtimesNoFollow sets the access and modification times of path without
// following a symlink at path. The handle is opened with
// FILE_FLAG_OPEN_REPARSE_POINT so SetFileTime applies to the link itself.
func UtimesNoFollow(path string, atim, mtim time.Time) error {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return &os.PathError{Op: "utimes", Path: path, Err: err}
	}
	h, err := windows.CreateFile(pathp, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return &os.PathError{Op: "utimes", Path: path, Err: err}
	}
	defer windows.CloseHandle(h)

	a := windows.NsecToFiletime(atim.UnixNano())
	m := windows.NsecToFiletime(mtim.UnixNano())
	if err := windows.SetFileTime(h, nil, &a, &m); err != nil {
		return &os.PathError{Op: "utimes", Path: path, Err: err}
	}
	return nil
}
