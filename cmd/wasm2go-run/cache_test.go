package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCacheEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		env  string
		want bool
	}{
		{
			name: "flag off disables cache",
			cfg:  Config{Cache: "off"},
			env:  "",
			want: false,
		},
		{
			name: "env 0 disables cache",
			cfg:  Config{},
			env:  "0",
			want: false,
		},
		{
			name: "env false disables cache",
			cfg:  Config{},
			env:  "false",
			want: false,
		},
		{
			name: "env off disables cache case insensitive",
			cfg:  Config{},
			env:  "OFF",
			want: false,
		},
		{
			name: "default empty env enables cache",
			cfg:  Config{},
			env:  "",
			want: true,
		},
		{
			name: "env true enables cache",
			cfg:  Config{},
			env:  "true",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WASM2GO_RUN_CACHE", tt.env)

			got := cacheEnabled(tt.cfg)
			if got != tt.want {
				t.Errorf("cacheEnabled(%+v) = %v, want %v", tt.cfg, got, tt.want)
			}
		})
	}
}

func TestTranspileCacheKeyIsStableAndSensitive(t *testing.T) {
	tmpDir := t.TempDir()

	wasmA := filepath.Join(tmpDir, "a.wasm")
	content := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmA, content, 0644); err != nil {
		t.Fatal(err)
	}

	key1 := transpileCacheKey(wasmA)
	if key1 == "" {
		t.Fatal("transpileCacheKey returned empty string")
	}

	t.Run("same file gives same key", func(t *testing.T) {
		key2 := transpileCacheKey(wasmA)
		if key1 != key2 {
			t.Errorf("same wasm should give same key, got %q and %q", key1, key2)
		}
	})

	t.Run("different wasm bytes gives different key", func(t *testing.T) {
		wasmB := filepath.Join(tmpDir, "b.wasm")
		if err := os.WriteFile(wasmB, append(content, 0x01), 0644); err != nil {
			t.Fatal(err)
		}
		key3 := transpileCacheKey(wasmB)
		if key1 == key3 {
			t.Errorf("different wasm bytes should give different key, got %q", key1)
		}
	})

	t.Run("different wasm2go identity gives different key", func(t *testing.T) {
		oldIdentity := wasm2goIdentity
		wasm2goIdentity = func() string { return "different" }
		defer func() { wasm2goIdentity = oldIdentity }()

		key4 := transpileCacheKey(wasmA)
		if key1 == key4 {
			t.Errorf("different wasm2go identity should give different key, got %q", key1)
		}
	})

	t.Run("different cache key version gives different key", func(t *testing.T) {
		oldVersion := cacheKeyVersion
		cacheKeyVersion = 999
		defer func() { cacheKeyVersion = oldVersion }()

		key5 := transpileCacheKey(wasmA)
		if key1 == key5 {
			t.Errorf("different cacheKeyVersion should give different key, got %q", key1)
		}
	})
}

func TestCacheDirResolution(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "WASM2GO_RUN_CACHE_DIR takes precedence",
			env: map[string]string{
				"WASM2GO_RUN_CACHE_DIR": "/tmp/custom-cache",
				"XDG_CACHE_HOME":        "/tmp/xdg-cache",
			},
			want: "/tmp/custom-cache",
		},
		{
			name: "XDG_CACHE_HOME used when WASM2GO_RUN_CACHE_DIR unset",
			env: map[string]string{
				"WASM2GO_RUN_CACHE_DIR": "",
				"XDG_CACHE_HOME":        "/tmp/xdg-cache",
			},
			want: filepath.Join("/tmp/xdg-cache", "wasm2go-run"),
		},
		{
			name: "default to ~/.cache when neither set",
			env: map[string]string{
				"WASM2GO_RUN_CACHE_DIR": "",
				"XDG_CACHE_HOME":        "",
			},
			want: filepath.Join(os.Getenv("HOME"), ".cache", "wasm2go-run"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got := cacheDir()
			if got != tt.want {
				t.Errorf("cacheDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCacheTranspileStoreAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", tmpDir)

	key := "abc123def456789"
	wantSrc := "package wasm\n\nfunc Add(a, b int) int { return a + b }\n"
	meta := currentTranspileCacheMeta()
	meta.WasmSize = 1024

	if err := cachePutTranspile(key, wantSrc, meta); err != nil {
		t.Fatalf("cachePutTranspile failed: %v", err)
	}

	transpileDir := filepath.Join(tmpDir, "transpile", key)
	modulePath := filepath.Join(transpileDir, "module.go")
	metaPath := filepath.Join(transpileDir, "meta.json")

	moduleData, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatalf("module.go not found after put: %v", err)
	}
	if string(moduleData) != wantSrc {
		t.Errorf("module.go content mismatch:\ngot  %q\nwant %q", string(moduleData), wantSrc)
	}

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("meta.json not found after put: %v", err)
	}
	var gotMeta transpileCacheMeta
	if err := json.Unmarshal(metaData, &gotMeta); err != nil {
		t.Fatalf("meta.json invalid JSON: %v", err)
	}
	if gotMeta.Wasm2goID != meta.Wasm2goID {
		t.Errorf("meta.json wasm2goID = %q, want %q", gotMeta.Wasm2goID, meta.Wasm2goID)
	}
	if gotMeta.PostprocessVersion != meta.PostprocessVersion {
		t.Errorf("meta.json postprocessVersion = %d, want %d", gotMeta.PostprocessVersion, meta.PostprocessVersion)
	}
	if gotMeta.WasmSize != meta.WasmSize {
		t.Errorf("meta.json wasmSize = %d, want %d", gotMeta.WasmSize, meta.WasmSize)
	}

	gotSrc, ok := cacheGetTranspile(key)
	if !ok {
		t.Fatalf("cacheGetTranspile returned miss for existing key")
	}
	if gotSrc != wantSrc {
		t.Errorf("cacheGetTranspile returned wrong source:\ngot  %q\nwant %q", gotSrc, wantSrc)
	}
}

func TestTranspileCachedConcurrentPopulationBothSucceedWithConsistentModule(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

	invocationLog := filepath.Join(tmpDir, "invocations.log")
	installFakeWasm2go(t, tmpDir, `#!/bin/sh
sleep 0.2
echo 1 >> "`+invocationLog+`"
printf '%s\n' 'package main' 'func New() *Module { return nil }'
`)

	wasmPath := testdataWasm("proc_exit-success.wasm")
	wantSrc := "package main\nfunc New() *Module { return nil }\n"
	cfg := Config{}

	const workers = 2
	var wg sync.WaitGroup
	wg.Add(workers)
	errs := make([]error, workers)
	srcs := make([]string, workers)

	for i := range workers {
		go func(i int) {
			defer wg.Done()
			srcs[i], errs[i] = transpileCached(wasmPath, cfg)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: transpileCached failed: %v", i, err)
		}
	}
	for i, src := range srcs {
		if src != wantSrc {
			t.Errorf("worker %d: unexpected source:\ngot  %q\nwant %q", i, src, wantSrc)
		}
	}

	key := transpileCacheKey(wasmPath)
	modulePath := filepath.Join(cacheDir, "transpile", key, "module.go")
	moduleData, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatalf("module.go not found after concurrent populate: %v", err)
	}
	if string(moduleData) != wantSrc {
		t.Errorf("module.go content mismatch:\ngot  %q\nwant %q", string(moduleData), wantSrc)
	}

	invocations, err := os.ReadFile(invocationLog)
	if err != nil {
		t.Fatalf("wasm2go invocation log missing: %v", err)
	}
	if got, want := strings.Count(string(invocations), "\n"), 1; got != want {
		t.Errorf("wasm2go invocation count = %d, want %d (single writer for concurrent cache populate)", got, want)
	}
}

func TestTranspileCachedFailedTranspileDoesNotLeaveCacheEntry(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

	installFakeWasm2go(t, tmpDir, `#!/bin/sh
echo "fake wasm2go failed" >&2
exit 1
`)

	wasmPath := filepath.Join(tmpDir, "tiny.wasm")
	wasmBytes := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmPath, wasmBytes, 0644); err != nil {
		t.Fatal(err)
	}
	key := transpileCacheKey(wasmPath)
	if key == "" {
		t.Fatal("transpileCacheKey returned empty string")
	}

	_, err := transpileCached(wasmPath, Config{})
	if err == nil {
		t.Fatal("expected transpileCached error when wasm2go exits non-zero")
	}

	if _, hit := cacheGetTranspile(key); hit {
		t.Error("cacheGetTranspile returned hit after failed transpile")
	}

	transpileDir := filepath.Join(cacheDir, "transpile", key)
	for _, name := range []string{cacheFileModule, cacheFileMeta} {
		path := filepath.Join(transpileDir, name)
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("%s should not exist after failed transpile", name)
		} else if !os.IsNotExist(statErr) {
			t.Fatalf("stat %s: %v", name, statErr)
		}
	}

	tmpMatches, err := filepath.Glob(filepath.Join(transpileDir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob tmp files: %v", err)
	}
	if len(tmpMatches) > 0 {
		t.Errorf("partial tmp files left behind: %v", tmpMatches)
	}

	if _, statErr := os.Stat(transpileDir); statErr == nil {
		t.Errorf("transpile cache entry directory %q should not exist after failed transpile", transpileDir)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat transpile dir: %v", statErr)
	}
}
