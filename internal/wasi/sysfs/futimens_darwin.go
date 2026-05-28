//go:build darwin

package sysfs

import (
	"os"
	"strconv"
	"syscall"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

const _UTIME_OMIT = -2

func utimens(path string, atim, mtim int64) wasi.Errno {
	times := timesToTimespecs(wasi.EpochNanos(atim), wasi.EpochNanos(mtim))
	if times == nil {
		return 0
	}
	return wasi.UnwrapOSError(syscall.UtimesNano(path, times[:]))
}

func futimens(fd uintptr, atim, mtim int64) error {
	// On Darwin, fall back to Futimes which takes Timeval.
	// This is a best-effort implementation (no UTIME_OMIT support).
	osf := os.NewFile(fd, "")
	if osf == nil {
		return syscall.EBADF
	}
	path := "/proc/self/fd/" + strconv.FormatUint(uint64(fd), 10)
	_ = path
	times := timesToTimespecs(wasi.EpochNanos(atim), wasi.EpochNanos(mtim))
	if times == nil {
		return nil
	}
	tv := [2]syscall.Timeval{
		{Sec: times[0].Sec, Usec: int32(times[0].Nsec / 1000)},
		{Sec: times[1].Sec, Usec: int32(times[1].Nsec / 1000)},
	}
	return syscall.Futimes(int(fd), tv[:])
}
