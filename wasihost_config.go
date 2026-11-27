package wasihost

import (
	"io"
	"io/fs"
	"time"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
)

// ModuleConfig holds configuration for a WASI module execution.
//
// The builder methods, such as WithArgs, WithEnv, WithWalltime and WithOsyield,
// do not change the receiver. Each returns a deep copy with the change applied.
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
	osyield     wasi.Osyield
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

// FSPreopens returns preopens with mount guest paths for ctx.FS() path resolution.
func (c *ModuleConfig) FSPreopens() ([]wasi.FS, []string) {
	if c.fsConfig == nil {
		return nil, nil
	}
	return c.fsConfig.mountPreopens()
}

// ClockWalltime returns configured walltime when set.
func (c *ModuleConfig) ClockWalltime() (wasi.Walltime, wasi.ClockResolution, bool) {
	if c.walltime == nil {
		return nil, 0, false
	}
	return c.walltime, c.walltimeRes, true
}

// ClockNanotime returns configured nanotime when set.
func (c *ModuleConfig) ClockNanotime() (wasi.Nanotime, wasi.ClockResolution, bool) {
	if c.nanotime == nil {
		return nil, 0, false
	}
	return c.nanotime, c.nanotimeRes, true
}

// ClockNanosleep returns configured nanosleep when set.
func (c *ModuleConfig) ClockNanosleep() (wasi.Nanosleep, bool) {
	if c.nanosleep == nil {
		return nil, false
	}
	return c.nanosleep, true
}

// ClockOsyield returns configured osyield when set.
func (c *ModuleConfig) ClockOsyield() (wasi.Osyield, bool) {
	if c.osyield == nil {
		return nil, false
	}
	return c.osyield, true
}

// applyModuleConfig applies a ModuleConfig to the State. It sets the
// WASI context from the config. Panics if the WASI context cannot be created.
func (s *State) applyModuleConfig(cfg *ModuleConfig) {
	ctx, err := wasi.NewContext(cfg)
	if err != nil {
		panic(err)
	}
	s.ctx = ctx
	s.seedPreopenRights(cfg)
}

func (s *State) seedPreopenRights(cfg *ModuleConfig) {
	if cfg == nil || cfg.fsConfig == nil || s.ctx == nil || s.ctx.FSContext() == nil {
		return
	}
	fsList, _ := cfg.fsConfig.preopens()
	_, mountPaths := cfg.fsConfig.mountPreopens()
	for i, fsys := range fsList {
		fd := int32(3 + i)
		fe, ok := s.lookupFD(fd)
		if !ok {
			continue
		}
		if i < len(mountPaths) {
			fe.GuestPath = mountPaths[i]
		}
		if fsysReadOnly(fsys) {
			fe.RightsBase = rightsReadOnlyDirPreopen
			fe.RightsInheriting = rightsReadOnlyDirPreopen
		} else {
			fe.RightsBase = rightsWritableDirPreopenBase
			fe.RightsInheriting = rightsWritableDirPreopenInheriting
		}
	}
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

// WithSysNanotime returns a new ModuleConfig with monotonic time from time.Since.
func (c *ModuleConfig) WithSysNanotime() *ModuleConfig {
	return c.WithNanotime(func() int64 {
		return time.Since(nanotimeBase).Nanoseconds()
	}, SysClockResolutionNanotime)
}

// nanotimeBase is the monotonic origin for WithSysNanotime. It is fixed at
// package initialization rather than at config time, so the clock is already
// positive when a guest first reads it, even on hosts with a coarse
// monotonic clock such as Windows.
var nanotimeBase = time.Now()

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

// WithOsyield returns a copy whose sched_yield calls fn. The default is
// runtime.Gosched.
func (c *ModuleConfig) WithOsyield(fn wasi.Osyield) *ModuleConfig {
	clone := c.clone()
	clone.osyield = fn
	return clone
}

// WithRandSource returns a new ModuleConfig with the given random source.
func (c *ModuleConfig) WithRandSource(r io.Reader) *ModuleConfig {
	clone := c.clone()
	clone.randSource = r
	return clone
}
