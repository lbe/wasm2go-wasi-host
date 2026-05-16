package main

import (
	"io"
	"testing"
)

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

			exitCode, err := execute(binaryPath, tmpDir, io.Discard, io.Discard)
			if err != nil {
				t.Fatalf("execute failed: %v", err)
			}

			if exitCode != tt.expectedCode {
				t.Errorf("got %d, want %d", exitCode, tt.expectedCode)
			}
		})
	}
}
