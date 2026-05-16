package main

import (
	"bytes"
	"io"
	"testing"
)

func TestStdoutAndEnv(t *testing.T) {
	t.Run("fd_write-to-stdout", func(t *testing.T) {
		wasmFile := "../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/fd_write-to-stdout.wasm"
		wasmPath := testdata(wasmFile)
		cfg := Config{}

		tmpDir, binaryPath, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}

		var stdout bytes.Buffer
		exitCode, err := execute(binaryPath, tmpDir, nil, &stdout, io.Discard)
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}

		if exitCode != 0 {
			t.Errorf("got exit code %d, want 0", exitCode)
		}
		if got := stdout.String(); got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("environ_get-multiple-variables", func(t *testing.T) {
		wasmFile := "../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/environ_get-multiple-variables.wasm"
		wasmPath := testdata(wasmFile)
		cfg := Config{
			Env: []string{"a=text", "b=escap \" ing", "c=new\nline"},
		}

		tmpDir, binaryPath, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}

		exitCode, err := execute(binaryPath, tmpDir, nil, io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}

		if exitCode != 0 {
			t.Errorf("got exit code %d, want 0", exitCode)
		}
	})
}


func TestProcExit(t *testing.T) {
	tests := []struct {
		name         string
		wasmFile     string
		expectedCode int
	}{
		{
			name:         "proc_exit(0)",
			wasmFile:     "../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/proc_exit-success.wasm",
			expectedCode: 0,
		},
		{
			name:         "proc_exit(33)",
			wasmFile:     "../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/proc_exit-failure.wasm",
			expectedCode: 33,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wasmPath := testdata(tt.wasmFile)
			cfg := Config{} // Default config

			tmpDir, binaryPath, err := compile(wasmPath, cfg)
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}

			exitCode, err := execute(binaryPath, tmpDir, nil, io.Discard, io.Discard)
			if err != nil {
				t.Fatalf("execute failed: %v", err)
			}

			if exitCode != tt.expectedCode {
				t.Errorf("got %d, want %d", exitCode, tt.expectedCode)
			}
		})
	}
}


func TestDirMount(t *testing.T) {
	wasmFile := "../../wasi-testsuite/tests/c/testsuite/wasm32-wasip1/fopen-with-access.wasm"
	wasmPath := testdata(wasmFile)
	hostDir := testdata("../../wasi-testsuite/tests/c/testsuite/wasm32-wasip1/fs-tests.dir")

	t.Run("with-dir-mount", func(t *testing.T) {
		cfg := Config{
			Dirs: []DirMount{{Host: hostDir, Guest: "fs-tests.dir"}},
		}

		tmpDir, binaryPath, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}

		exitCode, err := execute(binaryPath, tmpDir, nil, io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}

		if exitCode != 0 {
			t.Errorf("got exit code %d, want 0", exitCode)
		}
	})

	t.Run("without-dir-mount", func(t *testing.T) {
		cfg := Config{}

		tmpDir, binaryPath, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}

		exitCode, err := execute(binaryPath, tmpDir, nil, io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}

		if exitCode == 0 {
			t.Error("got exit code 0, want non-zero (preopen is required)")
		}
	})
}
