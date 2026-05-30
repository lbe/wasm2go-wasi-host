package main

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"
)

// mainTmpl is the Go template for generating the runner main.go file.
const mainTmpl = `package main

import (
	"crypto/rand"
	"os"

	"github.com/lbe/wasm2go-wasi-host"
	wasm "{{.ModuleName}}/module"
)

func main() {
	var mod *wasm.Module
	state := wasihost.New(
		func() []byte { return *mod.Xmemory().Slice() },
		wasihost.NewModuleConfig().{{.WasmArgs}}{{.Env}}{{.Dirs}}
		WithSysWalltime().
		WithSysNanotime().
		WithSysNanosleep().
		WithRandSource(rand.Reader).
		WithStdin(os.Stdin).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr),
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

// generateMain renders the wrapper main.go that wires wasihost to the
// transpiled wasm2go module.
func generateMain(cfg Config, imports []string, moduleName string) (string, error) {
	tmpl, err := template.New("main").Parse(mainTmpl)
	if err != nil {
		return "", err
	}

	data := templateData{
		ModuleName: moduleName,
		WasmArgs:   formatWasmArgs(cfg),
		Env:        formatEnvVars(cfg.Env),
		Dirs:       formatDirMounts(cfg.Dirs),
		Imports:    strings.Join(importsList(imports), ", "),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// templateData holds the data passed to the main template.
type templateData struct {
	ModuleName string
	WasmArgs   string
	Env        string
	Dirs       string
	Imports    string
}

// formatWasmArgs generates the WithArgs configuration line for the WASM module.
// Returns an empty string if no path or arguments are provided.
func formatWasmArgs(cfg Config) string {
	if cfg.WasmPath == "" && len(cfg.WasmArgs) == 0 {
		return ""
	}
	allArgs := []string{strconv.Quote(cfg.WasmPath)}
	for _, arg := range cfg.WasmArgs {
		allArgs = append(allArgs, strconv.Quote(arg))
	}
	return "\n\t\tWithArgs(" + strings.Join(allArgs, ", ") + ")."
}

// parseEnvVar splits an environment variable string "KEY=VALUE" into key and value.
// If no '=' is present, value is empty.
func parseEnvVar(e string) (key, value string) {
	if idx := strings.Index(e, "="); idx >= 0 {
		return e[:idx], e[idx+1:]
	}
	return e, ""
}

// formatEnvVars generates WithEnv lines for each environment variable.
// Each env string is expected to be in "KEY=VALUE" format.
func formatEnvVars(env []string) string {
	var sb strings.Builder
	for _, e := range env {
		key, value := parseEnvVar(e)
		sb.WriteString("\n\t\tWithEnv(")
		sb.WriteString(strconv.Quote(key))
		sb.WriteString(", ")
		sb.WriteString(strconv.Quote(value))
		sb.WriteString(").")
	}
	return sb.String()
}

// formatDirMounts generates WithHostDirectoryPreopen lines for each directory mount.
func formatDirMounts(dirs []DirMount) string {
	var sb strings.Builder
	for _, d := range dirs {
		sb.WriteString("\n\t\tWithHostDirectoryPreopen(")
		sb.WriteString(strconv.Quote(d.Guest))
		sb.WriteString(", ")
		sb.WriteString(strconv.Quote(d.Host))
		sb.WriteString(").")
	}
	return sb.String()
}

// importsList returns a list of import names, all set to "state".
func importsList(imports []string) []string {
	result := make([]string, len(imports))
	for i := range imports {
		result[i] = "state"
	}
	return result
}

// generateGoMod returns a minimal go.mod for the ephemeral runner workspace.
func generateGoMod(moduleName, wasiHostPath string) string {
	res := "module " + moduleName + "\n\ngo 1.26\n\nrequire github.com/lbe/wasm2go-wasi-host v0.0.0\n"
	if wasiHostPath != "" {
		res += "\nreplace github.com/lbe/wasm2go-wasi-host => " + wasiHostPath + "\n"
	}
	return res
}
