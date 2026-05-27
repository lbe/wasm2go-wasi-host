package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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

func TestProcExitRepeatCompileAndRunWithCache(t *testing.T) {
	requireWasm2go(t)

	cacheDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

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
			cfg := Config{}

			// First compile: full pipeline, populates tier-1 and tier-2 caches.
			buildDir1, binaryPath1, err := compile(wasmPath, cfg)
			if err != nil {
				t.Fatalf("first compile failed: %v", err)
			}
			defer os.RemoveAll(buildDir1)
			if binaryPath1 == "" {
				t.Fatal("first compile returned empty binaryPath")
			}

			// Verify tier-1 cache: transpile/<tkey>/module.go exists.
			tkey := transpileCacheKey(wasmPath)
			tier1ModulePath := filepath.Join(cacheDir, cacheSubdirTranspile, tkey, cacheFileModule)
			if _, statErr := os.Stat(tier1ModulePath); statErr != nil {
				t.Fatalf("tier-1 cache module.go not found after first compile: %v", statErr)
			}

			// Verify tier-2 cache: build/<bkey>/runner exists.
			bkey := buildCacheKey(wasmPath, cfg)
			tier2RunnerPath := tier2CachedRunnerPath(bkey)
			if _, statErr := os.Stat(tier2RunnerPath); statErr != nil {
				t.Fatalf("tier-2 cache runner not found after first compile: %v", statErr)
			}

			// Install fake-fail wasm2go and fake-fail go.
			installFakeFailWasm2go(t)
			installFakeFailGo(t)

			// Second compile: should hit tier-1+tier-2 cache, zero toolchain invocations.
			buildDir2, binaryPath2, err := compile(wasmPath, cfg)
			if err != nil {
				t.Fatalf("second compile failed (should have hit cache): %v", err)
			}
			defer os.RemoveAll(buildDir2)

			if binaryPath2 == "" {
				t.Fatal("second compile returned empty binaryPath")
			}

			// Verify the second compile runner is byte-identical to the tier-2 cache.
			secondRunnerBytes, err := os.ReadFile(binaryPath2)
			if err != nil {
				t.Fatalf("failed to read second compile runner: %v", err)
			}
			cachedRunnerBytes, err := os.ReadFile(tier2RunnerPath)
			if err != nil {
				t.Fatalf("failed to read tier-2 cache runner: %v", err)
			}
			if !bytes.Equal(secondRunnerBytes, cachedRunnerBytes) {
				t.Errorf("second compile runner does not match tier-2 cache runner")
			}

			// Verify the binary is within the build dir.
			assertBinaryWithinBuildDir(t, buildDir2, binaryPath2)

			// Execute the cached binary and verify exit code.
			exitCode, err := execute(binaryPath2, buildDir2, nil, io.Discard, io.Discard)
			if err != nil {
				t.Fatalf("execute failed: %v", err)
			}

			if exitCode != tt.expectedCode {
				t.Errorf("got exit code %d, want %d", exitCode, tt.expectedCode)
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

	t.Run("writable-mount-is-actually-writable", func(t *testing.T) {
		wasmFile := "../../wasi-testsuite/tests/rust/testsuite/wasm32-wasip1/path_open_read_write.wasm"
		wPath := testdata(wasmFile)
		hostDir := t.TempDir()

		cfg := Config{
			Dirs:     []DirMount{{Host: hostDir, Guest: "scratch"}},
			WasmArgs: []string{"scratch"},
		}

		tmpDir, binaryPath, err := compile(wPath, cfg)
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
