package wasihost_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeadAdapterTypesRemovedFromWasihostAdapters verifies production wasihost
// sources no longer declare legacy fd-table adapter types after sysfs migration.
func TestDeadAdapterTypesRemovedFromWasihostAdapters(t *testing.T) {
	projectRoot := mustProjectRoot(t)

	stubsPath := filepath.Join(projectRoot, "wasihost_adapters_stubs.go")
	if _, err := os.Stat(stubsPath); err == nil {
		t.Errorf("legacy adapter stubs file still exists: wasihost_adapters_stubs.go")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat wasihost_adapters_stubs.go: %v", err)
	}

	deadTypes := []string{
		"FSFileWrap",
		"DirEntriesFile",
		"osFile",
		"synthDirEntry",
		"wasiDirInfo",
	}

	paths, err := filepath.Glob(filepath.Join(projectRoot, "wasihost*.go"))
	if err != nil {
		t.Fatalf("glob wasihost*.go: %v", err)
	}

	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}

		declared := typeNamesInFile(f)
		for _, name := range deadTypes {
			if declared[name] {
				rel, _ := filepath.Rel(projectRoot, path)
				t.Errorf("dead adapter type %q still declared in %s", name, rel)
			}
		}
	}
}

func typeNamesInFile(f *ast.File) map[string]bool {
	names := make(map[string]bool)
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			names[typeSpec.Name.Name] = true
		}
	}
	return names
}
