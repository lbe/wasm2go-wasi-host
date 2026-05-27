package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentCachePutBuildSingleWriterSafeReaders(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

	tmpDir := t.TempDir()
	wasmPath := filepath.Join(tmpDir, "test.wasm")
	wasmBytes := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmPath, wasmBytes, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{WasmPath: wasmPath}
	key := buildCacheKey(wasmPath, cfg)
	if key == "" {
		t.Fatal("buildCacheKey returned empty string")
	}

	meta := currentBuildCacheMeta(wasmPath)

	// Use a barrier to ensure both goroutines enter cachePutBuild simultaneously.
	start := make(chan struct{})
	const workers = 2
	var wg sync.WaitGroup
	wg.Add(workers)

	errs := make([]error, workers)
	runnerBytes := bytes.Repeat([]byte{0xCC}, 4096)

	for i := range workers {
		go func(i int) {
			defer wg.Done()
			<-start // barrier: both workers start together
			errs[i] = cachePutBuild(key, runnerBytes, meta)
		}(i)
	}
	close(start) // release both workers simultaneously
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: cachePutBuild failed: %v", i, err)
		}
	}

	// The cache entry must be readable and consistent.
	gotFromCache, hit := cacheGetBuild(key)
	if !hit {
		t.Fatal("cacheGetBuild returned miss after concurrent writes")
	}
	if !bytes.Equal(gotFromCache, runnerBytes) {
		t.Errorf("cacheGetBuild returned unexpected bytes")
	}

	// Verify the runner file is clean (not corrupted by concurrent writes).
	runnerPath := filepath.Join(cacheDir, cacheSubdirBuild, key, compileBinaryName)
	gotBytes, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatalf("failed to read cached runner: %v", err)
	}
	if !bytes.Equal(gotBytes, runnerBytes) {
		t.Errorf("runner file content mismatch")
	}
}
