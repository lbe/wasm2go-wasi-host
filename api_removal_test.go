package wasihost_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	// 5. Check group_a_test.go for parent escape references and internal mount logic
	groupA, err := os.ReadFile("group_a_test.go")
	if err != nil {
		t.Fatalf("failed to read group_a_test.go: %v", err)
	}
	groupAContent := string(groupA)
	if strings.Contains(groupAContent, "parent directory escape") {
		t.Errorf("group_a_test.go still contains reference to 'parent directory escape'; it should be removed")
	}
	if strings.Contains(groupAContent, "../outside.txt") {
		t.Errorf("group_a_test.go still contains reference to '../outside.txt'; it should be removed")
	}

	for _, ref := range unsafeReferences {
		if strings.Contains(groupAContent, ref) {
			t.Errorf("group_a_test.go still contains reference to %s; it should be removed", ref)
		}
	}
}
