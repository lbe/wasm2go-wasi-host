//go:build e2e

package wasihost_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var fsTestsCopyMu sync.Mutex

// wasiTestsuiteCase is a single wasi-testsuite wasm binary to run end-to-end.
// Expected exit code and output come from the test's own <name>.json.
type wasiTestsuiteCase struct {
	name      string
	stdioOnly bool // force wasm-only invocation (ignore JSON dirs)
}

// wasiTestsuiteJSON is the legacy wasi-testsuite per-test config. ExitCode,
// Stdout and Stderr are expectations; nil means "not declared" (exit code
// defaults to 0, undeclared streams are not checked), matching run-tests.
type wasiTestsuiteJSON struct {
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	Dirs     []string          `json:"dirs"`
	Root     string            `json:"root"`
	ExitCode *int              `json:"exit_code"`
	Stdout   *string           `json:"stdout"`
	Stderr   *string           `json:"stderr"`
}

// suiteGuestEnv holds extra guest environment variables per suite.
//
// The Rust suite reads ERRNO_MODE_* to choose which platform's errno it
// asserts; with none set it accepts any listed platform's errno, which hides
// wrong translations. This host reports Linux errnos on every Unix host
// (including macOS), so the suite runs in strict Unix mode. Only the Rust
// suite reads these variables; the AssemblyScript environ tests count
// variables, so the others get none.
var suiteGuestEnv = map[string]map[string]string{
	"rust": {"ERRNO_MODE_UNIX": "1"},
}

var (
	wasiTestsuiteOnce sync.Once
	wasm2goRunPath    string
)

// runWasiTestsuiteCases builds wasm2go-run and executes each case as a
// sub-test. Every case runs the wasm binary named <name>.wasm from the
// wasi-testsuite testsuite identified by suiteDir (e.g. "rust" or "c")
// against the JSON root directory preopened at guest "/" when the suite
// config names one.
func runWasiTestsuiteCases(t *testing.T, suiteDir string, cases []wasiTestsuiteCase) {
	t.Helper()

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	var binPath string
	wasiTestsuiteOnce.Do(func() {
		wasm2goRunPath = filepath.Join(repoRoot, "bin", "wasm2go-run")
		if runtime.GOOS == "windows" {
			// CreateProcess appends .exe to a name without an extension.
			wasm2goRunPath += ".exe"
		}
		buildCmd := exec.Command("go", "build", "-o", wasm2goRunPath, "./cmd/wasm2go-run")
		buildCmd.Dir = repoRoot
		if out, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("failed to build wasm2go-run: %v\n%s", err, string(out))
		}
	})
	binPath = wasm2goRunPath

	testsDir := filepath.Join(repoRoot, "wasi-testsuite", "tests", suiteDir, "testsuite", "wasm32-wasip1")

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wasmPath := filepath.Join(testsDir, tc.name+".wasm")
			cfg, _ := loadWasiTestsuiteJSON(testsDir, tc.name)
			argv, err := wasiTestsuiteArgv(t, testsDir, wasmPath, tc.name, tc.stdioOnly, suiteGuestEnv[suiteDir])
			if err != nil {
				t.Fatalf("build argv: %v", err)
			}

			cmd := exec.Command(binPath, argv...)
			cmd.Dir = repoRoot
			cmd.Env = append(os.Environ(), "WASM2GO_WASIHOST_PATH="+repoRoot)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			exitCode := 0
			if err := cmd.Run(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("failed to run: %v", err)
				}
			}

			for _, problem := range checkWasiTestsuiteExpectations(cfg, exitCode, stdout.String(), stderr.String()) {
				t.Errorf("%s\nstdout:\n%s\nstderr:\n%s", problem,
					strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
			}
		})
	}
}

// checkWasiTestsuiteExpectations compares a run against the test's JSON the
// way wasi-testsuite run-tests does: the exit code must match exactly
// (default 0), and a declared stdout/stderr must be what the stream starts with
// (run-tests reads len(expected) bytes and compares). Undeclared output is
// not checked.
func checkWasiTestsuiteExpectations(cfg wasiTestsuiteJSON, exitCode int, stdout, stderr string) []string {
	var problems []string
	wantExit := 0
	if cfg.ExitCode != nil {
		wantExit = *cfg.ExitCode
	}
	if exitCode != wantExit {
		problems = append(problems, fmt.Sprintf("exit code %d, want %d", exitCode, wantExit))
	}
	if cfg.Stdout != nil && !strings.HasPrefix(stdout, *cfg.Stdout) {
		problems = append(problems, fmt.Sprintf("stdout does not start with %q", *cfg.Stdout))
	}
	if cfg.Stderr != nil && !strings.HasPrefix(stderr, *cfg.Stderr) {
		problems = append(problems, fmt.Sprintf("stderr does not start with %q", *cfg.Stderr))
	}
	return problems
}

func wasiTestsuiteArgv(t *testing.T, testsDir, wasmPath, name string, stdioOnly bool, extraEnv map[string]string) ([]string, error) {
	t.Helper()
	cfg, hasJSON := loadWasiTestsuiteJSON(testsDir, name)
	argv := make([]string, 0, 8)
	for key, value := range cfg.Env {
		argv = append(argv, "--env", key+"="+value)
	}
	for key, value := range extraEnv {
		argv = append(argv, "--env", key+"="+value)
	}

	dirs := cfg.Dirs
	args := cfg.Args
	var root string
	switch {
	case stdioOnly:
		// No directory preopens; args/env still come from JSON when present.
	case cfg.Root != "":
		// Suite contract: one host directory preopened at guest "/".
		root = cfg.Root
	case !hasJSON:
		dirs = []string{"fs-tests.dir"}
		args = []string{"fs-tests.dir"}
	}

	if root != "" {
		dst, err := cloneTestsuiteGuestDir(t, testsDir, root)
		if err != nil {
			return nil, err
		}
		argv = append(argv, "--dir", dst+"::/")
	}
	for _, guestDir := range dirs {
		dst, err := cloneTestsuiteGuestDir(t, testsDir, guestDir)
		if err != nil {
			return nil, err
		}
		argv = append(argv, "--dir", dst+"::"+guestDir)
	}
	argv = append(argv, wasmPath)
	argv = append(argv, args...)
	return argv, nil
}

func copyDirTree(src, dst string) error {
	return os.CopyFS(dst, os.DirFS(src))
}

func cloneTestsuiteGuestDir(t *testing.T, testsDir, guestDir string) (string, error) {
	t.Helper()
	src := filepath.Join(testsDir, guestDir)
	dst := filepath.Join(t.TempDir(), guestDir)

	fsTestsCopyMu.Lock()
	defer fsTestsCopyMu.Unlock()
	if err := cleanupTestsuiteOutput(src); err != nil {
		return "", err
	}
	if err := copyDirTree(src, dst); err != nil {
		return "", err
	}
	return dst, cleanupTestsuiteOutput(dst)
}

// cleanupTestsuiteOutput mirrors wasi-testsuite's pre-run cleanup of *.cleanup
// artifacts under a mounted directory.
func cleanupTestsuiteOutput(hostDir string) error {
	return filepath.WalkDir(hostDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(d.Name(), ".cleanup") {
			return nil
		}
		if d.IsDir() {
			return os.RemoveAll(path)
		}
		return os.Remove(path)
	})
}

func loadWasiTestsuiteJSON(testsDir, name string) (wasiTestsuiteJSON, bool) {
	data, err := os.ReadFile(filepath.Join(testsDir, name+".json"))
	if err != nil {
		return wasiTestsuiteJSON{}, false
	}
	var cfg wasiTestsuiteJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return wasiTestsuiteJSON{}, false
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	return cfg, true
}
