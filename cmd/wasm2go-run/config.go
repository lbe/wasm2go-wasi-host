package main

import (
	"errors"
	"io"
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

func parseConfig(args []string, stdout io.Writer) (Config, error) {
	return Config{}, nil
}
