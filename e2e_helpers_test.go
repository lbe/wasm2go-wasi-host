//go:build e2e

package wasihost_test

import (
	"encoding/json"
	"errors"
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
type wasiTestsuiteCase struct {
	name      string
	wantPass  bool
	stdioOnly bool // force wasm-only invocation (ignore JSON dirs)
}

type wasiTestsuiteJSON struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
	Dirs []string          `json:"dirs"`
	Root string            `json:"root"`
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

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("e2e test requires Unix-like environment")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	var binPath string
	wasiTestsuiteOnce.Do(func() {
		wasm2goRunPath = filepath.Join(repoRoot, "bin", "wasm2go-run")
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
			argv, err := wasiTestsuiteArgv(t, testsDir, wasmPath, tc.name, tc.stdioOnly)
			if err != nil {
				t.Fatalf("build argv: %v", err)
			}

			cmd := exec.Command(binPath, argv...)
			cmd.Dir = repoRoot
			cmd.Env = append(os.Environ(), "WASM2GO_WASIHOST_PATH="+repoRoot)

			out, err := cmd.CombinedOutput()
			exitCode := 0
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("failed to run: %v", err)
				}
			}

			if tc.wantPass && exitCode != 0 {
				t.Errorf("exited %d, want 0\nstderr:\n%s", exitCode, strings.TrimSpace(string(out)))
			}
			if !tc.wantPass && exitCode == 0 {
				t.Errorf("exited 0, want non-zero")
			}
		})
	}
}

func wasiTestsuiteArgv(t *testing.T, testsDir, wasmPath, name string, stdioOnly bool) ([]string, error) {
	t.Helper()
	cfg, hasJSON := loadWasiTestsuiteJSON(testsDir, name)
	argv := make([]string, 0, 8)
	for key, value := range cfg.Env {
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
