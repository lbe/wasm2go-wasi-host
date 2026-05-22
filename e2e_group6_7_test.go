package wasihost_test

import "testing"

// TestGroup6_7WasiTestsuiteInventoryPasses verifies that the Groups 6–7
// wasi-testsuite inventory tests (path_filestat, fd_filestat_set,
// truncation_rights, path_open_preopen) pass end-to-end via the built
// wasm2go-run binary, that Group 6a (fstflags_validate) does not regress, and
// that smoke tests do not regress.
//
// stat-dev-ino lives in the C testsuite directory.
func TestGroup6_7WasiTestsuiteInventoryPasses(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
		// Group 6–7 inventory (must pass).
		{name: "path_filestat", wantPass: true},
		{name: "fd_filestat_set", wantPass: true},
		{name: "truncation_rights", wantPass: true},
		{name: "path_open_preopen", wantPass: true},
		// Group 6a regression (must pass).
		{name: "fstflags_validate", wantPass: true},
		// Smoke tests (must not regress).
		{name: "path_open_read_write", wantPass: true},
		{name: "stdio", wantPass: true},
		{name: "sched_yield", wantPass: true},
		{name: "nofollow_errors", wantPass: true},
		{name: "path_open_missing", wantPass: true},
		{name: "overwrite_preopen", wantPass: true},
	})
	runWasiTestsuiteCases(t, "c", []wasiTestsuiteCase{
		// C testsuite inventory (must pass).
		{name: "stat-dev-ino", wantPass: true},
	})
}
