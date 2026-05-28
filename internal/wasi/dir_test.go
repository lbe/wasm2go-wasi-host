package wasi

import (
	"io/fs"
	"strings"
	"testing"
)

func TestDirentFieldsAccessible(t *testing.T) {
	d := &Dirent{
		Ino:  Inode(42),
		Name: "testdir",
		Type: fs.ModeDir,
	}

	if d.Ino != Inode(42) {
		t.Errorf("Dirent.Ino: got %d, want 42", d.Ino)
	}
	if d.Name != "testdir" {
		t.Errorf("Dirent.Name: got %q, want %q", d.Name, "testdir")
	}
	if d.Type != fs.ModeDir {
		t.Errorf("Dirent.Type: got %v, want %v", d.Type, fs.ModeDir)
	}
}

func TestDirentStringRepresentation(t *testing.T) {
	d := &Dirent{
		Ino:  Inode(7),
		Name: "hello",
		Type: fs.ModeDir,
	}

	s := d.String()
	if !strings.Contains(s, "hello") {
		t.Errorf("Dirent.String() = %q, want it to contain %q", s, "hello")
	}
	if !strings.Contains(s, "7") {
		t.Errorf("Dirent.String() = %q, want it to contain ino 7", s)
	}
}

func TestDirentIsDirByType(t *testing.T) {
	dirEnt := &Dirent{Type: fs.ModeDir}
	if !dirEnt.IsDir() {
		t.Error("Dirent{Type: fs.ModeDir}.IsDir() = false, want true")
	}

	fileEnt := &Dirent{Type: 0}
	if fileEnt.IsDir() {
		t.Error("Dirent{Type: 0}.IsDir() = true, want false")
	}
}

func TestFileTypeIsFileModeAlias(t *testing.T) {
	var ft = fs.ModeDir
	if ft != fs.ModeDir {
		t.Errorf("FileType alias broken: got %v, want %v", ft, fs.ModeDir)
	}
}

func TestDirFileIsAppendReturnsFalse(t *testing.T) {
	var df DirFile
	if df.IsAppend() {
		t.Error("DirFile.IsAppend() = true, want false")
	}
}

func TestDirFileSetAppendReturnsEISDIR(t *testing.T) {
	var df DirFile
	err := df.SetAppend(true)
	if err != EISDIR {
		t.Errorf("DirFile.SetAppend(true) = %v, want EISDIR", err)
	}
}

func TestDirFileIsDirReturnsTrueNoError(t *testing.T) {
	var df DirFile
	isDir, err := df.IsDir()
	if !isDir {
		t.Error("DirFile.IsDir() first return = false, want true")
	}
	if err != 0 {
		t.Errorf("DirFile.IsDir() second return = %v, want 0", err)
	}
}

func TestDirFileReadReturnsEISDIR(t *testing.T) {
	var df DirFile
	n, err := df.Read(make([]byte, 10))
	if n != 0 {
		t.Errorf("DirFile.Read() n = %d, want 0", n)
	}
	if err != EISDIR {
		t.Errorf("DirFile.Read() err = %v, want EISDIR", err)
	}
}

func TestDirFilePreadReturnsEISDIR(t *testing.T) {
	var df DirFile
	n, err := df.Pread(make([]byte, 10), 0)
	if n != 0 {
		t.Errorf("DirFile.Pread() n = %d, want 0", n)
	}
	if err != EISDIR {
		t.Errorf("DirFile.Pread() err = %v, want EISDIR", err)
	}
}

func TestDirFileWriteReturnsEISDIR(t *testing.T) {
	var df DirFile
	n, err := df.Write([]byte("hello"))
	if n != 0 {
		t.Errorf("DirFile.Write() n = %d, want 0", n)
	}
	if err != EISDIR {
		t.Errorf("DirFile.Write() err = %v, want EISDIR", err)
	}
}

func TestDirFilePwriteReturnsEISDIR(t *testing.T) {
	var df DirFile
	n, err := df.Pwrite([]byte("hello"), 0)
	if n != 0 {
		t.Errorf("DirFile.Pwrite() n = %d, want 0", n)
	}
	if err != EISDIR {
		t.Errorf("DirFile.Pwrite() err = %v, want EISDIR", err)
	}
}

func TestDirFileTruncateReturnsEISDIR(t *testing.T) {
	var df DirFile
	err := df.Truncate(0)
	if err != EISDIR {
		t.Errorf("DirFile.Truncate(0) = %v, want EISDIR", err)
	}
}
