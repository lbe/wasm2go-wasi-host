package wasihost_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestDeadAdapterTypesRemovedFromWasihostAdapters verifies legacy fd-table
// adapters were deleted from wasihost_adapters.go after sysfs migration.
func TestDeadAdapterTypesRemovedFromWasihostAdapters(t *testing.T) {
	projectRoot := mustProjectRoot(t)
	path := filepath.Join(projectRoot, "wasihost_adapters.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	deadTypes := []string{
		"FSFileWrap",
		"DirEntriesFile",
		"osFile",
		"synthDirEntry",
		"wasiDirInfo",
	}

	declared := typeNamesInFile(f)
	for _, name := range deadTypes {
		if declared[name] {
			t.Errorf("dead adapter type %q still declared in wasihost_adapters.go", name)
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
