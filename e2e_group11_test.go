package wasihost_test

import "testing"

// TestGroup11WasiTestsuiteInventoryPasses verifies that the Group 11
// wasi-testsuite test (poll_oneoff_stdio) passes end-to-end via the built
// wasm2go-run binary, and that smoke tests do not regress.
//
// poll_oneoff_stdio covers poll_oneoff with stdio subscriptions, confirming
// that one-shot polling on standard I/O file descriptors is supported.
//
// Smoke tests:
// path_open_read_write: combined path_open with rights for read/write.
// stdio: standard I/O file operations.
// sched_yield: scheduling yield system call.
// overwrite_preopen: preopened file descriptor overwrite behavior.
func TestGroup11WasiTestsuiteInventoryPasses(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
		// Group 11 inventory (must pass).
		{name: "poll_oneoff_stdio", wantPass: true, stdioOnly: true},
		// Smoke tests (must not regress).
		{name: "path_open_read_write", wantPass: true},
		{name: "stdio", wantPass: true},
		{name: "sched_yield", wantPass: true},
		{name: "overwrite_preopen", wantPass: true},
	})
}
