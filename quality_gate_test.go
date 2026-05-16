package wasihost_test

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestSourceCodeIsFormatted(t *testing.T) {
	cmd := exec.Command("gofmt", "-l", ".")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		t.Fatalf("gofmt failed: %v", err)
	}

	if out.Len() > 0 {
		t.Errorf("The following files are not formatted with gofmt:\n%s", out.String())
	}
}
