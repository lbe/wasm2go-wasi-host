//go:build !linux

package wasi

import (
	"os"
	"os/exec"
	"testing"
)

func TestNewStat_tMapsLinuxSyscallStatTimestampsToEpochNanos(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GOOS=linux go build: %v\n%s", err, out)
	}
}
