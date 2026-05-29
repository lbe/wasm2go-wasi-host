package wasi_test

import (
	"io"
	"io/fs"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// testConfig is a local stub implementing wasi.ModuleConfig for tests,
// avoiding a dependency on the wasihost subpackage.
type testConfig struct {
	args    [][]byte
	environ [][]byte
	fs      []wasi.FS
	guest   []string
}

// testDirFS is a minimal FS implementation for testing FSConfig.
type testDirFS struct{}

func (testDirFS) PathFor() string { return "/test" }
func (testDirFS) OpenFile(path string, flag wasi.Oflag, perm fs.FileMode) (wasi.File, wasi.Errno) {
	return nil, 0
}
func (testDirFS) Lstat(path string) (wasi.Stat_t, wasi.Errno) { return wasi.Stat_t{}, 0 }
func (testDirFS) Stat(path string) (wasi.Stat_t, wasi.Errno)  { return wasi.Stat_t{}, 0 }
func (testDirFS) Mkdir(string, fs.FileMode) wasi.Errno        { return 0 }
func (testDirFS) Chmod(string, fs.FileMode) wasi.Errno        { return 0 }
func (testDirFS) Rename(string, string) wasi.Errno            { return 0 }
func (testDirFS) Rmdir(string) wasi.Errno                     { return 0 }
func (testDirFS) Unlink(string) wasi.Errno                    { return 0 }
func (testDirFS) Link(string, string) wasi.Errno              { return 0 }
func (testDirFS) Symlink(string, string) wasi.Errno           { return 0 }
func (testDirFS) Readlink(string) (string, wasi.Errno)        { return "", 0 }
func (testDirFS) Utimens(string, int64, int64) wasi.Errno     { return 0 }

func (c *testConfig) Args() [][]byte        { return c.args }
func (c *testConfig) Environ() [][]byte     { return c.environ }
func (c *testConfig) Stdin() io.Reader      { return nil }
func (c *testConfig) Stdout() io.Writer     { return nil }
func (c *testConfig) Stderr() io.Writer     { return nil }
func (c *testConfig) RandSource() io.Reader { return nil }
func (c *testConfig) FSConfig() ([]wasi.FS, []string) {
	return c.fs, c.guest
}

func (c *testConfig) withArgs(args ...string) *testConfig {
	c.args = make([][]byte, len(args))
	for i, arg := range args {
		c.args[i] = []byte(arg)
	}
	return c
}

func (c *testConfig) withEnv(env string) *testConfig {
	c.environ = append(c.environ, []byte(env))
	return c
}

func newTestConfig() *testConfig {
	return &testConfig{}
}

// TestContextNewContextBuildsFromModuleConfig verifies that NewContext
// constructs a Context from a ModuleConfig's args, env, and other settings.
func TestContextNewContextBuildsFromModuleConfig(t *testing.T) {
	t.Run("args are propagated from config", func(t *testing.T) {
		config := newTestConfig().
			withArgs("prog", "arg1")

		ctx, err := wasi.NewContext(config)
		if err != nil {
			t.Fatalf("NewContext: %v", err)
		}

		got := ctx.Args()
		if len(got) != 2 {
			t.Fatalf("Args() returned %d entries, want 2", len(got))
		}
		if string(got[0]) != "prog" {
			t.Errorf("Args()[0] = %q, want %q", string(got[0]), "prog")
		}
		if string(got[1]) != "arg1" {
			t.Errorf("Args()[1] = %q, want %q", string(got[1]), "arg1")
		}
	})

	t.Run("environ is propagated from config", func(t *testing.T) {
		config := newTestConfig().
			withEnv("A=B")

		ctx, err := wasi.NewContext(config)
		if err != nil {
			t.Fatalf("NewContext: %v", err)
		}

		got := ctx.Environ()
		if len(got) != 1 {
			t.Fatalf("Environ() returned %d entries, want 1", len(got))
		}
		if string(got[0]) != "A=B" {
			t.Errorf("Environ()[0] = %q, want %q", string(got[0]), "A=B")
		}
	})

	t.Run("nil stdin does not panic", func(t *testing.T) {
		config := newTestConfig()
		// ModuleConfig without args or env should not panic
		ctx, err := wasi.NewContext(config)
		if err != nil {
			t.Fatalf("NewContext: %v", err)
		}

		gotArgs := ctx.Args()
		gotEnv := ctx.Environ()
		if len(gotArgs) != 0 {
			t.Errorf("Args() with nil config returned %d entries, want 0", len(gotArgs))
		}
		if len(gotEnv) != 0 {
			t.Errorf("Environ() with nil config returned %d entries, want 0", len(gotEnv))
		}
	})
}
