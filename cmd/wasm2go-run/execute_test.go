package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestExecute(t *testing.T) {
	t.Run("returns 0 on success and cleans up buildDir", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "wasm2go-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir) // cleanup if test fails early

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		// /bin/true should exist on most Unix-like systems and exit 0
		exitCode, err := execute("/usr/bin/true", tempDir, nil, stdout, stderr)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}

		if _, err := os.Stat(tempDir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("expected buildDir %s to be removed, but it still exists or got error: %v", tempDir, err)
		}
	})

	t.Run("returns non-zero exit code on failure and cleans up buildDir", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "wasm2go-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		// /bin/false should exist and exit 1
		exitCode, err := execute("/usr/bin/false", tempDir, nil, stdout, stderr)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}

		if _, err := os.Stat(tempDir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("expected buildDir %s to be removed, but it still exists", tempDir)
		}
	})

	t.Run("returns error when binary does not exist and cleans up buildDir", func(t *testing.T) {
		buildDir, err := os.MkdirTemp("", "wasm2go-test-*")
		if err != nil {
			t.Fatalf("failed to create buildDir: %v", err)
		}
		defer os.RemoveAll(buildDir)

		binDir := t.TempDir()
		nonExistentPath := filepath.Join(binDir, "non-existent-binary")

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		exitCode, err := execute(nonExistentPath, buildDir, nil, stdout, stderr)
		if err == nil {
			t.Error("expected error for non-existent binary, got nil")
		}
		if exitCode != -1 {
			t.Errorf("expected exit code -1, got %d", exitCode)
		}

		if _, err := os.Stat(buildDir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("expected buildDir %s to be removed, but it still exists", buildDir)
		}
	})

	t.Run("streams stdout and stderr", func(t *testing.T) {
		scriptDir := t.TempDir()
		scriptPath := filepath.Join(scriptDir, "test-script.sh")
		scriptContent := "#!/bin/sh\necho 'hello stdout'\necho 'hello stderr' >&2\n"
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
			t.Fatalf("failed to write test script: %v", err)
		}
		if err := os.Chmod(scriptPath, 0755); err != nil {
			t.Fatalf("failed to chmod test script: %v", err)
		}

		buildDir, err := os.MkdirTemp("", "execute-test-*")
		if err != nil {
			t.Fatalf("failed to create buildDir: %v", err)
		}
		defer os.RemoveAll(buildDir)

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		exitCode, err := execute(scriptPath, buildDir, nil, stdout, stderr)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}

		if stdout.String() != "hello stdout\n" {
			t.Errorf("expected 'hello stdout\\n', got %q", stdout.String())
		}
		if stderr.String() != "hello stderr\n" {
			t.Errorf("expected 'hello stderr\\n', got %q", stderr.String())
		}

		if _, err := os.Stat(buildDir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("expected buildDir %s to be removed, but it still exists", buildDir)
		}
	})

	t.Run("wires stdin to child process", func(t *testing.T) {
		scriptDir := t.TempDir()
		scriptPath := filepath.Join(scriptDir, "test-stdin.sh")
		scriptContent := "#!/bin/sh\nread input\necho \"got $input\"\n"
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
			t.Fatalf("failed to write test script: %v", err)
		}

		buildDir, err := os.MkdirTemp("", "execute-stdin-test-*")
		if err != nil {
			t.Fatalf("failed to create buildDir: %v", err)
		}
		defer os.RemoveAll(buildDir)

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		// Mock os.Stdin by piping from a reader
		input := "hello stdin"
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}

		go func() {
			w.Write([]byte(input + "\n"))
			w.Close()
		}()

		exitCode, err := execute(scriptPath, buildDir, r, stdout, stderr)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}

		expected := "got hello stdin\n"
		if stdout.String() != expected {
			t.Errorf("expected %q, got %q", expected, stdout.String())
		}

		if _, err := os.Stat(buildDir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("expected buildDir %s to be removed, but it still exists", buildDir)
		}
	})
}
