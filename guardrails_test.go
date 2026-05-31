package wasihost_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAntiRegressionGuardrails_NoDuplicateConfigPackage is a lightweight
// quality-gate that prevents reintroduction of a duplicate wasihost
// subpackage and duplicate production ModuleConfig type definitions.
//
// It fails if:
//   - Known duplicate subpackage config paths exist under wasihost/.
//   - wasihost/*.go contains ModuleConfig or FSConfig implementation files.
//   - More than one production "type ModuleConfig struct" definition exists
//     outside docs/ and test fixtures.
func TestAntiRegressionGuardrails_NoDuplicateConfigPackage(t *testing.T) {
	projectRoot := mustProjectRoot(t)

	// --- Guard 1: No reintroduced duplicate subpackage config files ---
	for _, rel := range []string{
		"wasihost/config.go",
		"wasihost/fsconfig.go",
		"wasihost/config_test.go",
		"wasihost/fsconfig_test.go",
	} {
		fullPath := filepath.Join(projectRoot, rel)
		if _, err := os.Stat(fullPath); err == nil {
			t.Errorf("duplicate subpackage config file still exists: %s", rel)
		} else if !os.IsNotExist(err) {
			t.Errorf("unexpected error checking %s: %v", rel, err)
		}
	}

	// --- Guard 2: No wasihost/ subpackage with other config implementation files ---
	subpkg := filepath.Join(projectRoot, "wasihost")
	if entries, err := os.ReadDir(subpkg); err == nil {
		var offending []string
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			name := strings.ToLower(e.Name())
			if name == "config.go" || name == "fsconfig.go" ||
				strings.HasPrefix(name, "module_config") ||
				strings.HasPrefix(name, "fs_config") {
				offending = append(offending, e.Name())
			}
		}
		if len(offending) > 0 {
			t.Errorf("wasihost/ subpackage contains forbidden config files: %v; "+
				"config implementation must live only in root-level wasihost*.go", offending)
		}
	}
	if _, err := os.Stat(subpkg); err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat wasihost/: %v", err)
	}

	// --- Guard 3: Exactly one production ModuleConfig struct definition ---
	var moduleConfigDefs int
	var moduleConfigFiles []string

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "wasi-testsuite", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		count := strings.Count(string(data), "type ModuleConfig struct")
		if count > 0 {
			moduleConfigDefs += count
			rel, _ := filepath.Rel(projectRoot, path)
			moduleConfigFiles = append(moduleConfigFiles, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking project: %v", err)
	}

	if moduleConfigDefs != 1 {
		t.Errorf("expected exactly 1 production 'type ModuleConfig struct', got %d in: %v",
			moduleConfigDefs, moduleConfigFiles)
	}
}

// TestAntiRegressionGuardrails_LegacyOptionAPIRemoved verifies the legacy
// Option functional API has been completely removed from production wasihost
// sources.
func TestAntiRegressionGuardrails_LegacyOptionAPIRemoved(t *testing.T) {
	projectRoot := mustProjectRoot(t)

	optionsFile := filepath.Join(projectRoot, "wasihost_options.go")
	if _, err := os.Stat(optionsFile); err == nil {
		t.Errorf("legacy options file wasihost_options.go still exists; it should be deleted")
	} else if !os.IsNotExist(err) {
		t.Errorf("unexpected error checking for wasihost_options.go: %v", err)
	}

	source := readWasihostProductionGoSource(t, projectRoot)

	if strings.Contains(source, "type Option func(*State)") {
		t.Errorf("legacy Option type still defined in wasihost production sources")
	}

	for name, pattern := range map[string]*regexp.Regexp{
		"WithArgs":                 regexp.MustCompile(`(?m)^func WithArgs\(`),
		"WithHostDirectoryPreopen": regexp.MustCompile(`(?m)^func WithHostDirectoryPreopen\(`),
	} {
		if pattern.MatchString(source) {
			t.Errorf("legacy package-level %s function still exists in wasihost production sources", name)
		}
	}
}

func readWasihostProductionGoSource(t *testing.T, projectRoot string) string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(projectRoot, "wasihost*.go"))
	if err != nil {
		t.Fatalf("glob wasihost*.go: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no wasihost*.go files found")
	}

	var content strings.Builder
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatalf("failed to read %s: %v", p, readErr)
		}
		content.Write(data)
		content.WriteByte('\n')
	}
	return content.String()
}

func mustProjectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	return wd
}

func ciLinuxAmd64BuildEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+3)
	for _, e := range env {
		if strings.HasPrefix(e, "GOOS=") || strings.HasPrefix(e, "GOARCH=") || strings.HasPrefix(e, "CGO_ENABLED=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
}

func mustLinuxAmd64GoBuild(t *testing.T, dir, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", pkg)
	cmd.Dir = dir
	cmd.Env = ciLinuxAmd64BuildEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GOOS=linux GOARCH=amd64 go build %s: %v\n%s", pkg, err, out)
	}
}

func TestDocsCompliance(t *testing.T) {
	// 1. Check README.md for WASI-style filesystem access description
	t.Run("README mentions preopens and read-only fs.FS", func(t *testing.T) {
		content, err := os.ReadFile("README.md")
		if err != nil {
			t.Fatal(err)
		}
		s := string(content)
		if !strings.Contains(s, "preopened directories") && !strings.Contains(s, "Preopened Directory") {
			t.Error("README.md does not mention 'preopened directories'")
		}
		if !strings.Contains(s, "read-only fs.FS preopens") && !strings.Contains(s, "read-only fs.FS") {
			t.Error("README.md does not mention 'read-only fs.FS preopens'")
		}
	})

	// 2. Check ARCHITECTURE.md for WASI-style filesystem access description
	t.Run("ARCHITECTURE mentions preopened directories and read-only fs.FS", func(t *testing.T) {
		content, err := os.ReadFile("ARCHITECTURE.md")
		if err != nil {
			t.Fatal(err)
		}
		s := string(content)
		if !strings.Contains(s, "preopened directories") && !strings.Contains(s, "Preopened Directory") {
			t.Error("ARCHITECTURE.md does not mention 'preopened directories'")
		}
		if !strings.Contains(s, "read-only fs.FS preopens") && !strings.Contains(s, "read-only fs.FS") {
			t.Error("ARCHITECTURE.md does not mention 'read-only fs.FS preopens'")
		}
	})

	// 3. Check runner test for a mutation test
	t.Run("E2E script or runner test includes mutation wasi-testsuite binary", func(t *testing.T) {
		// We want to ensure at least one test from wasi-testsuite that performs
		// create/write/remove is part of our verified compliance.

		// Check runner_test.go for path_open_read_write.wasm (Rust) or similar
		runnerTest, err := os.ReadFile("cmd/wasm2go-run/runner_test.go")
		if err != nil {
			t.Fatal(err)
		}

		mutationTests := []string{
			"file_pread_pwrite.wasm",
			"path_rename.wasm",
			"path_link.wasm",
			"path_symlink.wasm",
			"pwrite-with-access.wasm",
			"path_open_read_write.wasm",
		}

		found := false
		for _, test := range mutationTests {
			if strings.Contains(string(runnerTest), test) {
				found = true
				break
			}
		}

		if !found {
			t.Error("No known mutation tests from wasi-testsuite found in cmd/wasm2go-run/runner_test.go")
		}
	})
}

func TestRemovalOfOldMountAPIsAndUnsafeSemantics(t *testing.T) {
	// 1. Check for removed public API symbols in all wasihost package sources.
	paths, err := filepath.Glob("wasihost*.go")
	if err != nil {
		t.Fatalf("glob wasihost*.go: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no wasihost*.go files found")
	}
	var content strings.Builder
	for _, p := range paths {
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatalf("failed to read %s: %v", p, readErr)
		}
		content.Write(data)
	}
	source := content.String()

	removedSymbols := []string{
		"WithMount",
		"WithWritableMount",
	}

	for _, symbol := range removedSymbols {
		if strings.Contains(source, "func "+symbol) {
			t.Errorf("public API still contains %s; it should be removed", symbol)
		}
	}

	// 2. Check for removed internal/unsafe logic references
	unsafeReferences := []string{
		"mountHostPaths",
	}

	for _, ref := range unsafeReferences {
		if strings.Contains(source, ref) {
			t.Errorf("implementation still contains reference to %s; it should be removed/replaced", ref)
		}
	}

	// 3. Check for documentation updates
	docs := []string{"README.md", "ARCHITECTURE.md"}
	for _, doc := range docs {
		d, readErr := os.ReadFile(doc)
		if readErr != nil {
			t.Errorf("failed to read %s: %v", doc, readErr)
			continue
		}
		c := string(d)
		for _, symbol := range removedSymbols {
			if strings.Contains(c, symbol) {
				t.Errorf("%s still mentions removed symbol %s", doc, symbol)
			}
		}
	}

	// 4. Ensure root_writable_mount_fallback_test.go is gone or renamed/purged of old logic
	if _, statErr := os.Stat("root_writable_mount_fallback_test.go"); statErr == nil {
		t.Errorf("root_writable_mount_fallback_test.go still exists; it should be removed as the behavior it tests is now invalid")
	}

	// 5. Check syscall_foundation_test.go for parent escape references and internal mount logic
	groupA, err := os.ReadFile("syscall_foundation_test.go")
	if err != nil {
		t.Fatalf("failed to read syscall_foundation_test.go: %v", err)
	}
	groupAContent := string(groupA)
	if strings.Contains(groupAContent, "parent directory escape") {
		t.Errorf("syscall_foundation_test.go still contains reference to 'parent directory escape'; it should be removed")
	}
	if strings.Contains(groupAContent, "../outside.txt") {
		t.Errorf("syscall_foundation_test.go still contains reference to '../outside.txt'; it should be removed")
	}

	for _, ref := range unsafeReferences {
		if strings.Contains(groupAContent, ref) {
			t.Errorf("syscall_foundation_test.go still contains reference to %s; it should be removed", ref)
		}
	}
}

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

// TestAntiRegressionGuardrails_Wasm2goRunTemplateUsesModuleConfig verifies wasm2go-run template.go
// uses ModuleConfig builder only and does not reference legacy root options.
func TestAntiRegressionGuardrails_Wasm2goRunTemplateUsesModuleConfig(t *testing.T) {
	root := mustProjectRoot(t)

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
}

// TestGuardrails_E2EManifestUnitMirrorsValid ensures testdata/e2e_cases.json
// unitMirror entries reference real test functions.
func TestGuardrails_E2EManifestUnitMirrorsValid(t *testing.T) {
	root := mustProjectRoot(t)

	data, err := os.ReadFile(filepath.Join(root, "testdata/e2e_cases.json"))
	if err != nil {
		t.Fatalf("read e2e manifest: %v", err)
	}
	var cases []struct {
		Name       string   `json:"name"`
		UnitMirror []string `json:"unitMirror"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse e2e manifest: %v", err)
	}

	known := collectTestFunctionNames(t, root)
	for _, c := range cases {
		for _, fn := range c.UnitMirror {
			if !known[fn] {
				t.Errorf("e2e case %q: unitMirror references unknown test %q", c.Name, fn)
			}
		}
	}
}

// TestAntiRegressionGuardrails_CILinuxBuildGuardrail ensures the module cross-builds
// for linux/amd64 (CI target) via GOOS=linux GOARCH=amd64 go build ./...
func TestAntiRegressionGuardrails_CILinuxBuildGuardrail(t *testing.T) {
	mustLinuxAmd64GoBuild(t, mustProjectRoot(t), "./...")
}

func collectTestFunctionNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	globs := []string{
		"syscall_*_test.go",
		"wasihost_*_test.go",
		"state_test.go",
		filepath.Join("cmd", "wasm2go-run", "*_test.go"),
	}
	re := regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	for _, pattern := range globs {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatalf("glob %q: %v", pattern, err)
		}
		for _, path := range matches {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read %s: %v", path, readErr)
			}
			for _, m := range re.FindAllStringSubmatch(string(content), -1) {
				names[m[1]] = true
			}
		}
	}
	return names
}
