//go:build unix

// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package sysfs

import (
	"os"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// osFile must implement Poll and SetNonblock for poll_oneoff on host fds.
var _ wasi.PollableFile = (*osFile)(nil)

func TestOsFilePollNonblockingPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	pf, ok := newOsFile("", 0, 0, r).(wasi.PollableFile)
	if !ok {
		t.Fatal("osFile must implement wasi.PollableFile")
	}
	if errno := pf.SetNonblock(true); errno != 0 {
		t.Fatalf("SetNonblock(true): errno %d", errno)
	}

	ready, errno := pf.Poll(wasi.POLLIN, 0)
	if errno != 0 {
		t.Fatalf("Poll empty pipe: errno %d", errno)
	}
	if ready {
		t.Fatal("Poll empty nonblocking pipe: got ready, want not ready")
	}

	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatalf("Write pipe: %v", err)
	}

	ready, errno = pf.Poll(wasi.POLLIN, 0)
	if errno != 0 {
		t.Fatalf("Poll after write: errno %d", errno)
	}
	if !ready {
		t.Fatal("Poll pipe after write: got not ready, want ready")
	}
}
