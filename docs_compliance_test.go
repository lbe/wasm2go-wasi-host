package wasihost_test

import (
	"os"
	"strings"
	"testing"
)

func TestDocsCompliance(t *testing.T) {
	// 1. Check README.md for WASI-style filesystem access description
	t.Run("README mentions preopens and read-only fs.FS", func(t *testing.T) {
		content, err := os.ReadFile("README.md")
		if err != nil {
			t.Fatal(err)
		}
		s := string(content)
		if !strings.Contains(s, "preopened directories") && !strings.Contains(s, "Preopened Directory") {
			t.Error("README.md does not mention 'preopened directories'")
		}
		if !strings.Contains(s, "read-only fs.FS preopens") && !strings.Contains(s, "read-only fs.FS") {
			t.Error("README.md does not mention 'read-only fs.FS preopens'")
		}
	})

	// 2. Check ARCHITECTURE.md for WASI-style filesystem access description
	t.Run("ARCHITECTURE mentions preopened directories and read-only fs.FS", func(t *testing.T) {
		content, err := os.ReadFile("ARCHITECTURE.md")
		if err != nil {
			t.Fatal(err)
		}
		s := string(content)
		if !strings.Contains(s, "preopened directories") && !strings.Contains(s, "Preopened Directory") {
			t.Error("ARCHITECTURE.md does not mention 'preopened directories'")
		}
		if !strings.Contains(s, "read-only fs.FS preopens") && !strings.Contains(s, "read-only fs.FS") {
			t.Error("ARCHITECTURE.md does not mention 'read-only fs.FS preopens'")
		}
	})

	// 3. Check runner test for a mutation test
	t.Run("E2E script or runner test includes mutation wasi-testsuite binary", func(t *testing.T) {
		// We want to ensure at least one test from wasi-testsuite that performs
		// create/write/remove is part of our verified compliance.

		// Check runner_test.go for path_open_read_write.wasm (Rust) or similar
		runnerTest, err := os.ReadFile("cmd/wasm2go-run/runner_test.go")
		if err != nil {
			t.Fatal(err)
		}

		mutationTests := []string{
			"file_pread_pwrite.wasm",
			"path_rename.wasm",
			"path_link.wasm",
			"path_symlink.wasm",
			"pwrite-with-access.wasm",
			"path_open_read_write.wasm",
		}

		found := false
		for _, test := range mutationTests {
			if strings.Contains(string(runnerTest), test) {
				found = true
				break
			}
		}

		if !found {
			t.Error("No known mutation tests from wasi-testsuite found in cmd/wasm2go-run/runner_test.go")
		}
	})
}
