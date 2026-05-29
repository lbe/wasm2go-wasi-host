package wasi

import (
	"bytes"
	"fmt"
	"io"
	"math"
)

// ModuleConfig is the interface wasi.NewContext accepts to initialize
// a Context. The concrete type lives in the wasihost package, avoiding
// a circular import in the production code.
type ModuleConfig interface {
	Args() [][]byte
	Environ() [][]byte
	Stdin() io.Reader
	Stdout() io.Writer
	Stderr() io.Writer
	RandSource() io.Reader
	FSConfig() (fsList []FS, guestPaths []string)
}

// Context holds the execution state for one WASI guest instance.
type Context struct {
	args    [][]byte
	environ [][]byte
	// argsSize and environSize track total byte sizes for validation
	argsSize    uint32
	environSize uint32
	// Clock fields
	walltime    Walltime
	walltimeRes ClockResolution
	nanotime    Nanotime
	nanotimeRes ClockResolution
	nanosleep   Nanosleep
	osyield     Osyield
	// FS context
	fsc *FSContext
	// Random source
	randSource io.Reader
}

// NewContext builds a Context from the fields exposed by a ModuleConfig.
// It validates args and environ and applies nil-safe defaults.
func NewContext(config ModuleConfig) (*Context, error) {
	args := config.Args()
	environ := config.Environ()

	if args == nil {
		args = [][]byte{}
	}
	if environ == nil {
		environ = [][]byte{}
	}

	// Validate args: no NUL bytes, count < math.MaxUint32, total size < math.MaxUint32
	if len(args) >= math.MaxUint32 {
		return nil, fmt.Errorf("args count %d exceeds maximum %d", len(args), math.MaxUint32-1)
	}
	for i, a := range args {
		if bytes.IndexByte(a, 0) >= 0 {
			return nil, fmt.Errorf("args[%d] contains NUL byte", i)
		}
	}

	var argsSize uint32
	for _, a := range args {
		// Check for overflow before adding
		if uint32(len(a)) > math.MaxUint32-argsSize {
			return nil, fmt.Errorf("args total size exceeds maximum %d", math.MaxUint32-1)
		}
		argsSize += uint32(len(a))
	}

	// validateEnviron: no NUL bytes; each entry must contain '=' with no '=' in the key
	// (the substring before the first '='). Values may contain '='.
	for i, e := range environ {
		if bytes.IndexByte(e, 0) >= 0 {
			return nil, fmt.Errorf("environ[%d] contains NUL byte", i)
		}
		eqIdx := bytes.IndexByte(e, '=')
		if eqIdx < 0 {
			return nil, fmt.Errorf("environ[%d] missing '=' separator: %q", i, string(e))
		}
		if bytes.IndexByte(e[:eqIdx], '=') >= 0 {
			return nil, fmt.Errorf("environ[%d] key contains '=': %q", i, string(e))
		}
	}

	var environSize uint32
	for _, e := range environ {
		// Check for overflow before adding
		if uint32(len(e)) > math.MaxUint32-environSize {
			return nil, fmt.Errorf("environ total size exceeds maximum %d", math.MaxUint32-1)
		}
		environSize += uint32(len(e))
	}

	// Defaults: walltime returns (0, 0), nanotime returns 0, nanosleep/osyield are no-ops
	wt := func() (sec int64, nsec int32) { return 0, 0 }
	var wtRes ClockResolution
	nt := func() int64 { return 0 }
	var ntRes ClockResolution
	ns := func(ns int64) {}
	osy := func() {}

	// Get randSource from config, create deterministic reader if nil
	randSource := config.RandSource()
	if randSource == nil {
		// Create a deterministic reader (bytes.Reader with fixed content)
		randSource = bytes.NewReader([]byte("wasm2go-wasi-host-deterministic-seed"))
	}

	// Get FS config and initialize FS context
	fsList, guestPaths := config.FSConfig()
	fsc, err := InitFSContext(
		config.Stdin(),
		config.Stdout(),
		config.Stderr(),
		fsList,
		guestPaths,
	)
	if err != nil {
		return nil, err
	}

	return &Context{
		args:        args,
		environ:     environ,
		argsSize:    argsSize,
		environSize: environSize,
		walltime:    wt,
		walltimeRes: wtRes,
		nanotime:    nt,
		nanotimeRes: ntRes,
		nanosleep:   ns,
		osyield:     osy,
		fsc:         fsc,
		randSource:  randSource,
	}, nil
}

// Args returns the argument list passed to the guest.
func (c *Context) Args() [][]byte { return c.args }

// Environ returns the environment variable list passed to the guest.
func (c *Context) Environ() [][]byte { return c.environ }

// Walltime returns the current time in seconds and nanoseconds.
func (c *Context) Walltime() (sec int64, nsec int32) {
	return c.walltime()
}

// Nanotime returns the current monotonic time in nanoseconds.
func (c *Context) Nanotime() int64 {
	return c.nanotime()
}

// Nanosleep sleeps for the given number of nanoseconds.
func (c *Context) Nanosleep(ns int64) error {
	c.nanosleep(ns)
	return nil
}

// Osyield yields the processor.
func (c *Context) Osyield() error {
	c.osyield()
	return nil
}

// RandSource returns a reader for random data.
func (c *Context) RandSource() io.Reader {
	return c.randSource
}

// FSContext returns the filesystem context.
func (c *Context) FSContext() *FSContext {
	return c.fsc
}
