package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

// execute runs the compiled guest binary and removes buildDir when finished.
// The returned exit code is the guest process status; non-exit errors are
// returned as the second value.
func execute(binaryPath, buildDir string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	defer os.RemoveAll(buildDir)

	cmd := exec.Command(binaryPath)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}

	return 0, nil
}
