package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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

func TestBuildCacheKeyIsStableAndSensitive(t *testing.T) {
	tmpDir := t.TempDir()

	wasmA := filepath.Join(tmpDir, "a.wasm")
	content := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmA, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Env:      []string{"FOO=bar"},
		Dirs:     []DirMount{{Host: "/host", Guest: "/guest"}},
		WasmArgs: []string{"arg1"},
	}

	key1 := buildCacheKey(wasmA, cfg)

	t.Run("same wasm and cfg gives same key", func(t *testing.T) {
		key2 := buildCacheKey(wasmA, cfg)
		if key1 != key2 {
			t.Errorf("same inputs should give same key, got %q and %q", key1, key2)
		}
	})

	t.Run("different wasm bytes gives different key", func(t *testing.T) {
		wasmB := filepath.Join(tmpDir, "b.wasm")
		if err := os.WriteFile(wasmB, append(content, 0x01), 0644); err != nil {
			t.Fatal(err)
		}
		key2 := buildCacheKey(wasmB, cfg)
		if key1 == key2 {
			t.Errorf("different wasm bytes should give different key, got %q", key1)
		}
	})

	t.Run("different cfg.Env gives different key", func(t *testing.T) {
		cfg2 := cfg
		cfg2.Env = []string{"FOO=baz"}
		key2 := buildCacheKey(wasmA, cfg2)
		if key1 == key2 {
			t.Errorf("different cfg.Env should give different key, got %q", key1)
		}
	})

	t.Run("different cfg.Dirs gives different key", func(t *testing.T) {
		cfg2 := cfg
		cfg2.Dirs = []DirMount{{Host: "/other", Guest: "/guest"}}
		key2 := buildCacheKey(wasmA, cfg2)
		if key1 == key2 {
			t.Errorf("different cfg.Dirs should give different key, got %q", key1)
		}
	})

	t.Run("different cfg.WasmArgs gives different key", func(t *testing.T) {
		cfg2 := cfg
		cfg2.WasmArgs = []string{"arg2"}
		key2 := buildCacheKey(wasmA, cfg2)
		if key1 == key2 {
			t.Errorf("different cfg.WasmArgs should give different key, got %q", key1)
		}
	})

	t.Run("different cfg.WasmPath gives different key", func(t *testing.T) {
		wasmB := filepath.Join(tmpDir, "c.wasm")
		if err := os.WriteFile(wasmB, content, 0644); err != nil {
			t.Fatal(err)
		}
		cfg2 := cfg
		cfg2.WasmPath = wasmB
		key2 := buildCacheKey(wasmA, cfg2)
		if key1 == key2 {
			t.Errorf("different cfg.WasmPath should give different key, got %q", key1)
		}
	})

	t.Run("different buildKeyVersion gives different key", func(t *testing.T) {
		oldVersion := buildKeyVersion
		buildKeyVersion = 999
		defer func() { buildKeyVersion = oldVersion }()

		key2 := buildCacheKey(wasmA, cfg)
		if key1 == key2 {
			t.Errorf("different buildKeyVersion should give different key, got %q", key1)
		}
	})

	t.Run("different wasiHostFingerprint gives different key", func(t *testing.T) {
		old := wasiHostFingerprint
		wasiHostFingerprint = func() string { return "different-host" }
		defer func() { wasiHostFingerprint = old }()

		key2 := buildCacheKey(wasmA, cfg)
		if key1 == key2 {
			t.Errorf("different wasiHostFingerprint should give different key, got %q", key1)
		}
	})

	t.Run("different goVersion gives different key", func(t *testing.T) {
		old := goVersion
		goVersion = "go1.24"
		defer func() { goVersion = old }()

		key2 := buildCacheKey(wasmA, cfg)
		if key1 == key2 {
			t.Errorf("different goVersion should give different key, got %q", key1)
		}
	})

	t.Run("different goos gives different key", func(t *testing.T) {
		old := goos
		goos = "darwin"
		defer func() { goos = old }()

		key2 := buildCacheKey(wasmA, cfg)
		if key1 == key2 {
			t.Errorf("different goos should give different key, got %q", key1)
		}
	})

	t.Run("different goarch gives different key", func(t *testing.T) {
		old := goarch
		goarch = "arm64"
		defer func() { goarch = old }()

		key2 := buildCacheKey(wasmA, cfg)
		if key1 == key2 {
			t.Errorf("different goarch should give different key, got %q", key1)
		}
	})

	t.Run("different wasm2goRunVersion gives different key", func(t *testing.T) {
		old := wasm2goRunVersion
		wasm2goRunVersion = "v1.0.0"
		defer func() { wasm2goRunVersion = old }()

		key2 := buildCacheKey(wasmA, cfg)
		if key1 == key2 {
			t.Errorf("different wasm2goRunVersion should give different key, got %q", key1)
		}
	})

	t.Run("env order does not affect key", func(t *testing.T) {
		cfg1 := Config{Env: []string{"B=2", "A=1"}}
		cfg2 := Config{Env: []string{"A=1", "B=2"}}
		k1 := buildCacheKey(wasmA, cfg1)
		k2 := buildCacheKey(wasmA, cfg2)
		if k1 != k2 {
			t.Errorf("different env order should give same key, got %q and %q", k1, k2)
		}
	})

	t.Run("dir order does not affect key", func(t *testing.T) {
		cfg1 := Config{Dirs: []DirMount{{Host: "/b", Guest: "/g2"}, {Host: "/a", Guest: "/g1"}}}
		cfg2 := Config{Dirs: []DirMount{{Host: "/a", Guest: "/g1"}, {Host: "/b", Guest: "/g2"}}}
		k1 := buildCacheKey(wasmA, cfg1)
		k2 := buildCacheKey(wasmA, cfg2)
		if k1 != k2 {
			t.Errorf("different dir order should give same key, got %q and %q", k1, k2)
		}
	})

	t.Run("WasmPath is normalized to absolute", func(t *testing.T) {
		relPath := filepath.Join(".", filepath.Base(wasmA))
		origWd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Chdir(origWd); err != nil {
				t.Fatalf("restore wd: %v", err)
			}
		}()

		keyRel := buildCacheKey(relPath, cfg)
		keyAbs := buildCacheKey(wasmA, cfg)
		if keyRel != keyAbs {
			t.Errorf("relative and absolute paths should give same key, got %q and %q", keyRel, keyAbs)
		}
	})
}

func TestWasiHostFingerprintIsStableAndSensitiveToSourceChanges(t *testing.T) {
	tmpDir := t.TempDir()

	oldPath := wasiHostPath
	wasiHostPath = func() string { return tmpDir }
	defer func() { wasiHostPath = oldPath }()

	wasihostGo := filepath.Join(tmpDir, "wasihost.go")
	if err := os.WriteFile(wasihostGo, []byte("package wasihost\n// version 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fp1 := wasiHostFingerprint()
	if fp1 == "" {
		t.Fatal("wasiHostFingerprint returned empty string")
	}

	fp2 := wasiHostFingerprint()
	if fp1 != fp2 {
		t.Errorf("wasiHostFingerprint not stable for unchanged sources: %q vs %q", fp1, fp2)
	}

	if err := os.WriteFile(wasihostGo, []byte("package wasihost\n// version 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fp3 := wasiHostFingerprint()
	if fp1 == fp3 {
		t.Errorf("wasiHostFingerprint should change when host source changes, got %q both times", fp1)
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

func TestCacheBuildStoreAndRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", tmpDir)

	key := "buildkey123456789"
	wantBytes := []byte{0x7f, 0x45, 0x4c, 0x46}
	meta := buildCacheMeta{
		TranspileKey:        "transpilekeyabc",
		BuildKeyVersion:     42,
		WasiHostFingerprint: "fingerprint-xyz",
		GoVersion:           "go1.23",
		Goos:                "linux",
		Goarch:              "amd64",
		Wasm2goRunVersion:   "v0.0.1",
	}

	if err := cachePutBuild(key, wantBytes, meta); err != nil {
		t.Fatalf("cachePutBuild failed: %v", err)
	}

	buildDir := filepath.Join(tmpDir, cacheSubdirBuild, key)
	runnerPath := filepath.Join(buildDir, compileBinaryName)
	metaPath := filepath.Join(buildDir, cacheFileMeta)

	runnerData, err := os.ReadFile(runnerPath)
	if err != nil {
		t.Fatalf("runner binary not found after put: %v", err)
	}
	if !bytes.Equal(runnerData, wantBytes) {
		t.Errorf("runner binary content mismatch:\ngot  %q\nwant %q", runnerData, wantBytes)
	}

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("meta.json not found after put: %v", err)
	}
	var gotMeta buildCacheMeta
	if err := json.Unmarshal(metaData, &gotMeta); err != nil {
		t.Fatalf("meta.json invalid JSON: %v", err)
	}
	if gotMeta.TranspileKey != meta.TranspileKey {
		t.Errorf("meta.json transpileKey = %q, want %q", gotMeta.TranspileKey, meta.TranspileKey)
	}
	if gotMeta.BuildKeyVersion != meta.BuildKeyVersion {
		t.Errorf("meta.json buildKeyVersion = %d, want %d", gotMeta.BuildKeyVersion, meta.BuildKeyVersion)
	}
	if gotMeta.WasiHostFingerprint != meta.WasiHostFingerprint {
		t.Errorf("meta.json wasiHostFingerprint = %q, want %q", gotMeta.WasiHostFingerprint, meta.WasiHostFingerprint)
	}
	if gotMeta.GoVersion != meta.GoVersion {
		t.Errorf("meta.json goVersion = %q, want %q", gotMeta.GoVersion, meta.GoVersion)
	}
	if gotMeta.Goos != meta.Goos {
		t.Errorf("meta.json goos = %q, want %q", gotMeta.Goos, meta.Goos)
	}
	if gotMeta.Goarch != meta.Goarch {
		t.Errorf("meta.json goarch = %q, want %q", gotMeta.Goarch, meta.Goarch)
	}
	if gotMeta.Wasm2goRunVersion != meta.Wasm2goRunVersion {
		t.Errorf("meta.json wasm2goRunVersion = %q, want %q", gotMeta.Wasm2goRunVersion, meta.Wasm2goRunVersion)
	}

	gotBytes, ok := cacheGetBuild(key)
	if !ok {
		t.Fatalf("cacheGetBuild returned miss for existing key")
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Errorf("cacheGetBuild returned wrong bytes:\ngot  %q\nwant %q", gotBytes, wantBytes)
	}

	if _, ok := cacheGetBuild("nonexistent"); ok {
		t.Error("cacheGetBuild returned hit for nonexistent key")
	}
}

func TestCacheGetBuildMissOnStaleMetadata(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

	tmpDir := t.TempDir()
	wasmPath := filepath.Join(tmpDir, "test.wasm")
	wasmBytes := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(wasmPath, wasmBytes, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{WasmPath: wasmPath}

	bkey := buildCacheKey(wasmPath, cfg)
	if bkey == "" {
		t.Fatal("buildCacheKey returned empty string")
	}

	// Capture current helper values so we can restore them after the test.
	origGoVersion := goVersion
	origGoos := goos
	origGoarch := goarch
	origWasm2goRunVersion := wasm2goRunVersion
	origBuildKeyVersion := buildKeyVersion
	origWasiHostFingerprint := wasiHostFingerprint
	defer func() {
		goVersion = origGoVersion
		goos = origGoos
		goarch = origGoarch
		wasm2goRunVersion = origWasm2goRunVersion
		buildKeyVersion = origBuildKeyVersion
		wasiHostFingerprint = origWasiHostFingerprint
	}()

	// Prepopulate the tier-2 build cache with a runner and matching metadata.
	fakeRunner := []byte{0x7f, 0x45, 0x4c, 0x46, 0x00, 0x00, 0x00, 0x00}
	meta := currentBuildCacheMeta(wasmPath)
	if err := cachePutBuild(bkey, fakeRunner, meta); err != nil {
		t.Fatalf("cachePutBuild failed: %v", err)
	}

	// Verify the cache entry is readable with matching metadata.
	gotBytes, hit := cacheGetBuild(bkey)
	if !hit {
		t.Fatal("expected cacheGetBuild hit with matching metadata before tampering")
	}
	if len(gotBytes) == 0 {
		t.Fatal("cacheGetBuild returned empty runner with matching metadata")
	}

	// --- Helper metadata fields ---

	t.Run("stale_buildKeyVersion", func(t *testing.T) {
		// Restore helpers to original values first.
		buildKeyVersion = origBuildKeyVersion

		staleMeta := currentBuildCacheMeta(wasmPath)
		staleMeta.BuildKeyVersion = origBuildKeyVersion + 999
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, staleMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit for stale buildKeyVersion; expected miss")
		}
	})

	t.Run("stale_goVersion", func(t *testing.T) {
		staleMeta := currentBuildCacheMeta(wasmPath)
		staleMeta.GoVersion = "go0.0.0-stale"
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, staleMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit for stale goVersion; expected miss")
		}
	})

	t.Run("stale_goos", func(t *testing.T) {
		staleMeta := currentBuildCacheMeta(wasmPath)
		staleMeta.Goos = "fakeos"
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, staleMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit for stale goos; expected miss")
		}
	})

	t.Run("stale_goarch", func(t *testing.T) {
		staleMeta := currentBuildCacheMeta(wasmPath)
		staleMeta.Goarch = "fakearch"
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, staleMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit for stale goarch; expected miss")
		}
	})

	t.Run("stale_wasm2goRunVersion", func(t *testing.T) {
		staleMeta := currentBuildCacheMeta(wasmPath)
		staleMeta.Wasm2goRunVersion = "v0.0.0-stale"
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, staleMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit for stale wasm2goRunVersion; expected miss")
		}
	})

	t.Run("stale_wasiHostFingerprint", func(t *testing.T) {
		staleMeta := currentBuildCacheMeta(wasmPath)
		staleMeta.WasiHostFingerprint = "stale-fingerprint-xyz"
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, staleMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit for stale wasiHostFingerprint; expected miss")
		}
	})

	t.Run("stale_transpileKey", func(t *testing.T) {
		staleMeta := currentBuildCacheMeta(wasmPath)
		staleMeta.TranspileKey = "stale-transpile-key"
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, staleMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit for stale transpileKey; expected miss")
		}
	})

	// --- Helper stub approach: change state between put and get ---

	t.Run("changed_wasiHostFingerprint_between_put_and_get", func(t *testing.T) {
		// Rewrite meta with current values, then change the helper.
		freshMeta := currentBuildCacheMeta(wasmPath)
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, freshMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		// Simulate the host source being updated after cache population.
		wasiHostFingerprint = func() string { return "changed-fingerprint-after-cache" }

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit after wasiHostFingerprint helper changed; expected miss")
		}
	})

	t.Run("changed_goVersion_between_put_and_get", func(t *testing.T) {
		freshMeta := currentBuildCacheMeta(wasmPath)
		buildDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
		if err := writeCacheMeta(buildDir, freshMeta); err != nil {
			t.Fatalf("writeCacheMeta failed: %v", err)
		}

		goVersion = "go99.99"

		_, hit := cacheGetBuild(bkey)
		if hit {
			t.Error("cacheGetBuild returned hit after goVersion helper changed; expected miss")
		}
	})
}

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

func TestFailedBuildDoesNotPolluteTier2Cache(t *testing.T) {
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

	// Install a fake go that fails only on "build" subcommand but delegates
	// "mod tidy" to the real go. This ensures buildCompileBinary gets past
	// tidy but fails at the actual build step.
	fakeGoDir := t.TempDir()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	fakeGoScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"build\" ]; then\n" +
		"  echo 'fake go build failure' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"exec " + realGo + " \"$@\"\n"
	if writeErr := os.WriteFile(filepath.Join(fakeGoDir, "go"), []byte(fakeGoScript), 0o755); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", fakeGoDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Compile should fail because go build exits 1.
	buildDir, binaryPath, err := compile(wasmPath, cfg)
	if err == nil {
		if buildDir != "" {
			defer os.RemoveAll(buildDir)
		}
		t.Fatalf("expected compile error when go build fails, got binaryPath=%q", binaryPath)
	}
	if buildDir != "" {
		t.Errorf("expected empty buildDir on failure, got %q", buildDir)
	}
	if binaryPath != "" {
		t.Errorf("expected empty binaryPath on failure, got %q", binaryPath)
	}

	// Verify no tier-2 cache entry was created by the failed build.
	if _, hit := cacheGetBuild(bkey); hit {
		t.Error("cacheGetBuild returned hit after failed build; no tier-2 cache entry should exist")
	}

	// Verify no build/<key>/runner or meta.json exist.
	buildEntryDir := filepath.Join(cacheDir, cacheSubdirBuild, bkey)
	for _, name := range []string{compileBinaryName, cacheFileMeta} {
		path := filepath.Join(buildEntryDir, name)
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("%s should not exist after failed build", name)
		} else if !os.IsNotExist(statErr) {
			t.Fatalf("stat %s: %v", name, statErr)
		}
	}

	// Verify no .tmp files or entry directories left under build/.
	buildCacheDir := filepath.Join(cacheDir, cacheSubdirBuild)
	entries, readErr := os.ReadDir(buildCacheDir)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("failed to read build cache dir: %v", readErr)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".tmp" {
			t.Errorf(".tmp file left behind in build cache after failed build: %s", entry.Name())
		}
		// No entry directory should exist at all for this key.
		if entry.Name() == bkey {
			t.Errorf("build cache entry directory %q should not exist after failed build", bkey)
		}
	}
}
