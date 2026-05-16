package main

import (
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
		wasmPath := testdata("../../wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/proc_exit-success.wasm")
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
}
