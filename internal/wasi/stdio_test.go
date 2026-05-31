// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"bytes"
	"testing"
)

// TestStdinFileReadAndPoll verifies that StdinFile correctly wraps an io.Reader
// and implements Read and Poll methods. It also tests that noop stdio (when
// no reader is provided) returns 0 bytes and is always ready for POLLIN.
func TestStdinFileReadAndPoll(t *testing.T) {
	// Test with a real reader
	input := bytes.NewBufferString("test data")
	stdin := &StdinFile{Reader: input}

	// Read should return the data
	buf := make([]byte, 10)
	n, errno := stdin.Read(buf)
	if errno != 0 {
		t.Fatalf("StdinFile.Read: unexpected errno %d", errno)
	}
	if n != 9 {
		t.Errorf("StdinFile.Read: got %d bytes, want 9", n)
	}
	if string(buf[:n]) != "test data" {
		t.Errorf("StdinFile.Read: got %q, want %q", string(buf[:n]), "test data")
	}

	// Poll with POLLIN should succeed
	ready, errno := stdin.Poll(POLLIN, 1000)
	if errno != 0 {
		t.Fatalf("StdinFile.Poll: unexpected errno %d", errno)
	}
	if !ready {
		t.Error("StdinFile.Poll: expected ready=true for POLLIN on readable stream")
	}

	// Test noop stdin (no reader provided)
	noopStdin := &noopStdinFile{}

	// Read should return 0 bytes (EOF-like behavior)
	n, errno = noopStdin.Read(buf)
	if errno != 0 {
		t.Fatalf("noopStdinFile.Read: unexpected errno %d", errno)
	}
	if n != 0 {
		t.Errorf("noopStdinFile.Read: got %d bytes, want 0", n)
	}

	// Poll with POLLIN should return ready=true
	ready, errno = noopStdin.Poll(POLLIN, 1000)
	if errno != 0 {
		t.Fatalf("noopStdinFile.Poll: unexpected errno %d", errno)
	}
	if !ready {
		t.Error("noopStdinFile.Poll: expected ready=true for POLLIN")
	}

	// Poll with POLLOUT should return ready=false with ENOTSUP
	ready, errno = noopStdin.Poll(POLLOUT, 1000)
	if errno != ENOTSUP {
		t.Errorf("noopStdinFile.Poll(POLLOUT): got errno %d, want ENOTSUP", errno)
	}
	if ready {
		t.Error("noopStdinFile.Poll(POLLOUT): expected ready=false")
	}
}

// TestWriterFileWrite verifies that writerFile correctly wraps an io.Writer
// and implements Write method for stdout/stderr.
func TestWriterFileWrite(t *testing.T) {
	// Test with a real writer
	var buf bytes.Buffer
	writer := &writerFile{w: &buf}

	// Write should succeed and data should appear in buffer
	data := []byte("hello world")
	n, errno := writer.Write(data)
	if errno != 0 {
		t.Fatalf("writerFile.Write: unexpected errno %d", errno)
	}
	if n != len(data) {
		t.Errorf("writerFile.Write: got %d bytes, want %d", n, len(data))
	}
	if buf.String() != "hello world" {
		t.Errorf("writerFile.Write: buffer = %q, want %q", buf.String(), "hello world")
	}
}

// TestInitFSContextCustomStdio verifies InitFSContext wires custom
// stdin/stdout/stderr readers and writers to fd 0/1/2.
func TestInitFSContextCustomStdio(t *testing.T) {
	var stdin bytes.Buffer
	stdin.WriteString("input")
	var stdout, stderr bytes.Buffer

	fsc, err := InitFSContext(&stdin, &stdout, &stderr, nil, nil)
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}
	defer fsc.Close()

	feIn, ok := fsc.LookupFile(FdStdin)
	if !ok {
		t.Fatal("LookupFile(0): stdin missing")
	}
	buf := make([]byte, 8)
	n, errno := feIn.File.Read(buf)
	if errno != 0 {
		t.Fatalf("stdin Read: errno %d", errno)
	}
	if got, want := string(buf[:n]), "input"; got != want {
		t.Errorf("stdin Read: got %q, want %q", got, want)
	}

	feOut, ok := fsc.LookupFile(FdStdout)
	if !ok {
		t.Fatal("LookupFile(1): stdout missing")
	}
	if _, errno := feOut.File.Write([]byte("out")); errno != 0 {
		t.Fatalf("stdout Write: errno %d", errno)
	}
	if got, want := stdout.String(), "out"; got != want {
		t.Errorf("stdout buffer = %q, want %q", got, want)
	}

	feErr, ok := fsc.LookupFile(FdStderr)
	if !ok {
		t.Fatal("LookupFile(2): stderr missing")
	}
	if _, errno := feErr.File.Write([]byte("err")); errno != 0 {
		t.Fatalf("stderr Write: errno %d", errno)
	}
	if got, want := stderr.String(), "err"; got != want {
		t.Errorf("stderr buffer = %q, want %q", got, want)
	}
}

// TestNoopStdoutFileWrite verifies that noopStdoutFile silently
// discards all data and reports success.
func TestNoopStdoutFileWrite(t *testing.T) {
	// Test write silently discards all data
	noopStdout := &noopStdoutFile{}

	// Write silently discards all data and reports zero bytes written
	n, errno := noopStdout.Write([]byte("hello"))
	if errno != 0 {
		t.Fatalf("noopStdoutFile.Write: unexpected errno %d", errno)
	}
	if n != 0 {
		t.Errorf("noopStdoutFile.Write: got %d bytes, want 0", n)
	}
}
