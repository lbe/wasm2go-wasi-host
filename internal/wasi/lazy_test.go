// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"io/fs"
	"testing"
)

// failingFS is an FS implementation that returns specified errors on OpenFile.
type failingFS struct {
	err Errno
}

func (f *failingFS) PathFor() string { return "/failing" }

func (f *failingFS) OpenFile(path string, flag Oflag, perm fs.FileMode) (File, Errno) {
	return nil, f.err
}

func (f *failingFS) Lstat(path string) (Stat_t, Errno)  { return Stat_t{}, EIO }
func (f *failingFS) Stat(path string) (Stat_t, Errno)   { return Stat_t{}, EIO }
func (f *failingFS) Mkdir(string, fs.FileMode) Errno    { return ENOSYS }
func (f *failingFS) Chmod(string, fs.FileMode) Errno    { return ENOSYS }
func (f *failingFS) Rename(string, string) Errno        { return ENOSYS }
func (f *failingFS) Rmdir(string) Errno                 { return ENOSYS }
func (f *failingFS) Unlink(string) Errno                { return ENOSYS }
func (f *failingFS) Link(string, string) Errno          { return ENOSYS }
func (f *failingFS) Symlink(string, string) Errno       { return ENOSYS }
func (f *failingFS) Readlink(string) (string, Errno)    { return "", ENOSYS }
func (f *failingFS) Utimens(string, int64, int64) Errno { return ENOSYS }

// TestLazyDirSurfacesOpenFailuresAsEBADF verifies that lazyDir returns EBADF
// instead of panicking when the underlying FS returns errors other than ENOENT.
func TestLazyDirSurfacesOpenFailuresAsEBADF(t *testing.T) {
	tests := []struct {
		name    string
		fsErr   Errno
		wantErr Errno
	}{
		{
			name:    "EACCES on OpenFile returns EBADF on Stat",
			fsErr:   EACCES,
			wantErr: EBADF,
		},
		{
			name:    "EINVAL on OpenFile returns EBADF on Stat",
			fsErr:   EINVAL,
			wantErr: EBADF,
		},
		{
			name:    "EIO on OpenFile returns EBADF on Stat",
			fsErr:   EIO,
			wantErr: EBADF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &failingFS{err: tt.fsErr}
			ld := &lazyDir{fs: fs}

			// Stat() should return EBADF, not panic
			_, gotErr := ld.Stat()
			if gotErr != tt.wantErr {
				t.Errorf("Stat() error = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

// TestLazyDirStatReturnsEBADF verifies that Stat returns EBADF on error.
func TestLazyDirStatReturnsEBADF(t *testing.T) {
	fs := &failingFS{err: EACCES}
	ld := &lazyDir{fs: fs}

	_, gotErr := ld.Stat()
	if gotErr != EBADF {
		t.Errorf("Stat() error = %v, want EBADF", gotErr)
	}
}

// TestLazyDirReaddirReturnsEBADF verifies that Readdir returns EBADF on error.
func TestLazyDirReaddirReturnsEBADF(t *testing.T) {
	fs := &failingFS{err: EACCES}
	ld := &lazyDir{fs: fs}

	_, gotErr := ld.Readdir(10)
	if gotErr != EBADF {
		t.Errorf("Readdir() error = %v, want EBADF", gotErr)
	}
}

// TestLazyDirInoReturnsEBADF verifies that Ino returns EBADF on error.
func TestLazyDirInoReturnsEBADF(t *testing.T) {
	fs := &failingFS{err: EACCES}
	ld := &lazyDir{fs: fs}

	_, gotErr := ld.Ino()
	if gotErr != EBADF {
		t.Errorf("Ino() error = %v, want EBADF", gotErr)
	}
}
