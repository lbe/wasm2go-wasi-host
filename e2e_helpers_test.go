package wasihost_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// wasiTestsuiteCase is a single wasi-testsuite wasm binary to run end-to-end.
type wasiTestsuiteCase struct {
	name      string
	wantPass  bool
	stdioOnly bool
}

var (
	wasiTestsuiteOnce sync.Once
	wasm2goRunPath    string
)

// runWasiTestsuiteCases builds wasm2go-run and executes each case as a
// sub-test. Every case runs the wasm binary named <name>.wasm from the
// wasi-testsuite testsuite identified by suiteDir (e.g. "rust" or "c")
// against fs-tests.dir.
func runWasiTestsuiteCases(t *testing.T, suiteDir string, cases []wasiTestsuiteCase) {
	t.Helper()

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("e2e test requires Unix-like environment")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Ensure the wasm2go-run binary is built only once.
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
	fsTestsDir := filepath.Join(testsDir, "fs-tests.dir")

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wasmPath := filepath.Join(testsDir, tc.name+".wasm")
			var argv []string
			if tc.stdioOnly {
				argv = []string{
					wasmPath,
				}
			} else {
				argv = []string{
					"--dir", fsTestsDir + "::fs-tests.dir",
					wasmPath,
					"fs-tests.dir",
				}
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
