package wasihost

import (
	"os"
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

	// state.args must match config args (set from config, not from options)
	if len(state.args) != 2 {
		t.Fatalf("state.args = %d entries, want 2", len(state.args))
	}
	if state.args[0] != "app" {
		t.Errorf("state.args[0] = %q, want %q", state.args[0], "app")
	}

	// state.env must match config environ
	if len(state.env) != 2 {
		t.Fatalf("state.env = %d entries, want 2", len(state.env))
	}

	// fds table must be initialized with stdin/stdout/stderr slots
	if len(state.fds) < 3 {
		t.Fatalf("state.fds has %d entries, want >= 3", len(state.fds))
	}

	// invalidated map must be initialized
	if state.invalidated == nil {
		t.Fatal("state.invalidated is nil; expected initialized map")
	}

	// mounts should be empty (no Option-based mounts)
	if len(state.mounts) != 0 {
		t.Errorf("state.mounts = %d entries, want 0 (config-only ctor should not use Option-based mounts)", len(state.mounts))
	}
}

func TestStateNewWithFSConfigPopulatesContextFSCAndMounts(t *testing.T) {
	// This test verifies that when ModuleConfig has an FSConfig with mounts,
	// the State is properly initialized with:
	// - state.ctx.fsc non-nil (filesystem context in wasi.Context)
	// - state.mounts has one writable preopen at "/"
	// - args/env still propagated on state.ctx and state.args/state.env

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

	// state.mounts should have one writable preopen at "/"
	if len(state.mounts) != 1 {
		t.Fatalf("state.mounts has %d entries, want 1", len(state.mounts))
	}

	mount := state.mounts[0]
	if mount.guestPath != "/" {
		t.Errorf("mount.guestPath = %q, want %q", mount.guestPath, "/")
	}
	if !mount.writable {
		t.Error("mount is not writable; expected writable DirFS mount")
	}
	// DirFS mounts should have hostRoot set
	if mount.hostRoot == "" {
		t.Error("mount.hostRoot is empty; expected DirFS mount to have hostRoot set")
	}

	// args/env propagation check
	if len(state.ctx.Args()) != 1 || string(state.ctx.Args()[0]) != "app" {
		t.Errorf("state.ctx.Args() = %v, want [app]", state.ctx.Args())
	}
	if len(state.ctx.Environ()) != 1 || string(state.ctx.Environ()[0]) != "KEY=value" {
		t.Errorf("state.ctx.Environ() = %v, want [KEY=value]", state.ctx.Environ())
	}

	// state.args and state.env should also be set
	if len(state.args) != 1 || state.args[0] != "app" {
		t.Errorf("state.args = %v, want [app]", state.args)
	}
	if len(state.env) != 1 || state.env[0] != "KEY=value" {
		t.Errorf("state.env = %v, want [KEY=value]", state.env)
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
		if entry.Name != "/" {
			t.Errorf("expected entry.Name to be '/', got %s", entry.Name)
		}
	}
}
