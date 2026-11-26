//go:build windows

package sysfs

import "testing"

func TestGuestPathOKWindows(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"a/b.txt", true},
		{"/abs/path", true},
		{"..", true},
		{`..\..\outside`, false},
		{`a\b`, false},
		{"C:/x", false},
		{"file.txt:stream", false},
	}
	for _, tc := range tests {
		if got := GuestPathOK(tc.path); got != tc.want {
			t.Errorf("GuestPathOK(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
