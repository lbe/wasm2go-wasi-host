package main

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"
)

// generateMain renders the wrapper main.go that wires wasihost to the
// transpiled wasm2go module.
func generateMain(cfg Config, imports []string, moduleName string) (string, error) {
	const mainTmpl = `package main

import (
	"os"

	"github.com/lbe/wasm2go-wasi-host"
	wasm "{{.ModuleName}}/module"
)

func main() {
	var mod *wasm.Module
	state := wasihost.New(
		func() []byte { return *mod.Xmemory().Slice() },
		wasihost.WithArgs({{.WasmArgs}}),
{{- range .Env}}
		wasihost.WithEnv({{.}}),
{{- end}}
{{- range .Dirs}}
		wasihost.WithHostDirectoryPreopen({{.Guest}}, {{.Host}}),
{{- end}}
		wasihost.WithStdin(os.Stdin),
		wasihost.WithStdout(os.Stdout),
		wasihost.WithStderr(os.Stderr),
	)

	mod = wasm.New({{.Imports}})

	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(wasihost.ExitError); ok {
				os.Exit(int(e.Code))
			}
			panic(r)
		}
	}()

	mod.X_start()
}
`
	tmpl, err := template.New("main").Parse(mainTmpl)
	if err != nil {
		return "", err
	}

	wasmArgs := []string{strconv.Quote(cfg.WasmPath)}
	for _, arg := range cfg.WasmArgs {
		wasmArgs = append(wasmArgs, strconv.Quote(arg))
	}

	envQuoted := make([]string, len(cfg.Env))
	for i, e := range cfg.Env {
		envQuoted[i] = strconv.Quote(e)
	}

	type DirQuoted struct {
		Host  string
		Guest string
	}
	dirsQuoted := make([]DirQuoted, len(cfg.Dirs))
	for i, d := range cfg.Dirs {
		dirsQuoted[i] = DirQuoted{
			Host:  strconv.Quote(d.Host),
			Guest: strconv.Quote(d.Guest),
		}
	}

	importsList := make([]string, len(imports))
	for i := range imports {
		importsList[i] = "state"
	}

	data := struct {
		ModuleName string
		WasmArgs   string
		Env        []string
		Dirs       []DirQuoted
		Imports    string
	}{
		ModuleName: moduleName,
		WasmArgs:   strings.Join(wasmArgs, ", "),
		Env:        envQuoted,
		Dirs:       dirsQuoted,
		Imports:    strings.Join(importsList, ", "),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateGoMod returns a minimal go.mod for the ephemeral runner workspace.
func generateGoMod(moduleName, wasiHostPath string) string {
	res := "module " + moduleName + "\n\ngo 1.26\n\nrequire github.com/lbe/wasm2go-wasi-host v0.0.0\n"
	if wasiHostPath != "" {
		res += "\nreplace github.com/lbe/wasm2go-wasi-host => " + wasiHostPath + "\n"
	}
	return res
}
