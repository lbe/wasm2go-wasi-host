//go:build windows

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"errors"
	"io"
	"syscall"
	"unsafe"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

const (
	nonBlockingFileReadSupported  = true
	nonBlockingFileWriteSupported = false
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	// procPeekNamedPipe exposes PeekNamedPipe from kernel32.
	procPeekNamedPipe = kernel32.NewProc("PeekNamedPipe")
)

// readFd returns ENOSYS on unsupported platforms.
//
// PeekNamedPipe: https://learn.microsoft.com/en-us/windows/win32/api/namedpipeapi/nf-namedpipeapi-peeknamedpipe
// "GetFileType can assist in determining what device type the handle refers to. A console handle presents as FILE_TYPE_CHAR."
// https://learn.microsoft.com/en-us/windows/console/console-handles
func readFd(fd uintptr, buf []byte) (int, wasi.Errno) {
	handle := syscall.Handle(fd)
	fileType, err := syscall.GetFileType(handle)
	if err != nil {
		return 0, wasi.UnwrapOSError(err)
	}
	if fileType&syscall.FILE_TYPE_CHAR == 0 {
		return -1, wasi.ENOSYS
	}
	n, errno := peekNamedPipe(handle)
	if errno == syscall.ERROR_BROKEN_PIPE {
		return 0, 0
	}
	if n == 0 {
		return -1, wasi.EAGAIN
	}
	un, err := syscall.Read(handle, buf[0:n])
	return un, wasi.UnwrapOSError(err)
}

func writeFd(uintptr, []byte) (int, wasi.Errno) {
	return -1, wasi.ENOSYS
}

// preadFd reads at off without changing the handle's file position. Like
// internal/poll on Windows, it saves the position, performs an overlapped
// ReadFile at off, and restores the saved position.
func preadFd(fd uintptr, buf []byte, off int64) (int, wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	h := syscall.Handle(fd)
	cur, err := syscall.Seek(h, 0, io.SeekCurrent)
	if err != nil {
		return 0, wasi.UnwrapOSError(err)
	}
	ol := &syscall.Overlapped{Offset: uint32(off), OffsetHigh: uint32(off >> 32)}
	var done uint32
	err = syscall.ReadFile(h, buf, &done, ol)
	if restoreErr := restoreFilePos(h, cur); restoreErr != nil && err == nil {
		err = restoreErr
	}
	if errors.Is(err, syscall.ERROR_HANDLE_EOF) {
		return 0, 0
	}
	return int(done), wasi.UnwrapOSError(err)
}

// pwriteFd writes at off without changing the handle's file position. See
// preadFd for the approach.
func pwriteFd(fd uintptr, buf []byte, off int64) (int, wasi.Errno) {
	if len(buf) == 0 {
		return 0, 0
	}
	h := syscall.Handle(fd)
	cur, err := syscall.Seek(h, 0, io.SeekCurrent)
	if err != nil {
		return 0, wasi.UnwrapOSError(err)
	}
	ol := &syscall.Overlapped{Offset: uint32(off), OffsetHigh: uint32(off >> 32)}
	var done uint32
	err = syscall.WriteFile(h, buf, &done, ol)
	if restoreErr := restoreFilePos(h, cur); restoreErr != nil && err == nil {
		err = restoreErr
	}
	return int(done), wasi.UnwrapOSError(err)
}

func restoreFilePos(h syscall.Handle, pos int64) error {
	_, err := syscall.Seek(h, pos, io.SeekStart)
	return err
}

// peekNamedPipe partially exposes PeekNamedPipe from the Win32 API
// see https://learn.microsoft.com/en-us/windows/win32/api/namedpipeapi/nf-namedpipeapi-peeknamedpipe
func peekNamedPipe(handle syscall.Handle) (uint32, syscall.Errno) {
	var totalBytesAvail uint32
	totalBytesPtr := unsafe.Pointer(&totalBytesAvail)
	_, _, errno := syscall.SyscallN(
		procPeekNamedPipe.Addr(),
		uintptr(handle),        // [in]            HANDLE  hNamedPipe,
		0,                      // [out, optional] LPVOID  lpBuffer,
		0,                      // [in]            DWORD   nBufferSize,
		0,                      // [out, optional] LPDWORD lpBytesRead
		uintptr(totalBytesPtr), // [out, optional] LPDWORD lpTotalBytesAvail,
		0)                      // [out, optional] LPDWORD lpBytesLeftThisMessage
	return totalBytesAvail, errno
}

func rmdir(path string) wasi.Errno {
	err := syscall.Rmdir(path)
	return wasi.UnwrapOSError(err)
}
