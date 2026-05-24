package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompile(t *testing.T) {
	wasmPath := testdataWasm("proc_exit-success.wasm")

	t.Run("success_produces_executable", func(t *testing.T) {
		cfg := Config{WasmPath: wasmPath}

		buildDir, binaryPath, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		defer os.RemoveAll(buildDir)

		assertCompileOutput(t, buildDir, binaryPath)
	})

	t.Run("second_compile_uses_tier1_cache", func(t *testing.T) {
		requireWasm2go(t)

		cacheDir := t.TempDir()
		t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

		wasmPath := testdataWasm("proc_exit-success.wasm")
		cfg := Config{WasmPath: wasmPath}

		buildDir1, binaryPath1, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("first compile failed: %v", err)
		}
		defer os.RemoveAll(buildDir1)
		if binaryPath1 == "" {
			t.Fatal("first compile returned empty binaryPath")
		}

		if _, statErr := os.Stat(tier1CachedModulePath(wasmPath)); statErr != nil {
			t.Fatalf("tier-1 cache not populated after first compile: %v", statErr)
		}

		installFakeFailWasm2go(t)

		buildDir2, binaryPath2, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("second compile failed (should have hit tier-1 cache): %v", err)
		}
		defer os.RemoveAll(buildDir2)
		if binaryPath2 == "" {
			t.Fatal("second compile returned empty binaryPath")
		}
	})

	t.Run("failure_returns_error", func(t *testing.T) {
		cfg := Config{WasmPath: "non-existent.wasm"}

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

func assertCompileOutput(t *testing.T, buildDir, binaryPath string) {
	t.Helper()
	if buildDir == "" {
		t.Error("buildDir is empty")
	}
	if binaryPath == "" {
		t.Error("binaryPath is empty")
	}

	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("failed to stat binary: %v", err)
	}

	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Errorf("binary is not executable: %v", info.Mode())
	}

	absBuildDir, _ := filepath.Abs(buildDir)
	absBinaryPath, _ := filepath.Abs(binaryPath)
	rel, relErr := filepath.Rel(absBuildDir, absBinaryPath)
	if relErr != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("binaryPath %q is not within buildDir %q", absBinaryPath, absBuildDir)
	}
}
