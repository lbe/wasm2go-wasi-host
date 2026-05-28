//go:build linux || darwin

package sysfs

import (
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

const wasiUTIME_OMIT = wasi.EpochNanos(-2) // _UTIME_OMIT sentinel

func timesToTimespecs(atim wasi.EpochNanos, mtim wasi.EpochNanos) *[2]syscall.Timespec {
	if atim == wasiUTIME_OMIT && mtim == wasiUTIME_OMIT {
		return nil
	}
	times := &[2]syscall.Timespec{}
	switch {
	case atim == wasiUTIME_OMIT:
		times[0] = syscall.Timespec{Nsec: _UTIME_OMIT}
		times[1] = syscall.NsecToTimespec(int64(mtim))
	case mtim == wasiUTIME_OMIT:
		times[0] = syscall.NsecToTimespec(int64(atim))
		times[1] = syscall.Timespec{Nsec: _UTIME_OMIT}
	default:
		times[0] = syscall.NsecToTimespec(int64(atim))
		times[1] = syscall.NsecToTimespec(int64(mtim))
	}
	return times
}
