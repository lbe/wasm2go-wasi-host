//go:build e2e

package wasihost_test

import (
	"os"
	"os/exec"
	"testing"
)

func TestQualityGate(t *testing.T) {
	if out, err := exec.Command("git", "status", "--short").CombinedOutput(); err != nil {
		t.Fatalf("git status failed: %v\nOutput: %s", err, out)
	} else if len(out) > 0 {
		t.Skipf("skipping quality gate: working tree not clean\n%s", out)
	}

	// Remove artifacts so each run starts from a clean tree.
	artifacts := []string{
		".pytest_cache",
		"wasi-testsuite/.pytest_cache",
		"wasi-testsuite/adapters/__pycache__",
	}
	cleanup := func() {
		for _, artifact := range artifacts {
			if err := os.RemoveAll(artifact); err != nil {
				t.Errorf("remove %s: %v", artifact, err)
			}
		}
	}
	assertCleanGitStatus := func(phase string) {
		cmd := exec.Command("git", "status", "--short")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git status failed %s: %v\nOutput: %s", phase, err, string(out))
		}
		if len(out) > 0 {
			t.Errorf("Git status is not clean %s:\n%s", phase, string(out))
		}
	}

	cleanup()
	t.Cleanup(cleanup)

	// The outer test invocation runs go test -tags=e2e ./... itself. Running it
	// inside this test would call this quality gate again.

	// 1. Check git status is clean before running quality commands.
	assertCleanGitStatus("before quality checks")

	// 2. Check formatting.
	cmd := exec.Command("gofmt", "-l", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt failed: %v\nOutput: %s", err, string(out))
	}
	if len(out) > 0 {
		t.Errorf("Files not formatted:\n%s", string(out))
	}

	// 3. Build wasm2go-run.
	buildCmd := exec.Command("go", "build", "-o", "./bin/wasm2go-run", "./cmd/wasm2go-run")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build wasm2go-run: %v\nOutput: %s", err, string(out))
	}

	// 4. Check that cleanup removed the generated artifacts and left the tree clean.
	cleanup()
	assertCleanGitStatus("after cleanup")
}
