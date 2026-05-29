package wasi_test

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// Helper functions to call Context methods via reflection

func callWalltime(ctx *wasi.Context) (sec int64, nsec int32, ok bool) {
	v := reflect.ValueOf(ctx)
	m := v.MethodByName("Walltime")
	if !m.IsValid() {
		return 0, 0, false
	}
	results := m.Call(nil)
	if len(results) != 2 {
		return 0, 0, false
	}
	return results[0].Int(), int32(results[1].Int()), true
}

func callNanotime(ctx *wasi.Context) (int64, bool) {
	v := reflect.ValueOf(ctx)
	m := v.MethodByName("Nanotime")
	if !m.IsValid() {
		return 0, false
	}
	results := m.Call(nil)
	if len(results) != 1 {
		return 0, false
	}
	return results[0].Int(), true
}

func callNanosleep(ctx *wasi.Context, ns int64) bool {
	v := reflect.ValueOf(ctx)
	m := v.MethodByName("Nanosleep")
	if !m.IsValid() {
		return false
	}
	args := []reflect.Value{reflect.ValueOf(ns)}
	results := m.Call(args)
	return len(results) >= 1 && results[0].IsNil()
}

func callOsyield(ctx *wasi.Context) bool {
	v := reflect.ValueOf(ctx)
	m := v.MethodByName("Osyield")
	if !m.IsValid() {
		return false
	}
	results := m.Call(nil)
	return len(results) >= 1 && results[0].IsNil()
}

func callRandSource(ctx *wasi.Context) (io.Reader, bool) {
	v := reflect.ValueOf(ctx)
	m := v.MethodByName("RandSource")
	if !m.IsValid() {
		return nil, false
	}
	results := m.Call(nil)
	if len(results) != 1 {
		return nil, false
	}
	r, ok := results[0].Interface().(io.Reader)
	return r, ok
}

func TestContextImplementsRequiredFieldsValidationDefaultsAndFSInit(t *testing.T) {
	t.Run("args and environ propagation", func(t *testing.T) {
		cfg := &testConfig{
			args:    [][]byte{[]byte("prog"), []byte("arg1")},
			environ: [][]byte{[]byte("A=B")},
		}
		ctx, err := wasi.NewContext(cfg)
		if err != nil {
			t.Fatalf("NewContext: %v", err)
		}

		gotArgs := ctx.Args()
		if len(gotArgs) != 2 {
			t.Fatalf("Args() returned %d entries, want 2", len(gotArgs))
		}
		if string(gotArgs[0]) != "prog" {
			t.Errorf("Args()[0] = %q, want %q", string(gotArgs[0]), "prog")
		}
		if string(gotArgs[1]) != "arg1" {
			t.Errorf("Args()[1] = %q, want %q", string(gotArgs[1]), "arg1")
		}

		gotEnv := ctx.Environ()
		if len(gotEnv) != 1 {
			t.Fatalf("Environ() returned %d entries, want 1", len(gotEnv))
		}
		if string(gotEnv[0]) != "A=B" {
			t.Errorf("Environ()[0] = %q, want %q", string(gotEnv[0]), "A=B")
		}
	})

	t.Run("nil config paths do not panic", func(t *testing.T) {
		cfg := &testConfig{}
		ctx, err := wasi.NewContext(cfg)
		if err != nil {
			t.Fatalf("NewContext with nil paths: %v", err)
		}
		if ctx.Args() == nil {
			t.Error("Args() should return non-nil empty slice, got nil")
		}
		if ctx.Environ() == nil {
			t.Error("Environ() should return non-nil empty slice, got nil")
		}
	})

	t.Run("invalid args with NUL byte are rejected", func(t *testing.T) {
		cfg := &testConfig{
			args: [][]byte{[]byte("prog\x00bad")},
		}
		_, err := wasi.NewContext(cfg)
		if err == nil {
			t.Fatal("NewContext should reject args with NUL bytes, got nil error")
		}
		if !strings.Contains(err.Error(), "NUL") {
			t.Errorf("error should mention NUL, got: %v", err)
		}
	})

	t.Run("invalid environ with NUL byte are rejected", func(t *testing.T) {
		cfg := &testConfig{
			environ: [][]byte{[]byte("A=B\x00bad")},
		}
		_, err := wasi.NewContext(cfg)
		if err == nil {
			t.Fatal("NewContext should reject environ with NUL bytes, got nil error")
		}
		if !strings.Contains(err.Error(), "NUL") {
			t.Errorf("error should mention NUL, got: %v", err)
		}
	})

	t.Run("environ value may contain equals signs", func(t *testing.T) {
		cfg := &testConfig{
			environ: [][]byte{[]byte("A=B=C"), []byte("URL=https://example.com?x=1")},
		}
		ctx, err := wasi.NewContext(cfg)
		if err != nil {
			t.Fatalf("NewContext should accept environ values containing '=': %v", err)
		}
		got := ctx.Environ()
		if len(got) != 2 || string(got[0]) != "A=B=C" || string(got[1]) != "URL=https://example.com?x=1" {
			t.Fatalf("Environ() = %q, want [A=B=C URL=https://example.com?x=1]", got)
		}
	})

	t.Run("invalid environ missing equals separator is rejected", func(t *testing.T) {
		cfg := &testConfig{
			environ: [][]byte{[]byte("NO_SEPARATOR")},
		}
		_, err := wasi.NewContext(cfg)
		if err == nil {
			t.Fatal("NewContext should reject environ without '=', got nil error")
		}
		if !strings.Contains(err.Error(), "=") {
			t.Errorf("error should mention '=', got: %v", err)
		}
	})

	// NewContext accepts any ModuleConfig, including one with empty FSConfig
	// (nil slices). FS initialization must not panic in that case.
	t.Run("NewContext with empty FSConfig does not panic", func(t *testing.T) {
		ctx, err := wasi.NewContext(&testConfig{})
		if err != nil {
			t.Fatalf("NewContext with empty FSConfig: %v", err)
		}
		if ctx == nil {
			t.Fatal("NewContext returned nil context with empty FSConfig")
		}
	})

	// Defaults and FS initialization tests
	t.Run("defaults are non-nil and callable", func(t *testing.T) {
		cfg := &testConfig{}
		ctx, err := wasi.NewContext(cfg)
		if err != nil {
			t.Fatalf("NewContext: %v", err)
		}

		// Test that clock methods exist and return expected defaults
		// Walltime returns (0, 0) by default
		wt, wtRes, ok := callWalltime(ctx)
		if !ok {
			t.Error("Context should have Walltime method that returns (0, 0) by default")
		} else if wt != 0 || wtRes != 0 {
			t.Errorf("Walltime() = (%d, %d), want (0, 0)", wt, wtRes)
		}

		// Nanotime returns 0 by default
		nt, ok := callNanotime(ctx)
		if !ok {
			t.Error("Context should have Nanotime method that returns 0 by default")
		} else if nt != 0 {
			t.Errorf("Nanotime() = %d, want 0", nt)
		}

		// Nanosleep(0) is a no-op by default
		if !callNanosleep(ctx, 0) {
			t.Error("Context should have Nanosleep method that accepts 0 without error")
		}

		// Osyield is a no-op by default
		if !callOsyield(ctx) {
			t.Error("Context should have Osyield method that is callable")
		}

		// RandSource returns a reader that can be read
		rng, ok := callRandSource(ctx)
		if !ok {
			t.Error("Context should have RandSource method")
		} else {
			buf := make([]byte, 16)
			n, err := rng.Read(buf)
			if err != nil {
				t.Errorf("RandSource().Read() returned error %v", err)
			}
			if n != 16 {
				t.Errorf("RandSource().Read() returned %d bytes, want 16", n)
			}
		}
	})

	t.Run("FSConfig with one DirMount creates preopen at fd 3", func(t *testing.T) {
		cfg := &testConfig{
			fs:    []wasi.FS{&testDirFS{}},
			guest: []string{"/test"},
		}
		ctx, err := wasi.NewContext(cfg)
		if err != nil {
			t.Fatalf("NewContext: %v", err)
		}

		// FSContext() should return non-nil FSContext via reflection
		fsc, ok := callFSContext(ctx)
		if !ok {
			t.Fatal("Context should have FSContext method")
		}
		if fsc == nil {
			t.Fatal("FSContext() returned nil")
		}

		// fd 3 should be the preopen directory
		fe, ok := fsc.LookupFile(3)
		if !ok {
			t.Fatal("LookupFile(3): preopen dir not found at fd 3")
		}
		if !fe.IsPreopen {
			t.Fatal("LookupFile(3): entry is not marked IsPreopen")
		}
	})
}

func callFSContext(ctx *wasi.Context) (*wasi.FSContext, bool) {
	v := reflect.ValueOf(ctx)
	m := v.MethodByName("FSContext")
	if !m.IsValid() {
		return nil, false
	}
	results := m.Call(nil)
	if len(results) != 1 {
		return nil, false
	}
	fsc, ok := results[0].Interface().(*wasi.FSContext)
	return fsc, ok
}
