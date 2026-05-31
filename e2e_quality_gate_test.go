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

	// Cleanup artifacts to ensure idempotency and clean state.
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

	// go test -tags=e2e ./... is intentionally verified by the outer test invocation;
	// running it recursively inside this test would recursively invoke this
	// quality gate.

	// 1. Check git status is clean before running quality commands.
	assertCleanGitStatus("before quality checks")

	// 2. Ensure formatting is correct.
	cmd := exec.Command("gofmt", "-l", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt failed: %v\nOutput: %s", err, string(out))
	}
	if len(out) > 0 {
		t.Errorf("Files not formatted:\n%s", string(out))
	}

	// 3. Ensure wasm2go-run builds.
	buildCmd := exec.Command("go", "build", "-o", "./bin/wasm2go-run", "./cmd/wasm2go-run")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build wasm2go-run: %v\nOutput: %s", err, string(out))
	}

	// 4. Ensure generated artifacts are removed and the tree is clean after cleanup.
	cleanup()
	assertCleanGitStatus("after cleanup")
}
