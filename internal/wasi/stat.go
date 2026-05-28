package wasi

import (
	"io/fs"
	"syscall"
)

// Inode is the file serial number, or zero if unknown.
type Inode = uint64

// EpochNanos is a timestamp in epoch nanoseconds, or zero if unknown.
type EpochNanos = int64

// Stat_t is similar to syscall.Stat_t, except available on all operating
// systems, including Windows.
type Stat_t struct {
	Dev   uint64
	Ino   Inode
	Mode  fs.FileMode
	Nlink uint64
	Size  int64
	Atim  EpochNanos
	Mtim  EpochNanos
	Ctim  EpochNanos
}

// NewStat_t fills a new Stat_t from info, including OS-specific details from
// fs.FileInfo Sys(). When Sys is already a *Stat_t, it is returned as-is.
func NewStat_t(info fs.FileInfo) Stat_t {
	if st, ok := info.Sys().(*Stat_t); ok {
		return *st
	}
	return statFromFileInfo(info)
}

func timespecToEpochNanos(ts syscall.Timespec) EpochNanos {
	return EpochNanos(ts.Sec)*1e9 + EpochNanos(ts.Nsec)
}

func statFromFileInfo(info fs.FileInfo) Stat_t {
	if d, ok := info.Sys().(*syscall.Stat_t); ok {
		return Stat_t{
			Dev:   uint64(d.Dev),
			Ino:   Inode(d.Ino),
			Mode:  info.Mode(),
			Nlink: uint64(d.Nlink),
			Size:  d.Size,
			Atim:  timespecToEpochNanos(d.Atimespec),
			Mtim:  timespecToEpochNanos(d.Mtimespec),
			Ctim:  timespecToEpochNanos(d.Ctimespec),
		}
	}
	return defaultStatFromFileInfo(info)
}

func defaultStatFromFileInfo(info fs.FileInfo) Stat_t {
	st := Stat_t{}
	st.Ino = 0
	st.Dev = 0
	st.Mode = info.Mode()
	st.Nlink = 1
	st.Size = info.Size()
	mtim := info.ModTime().UnixNano()
	st.Atim = mtim
	st.Mtim = mtim
	st.Ctim = mtim
	return st
}
