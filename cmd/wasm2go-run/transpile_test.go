package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseImports(t *testing.T) {
	t.Run("single import", func(t *testing.T) {
		src := `package main
func New(v0 Xwasi_snapshot_preview1) *Module { return nil }`
		want := []string{"Xwasi_snapshot_preview1"}
		got, err := parseImports(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("multiple imports", func(t *testing.T) {
		src := `package main
func New(v0 Xenv, v1 Xwasi_snapshot_preview1) *Module { return nil }`
		want := []string{"Xenv", "Xwasi_snapshot_preview1"}
		got, err := parseImports(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("missing New function", func(t *testing.T) {
		src := `package main
func NotNew() {}`
		_, err := parseImports(src)
		if err == nil {
			t.Error("expected error when New function is missing, got nil")
		}
	})
}

func TestTranspile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		wasmPath := testdataWasm("proc_exit-success.wasm")
		src, err := transpile(wasmPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(src, "func (m *Module) X_start()") {
			t.Errorf("expected source to contain X_start function")
		}
		if !strings.Contains(src, "func New(") {
			t.Errorf("expected source to contain New function")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := transpile("/path/to/nowhere.wasm")
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})

	t.Run("wasm2go not on PATH", func(t *testing.T) {
		t.Setenv("PATH", "")
		wasmPath := testdata("../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/proc_exit-success.wasm")
		_, err := transpile(wasmPath)
		if err == nil {
			t.Error("expected error when wasm2go is not on PATH, got nil")
		}
	})

	t.Run("deduplicates duplicate import methods", func(t *testing.T) {
		wasmPath := testdataWasm("fd_write-to-stdout.wasm")
		src, err := transpile(wasmPath)
		if err != nil {
			t.Fatalf("transpile failed: %v", err)
		}
		assertNoDuplicateInterfaceMethods(t, src)
	})
}

func TestTranspileCached(t *testing.T) {
	t.Run("cache disabled invokes wasm2go and does not touch cache root", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := t.TempDir()
		t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

		markerPath := filepath.Join(tmpDir, "wasm2go-was-called")
		installFakeWasm2go(t, tmpDir, `#!/bin/sh
echo 'package main
func New() *Module { return nil }'
touch `+markerPath+`
`)

		wasmPath := testdataWasm("proc_exit-success.wasm")
		cfg := Config{Cache: "off"}

		src, err := transpileCached(wasmPath, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(src, "func New(") {
			t.Errorf("expected source from wasm2go, got:\n%s", src)
		}
		if _, err := os.Stat(markerPath); err != nil {
			t.Errorf("wasm2go should have been invoked when cache is disabled")
		}
		if _, err := os.Stat(filepath.Join(cacheDir, "transpile")); err == nil {
			t.Errorf("cache root should not be created when cache is disabled")
		}
	})

	t.Run("cache hit returns cached source without invoking wasm2go", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := t.TempDir()
		t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

		installFakeWasm2go(t, tmpDir, `#!/bin/sh
echo "fake wasm2go should not be called" >&2
exit 1
`)

		wasmPath := testdataWasm("proc_exit-success.wasm")
		key := transpileCacheKey(wasmPath)
		if key == "" {
			t.Fatal("transpileCacheKey returned empty string")
		}
		cachedSrc := "package cached\nfunc New() *Module { return nil }\n"
		if err := cachePutTranspile(key, cachedSrc, currentTranspileCacheMeta()); err != nil {
			t.Fatalf("cachePutTranspile failed: %v", err)
		}

		cfg := Config{}
		src, err := transpileCached(wasmPath, cfg)
		if err != nil {
			t.Fatalf("unexpected error on cache hit: %v", err)
		}
		if src != cachedSrc {
			t.Errorf("expected cached source, got:\n%s", src)
		}
	})
}

func TestTranspileCachedInvalidatesStaleMetadataAndRepopulates(t *testing.T) {
	runStaleMetadataRepopulateTest := func(t *testing.T, staleMeta transpileCacheMeta, staleReason string) {
		t.Helper()

		wasmPath, cacheDir, markerPath, key := setupFakeWasm2goCacheTest(t)

		staleSrc := "package stale\nfunc New() *Module { return nil }\n"
		if err := cachePutTranspile(key, staleSrc, staleMeta); err != nil {
			t.Fatalf("cachePutTranspile failed: %v", err)
		}

		src, err := transpileCached(wasmPath, Config{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, statErr := os.Stat(markerPath); statErr != nil {
			t.Errorf("expected wasm2go to be invoked when cache %s is stale", staleReason)
		}
		if src == staleSrc {
			t.Errorf("expected fresh source from wasm2go, got stale cached source")
		}

		modulePath := filepath.Join(cacheDir, "transpile", key, "module.go")
		cachedData, err := os.ReadFile(modulePath)
		if err != nil {
			t.Fatalf("cache module.go not found after repopulate: %v", err)
		}
		if string(cachedData) != src {
			t.Errorf("cached module.go does not match fresh source after repopulate")
		}
	}

	t.Run("mismatched wasm2go identity in meta.json triggers miss and repopulate", func(t *testing.T) {
		meta := currentTranspileCacheMeta()
		meta.Wasm2goID = "old-identity"
		runStaleMetadataRepopulateTest(t, meta, "metadata")
	})

	t.Run("mismatched postprocess version in meta.json triggers miss and repopulate", func(t *testing.T) {
		meta := currentTranspileCacheMeta()
		meta.PostprocessVersion = cacheKeyVersion - 1
		runStaleMetadataRepopulateTest(t, meta, "postprocessVersion")
	})
}

func setupFakeWasm2goCacheTest(t *testing.T) (wasmPath, cacheDir, markerPath, key string) {
	t.Helper()

	tmpDir := t.TempDir()
	cacheDir = t.TempDir()
	t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

	markerPath = filepath.Join(tmpDir, "wasm2go-was-called")
	installFakeWasm2go(t, tmpDir, `#!/bin/sh
printf '%s\n' 'package main' 'func New() *Module { return nil }'
touch `+markerPath+`
`)

	wasmPath = testdataWasm("proc_exit-success.wasm")
	key = transpileCacheKey(wasmPath)
	if key == "" {
		t.Fatal("transpileCacheKey returned empty string")
	}
	return wasmPath, cacheDir, markerPath, key
}

func TestTranspileCached_CacheMissPopulatesTier1Cache(t *testing.T) {
	t.Run("proc_exit-success.wasm stores byte-identical output and second call hits cache", func(t *testing.T) {
		if _, err := exec.LookPath("wasm2go"); err != nil {
			t.Skip("wasm2go not on PATH")
		}
		cacheDir := t.TempDir()
		t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

		wasmPath := testdataWasm("proc_exit-success.wasm")
		cfg := Config{}

		expected, err := transpile(wasmPath)
		if err != nil {
			t.Fatalf("transpile failed: %v", err)
		}

		got, err := transpileCached(wasmPath, cfg)
		if err != nil {
			t.Fatalf("first transpileCached failed: %v", err)
		}
		if got != expected {
			t.Errorf("transpileCached returned different bytes than transpile")
		}

		key := transpileCacheKey(wasmPath)
		modulePath := filepath.Join(cacheDir, "transpile", key, "module.go")
		cachedData, err := os.ReadFile(modulePath)
		if err != nil {
			t.Fatalf("cache module.go not found after cache miss: %v", err)
		}
		if string(cachedData) != expected {
			t.Errorf("cached module.go differs from expected")
		}

		// Second call with fake-fail wasm2go should hit cache without invoking wasm2go.
		tmpDir := t.TempDir()
		installFakeWasm2go(t, tmpDir, `#!/bin/sh
echo "fake wasm2go should not be called" >&2
exit 1
`)

		got2, err := transpileCached(wasmPath, cfg)
		if err != nil {
			t.Fatalf("second transpileCached failed (should have hit cache): %v", err)
		}
		if got2 != expected {
			t.Errorf("second transpileCached returned different bytes after cache population")
		}
	})

	t.Run("fd_write-to-stdout.wasm cached module.go parses as valid Go with no duplicate interface methods", func(t *testing.T) {
		if _, err := exec.LookPath("wasm2go"); err != nil {
			t.Skip("wasm2go not on PATH")
		}
		cacheDir := t.TempDir()
		t.Setenv("WASM2GO_RUN_CACHE_DIR", cacheDir)

		wasmPath := testdataWasm("fd_write-to-stdout.wasm")
		cfg := Config{}

		_, err := transpileCached(wasmPath, cfg)
		if err != nil {
			t.Fatalf("transpileCached failed: %v", err)
		}

		key := transpileCacheKey(wasmPath)
		modulePath := filepath.Join(cacheDir, "transpile", key, "module.go")
		cachedData, err := os.ReadFile(modulePath)
		if err != nil {
			t.Fatalf("cache module.go not found: %v", err)
		}

		assertNoDuplicateInterfaceMethods(t, string(cachedData))
	})
}

func TestDeduplicateInterfaceMethods(t *testing.T) {
	t.Run("deduplicates duplicate method in interface block", func(t *testing.T) {
		input := `type Xwasi_snapshot_preview1 interface {
	Xfd_write(v0 uint32, v1 uint32, v2 uint32, v3 uint32) uint32
	Xfd_write(v0 uint32, v1 uint32, v2 uint32, v3 uint32) uint32
	Xfd_close(v0 uint32) uint32
}`
		want := `type Xwasi_snapshot_preview1 interface {
	Xfd_write(v0 uint32, v1 uint32, v2 uint32, v3 uint32) uint32
	Xfd_close(v0 uint32) uint32
}`
		got := deduplicateInterfaceMethods(input)
		if got != want {
			t.Errorf("\ngot:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("preserves source with no duplicates", func(t *testing.T) {
		input := `type Xwasi_snapshot_preview1 interface {
	Xfd_write(v0 uint32, v1 uint32, v2 uint32, v3 uint32) uint32
	Xfd_close(v0 uint32) uint32
}
type Xenv interface {
	Xmalloc(v0 uint32) uint32
}`
		want := input
		got := deduplicateInterfaceMethods(input)
		if got != want {
			t.Errorf("\ngot:\n%s\nwant:\n%s", got, want)
		}
	})
}

// assertNoDuplicateInterfaceMethods fails the test if src contains any
// interface type with duplicate method names.
func assertNoDuplicateInterfaceMethods(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		t.Fatalf("unparseable Go source: %v", err)
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			itface, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			methods := make(map[string]bool)
			for _, field := range itface.Methods.List {
				for _, name := range field.Names {
					if methods[name.Name] {
						t.Errorf("duplicate method %q found in interface %q", name.Name, typeSpec.Name.Name)
					}
					methods[name.Name] = true
				}
			}
		}
	}
}
