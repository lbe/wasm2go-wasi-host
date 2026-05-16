package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompile(t *testing.T) {
	wasmPath := testdata("../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/proc_exit-success.wasm")

	t.Run("success_produces_executable", func(t *testing.T) {
		cfg := Config{
			WasmPath: wasmPath,
		}

		buildDir, binaryPath, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if buildDir == "" {
			t.Error("buildDir is empty")
		}
		if binaryPath == "" {
			t.Error("binaryPath is empty")
		}

		defer os.RemoveAll(buildDir)

		info, err := os.Stat(binaryPath)
		if err != nil {
			t.Fatalf("failed to stat binary: %v", err)
		}

		// Check if executable bit is set
		if runtime.GOOS != "windows" {
			if info.Mode()&0111 == 0 {
				t.Errorf("binary is not executable: %v", info.Mode())
			}
		}

		// buildDir must be parent of binaryPath
		absBuildDir, _ := filepath.Abs(buildDir)
		absBinaryPath, _ := filepath.Abs(binaryPath)
		rel, relErr := filepath.Rel(absBuildDir, absBinaryPath)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("binaryPath %q is not within buildDir %q", absBinaryPath, absBuildDir)
		}
	})

	t.Run("failure_returns_error", func(t *testing.T) {
		// Test with a non-existent wasm path to trigger failure
		cfg := Config{
			WasmPath: "non-existent.wasm",
		}

		buildDir, binaryPath, err := compile("non-existent.wasm", cfg)
		if err == nil {
			defer os.RemoveAll(buildDir)
			t.Error("expected error for non-existent wasm file, got nil")
		}
		if buildDir != "" {
			t.Errorf("expected empty buildDir on failure, got %q", buildDir)
		}
		if binaryPath != "" {
			t.Errorf("expected empty binaryPath on failure, got %q", binaryPath)
		}
	})
}
