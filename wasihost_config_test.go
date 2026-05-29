package wasihost

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

func TestModuleConfig(t *testing.T) {
	t.Run("NewModuleConfig returns non-nil", func(t *testing.T) {
		cfg := NewModuleConfig()
		if cfg == nil {
			t.Fatal("NewModuleConfig() returned nil, expected non-nil *ModuleConfig")
		}
	})

	t.Run("clone creates independent copy", func(t *testing.T) {
		original := &ModuleConfig{
			args:    [][]byte{[]byte("arg1"), []byte("arg2")},
			environ: [][]byte{[]byte("key=value")},
		}

		clone := original.clone()
		if clone == nil {
			t.Fatal("clone() returned nil, expected non-nil *ModuleConfig")
		}

		// Modify the clone's args
		clone.args[0] = []byte("modified")

		// Verify original is unchanged
		if string(original.args[0]) != "arg1" {
			t.Errorf("original.args[0] was modified, expected 'arg1', got '%s'", original.args[0])
		}
	})
}

func TestModuleConfig_WithArgs_WithEnv_WithName(t *testing.T) {
	t.Run("WithArgs sets args and returns independent copy", func(t *testing.T) {
		cfg := NewModuleConfig().WithArgs("a", "b")
		if len(cfg.args) != 2 {
			t.Fatalf("expected 2 args, got %d", len(cfg.args))
		}
		if string(cfg.args[0]) != "a" || string(cfg.args[1]) != "b" {
			t.Errorf("expected args [a b], got [%s %s]", cfg.args[0], cfg.args[1])
		}

		// Verify immutability: cfg2 is a new independent copy
		cfg2 := cfg.WithArgs("c")
		if len(cfg2.args) != 1 {
			t.Errorf("expected cfg2 to have 1 arg, got %d", len(cfg2.args))
		}
		if len(cfg.args) != 2 {
			t.Errorf("expected original cfg to still have 2 args, got %d", len(cfg.args))
		}
	})

	t.Run("WithEnv appends environ entry", func(t *testing.T) {
		cfg := NewModuleConfig().WithEnv("KEY", "val")
		if len(cfg.environ) != 1 {
			t.Fatalf("expected 1 environ entry, got %d", len(cfg.environ))
		}
		expected := "KEY=val"
		if string(cfg.environ[0]) != expected {
			t.Errorf("expected environ[%q], got %q", expected, cfg.environ[0])
		}
	})

	t.Run("WithName sets name", func(t *testing.T) {
		cfg := NewModuleConfig().WithName("mod")
		if cfg.name != "mod" {
			t.Errorf("expected name %q, got %q", "mod", cfg.name)
		}
	})
}

func TestModuleConfig_WithStdin_WithStdout_WithStderr(t *testing.T) {
	t.Run("WithStdin sets stdin and returns independent copy", func(t *testing.T) {
		cfg := NewModuleConfig()
		reader := strings.NewReader("input")
		result := cfg.WithStdin(reader)

		if result.stdin == nil {
			t.Fatal("expected stdin to be non-nil")
		}
		if result.stdin != reader {
			t.Error("expected stdin to be the reader")
		}

		// Verify immutability
		reader2 := strings.NewReader("input2")
		result.WithStdin(reader2)

		if result.stdin != reader {
			t.Error("expected original stdin to still be the reader")
		}
	})

	t.Run("WithStdout sets stdout and returns independent copy", func(t *testing.T) {
		cfg := NewModuleConfig()
		buf := &bytes.Buffer{}
		result := cfg.WithStdout(buf)

		if result.stdout != buf {
			t.Error("expected stdout to be the buffer")
		}

		// Verify immutability
		buf2 := &bytes.Buffer{}
		result.WithStdout(buf2)

		if result.stdout != buf {
			t.Error("expected original stdout to still be the buffer")
		}
	})

	t.Run("WithStderr sets stderr and returns independent copy", func(t *testing.T) {
		cfg := NewModuleConfig()
		buf := &bytes.Buffer{}
		result := cfg.WithStderr(buf)

		if result.stderr != buf {
			t.Error("expected stderr to be the buffer")
		}

		// Verify immutability
		buf2 := &bytes.Buffer{}
		result.WithStderr(buf2)

		if result.stderr != buf {
			t.Error("expected original stderr to still be the buffer")
		}
	})
}

func TestModuleConfig_ClockAndRandom(t *testing.T) {
	t.Run("WithWalltime sets walltime and resolution", func(t *testing.T) {
		customWalltime := func() (int64, int32) { return 1234567890, 42 }
		res := wasi.ClockResolution(1000)
		cfg := NewModuleConfig().WithWalltime(customWalltime, res)

		if cfg.walltime == nil {
			t.Fatal("expected walltime to be non-nil")
		}
		if cfg.walltimeRes != res {
			t.Errorf("expected walltimeRes %d, got %d", res, cfg.walltimeRes)
		}

		sec, nsec := cfg.walltime()
		if sec != 1234567890 || nsec != 42 {
			t.Errorf("walltime() = (%d, %d), want (1234567890, 42)", sec, nsec)
		}
	})

	t.Run("WithSysWalltime sets walltime from time.Now", func(t *testing.T) {
		cfg := NewModuleConfig().WithSysWalltime()

		if cfg.walltime == nil {
			t.Fatal("expected walltime to be non-nil")
		}

		sec, _ := cfg.walltime()
		// Jan 1, 2020 = 1577836800
		const jan2020 = int64(1577836800)
		if sec <= jan2020 {
			t.Errorf("walltime() sec = %d, want > %d (Jan 1 2020)", sec, jan2020)
		}
	})

	t.Run("WithNanotime sets nanotime and resolution", func(t *testing.T) {
		customNanotime := func() int64 { return 999000000 }
		res := wasi.ClockResolution(1)
		cfg := NewModuleConfig().WithNanotime(customNanotime, res)

		if cfg.nanotime == nil {
			t.Fatal("expected nanotime to be non-nil")
		}
		if cfg.nanotimeRes != res {
			t.Errorf("expected nanotimeRes %d, got %d", res, cfg.nanotimeRes)
		}

		got := cfg.nanotime()
		if got != 999000000 {
			t.Errorf("nanotime() = %d, want 999000000", got)
		}
	})

	t.Run("WithSysNanotime sets nanotime from time.Now", func(t *testing.T) {
		cfg := NewModuleConfig().WithSysNanotime()

		if cfg.nanotime == nil {
			t.Fatal("expected nanotime to be non-nil")
		}

		got := cfg.nanotime()
		// Should be a large positive number (nanoseconds since epoch)
		if got <= 0 {
			t.Errorf("nanotime() = %d, want > 0", got)
		}
	})

	t.Run("WithNanosleep sets nanosleep", func(t *testing.T) {
		called := false
		customNanosleep := func(ns int64) { called = true }
		cfg := NewModuleConfig().WithNanosleep(customNanosleep)

		if cfg.nanosleep == nil {
			t.Fatal("expected nanosleep to be non-nil")
		}

		cfg.nanosleep(0)
		if !called {
			t.Error("expected nanosleep to be called")
		}
	})

	t.Run("WithSysNanosleep sets nanosleep from time.Sleep", func(t *testing.T) {
		cfg := NewModuleConfig().WithSysNanosleep()

		if cfg.nanosleep == nil {
			t.Fatal("expected nanosleep to be non-nil")
		}

		// nanosleep(0) must not panic
		panicked := true
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("nanosleep(0) panicked: %v", r)
				} else {
					panicked = false
				}
			}()
			cfg.nanosleep(0)
		}()
		_ = panicked
	})

	t.Run("WithRandSource sets rand source", func(t *testing.T) {
		src := rand.Reader
		cfg := NewModuleConfig().WithRandSource(src)

		if cfg.randSource == nil {
			t.Fatal("expected randSource to be non-nil")
		}
		if cfg.randSource != src {
			t.Error("expected randSource to be the provided source")
		}
	})

	t.Run("clock methods return independent copies", func(t *testing.T) {
		cfg1 := NewModuleConfig().WithSysWalltime().WithSysNanosleep()
		cfg2 := cfg1.WithSysNanotime()

		// cfg1 should not have nanotime set
		if cfg1.nanotime != nil {
			t.Error("expected cfg1.nanotime to be nil")
		}
		// cfg2 should have nanotime set
		if cfg2.nanotime == nil {
			t.Error("expected cfg2.nanotime to be non-nil")
		}
		// Both should have walltime and nanosleep
		if cfg2.walltime == nil {
			t.Error("expected cfg2.walltime to be non-nil")
		}
		if cfg2.nanosleep == nil {
			t.Error("expected cfg2.nanosleep to be non-nil")
		}
	})
}

func TestModuleConfig_WithFS(t *testing.T) {
	t.Run("WithFS stores fsConfig and FSConfig returns preopens", func(t *testing.T) {
		cfg := NewModuleConfig().WithFS(NewFSConfig().WithDirMount("/", "/tmp"))
		fs, paths := cfg.FSConfig()
		if len(fs) != 1 {
			t.Fatalf("expected 1 preopen, got %d", len(fs))
		}
		if len(paths) != 1 {
			t.Fatalf("expected 1 guest path, got %d", len(paths))
		}
		if paths[0] != "" {
			t.Errorf("expected normalized guest path '', got %q", paths[0])
		}
	})

	t.Run("WithHostDirectoryPreopen still produces writable mount via FSConfig internally", func(t *testing.T) {
		cfg := NewModuleConfig().WithHostDirectoryPreopen("/data", "/tmp/data")
		fs, paths := cfg.FSConfig()
		if len(fs) != 1 {
			t.Fatalf("expected 1 preopen from WithHostDirectoryPreopen, got %d", len(fs))
		}
		if len(paths) != 1 || paths[0] != "data" {
			t.Fatalf("expected normalized guest path %q, got %v", "data", paths)
		}
		if d, ok := fs[0].(interface{ PathFor() string }); !ok || d.PathFor() != "/tmp/data" {
			t.Errorf("expected DirFS at /tmp/data, got %T", fs[0])
		}
	})

	t.Run("builder immutability: chained WithFS does not mutate prior config", func(t *testing.T) {
		cfg1 := NewModuleConfig()
		cfg2 := cfg1.WithFS(NewFSConfig().WithDirMount("/a", "/tmp/a"))
		cfg3 := cfg2.WithFS(NewFSConfig().WithDirMount("/b", "/tmp/b"))

		if fs, _ := cfg1.FSConfig(); fs != nil {
			t.Error("expected cfg1.FSConfig() to return nil, got non-nil")
		}

		fs2, _ := cfg2.FSConfig()
		if len(fs2) != 1 {
			t.Errorf("expected cfg2 to have 1 preopen, got %d", len(fs2))
		}

		fs3, _ := cfg3.FSConfig()
		if len(fs3) != 1 {
			t.Errorf("expected cfg3 to have 1 preopen, got %d", len(fs3))
		}
	})
}

func TestModuleConfig_CloneByteLevelIndependence(t *testing.T) {
	t.Run("clone produces byte-level independent args", func(t *testing.T) {
		orig := NewModuleConfig().WithArgs("hello")
		clone := orig.WithArgs("hello")

		// Mutate the first byte of the first arg in the clone
		clone.args[0][0] = 'X'

		// Original must be unaffected — byte-level independence
		if string(orig.args[0]) != "hello" {
			t.Errorf("original args[0] mutated after clone modification: got %q, want %q",
				string(orig.args[0]), "hello")
		}
	})

	t.Run("clone produces byte-level independent environ", func(t *testing.T) {
		orig := NewModuleConfig().WithEnv("KEY", "value")
		clone := orig.WithEnv("KEY", "value")

		// Mutate a byte in the first environ entry of the clone
		clone.environ[0][0] = 'X'

		// Original must be unaffected — byte-level independence
		if string(orig.environ[0]) != "KEY=value" {
			t.Errorf("original environ[0] mutated after clone modification: got %q, want %q",
				string(orig.environ[0]), "KEY=value")
		}
	})

	t.Run("clone via WithEnv does not share environ element backing arrays", func(t *testing.T) {
		cfg1 := NewModuleConfig().WithEnv("A", "1")
		cfg2 := cfg1.WithEnv("B", "2")

		// Mutate the second environ entry of cfg2 in-place
		cfg2.environ[1][0] = 'X'

		// cfg1's environ must be unaffected
		if len(cfg1.environ) != 1 {
			t.Fatalf("expected cfg1 to have 1 environ entry, got %d", len(cfg1.environ))
		}
		if string(cfg1.environ[0]) != "A=1" {
			t.Errorf("cfg1.environ[0] = %q, want %q", string(cfg1.environ[0]), "A=1")
		}
	})

	t.Run("clone via WithArgs does not share arg element backing arrays", func(t *testing.T) {
		cfg1 := NewModuleConfig().WithArgs("alpha")
		cfg2 := cfg1.WithArgs("beta")

		// Mutate the first arg of cfg2
		cfg2.args[0][0] = 'X'

		// cfg1's args must be unaffected
		if len(cfg1.args) != 1 {
			t.Fatalf("expected cfg1 to have 1 arg, got %d", len(cfg1.args))
		}
		if string(cfg1.args[0]) != "alpha" {
			t.Errorf("cfg1.args[0] = %q, want %q", string(cfg1.args[0]), "alpha")
		}
	})
}
