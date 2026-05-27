package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeFailGoScript = "#!/bin/sh\necho 'fake go should not be called' >&2\nexit 1\n"

// installFakeWasm2go writes a fake wasm2go executable into dir, prepends dir
// to PATH, and marks the file executable.
func installFakeWasm2go(t *testing.T, dir, script string) {
	t.Helper()
	fakeWasm2go := filepath.Join(dir, "wasm2go")
	if err := os.WriteFile(fakeWasm2go, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fakeWasm2go, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func testdata(rel string) string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	return filepath.Join(dir, rel)
}

// testdataWasm returns the absolute path to a WASM test file in the
// wasi-testsuite assemblyscript testsuite.
func testdataWasm(name string) string {
	return testdata("../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/" + name)
}

// requireWasm2go skips the test when wasm2go is not on PATH.
func requireWasm2go(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("wasm2go"); err != nil {
		t.Skip("wasm2go not on PATH")
	}
}

// installFakeFailWasm2go prepends a fake wasm2go to PATH that exits non-zero
// if invoked, proving a cache hit avoided calling wasm2go.
func installFakeFailWasm2go(t *testing.T) {
	t.Helper()
	installFakeWasm2go(t, t.TempDir(), `#!/bin/sh
echo "fake wasm2go should not be called" >&2
exit 1
`)
}

// tier1CachedModulePath returns the path to module.go for wasmPath in the
// active tier-1 cache (WASM2GO_RUN_CACHE_DIR must be set in tests).
func tier1CachedModulePath(wasmPath string) string {
	return filepath.Join(cacheTranspileEntryPath(transpileCacheKey(wasmPath)), cacheFileModule)
}

// tier2CachedRunnerPath returns the path to the cached runner binary for the
// given build key in the active tier-2 cache.
func tier2CachedRunnerPath(bkey string) string {
	return filepath.Join(cacheDir(), cacheSubdirBuild, bkey, compileBinaryName)
}

// installFakeFailGo writes a fake go executable that always fails into a temp
// directory and prepends that directory to PATH.
func installFakeFailGo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeFailGoScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// assertBinaryWithinBuildDir reports an error via t if binaryPath is not
// a descendant of buildDir.
func assertBinaryWithinBuildDir(t *testing.T, buildDir, binaryPath string) {
	t.Helper()
	absBuildDir, _ := filepath.Abs(buildDir)
	absBinaryPath, _ := filepath.Abs(binaryPath)
	rel, relErr := filepath.Rel(absBuildDir, absBinaryPath)
	if relErr != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("binaryPath %q is not within buildDir %q", absBinaryPath, absBuildDir)
	}
}
