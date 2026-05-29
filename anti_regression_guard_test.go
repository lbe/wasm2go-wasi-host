package wasihost_test

import (
	"os"
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
