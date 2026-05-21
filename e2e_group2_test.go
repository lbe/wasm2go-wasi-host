package wasihost_test

import "testing"

// TestGroup2WasiTestsuiteInventoryPasses verifies that the Group 2
// wasi-testsuite tests (close_preopen, dir_fd_op_failures, directory_seek,
// path_open_dirfd_not_dir) pass end-to-end via the built wasm2go-run binary,
// and that smoke tests do not regress.
func TestGroup2WasiTestsuiteInventoryPasses(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
		// Group 2 inventory (must pass).
		{name: "close_preopen", wantPass: true},
		{name: "dir_fd_op_failures", wantPass: true},
		{name: "directory_seek", wantPass: true},
		{name: "path_open_dirfd_not_dir", wantPass: true},
		// Smoke tests (must not regress).
		{name: "path_open_read_write", wantPass: true},
		{name: "stdio", wantPass: true},
		{name: "path_open_missing", wantPass: true},
		{name: "overwrite_preopen", wantPass: true},
	})
}
