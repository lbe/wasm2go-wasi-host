//go:build linux

package wasi

import (
	"io/fs"
	"syscall"
	"testing"
	"time"
)

func TestNewStat_tMapsLinuxSyscallStatTimestampsToEpochNanos(t *testing.T) {
	const (
		atimSec, atimNsec = int64(1), int64(2)
		mtimSec, mtimNsec = int64(3), int64(4)
		ctimSec, ctimNsec = int64(5), int64(6)
	)
	atim := syscall.Timespec{Sec: atimSec, Nsec: atimNsec}
	mtim := syscall.Timespec{Sec: mtimSec, Nsec: mtimNsec}
	ctim := syscall.Timespec{Sec: ctimSec, Nsec: ctimNsec}

	syscallStat := &syscall.Stat_t{
		Ino:  42,
		Size: 100,
		Atim: atim,
		Mtim: mtim,
		Ctim: ctim,
	}
	info := &syscallStatFileInfo{
		name: "linux-stat",
		mode: 0o644,
		stat: syscallStat,
	}

	wantAtim := timespecToEpochNanos(atim)
	wantMtim := timespecToEpochNanos(mtim)
	wantCtim := timespecToEpochNanos(ctim)

	st := NewStat_t(info)
	if st.Atim != wantAtim {
		t.Errorf("Atim: got %d, want %d", st.Atim, wantAtim)
	}
	if st.Mtim != wantMtim {
		t.Errorf("Mtim: got %d, want %d", st.Mtim, wantMtim)
	}
	if st.Ctim != wantCtim {
		t.Errorf("Ctim: got %d, want %d", st.Ctim, wantCtim)
	}
}

// syscallStatFileInfo implements fs.FileInfo with Sys() returning *syscall.Stat_t.
type syscallStatFileInfo struct {
	name string
	mode fs.FileMode
	stat *syscall.Stat_t
}

func (f *syscallStatFileInfo) Name() string       { return f.name }
func (f *syscallStatFileInfo) Size() int64        { return f.stat.Size }
func (f *syscallStatFileInfo) Mode() fs.FileMode  { return f.mode }
func (f *syscallStatFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (f *syscallStatFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f *syscallStatFileInfo) Sys() interface{}   { return f.stat }
