package wasihost_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGroup45WasiTestsuiteInventoryPasses builds wasm2go-run and executes each
// case as a sub-test. Every case runs the wasm binary named <name>.wasm from the
// wasi-testsuite testsuite identified by suiteDir (e.g. "rust" or "c") against
// fs-tests.dir.
func TestGroup45WasiTestsuiteInventoryPasses(t *testing.T) {
	t.Parallel()

	// Inventory from the plan: 5 Rust tests
	cases := []struct {
		name     string
		wantPass bool
	}{
		{"unlink_file_trailing_slashes", true},
		{"path_symlink_trailing_slashes", true},
		{"symlink_create", true},
		{"symlink_filestat", true},
		{"dangling_symlink", true},
	}

	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("e2e test requires Unix-like environment")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Ensure the wasm2go-run binary is built.
	binPath := filepath.Join(repoRoot, "bin", "wasm2go-run")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/wasm2go-run")
	buildCmd.Dir = repoRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build wasm2go-run: %v\n%s", err, string(out))
	}

	testsDir := filepath.Join(repoRoot, "wasi-testsuite", "tests", "rust", "testsuite", "wasm32-wasip1")
	fsTestsDir := filepath.Join(testsDir, "fs-tests.dir")

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wasmPath := filepath.Join(testsDir, tc.name+".wasm")
			argv := []string{
				"--dir", fsTestsDir + "::fs-tests.dir",
				wasmPath,
				"fs-tests.dir",
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

	// Verify smoke tests pass (from plan)
	smokes := []struct {
		name     string
		wantPass bool
	}{
		{"path_open_read_write", true},
		{"stdio", true},
		{"sched_yield", true},
		{"nofollow_errors", true},
		{"path_open_missing", true},
		{"overwrite_preopen", true},
	}
	for _, smoke := range smokes {
		t.Run(smoke.name, func(t *testing.T) {
			t.Parallel()
			wasmPath := filepath.Join(testsDir, smoke.name+".wasm")
			argv := []string{
				"--dir", fsTestsDir + "::fs-tests.dir",
				wasmPath,
				"fs-tests.dir",
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
				}
			}
			if smoke.wantPass && exitCode != 0 {
				t.Errorf("smoke %s exited %d, want 0\nstderr:\n%s", smoke.name, exitCode, strings.TrimSpace(string(out)))
			}
			if !smoke.wantPass && exitCode == 0 {
				t.Errorf("smoke %s exited 0, want non-zero", smoke.name)
			}
		})
	}
}
