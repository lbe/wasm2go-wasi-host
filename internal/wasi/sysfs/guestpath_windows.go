//go:build windows

package sysfs

import "strings"

// GuestPathOK reports whether a WASI guest path can be mapped onto the host.
// WASI paths use only '/' as a separator. On Windows a '\' would also act as a
// separator (so "..\.." could slip past '/'-based escape checks) and ':' would
// select a drive or an alternate data stream, so paths containing either are
// rejected.
func GuestPathOK(p string) bool {
	return !strings.ContainsAny(p, `\:`)
}
