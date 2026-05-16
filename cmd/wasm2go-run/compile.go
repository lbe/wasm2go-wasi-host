package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
)

func wasiHostPath() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version != "(devel)" {
		return os.Getenv("WASM2GO_WASIHOST_PATH")
	}
	// Try vcs.dir first (available when built with go build from a VCS tree)
	for _, setting := range info.Settings {
		if setting.Key == "vcs.dir" {
			return setting.Value
		}
	}
	// Fallback: compute from source file location (reliable in go test dev builds)
	_, filename, _, ok2 := runtime.Caller(0)
	if ok2 {
		// filename is .../cmd/wasm2go-run/compile.go — go up two dirs to repo root
		return filepath.Join(filepath.Dir(filename), "../..")
	}
	return os.Getenv("WASM2GO_WASIHOST_PATH")
}

func compile(wasmPath string, cfg Config) (string, string, error) {
	tmpDir, err := os.MkdirTemp("", "wasm2go-run-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	transpiled, err := transpile(wasmPath)
	if err != nil {
		return "", "", fmt.Errorf("transpilation failed: %w", err)
	}

	imports, err := parseImports(transpiled)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse imports: %w", err)
	}

	moduleDir := filepath.Join(tmpDir, "module")
	if err = os.Mkdir(moduleDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create module directory: %w", err)
	}
	if err = os.WriteFile(filepath.Join(moduleDir, "module.go"), []byte(transpiled), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write module.go: %w", err)
	}

	goMod := generateGoMod("tmprunner", wasiHostPath())
	if err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write go.mod: %w", err)
	}

	mainGo, err := generateMain(cfg, imports, "tmprunner")
	if err != nil {
		return "", "", fmt.Errorf("failed to generate main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(mainGo), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write main.go: %w", err)
	}

	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("go mod tidy failed: %v: %s", err, string(out))
	}

	binaryPath := filepath.Join(tmpDir, "runner")
	buildCmd := exec.Command("go", "build", "-o", "runner", ".")
	buildCmd.Dir = tmpDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("go build failed: %v: %s", err, string(out))
	}

	success = true
	return tmpDir, binaryPath, nil
}
