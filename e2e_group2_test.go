package wasihost_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGroup2WasiTestsuiteInventoryPasses verifies that the Group 2
// wasi-testsuite tests (close_preopen, dir_fd_op_failures, directory_seek,
// path_open_dirfd_not_dir) pass end-to-end via the built wasm2go-run binary,
// and that smoke tests do not regress.
func TestGroup2WasiTestsuiteInventoryPasses(t *testing.T) {
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

	fsTestsDir := filepath.Join(repoRoot, "wasi-testsuite", "tests", "rust", "testsuite", "wasm32-wasip1", "fs-tests.dir")
	testsDir := filepath.Join(repoRoot, "wasi-testsuite", "tests", "rust", "testsuite", "wasm32-wasip1")

	cases := []struct {
		name     string
		wasmFile string
		wantPass bool
	}{
		// Group 2 inventory (must pass).
		{name: "close_preopen", wantPass: true},
		{name: "dir_fd_op_failures", wantPass: true},
		{name: "directory_seek", wantPass: true},
		{name: "path_open_dirfd_not_dir", wantPass: true},
		// Smoke tests (must not regress).
		{name: "path_open_read_write", wantPass: true},
		{name: "stdio", wantPass: true},
		{name: "path_open_missing", wantPass: true},
		{name: "overwrite_preopen", wantPass: true},
	}

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
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("failed to run %s: %v", tc.name, err)
				}
			}

			if tc.wantPass && exitCode != 0 {
				t.Errorf("%s exited %d, want 0\nstderr:\n%s", tc.name, exitCode, strings.TrimSpace(string(out)))
			}
			if !tc.wantPass && exitCode == 0 {
				t.Errorf("%s exited 0, want non-zero", tc.name)
			}
		})
	}
}
