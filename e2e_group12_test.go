package wasihost_test

import "testing"

// TestGroup12WasiTestsuiteInventoryPasses verifies that the Group 12
// wasi-testsuite tests (sock_shutdown-not_sock and sock_shutdown-invalid_fd)
// pass end-to-end via the built wasm2go-run binary, and that smoke tests do
// not regress.
//
// sock_shutdown-not_sock: attempts sock_shutdown on a non-socket fd.
// sock_shutdown-invalid_fd: attempts sock_shutdown on an invalid fd.
//
// Both are expected to return the correct errno rather than trap.
//
// Smoke tests:
// path_open_read_write: combined path_open with rights for read/write.
// stdio: standard I/O file operations.
// sched_yield: scheduling yield system call.
// overwrite_preopen: preopened file descriptor overwrite behavior.
func TestGroup12WasiTestsuiteInventoryPasses(t *testing.T) {
	runWasiTestsuiteCases(t, "c", []wasiTestsuiteCase{
		{name: "sock_shutdown-not_sock", wantPass: true, stdioOnly: true},
		{name: "sock_shutdown-invalid_fd", wantPass: true, stdioOnly: true},
	})
	runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
		// Smoke tests (must not regress).
		{name: "path_open_read_write", wantPass: true},
		{name: "stdio", wantPass: true},
		{name: "sched_yield", wantPass: true},
		{name: "overwrite_preopen", wantPass: true},
	})
}
