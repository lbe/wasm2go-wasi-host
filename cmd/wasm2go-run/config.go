package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

var ErrVersionRequested = errors.New("version requested")

type DirMount struct {
	Host  string
	Guest string
}

// Config holds the runtime configuration for wasm2go-run.
type Config struct {
	Env      []string
	Dirs     []DirMount
	WasmPath string
	WasmArgs []string
	Cache    string
}

type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type dirSlice []DirMount

func (d *dirSlice) String() string {
	return fmt.Sprintf("%v", *d)
}

func (d *dirSlice) Set(value string) error {
	parts := strings.Split(value, "::")
	if len(parts) != 2 {
		parts = strings.Split(value, ":")
	}
	if len(parts) != 2 {
		return fmt.Errorf("invalid directory mount: %s", value)
	}
	*d = append(*d, DirMount{Host: parts[0], Guest: parts[1]})
	return nil
}

func parseConfig(args []string, stdout io.Writer) (Config, error) {
	var env stringSlice
	var dirs dirSlice
	var cacheMode string
	var version bool

	fs := flag.NewFlagSet("wasm2go-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&env, "env", "Environment variables")
	fs.Var(&dirs, "dir", "Directory mounts")
	fs.StringVar(&cacheMode, "cache", "", "Cache mode")
	fs.BoolVar(&version, "version", false, "Show version")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if version {
		fmt.Fprintf(stdout, "wasm2go-run %s\n", versionString())
		return Config{}, ErrVersionRequested
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return Config{}, errors.New("missing wasm path")
	}

	wasmArgs := remaining[1:]
	if len(wasmArgs) == 0 {
		wasmArgs = nil
	}
	cfg := Config{
		WasmPath: remaining[0],
		WasmArgs: wasmArgs,
		Env:      env,
		Dirs:     dirs,
		Cache:    cacheMode,
	}
	return cfg, nil
}
