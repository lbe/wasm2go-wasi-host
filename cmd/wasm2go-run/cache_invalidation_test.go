package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
