package wasihost_test

import (
	"os"
	"os/exec"
	"testing"
)

func TestQualityGate(t *testing.T) {
	// Cleanup artifacts to ensure idempotency and clean state
	cleanup := func() {
		artifacts := []string{
			"./wasm2go-run",
			"./wasm2go-run-debug",
			"adapters/__pycache__",
			".pytest_cache",
		}
		for _, a := range artifacts {
			os.RemoveAll(a)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	// 1. Check git status is clean
	cmd := exec.Command("git", "status", "--short")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status failed: %v\nOutput: %s", err, string(out))
	}
	if len(out) > 0 {
		t.Errorf("Git status is not clean:\n%s", string(out))
	}

	// 2. Ensure formatting is correct
	cmd = exec.Command("gofmt", "-l", ".")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt failed: %v\nOutput: %s", err, string(out))
	}
	if len(out) > 0 {
		t.Errorf("Files not formatted:\n%s", string(out))
	}

	// 3. Ensure linting passes
	cmd = exec.Command("golangci-lint", "run", "./...")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("golangci-lint failed:\n%s", string(out))
	}

	// 4. Ensure Python tests pass
	cmd = exec.Command("python3", "-m", "pytest", "adapters/wasm2go_test.py", "-q")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("python3 pytest failed:\n%s", string(out))
	}

	// 5. Ensure E2E AssemblyScript tests pass
	buildCmd := exec.Command("go", "build", "-o", "./wasm2go-run", "./cmd/wasm2go-run")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build wasm2go-run: %v\nOutput: %s", err, string(out))
	}

	e2eCmd := exec.Command("./scripts/e2e-assemblyscript.sh")
	if out, err := e2eCmd.CombinedOutput(); err != nil {
		t.Errorf("E2E AssemblyScript tests failed:\n%s", string(out))
	}
}
