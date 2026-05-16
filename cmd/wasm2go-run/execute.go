package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

func execute(binaryPath, buildDir string, stdout, stderr io.Writer) (int, error) {
	defer os.RemoveAll(buildDir)

	cmd := exec.Command(binaryPath)
	cmd.Stdin = os.Stdin
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
