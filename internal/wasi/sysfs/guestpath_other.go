//go:build !windows

package sysfs

// GuestPathOK reports whether a WASI guest path can be mapped onto the host.
// Outside Windows every byte except '/' is an ordinary filename byte.
func GuestPathOK(string) bool {
	return true
}
