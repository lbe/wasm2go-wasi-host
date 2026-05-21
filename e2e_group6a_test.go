package wasihost_test

import "testing"

// TestGroup6aWasiTestsuiteInventoryPasses verifies that the Group 6a
// wasi-testsuite test (fstflags_validate) passes end-to-end via the built
// wasm2go-run binary, and that smoke tests do not regress.
func TestGroup6aWasiTestsuiteInventoryPasses(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
		// Group 6a inventory (must pass).
		{name: "fstflags_validate", wantPass: true},
		// Smoke tests (must not regress).
		{name: "path_open_read_write", wantPass: true},
		{name: "stdio", wantPass: true},
		{name: "path_open_missing", wantPass: true},
		{name: "overwrite_preopen", wantPass: true},
	})
}
