package wasihost

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStateNewAcceptsModuleConfigAndStoresContext(t *testing.T) {
	memFunc := func() []byte { return make([]byte, 65536) }

	cfg := NewModuleConfig().WithArgs("prog")

	state := New(memFunc, cfg)

	if state == nil {
		t.Fatal("New returned nil")
	}

	if state.ctx == nil {
		t.Fatal("state.ctx is nil; expected New to build and store a wasi.Context from ModuleConfig")
	}

	if len(state.ctx.Args()) != 1 {
		t.Fatalf("state.ctx.Args() returned %d entries, want 1", len(state.ctx.Args()))
	}

	if string(state.ctx.Args()[0]) != "prog" {
		t.Errorf("state.ctx.Args()[0] = %q, want %q", string(state.ctx.Args()[0]), "prog")
	}
}

func TestStateNewConfigOnlyCtorPopulatesCtxAndArgs(t *testing.T) {
	// After refactor: New signature must be func(func() []byte, *ModuleConfig) *State
	// (config-only, no variadic options).
	gotType := reflect.TypeOf(New)
	if gotType.IsVariadic() {
		t.Fatalf("New is variadic (%s); expected non-variadic config-only signature func(func() []byte, *ModuleConfig) *State", gotType)
	}

	memFunc := func() []byte { return make([]byte, 65536) }

	cfg := NewModuleConfig().
		WithArgs("app", "--verbose").
		WithEnv("LANG", "C").
		WithEnv("PATH", "/usr/bin")

	state := New(memFunc, cfg)

	if state == nil {
		t.Fatal("New returned nil")
	}

	// ctx must be populated from config
	if state.ctx == nil {
		t.Fatal("state.ctx is nil; expected New to build and store a wasi.Context from ModuleConfig")
	}

	// args propagation via config
	if len(state.ctx.Args()) != 2 {
		t.Fatalf("ctx.Args() = %d entries, want 2", len(state.ctx.Args()))
	}
	if string(state.ctx.Args()[0]) != "app" {
		t.Errorf("ctx.Args()[0] = %q, want %q", string(state.ctx.Args()[0]), "app")
	}
	if string(state.ctx.Args()[1]) != "--verbose" {
		t.Errorf("ctx.Args()[1] = %q, want %q", string(state.ctx.Args()[1]), "--verbose")
	}

	// environ propagation via config
	if len(state.ctx.Environ()) != 2 {
		t.Fatalf("ctx.Environ() = %d entries, want 2", len(state.ctx.Environ()))
	}
	if string(state.ctx.Environ()[0]) != "LANG=C" {
		t.Errorf("ctx.Environ()[0] = %q, want %q", string(state.ctx.Environ()[0]), "LANG=C")
	}
	if string(state.ctx.Environ()[1]) != "PATH=/usr/bin" {
		t.Errorf("ctx.Environ()[1] = %q, want %q", string(state.ctx.Environ()[1]), "PATH=/usr/bin")
	}

	if state.ctx.FSContext() == nil {
		t.Fatal("state.ctx.FSContext() is nil; expected stdio preopens")
	}
	for fd := int32(0); fd < 3; fd++ {
		if _, ok := state.ctx.FSContext().LookupFile(fd); !ok {
			t.Fatalf("stdio fd %d not initialized in FSContext", fd)
		}
	}
}

func TestStateNewWithFSConfigPopulatesContextFSCAndMounts(t *testing.T) {
	// This test verifies that when ModuleConfig has an FSConfig with mounts,
	// New initializes the State with:
	// - state.ctx.fsc non-nil (filesystem context in wasi.Context)
	// - ctx.FS() has one writable preopen at "/"
	// - args/env propagated on state.ctx only

	// Create a temp directory for the mount
	tmpDir, err := os.MkdirTemp("", "wasihost-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	memFunc := func() []byte { return make([]byte, 65536) }

	cfg := NewModuleConfig().
		WithArgs("app").
		WithEnv("KEY", "value").
		WithFS(NewFSConfig().WithDirMount("/", tmpDir))

	state := New(memFunc, cfg)

	if state == nil {
		t.Fatal("New returned nil")
	}

	// state.ctx must be populated from config
	if state.ctx == nil {
		t.Fatal("state.ctx is nil; expected New to build and store a wasi.Context from ModuleConfig")
	}

	// state.ctx.fsc must be non-nil when FSConfig has mounts
	if state.ctx.FSContext() == nil {
		t.Fatal("state.ctx.FSContext() is nil; expected non-nil when FSConfig has preopens")
	}

	preopens := state.ctx.FS()
	if len(preopens) != 1 {
		t.Fatalf("ctx.FS() has %d preopens, want 1", len(preopens))
	}
	if preopens[0].GuestPath != "/" {
		t.Errorf("preopen GuestPath = %q, want %q", preopens[0].GuestPath, "/")
	}

	// args/env propagation check
	if len(state.ctx.Args()) != 1 || string(state.ctx.Args()[0]) != "app" {
		t.Errorf("state.ctx.Args() = %v, want [app]", state.ctx.Args())
	}
	if len(state.ctx.Environ()) != 1 || string(state.ctx.Environ()[0]) != "KEY=value" {
		t.Errorf("state.ctx.Environ() = %v, want [KEY=value]", state.ctx.Environ())
	}

	// FSContext should have the preopen at fd 3
	fsc := state.ctx.FSContext()
	entry, ok := fsc.LookupFile(3)
	if !ok {
		t.Error("expected preopen at fd 3 in FSContext")
	} else {
		if !entry.IsPreopen {
			t.Error("expected entry at fd 3 to be a preopen")
		}
		if entry.Name != "/" && entry.GuestPath != "/" {
			t.Errorf("expected preopen guest path '/', got Name=%q GuestPath=%q", entry.Name, entry.GuestPath)
		}
	}
}

// TestStateLookupFDUsesFSContextLookupFile verifies lookupFD exists on State
// and returns the FileEntry from ctx.FSContext().LookupFile (not s.fds[]).
func TestStateLookupFDUsesFSContextLookupFile(t *testing.T) {
	decl := findStateMethodDecl(t, "lookupFD")
	if decl == nil {
		t.Fatal("State must define func (s *State) lookupFD(fd int32) delegating to ctx.FSContext().LookupFile")
	}
	if stateMethodIndexesSFds(decl) {
		t.Fatal("lookupFD must not read s.fds[]; delegate to ctx.FSContext().LookupFile")
	}
	if !stateMethodCallsLookupFile(decl) {
		t.Fatal("lookupFD must call ctx.FSContext().LookupFile (or equivalent FSContext lookup)")
	}
}

func findStateMethodDecl(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()

	paths, err := filepath.Glob("wasihost*.go")
	if err != nil {
		t.Fatalf("glob wasihost*.go: %v", err)
	}

	fset := token.NewFileSet()
	for _, path := range paths {
		if filepath.Ext(path) == ".go" && len(path) > 8 && path[len(path)-8:] == "_test.go" {
			continue
		}
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != name || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			recvType := fn.Recv.List[0].Type
			star, ok := recvType.(*ast.StarExpr)
			if !ok {
				continue
			}
			ident, ok := star.X.(*ast.Ident)
			if ok && ident.Name == "State" {
				return fn
			}
		}
	}
	return nil
}

func stateMethodIndexesSFds(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		sel, ok := idx.X.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if ok && x.Name == "s" && sel.Sel.Name == "fds" {
			found = true
			return false
		}
		return true
	})
	return found
}

func stateMethodCallsLookupFile(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "LookupFile" {
			return true
		}
		found = true
		return false
	})
	return found
}

// TestFdReadUsesPreadWithFileOffset verifies Xfd_read uses positioned I/O via
// File.Pread with FileEntry.Offset instead of Seek-then-Read on entry.offset.
func TestFdReadUsesPreadWithFileOffset(t *testing.T) {
	decl := findStateMethodDecl(t, "Xfd_read")
	if decl == nil {
		t.Fatal("State must define Xfd_read")
	}
	if !funcDeclCallsPreadWithOffset(decl) {
		t.Fatal("Xfd_read must call fe.File.Pread(buf, fe.Offset) (or equivalent FileEntry.Offset)")
	}
	if funcDeclUsesSeekBeforeRead(decl) {
		t.Fatal("Xfd_read must not Seek to entry.offset before Read; use Pread with Offset instead")
	}
}

func funcDeclCallsPreadWithOffset(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Pread" {
			return true
		}
		if exprReferencesOffset(call.Args[1]) {
			found = true
			return false
		}
		return true
	})
	return found
}

func funcDeclUsesSeekBeforeRead(fn *ast.FuncDecl) bool {
	hasSeekToOffset := false
	hasSequentialRead := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Seek":
			if len(call.Args) >= 1 && exprReferencesOffset(call.Args[0]) {
				hasSeekToOffset = true
			}
		case "Read":
			hasSequentialRead = true
		}
		return true
	})
	return hasSeekToOffset && hasSequentialRead
}

func exprReferencesOffset(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "Offset" {
			found = true
			return false
		}
		ident, ok := n.(*ast.Ident)
		if ok && ident.Name == "offset" {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestFdPreadTellPositionedIOViaFileEntry verifies Xfd_pread uses fe.File.Pread
// with the syscall offset param (not fe.Offset) without mutating fe.Offset, and
// Xfd_tell returns fe.Offset from FileEntry.
func TestFdPreadTellPositionedIOViaFileEntry(t *testing.T) {
	pread := findStateMethodDecl(t, "Xfd_pread")
	if pread == nil {
		t.Fatal("State must define Xfd_pread")
	}
	if !funcDeclCallsPreadWithSyscallOffset(pread) {
		t.Fatal("Xfd_pread must call fe.File.Pread(buf, offset) using the syscall offset param, not fe.Offset")
	}
	if funcDeclAssignsToFEOffset(pread) {
		t.Fatal("Xfd_pread must not update fe.Offset")
	}

	tell := findStateMethodDecl(t, "Xfd_tell")
	if tell == nil {
		t.Fatal("State must define Xfd_tell")
	}
	if !funcDeclTellUsesFEOffset(tell) {
		t.Fatal("Xfd_tell must return fe.Offset from FileEntry, not entry.offset")
	}
}

func funcDeclCallsPreadWithSyscallOffset(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Pread" {
			return true
		}
		if exprReferencesFEOffset(call.Args[1]) {
			return true
		}
		if exprReferencesSyscallOffsetParam(call.Args[1]) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprReferencesSyscallOffsetParam(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if ok && (ident.Name == "offset" || ident.Name == "curOff") {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprReferencesFEOffset(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Offset" {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if ok && x.Name == "fe" {
			found = true
			return false
		}
		return true
	})
	return found
}

func funcDeclAssignsToFEOffset(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if exprReferencesFEOffset(lhs) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func funcDeclTellUsesFEOffset(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "PutUint64" {
			return true
		}
		if exprReferencesFEOffset(call.Args[1]) {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestFdCloseUsesFSContextCloseFile verifies Xfd_close delegates close to
// ctx.FSContext().CloseFile(fd), not only manual entry.file.Close and s.fds clear.
func TestFdCloseUsesFSContextCloseFile(t *testing.T) {
	decl := findStateMethodDecl(t, "Xfd_close")
	if decl == nil {
		t.Fatal("State must define Xfd_close")
	}
	if !stateMethodCallsCloseFile(decl) {
		t.Fatal("Xfd_close must call ctx.FSContext().CloseFile(fd), not only manual entry.file.Close and s.fds clear")
	}
}

func stateMethodCallsCloseFile(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "CloseFile" {
			return true
		}
		found = true
		return false
	})
	return found
}

// TestFdReaddirUsesDirentCacheRead verifies Xfd_readdir delegates directory
// listing to fe.DirentCache() and cache.Read, not legacy s.fds snapshot logic.
func TestFdReaddirUsesDirentCacheRead(t *testing.T) {
	decl := findStateMethodDecl(t, "Xfd_readdir")
	if decl == nil {
		t.Fatal("State must define Xfd_readdir")
	}
	if !stateMethodCallsDirentCache(decl) {
		t.Fatal("Xfd_readdir must call fe.DirentCache() (or equivalent FileEntry DirentCache)")
	}
	if !stateMethodCallsDirentCacheRead(decl) {
		t.Fatal("Xfd_readdir must call cache.Read on the DirentCache result")
	}
}

func stateMethodCallsDirentCache(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "DirentCache" {
			return true
		}
		found = true
		return false
	})
	return found
}

func stateMethodCallsDirentCacheRead(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Read" {
			return true
		}
		found = true
		return false
	})
	return found
}

// TestPathOpenUsesFSContextOpenFile verifies Xpath_open allocates fds via
// ctx.FSContext().OpenFile (not legacy s.allocFD/storeOpenedFD or os.OpenFile).
func TestPathOpenUsesFSContextOpenFile(t *testing.T) {
	decl := findStateMethodDecl(t, "Xpath_open")
	if decl == nil {
		t.Fatal("State must define Xpath_open")
	}
	if !stateMethodCallsFSContextOpenFile(decl) {
		t.Fatal("Xpath_open must call ctx.FSContext().OpenFile (or ctx.FS().OpenFile) for fd allocation")
	}
}

func stateMethodCallsFSContextOpenFile(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "OpenFile" {
			return true
		}
		if exprIsFSContextOrFSCall(sel.X) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprIsFSContextOrFSCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "FSContext", "FS":
		return true
	default:
		return false
	}
}

// TestPathCreateDirectoryUsesPreopenFSMkdir verifies Xpath_create_directory
// mutates via preopen.fsys.Mkdir (wasi.FS), not os.Mkdir on a host path.
func TestPathCreateDirectoryUsesPreopenFSMkdir(t *testing.T) {
	decl := findStateMethodDecl(t, "Xpath_create_directory")
	if decl == nil {
		t.Fatal("State must define Xpath_create_directory")
	}
	if !stateMethodCallsFsmkdir(decl) {
		t.Fatal("Xpath_create_directory must call preopen.fsys.Mkdir (wasi.FS), not os.Mkdir")
	}
	if stateMethodCallsOsMkdir(decl) {
		t.Fatal("Xpath_create_directory must not call os.Mkdir directly")
	}
}

func stateMethodCallsFsmkdir(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Mkdir" {
			return true
		}
		if exprIsFsmkdirReceiver(sel.X) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprIsFsmkdirReceiver(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if ok && ident.Name == "fsys" {
		return true
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if ok && sel.Sel.Name == "fsys" {
		return true
	}
	return false
}

func stateMethodCallsOsMkdir(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Mkdir" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestStateStructLegacyFieldsRemoved verifies State no longer duplicates
// ctx-owned runtime fields after state-cleanup-via-context migration.
func TestStateStructLegacyFieldsRemoved(t *testing.T) {
	fields := stateStructFieldNames(t)
	for _, name := range []string{
		"fds", "preopens", "mounts", "env", "args", "stdin", "stdout", "stderr", "startTime",
	} {
		if _, ok := fields[name]; ok {
			t.Errorf("State struct still contains legacy field %q; args/env/clock/stdio/fds must live on ctx", name)
		}
	}
}

func stateStructFieldNames(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "wasihost.go", nil, 0)
	if err != nil {
		t.Fatalf("parse wasihost.go: %v", err)
	}

	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "State" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				t.Fatal("State is not a struct type")
			}
			names := make(map[string]bool, len(st.Fields.List))
			for _, field := range st.Fields.List {
				for _, id := range field.Names {
					names[id.Name] = true
				}
			}
			return names
		}
	}
	t.Fatal("State struct not found in wasihost.go")
	return nil
}
