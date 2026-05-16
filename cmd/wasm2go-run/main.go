package main

import (
	"fmt"
	"os"
)

func main() {
	cfg, err := parseConfig(os.Args[1:], os.Stdout)
	if err != nil {
		if err == ErrVersionRequested {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	buildDir, binaryPath, err := compile(cfg.WasmPath, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	exitCode, err := execute(binaryPath, buildDir, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

