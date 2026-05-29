package wasihost

import (
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

// ModuleConfig holds configuration for a WASI module execution.
//
// The builder methods (WithArgs, WithEnv, WithName, etc.) follow an immutable
// pattern: each returns a new deep copy with the change applied, leaving the
// original unchanged.
type ModuleConfig struct {
	name        string
	args        [][]byte
	environ     [][]byte
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	randSource  io.Reader
	walltime    wasi.Walltime
	walltimeRes wasi.ClockResolution
	nanotime    wasi.Nanotime
	nanotimeRes wasi.ClockResolution
	nanosleep   wasi.Nanosleep
	fsConfig    *FSConfig
}

// NewModuleConfig returns a pointer to a zero-value ModuleConfig.
func NewModuleConfig() *ModuleConfig {
	return &ModuleConfig{}
}

// clone creates a deep copy of the struct. args, environ, and fsConfig are fully
// independent so that mutations on the copy never affect the original.
func (c *ModuleConfig) clone() *ModuleConfig {
	ret := *c
	if len(c.args) > 0 {
		ret.args = make([][]byte, len(c.args))
		for i, a := range c.args {
			ret.args[i] = make([]byte, len(a))
			copy(ret.args[i], a)
		}
	}
	if len(c.environ) > 0 {
		ret.environ = make([][]byte, len(c.environ))
		for i, e := range c.environ {
			ret.environ[i] = make([]byte, len(e))
			copy(ret.environ[i], e)
		}
	}
	if c.fsConfig != nil {
		ret.fsConfig = c.fsConfig.clone()
	}
	return &ret
}

// --- wasi.ModuleConfig interface methods ---

// Args returns the guest argument list.
func (c *ModuleConfig) Args() [][]byte { return c.args }

// Environ returns the guest environment variable list in "KEY=VALUE" form.
func (c *ModuleConfig) Environ() [][]byte { return c.environ }

// Stdin returns the reader used for guest fd 0, or nil if unset.
func (c *ModuleConfig) Stdin() io.Reader { return c.stdin }

// Stdout returns the writer used for guest fd 1, or nil if unset.
func (c *ModuleConfig) Stdout() io.Writer { return c.stdout }

// Stderr returns the writer used for guest fd 2, or nil if unset.
func (c *ModuleConfig) Stderr() io.Writer { return c.stderr }

// RandSource returns the reader used for random data (random_get), or nil if unset.
func (c *ModuleConfig) RandSource() io.Reader { return c.randSource }

// FSConfig returns the preopened filesystem mounts and their guest paths.
func (c *ModuleConfig) FSConfig() ([]wasi.FS, []string) {
	if c.fsConfig == nil {
		return nil, nil
	}
	return c.fsConfig.preopens()
}

// mountEntries builds State mount table entries from FSConfig preopens.
func (c *ModuleConfig) mountEntries() []mountEntry {
	if c.fsConfig == nil {
		return nil
	}
	fsList, guestPaths := c.fsConfig.mountPreopens()
	mounts := make([]mountEntry, 0, len(fsList))
	for i, fsys := range fsList {
		mounts = append(mounts, mountEntryFromWasiFS(fsys, guestPaths[i]))
	}
	return mounts
}

func mountEntryFromWasiFS(fsys wasi.FS, guestPath string) mountEntry {
	switch f := fsys.(type) {
	case sysfs.DirFS:
		return mountEntry{
			guestPath: guestPath,
			hostRoot:  f.Path,
			root:      os.DirFS(f.Path),
			writable:  true,
		}
	case *sysfs.DirFS:
		return mountEntry{
			guestPath: guestPath,
			hostRoot:  f.Path,
			root:      os.DirFS(f.Path),
			writable:  true,
		}
	case sysfs.ReadFS:
		return mountEntryFromWasiFS(f.FS, guestPath)
	case *sysfs.ReadFS:
		return mountEntryFromWasiFS(f.FS, guestPath)
	case sysfs.AdaptFS:
		return mountEntry{
			guestPath: guestPath,
			root:      f.FS,
			readonly:  true,
		}
	case *sysfs.AdaptFS:
		return mountEntry{
			guestPath: guestPath,
			root:      f.FS,
			readonly:  true,
		}
	default:
		return mountEntry{guestPath: guestPath, root: os.DirFS("."), readonly: true}
	}
}

// applyModuleConfig applies a ModuleConfig to the State. It sets the
// WASI context, args, environ, stdio, and mounts from the config. Panics
// if the WASI context cannot be created.
func (s *State) applyModuleConfig(cfg *ModuleConfig) {
	ctx, err := wasi.NewContext(cfg)
	if err != nil {
		panic(err)
	}
	s.ctx = ctx
	args := make([]string, len(cfg.args))
	for i, a := range cfg.args {
		args[i] = string(a)
	}
	s.args = args
	env := make([]string, len(cfg.environ))
	for i, e := range cfg.environ {
		env[i] = string(e)
	}
	s.env = env
	if cfg.stdin != nil {
		s.stdin = cfg.stdin
	}
	if cfg.stdout != nil {
		s.stdout = cfg.stdout
	}
	if cfg.stderr != nil {
		s.stderr = cfg.stderr
	}
	s.mounts = append(s.mounts, cfg.mountEntries()...)
}

// --- Builder methods ---

// WithArgs returns a new ModuleConfig with the given args.
func (c *ModuleConfig) WithArgs(args ...string) *ModuleConfig {
	clone := c.clone()
	clone.args = make([][]byte, len(args))
	for i, arg := range args {
		clone.args[i] = []byte(arg)
	}
	return clone
}

// WithEnv returns a new ModuleConfig with the given environment variable appended.
func (c *ModuleConfig) WithEnv(key, value string) *ModuleConfig {
	clone := c.clone()
	clone.environ = append(clone.environ, []byte(key+"="+value))
	return clone
}

// WithEnvRaw returns a new ModuleConfig with the given raw "KEY=VALUE"
// environment variable appended. It is an additive helper retained for
// template compatibility; call WithEnv for semantic key/value pairing.
func (c *ModuleConfig) WithEnvRaw(env string) *ModuleConfig {
	clone := c.clone()
	clone.environ = append(clone.environ, []byte(env))
	return clone
}

// WithName returns a new ModuleConfig with the given module name.
func (c *ModuleConfig) WithName(name string) *ModuleConfig {
	clone := c.clone()
	clone.name = name
	return clone
}

// WithStdin returns a new ModuleConfig with the given stdin reader.
func (c *ModuleConfig) WithStdin(r io.Reader) *ModuleConfig {
	clone := c.clone()
	clone.stdin = r
	return clone
}

// WithStdout returns a new ModuleConfig with the given stdout writer.
func (c *ModuleConfig) WithStdout(w io.Writer) *ModuleConfig {
	clone := c.clone()
	clone.stdout = w
	return clone
}

// WithStderr returns a new ModuleConfig with the given stderr writer.
func (c *ModuleConfig) WithStderr(w io.Writer) *ModuleConfig {
	clone := c.clone()
	clone.stderr = w
	return clone
}

// WithFS sets the filesystem configuration for preopened mounts.
func (c *ModuleConfig) WithFS(fsConfig *FSConfig) *ModuleConfig {
	clone := c.clone()
	if fsConfig != nil {
		clone.fsConfig = fsConfig.clone()
	}
	return clone
}

// WithHostDirectoryPreopen adds a writable host directory preopen at guestPath.
func (c *ModuleConfig) WithHostDirectoryPreopen(guestPath, hostPath string) *ModuleConfig {
	clone := c.clone()
	if clone.fsConfig == nil {
		clone.fsConfig = NewFSConfig()
	}
	clone.fsConfig = clone.fsConfig.WithDirMount(guestPath, hostPath)
	return clone
}

// WithReadOnlyFS adds a read-only guest filesystem mount that is explicitly
// marked as read-only in WASI preopens, reporting no write or mutation rights.
func (c *ModuleConfig) WithReadOnlyFS(guestPath string, root fs.FS) *ModuleConfig {
	clone := c.clone()
	if clone.fsConfig == nil {
		clone.fsConfig = NewFSConfig()
	}
	clone.fsConfig = clone.fsConfig.WithFSMount(guestPath, root)
	return clone
}

// WithWalltime returns a new ModuleConfig with the given walltime function and resolution.
func (c *ModuleConfig) WithWalltime(w wasi.Walltime, res wasi.ClockResolution) *ModuleConfig {
	clone := c.clone()
	clone.walltime = w
	clone.walltimeRes = res
	return clone
}

// SysClockResolutionWalltime is the resolution reported by the system
// wallclock in nanoseconds (1000 ns = 1 µs).
const SysClockResolutionWalltime = wasi.ClockResolution(1000)

// SysClockResolutionNanotime is the resolution reported by the system
// monotonic clock in nanoseconds (1 ns).
const SysClockResolutionNanotime = wasi.ClockResolution(1)

// WithSysWalltime returns a new ModuleConfig with walltime sourced from time.Now.
func (c *ModuleConfig) WithSysWalltime() *ModuleConfig {
	return c.WithWalltime(func() (int64, int32) {
		t := time.Now()
		return t.Unix(), int32(t.Nanosecond())
	}, SysClockResolutionWalltime)
}

// WithNanotime returns a new ModuleConfig with the given nanotime function and resolution.
func (c *ModuleConfig) WithNanotime(n wasi.Nanotime, res wasi.ClockResolution) *ModuleConfig {
	clone := c.clone()
	clone.nanotime = n
	clone.nanotimeRes = res
	return clone
}

// WithSysNanotime returns a new ModuleConfig with nanotime sourced from time.Now.
func (c *ModuleConfig) WithSysNanotime() *ModuleConfig {
	return c.WithNanotime(func() int64 {
		return time.Now().UnixNano()
	}, SysClockResolutionNanotime)
}

// WithNanosleep returns a new ModuleConfig with the given nanosleep function.
func (c *ModuleConfig) WithNanosleep(ns wasi.Nanosleep) *ModuleConfig {
	clone := c.clone()
	clone.nanosleep = ns
	return clone
}

// WithSysNanosleep returns a new ModuleConfig with nanosleep sourced from time.Sleep.
func (c *ModuleConfig) WithSysNanosleep() *ModuleConfig {
	return c.WithNanosleep(func(ns int64) { time.Sleep(time.Duration(ns)) })
}

// WithRandSource returns a new ModuleConfig with the given random source.
func (c *ModuleConfig) WithRandSource(r io.Reader) *ModuleConfig {
	clone := c.clone()
	clone.randSource = r
	return clone
}
