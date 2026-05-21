package wasihost_test

import (
	"runtime"
	"testing"
)

// TestGroup1WasiTestsuiteFdReaddirAndFdopendir verifies that the Group 1
// wasi-testsuite tests (fd_readdir, fdopendir-with-access) pass end-to-end
// via the built wasm2go-run binary, and that smoke tests do not regress.
//
// fd_readdir covers test_fd_readdir (empty dir, create file, cookie resume)
// and test_fd_readdir_lots (1000 files, cookie walk, 1002 total entries).
// fdopendir-with-access covers fdopendir combined with path_open access checks.
func TestGroup1WasiTestsuiteFdReaddirAndFdopendir(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("e2e test requires Unix-like environment")
	}

	// fd_readdir.wasm lives in the rust testsuite; run it via the shared helper.
	t.Run("fd_readdir", func(t *testing.T) {
		runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
			{name: "fd_readdir", wantPass: true},
		})
	})

	// fdopendir-with-access.wasm lives in the C testsuite directory.
	t.Run("fdopendir_with_access", func(t *testing.T) {
		runWasiTestsuiteCases(t, "c", []wasiTestsuiteCase{
			{name: "fdopendir-with-access", wantPass: true},
		})
	})

	// Smoke tests (must not regress).
	t.Run("smoke_tests", func(t *testing.T) {
		runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
			{name: "path_open_read_write", wantPass: true},
			{name: "stdio", wantPass: true},
			{name: "path_open_missing", wantPass: true},
			{name: "overwrite_preopen", wantPass: true},
		})
	})
}
