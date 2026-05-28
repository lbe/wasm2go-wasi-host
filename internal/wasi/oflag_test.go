package wasi

import "testing"

func TestOflagType(t *testing.T) {
	var f Oflag = 0
	if uint32(f) != 0 {
		t.Errorf("Oflag zero value: got %d, want 0", f)
	}
}

func TestO_RDONLY(t *testing.T) {
	if O_RDONLY != 0 {
		t.Errorf("O_RDONLY: got %d, want 0", O_RDONLY)
	}
}

func TestO_RDWR(t *testing.T) {
	if O_RDWR != 1 {
		t.Errorf("O_RDWR: got %d, want 1", O_RDWR)
	}
}

func TestO_WRONLY(t *testing.T) {
	if O_WRONLY != 2 {
		t.Errorf("O_WRONLY: got %d, want 2", O_WRONLY)
	}
}

func TestOflags(t *testing.T) {
	tests := []struct {
		name string
		got  Oflag
		want Oflag
	}{
		{"O_APPEND", O_APPEND, 1 << 2},
		{"O_CREAT", O_CREAT, 1 << 3},
		{"O_DIRECTORY", O_DIRECTORY, 1 << 4},
		{"O_DSYNC", O_DSYNC, 1 << 5},
		{"O_EXCL", O_EXCL, 1 << 6},
		{"O_NOFOLLOW", O_NOFOLLOW, 1 << 7},
		{"O_NONBLOCK", O_NONBLOCK, 1 << 8},
		{"O_RSYNC", O_RSYNC, 1 << 9},
		{"O_SYNC", O_SYNC, 1 << 10},
		{"O_TRUNC", O_TRUNC, 1 << 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s: got %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}
