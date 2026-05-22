package wasihost_test

import "testing"

// TestGroup3WasiTestsuiteInventoryPasses verifies that the Group 3
// wasi-testsuite test (interesting_paths) passes end-to-end via the built
// wasm2go-run binary, and that smoke tests do not regress.
func TestGroup3WasiTestsuiteInventoryPasses(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
		// Group 3 inventory (must pass).
		{name: "interesting_paths", wantPass: true},
		// Smoke tests (must not regress).
		{name: "path_open_read_write", wantPass: true},
		{name: "stdio", wantPass: true},
		{name: "sched_yield", wantPass: true},
		{name: "nofollow_errors", wantPass: true},
		{name: "path_open_missing", wantPass: true},
		{name: "overwrite_preopen", wantPass: true},
	})
}
