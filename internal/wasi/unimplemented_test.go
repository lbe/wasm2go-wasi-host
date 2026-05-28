package wasi

import (
	"testing"
)

func TestUnimplementedFSOpenFileReturnsENOSYS(t *testing.T) {
	var ufs UnimplementedFS
	_, errno := ufs.OpenFile("test", O_RDONLY, 0)
	if errno != ENOSYS {
		t.Errorf("OpenFile: got errno %s, want ENOSYS", errno)
	}
}

func TestUnimplementedFileReadReturnsENOSYS(t *testing.T) {
	var f UnimplementedFile
	n, errno := f.Read([]byte{0})
	if n != 0 {
		t.Errorf("Read: got n=%d, want 0", n)
	}
	if errno != ENOSYS {
		t.Errorf("Read: got errno %s, want ENOSYS", errno)
	}
}

func TestUnimplementedFileCloseReturnsZero(t *testing.T) {
	var f UnimplementedFile
	errno := f.Close()
	if errno != 0 {
		t.Errorf("Close: got errno %s, want 0", errno)
	}
}

func TestUnimplementedFSAllMethodsReturnENOSYS(t *testing.T) {
	var fs UnimplementedFS

	tests := []struct {
		name string
		got  Errno
	}{
		{"OpenFile", func() Errno { _, e := fs.OpenFile("test", O_RDONLY, 0); return e }()},
		{"Lstat", func() Errno { _, e := fs.Lstat("test"); return e }()},
		{"Stat", func() Errno { _, e := fs.Stat("test"); return e }()},
		{"Readlink", func() Errno { _, e := fs.Readlink("test"); return e }()},
		{"Mkdir", fs.Mkdir("test", 0755)},
		{"Chmod", fs.Chmod("test", 0755)},
		{"Rename", fs.Rename("a", "b")},
		{"Rmdir", fs.Rmdir("test")},
		{"Unlink", fs.Unlink("test")},
		{"Link", fs.Link("a", "b")},
		{"Symlink", fs.Symlink("a", "b")},
		{"Utimens", fs.Utimens("test", 0, 0)},
	}
	for _, tc := range tests {
		if tc.got != ENOSYS {
			t.Errorf("%s: got errno %s, want ENOSYS", tc.name, tc.got)
		}
	}
}

func TestUnimplementedFileMethodsReturnZeroOrENOSYS(t *testing.T) {
	var f UnimplementedFile

	tests := []struct {
		name string
		got  Errno
	}{
		{"SetAppend", f.SetAppend(true)},
		{"Stat", func() Errno { _, e := f.Stat(); return e }()},
		{"Read", func() Errno { _, e := f.Read(nil); return e }()},
		{"Pread", func() Errno { _, e := f.Pread(nil, 0); return e }()},
		{"Seek", func() Errno { _, e := f.Seek(0, 0); return e }()},
		{"Readdir", func() Errno { _, e := f.Readdir(0); return e }()},
		{"Write", func() Errno { _, e := f.Write(nil); return e }()},
		{"Pwrite", func() Errno { _, e := f.Pwrite(nil, 0); return e }()},
		{"Truncate", f.Truncate(0)},
		{"Sync", f.Sync()},
		{"Datasync", f.Datasync()},
		{"Utimens", f.Utimens(0, 0)},
		{"Close", f.Close()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			switch tc.name {
			case "Sync", "Datasync", "Close":
				if tc.got != 0 {
					t.Errorf("%s: got errno %s, want 0", tc.name, tc.got)
				}
			default:
				if tc.got != ENOSYS {
					t.Errorf("%s: got errno %s, want ENOSYS", tc.name, tc.got)
				}
			}
		})
	}

	// Dev returns zero and no error
	dev, errno := f.Dev()
	if dev != 0 || errno != 0 {
		t.Errorf("Dev: got dev=%d errno=%s, want dev=0 errno=0", dev, errno)
	}

	// Ino returns zero and no error
	ino, errno := f.Ino()
	if ino != 0 || errno != 0 {
		t.Errorf("Ino: got ino=%d errno=%s, want ino=0 errno=0", ino, errno)
	}

	// IsDir returns false and no error
	isDir, errno := f.IsDir()
	if isDir || errno != 0 {
		t.Errorf("IsDir: got isDir=%v errno=%s, want isDir=false errno=0", isDir, errno)
	}

	// IsAppend returns false (no error return)
	if f.IsAppend() {
		t.Error("IsAppend: got true, want false")
	}
}
