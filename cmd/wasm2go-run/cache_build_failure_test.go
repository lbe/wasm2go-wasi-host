package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
