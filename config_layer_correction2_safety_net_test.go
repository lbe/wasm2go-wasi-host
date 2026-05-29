package wasihost_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var legacyWasihostRootOptionRefs = []string{
	"wasihost.WithArgs",
	"wasihost.WithEnv",
	"wasihost.WithStdin",
	"wasihost.WithStdout",
	"wasihost.WithStderr",
	"wasihost.WithHostDirectoryPreopen",
	"wasihost.WithReadOnlyFS",
	"wasihost.Option",
}

// TestConfigLayerCorrection2FullSafetyNet verifies correction-2 acceptance:
// wasm2go-run template uses ModuleConfig builder only, wasm2go-run tests pass,
// go build ./cmd/wasm2go-run succeeds, and scripts/e2e-wasip1.sh passes.
func TestConfigLayerCorrection2FullSafetyNet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping correction-2 safety net in -short mode")
	}
	root := mustProjectRoot(t)

	t.Run("templateUsesModuleConfigBuilderOnly", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(root, "cmd/wasm2go-run/template.go"))
		if err != nil {
			t.Fatalf("read template.go: %v", err)
		}
		src := string(data)
		if !strings.Contains(src, "NewModuleConfig()") {
			t.Fatal("template.go must use NewModuleConfig() builder")
		}
		for _, bad := range legacyWasihostRootOptionRefs {
			if strings.Contains(src, bad) {
				t.Errorf("template.go contains legacy root option reference %q", bad)
			}
		}
	})

	t.Run("wasm2goRunTests", func(t *testing.T) {
		mustRunRepoCommand(t, root, "wasm2go-run tests", "go", "test", "./cmd/wasm2go-run", "-count=1")
	})

	t.Run("buildWasm2goRun", func(t *testing.T) {
		mustRunRepoCommand(t, root, "go build ./cmd/wasm2go-run", "go", "build", "-o", "/dev/null", "./cmd/wasm2go-run")
	})

	t.Run("e2eWasip1", func(t *testing.T) {
		mustRunRepoCommand(t, root, "build bin/wasm2go-run", "go", "build", "-o", "./bin/wasm2go-run", "./cmd/wasm2go-run")
		mustRunRepoCommand(t, root, "scripts/e2e-wasip1.sh", filepath.Join(root, "scripts/e2e-wasip1.sh"))
	})
}

func mustRunRepoCommand(t *testing.T, root, label string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", label, err, out)
	}
}
