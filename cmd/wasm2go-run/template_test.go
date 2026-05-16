package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestGenerateMain(t *testing.T) {
	t.Run("wires wasihost correctly for complex config", func(t *testing.T) {
		cfg := Config{
			WasmPath: "foo.wasm",
			Env:      []string{"K=V"},
			Dirs:     []DirMount{{Host: "/h", Guest: "g"}},
			WasmArgs: []string{"a"},
		}
		imports := []string{"Xwasi_snapshot_preview1"}
		moduleName := "myreplacemod"

		got, err := generateMain(cfg, imports, moduleName)
		if err != nil {
			t.Fatalf("generateMain failed: %v", err)
		}

		// Ensure it parses
		fset := token.NewFileSet()
		_, err = parser.ParseFile(fset, "main.go", got, 0)
		if err != nil {
			t.Errorf("generated code does not parse: %v\nCode:\n%s", err, got)
		}

		// Check package and import
		if !strings.Contains(got, "package main") {
			t.Error("expected 'package main'")
		}
		if !strings.Contains(got, `wasm "myreplacemod/module"`) {
			t.Error("expected aliased import: wasm \"myreplacemod/module\"")
		}

		// Check configuration wiring
		expectedSnippets := []string{
			`WithArgs("foo.wasm", "a")`,
			`WithEnv("K=V")`,
			`WithMount("g", os.DirFS("/h"))`,
			`WithStdin`,
			`WithStdout`,
			`WithStderr`,
			`wasm.New(state)`,
			`mod.X_start()`,
			`ExitError`,
			`os.Exit(int(e.Code))`,
		}

		for _, snippet := range expectedSnippets {
			if !strings.Contains(got, snippet) {
				t.Errorf("missing snippet: %s", snippet)
			}
		}
	})

	t.Run("handles multiple imports", func(t *testing.T) {
		cfg := Config{WasmPath: "foo.wasm"}
		imports := []string{"Xenv", "Xwasi_snapshot_preview1"}
		got, _ := generateMain(cfg, imports, "mod")

		if !strings.Contains(got, "wasm.New(state, state)") {
			t.Error("expected wasm.New(state, state) for two imports")
		}
	})

	t.Run("escapes environment variables correctly", func(t *testing.T) {
		cfg := Config{
			WasmPath: "x.wasm",
			Env:      []string{`b=escap " ing`},
		}
		got, _ := generateMain(cfg, []string{}, "mod")

		fset := token.NewFileSet()
		_, err := parser.ParseFile(fset, "main.go", got, 0)
		if err != nil {
			t.Errorf("generated code with escapes does not parse: %v\nCode:\n%s", err, got)
		}
		
		if !strings.Contains(got, `"b=escap \" ing"`) {
			t.Error("expected escaped environment variable string")
		}
	})
}

func TestGenerateGoMod(t *testing.T) {
	t.Run("contains replace directive when path provided", func(t *testing.T) {
		moduleName := "myreplacemod"
		wasiHostPath := "/abs/path/to/wasihost"
		got := generateGoMod(moduleName, wasiHostPath)

		if !strings.Contains(got, "module myreplacemod") {
			t.Error("missing module declaration")
		}
		if !strings.Contains(got, "require github.com/lbe/wasm2go-wasi-host v0.0.0") {
			t.Error("missing requirement")
		}
		if !strings.Contains(got, "replace github.com/lbe/wasm2go-wasi-host => /abs/path/to/wasihost") {
			t.Error("missing or incorrect replace directive")
		}
	})

	t.Run("omits replace directive when path is empty", func(t *testing.T) {
		got := generateGoMod("myothermod", "")
		if !strings.Contains(got, "module myothermod") {
			t.Error("missing module declaration")
		}
		if !strings.Contains(got, "require github.com/lbe/wasm2go-wasi-host") {
			t.Error("missing requirement")
		}
		if strings.Contains(got, "replace") {
			t.Error("should not contain replace directive when path is empty")
		}
	})
}
