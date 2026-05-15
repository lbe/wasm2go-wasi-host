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

type Config struct {
	Env      []string
	Dirs     []DirMount
	WasmPath string
	WasmArgs []string
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
	var version bool

	fs := flag.NewFlagSet("wasm2go-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&env, "env", "Environment variables")
	fs.Var(&dirs, "dir", "Directory mounts")
	fs.BoolVar(&version, "version", false, "Show version")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if version {
		fmt.Fprintln(stdout, "wasm2go-run version 0.0.1")
		return Config{}, ErrVersionRequested
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return Config{}, errors.New("missing wasm path")
	}

	var cfg Config
	cfg.WasmPath = remaining[0]
	cfg.WasmArgs = remaining[1:]
	if len(env) > 0 {
		cfg.Env = env
	}
	if len(dirs) > 0 {
		cfg.Dirs = dirs
	}
	if len(cfg.WasmArgs) == 0 {
		cfg.WasmArgs = nil
	}
	return cfg, nil
}
