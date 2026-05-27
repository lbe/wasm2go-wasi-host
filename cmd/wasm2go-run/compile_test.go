package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	t.Run("tier2_cache_hit_skips_toolchain", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

		wasmPath := testdataWasm("proc_exit-success.wasm")
		cfg := Config{WasmPath: wasmPath}

		bkey := buildCacheKey(wasmPath, cfg)
		if bkey == "" {
			t.Fatal("buildCacheKey returned empty string")
		}

		cachedRunner := []byte{0x7f, 0x45, 0x4c, 0x46, 0xCA, 0xFE, 0xBA, 0xBE}
		meta := buildCacheMeta{
			TranspileKey:      transpileCacheKey(wasmPath),
			BuildKeyVersion:   buildKeyVersion,
			GoVersion:         goVersion,
			Goos:              goos,
			Goarch:            goarch,
			Wasm2goRunVersion: wasm2goRunVersion,
		}
		if err := cachePutBuild(bkey, cachedRunner, meta); err != nil {
			t.Fatalf("cachePutBuild failed: %v", err)
		}

		cachedBuildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)

		installFakeFailWasm2go(t)
		installFakeFailGo(t)

		buildDir, binaryPath, err := compile(wasmPath, cfg)
		if err != nil {
			t.Fatalf("compile failed on tier-2 cache hit: %v", err)
		}
		defer os.RemoveAll(buildDir)

		if binaryPath == "" {
			t.Fatal("binaryPath is empty")
		}

		absBuildDir, _ := filepath.Abs(buildDir)
		absCachedBuildDir, _ := filepath.Abs(cachedBuildDir)
		if absBuildDir == absCachedBuildDir {
			t.Fatalf("binaryPath is directly inside cache build dir %q instead of a new temp dir", absCachedBuildDir)
		}

		assertBinaryWithinBuildDir(t, buildDir, binaryPath)

		info, err := os.Stat(binaryPath)
		if err != nil {
			t.Fatalf("failed to stat binary: %v", err)
		}
		if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
			t.Errorf("binary is not executable: %v", info.Mode())
		}

		gotBytes, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("failed to read binary: %v", err)
		}
		if !bytes.Equal(gotBytes, cachedRunner) {
			t.Errorf("binary content mismatch: got %d bytes, want %d bytes", len(gotBytes), len(cachedRunner))
		}
	})
}

func TestCompileCacheDisabledSkipsTier2Cache(t *testing.T) {
	requireWasm2go(t)

	cacheDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

	// Disable tier-2 cache via environment variable.
	t.Setenv("WASM2GO_RUN_CACHE", "0")

	wasmPath := testdataWasm("proc_exit-success.wasm")
	cfg := Config{WasmPath: wasmPath}

	// Prepopulate a tier-2 build cache entry so that a naïve cacheGetBuild
	// would return a hit.
	bkey := buildCacheKey(wasmPath, cfg)
	if bkey == "" {
		t.Fatal("buildCacheKey returned empty string")
	}

	fakeRunner := []byte{0x7f, 0x45, 0x4c, 0x46, 0xCA, 0xFE, 0xBA, 0xBE}
	meta := buildCacheMeta{
		TranspileKey:      transpileCacheKey(wasmPath),
		BuildKeyVersion:   buildKeyVersion,
		GoVersion:         goVersion,
		Goos:              goos,
		Goarch:            goarch,
		Wasm2goRunVersion: wasm2goRunVersion,
	}
	if err := cachePutBuild(bkey, fakeRunner, meta); err != nil {
		t.Fatalf("cachePutBuild failed: %v", err)
	}

	// Install a fake go that records invocations then delegates to the real go.
	// If the tier-2 cache were incorrectly consulted, the fake cached runner
	// would be returned and this fake go would NOT be called.
	fakeGoDir := t.TempDir()
	goInvocationLog := filepath.Join(fakeGoDir, "go-invocations.log")
	realGo, goLookErr := exec.LookPath("go")
	if goLookErr != nil {
		t.Skip("go not on PATH")
	}
	fakeGoScript := fmt.Sprintf("#!/bin/sh\necho invoked >> %s\nexec %s \"$@\"\n", goInvocationLog, realGo)
	if err := os.WriteFile(filepath.Join(fakeGoDir, "go"), []byte(fakeGoScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeGoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	buildDir, binaryPath, err := compile(wasmPath, cfg)
	if err != nil {
		t.Fatalf("compile failed with cache disabled: %v", err)
	}
	defer os.RemoveAll(buildDir)

	if binaryPath == "" {
		t.Fatal("binaryPath is empty")
	}

	// Verify the go toolchain was actually invoked (cache was bypassed).
	invocationData, readErr := os.ReadFile(goInvocationLog)
	if readErr != nil {
		t.Fatalf("go invocation log not found: %v", readErr)
	}
	if len(invocationData) == 0 {
		t.Fatal("go toolchain was not invoked; compile appears to have used the cached binary instead of bypassing the disabled cache")
	}

	// Verify no build/ entries were created under the cache root.
	buildDirEntries, err := os.ReadDir(filepath.Join(cacheDir, cacheSubdirBuild))
	if err == nil && len(buildDirEntries) > 0 {
		// The prepopulated entry is expected, but no NEW entries should be
		// written. Count entries: if more than the one we prepopulated exist,
		// cachePutBuild was called when it shouldn't have been.
		for _, entry := range buildDirEntries {
			if entry.Name() != bkey {
				t.Errorf("unexpected build cache entry %q created with cache disabled", entry.Name())
			}
		}
	}

	// Verify the binary is NOT the fake cached runner (proving cache was ignored).
	gotBytes, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("failed to read binary: %v", err)
	}
	if bytes.Equal(gotBytes, fakeRunner) {
		t.Error("binary matches the prepopulated cache entry; compile used the disabled cache instead of invoking the toolchain")
	}
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

	assertBinaryWithinBuildDir(t, buildDir, binaryPath)
}

func TestCompileTier2CacheMissPopulatesCache(t *testing.T) {
	requireWasm2go(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}

	cacheDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

	wasmPath := testdataWasm("proc_exit-success.wasm")
	cfg := Config{WasmPath: wasmPath}

	bkey := buildCacheKey(wasmPath, cfg)
	if bkey == "" {
		t.Fatal("buildCacheKey returned empty string")
	}
	tkey := transpileCacheKey(wasmPath)
	if tkey == "" {
		t.Fatal("transpileCacheKey returned empty string")
	}

	// First compile: cache miss, runs full pipeline.
	buildDir1, binaryPath1, err := compile(wasmPath, cfg)
	if err != nil {
		t.Fatalf("first compile failed: %v", err)
	}
	defer os.RemoveAll(buildDir1)
	if binaryPath1 == "" {
		t.Fatal("first compile returned empty binaryPath")
	}

	// Verify tier-2 build cache was populated: build/<bkey>/runner exists.
	cachedRunnerPath := tier2CachedRunnerPath(bkey)
	cachedRunnerBytes, err := os.ReadFile(cachedRunnerPath)
	if err != nil {
		t.Fatalf("tier-2 cache runner not found at %s after first compile: %v", cachedRunnerPath, err)
	}
	if len(cachedRunnerBytes) == 0 {
		t.Fatal("tier-2 cache runner is empty")
	}

	// Verify tier-1 transpile cache was populated: transpile/<tkey>/module.go exists.
	cachedModulePath := filepath.Join(cacheDir, cacheSubdirTranspile, tkey, cacheFileModule)
	if _, statErr := os.Stat(cachedModulePath); statErr != nil {
		t.Fatalf("tier-1 cache module.go not found at %s after first compile: %v", cachedModulePath, statErr)
	}

	// Read the runner from the first compile output for later comparison.
	firstRunnerBytes, err := os.ReadFile(binaryPath1)
	if err != nil {
		t.Fatalf("failed to read first compile runner: %v", err)
	}

	// Install fake-fail wasm2go and fake-fail go to PATH.
	installFakeFailWasm2go(t)
	installFakeFailGo(t)

	// Second compile: should hit tier-2 cache, zero toolchain invocations.
	buildDir2, binaryPath2, err := compile(wasmPath, cfg)
	if err != nil {
		t.Fatalf("second compile failed (should have hit tier-2 cache): %v", err)
	}
	defer os.RemoveAll(buildDir2)
	if binaryPath2 == "" {
		t.Fatal("second compile returned empty binaryPath")
	}

	// Verify runner bytes match the cached runner file.
	secondRunnerBytes, err := os.ReadFile(binaryPath2)
	if err != nil {
		t.Fatalf("failed to read second compile runner: %v", err)
	}
	if !bytes.Equal(secondRunnerBytes, cachedRunnerBytes) {
		t.Errorf("second compile runner does not match cached runner: got %d bytes, want %d bytes", len(secondRunnerBytes), len(cachedRunnerBytes))
	}

	// Also verify the first compile output matches the cache.
	if !bytes.Equal(firstRunnerBytes, cachedRunnerBytes) {
		t.Errorf("first compile runner does not match cached runner: got %d bytes, want %d bytes", len(firstRunnerBytes), len(cachedRunnerBytes))
	}
}
