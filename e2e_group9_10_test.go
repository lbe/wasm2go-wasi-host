package wasihost_test

import (
	"runtime"
	"testing"
)

// TestGroup9And10WasiTestsuite verifies that the Group 9-10
// wasi-testsuite tests (path_link, path_rename, renumber) pass end-to-end
// via the built wasm2go-run binary, and that smoke tests and Group 5
// regression tests do not regress.
//
// path_link covers test_path_link (create link, verify link, error cases).
// path_rename covers test_path_rename (rename file, directory, error cases).
// renumber covers test_renumber (duplicate file descriptors, renumbering).
//
// symlink_create (Group 5) covers test_symlink_create (create symlink, verify).
//
// Smoke tests:
// path_open_read_write: combined path_open with rights for read/write.
// stdio: standard I/O file operations.
// sched_yield: scheduling yield system call.
// nofollow_errors: path-based error handling with NOFOLLOW.
// path_open_missing: path_open on missing paths should fail.
// overwrite_preopen: preopened file descriptor overwrite behavior.
// file_truncation: file size truncation operations.
func TestGroup9And10WasiTestsuite(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("e2e test requires Unix-like environment")
	}

	// Group 9-10 testsuite cases.
	t.Run("group9_10_testsuite", func(t *testing.T) {
		runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
			{name: "path_link", wantPass: true},
			{name: "path_rename", wantPass: true},
			{name: "renumber", wantPass: true},
		})
	})

	// Group 5 regression: symlink_create must still pass.
	t.Run("group5_regression_symlink_create", func(t *testing.T) {
		runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
			{name: "symlink_create", wantPass: true},
		})
	})

	// Smoke tests (must not regress).
	t.Run("smoke_tests", func(t *testing.T) {
		runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
			{name: "path_open_read_write", wantPass: true},
			{name: "stdio", wantPass: true},
			{name: "sched_yield", wantPass: true},
			{name: "nofollow_errors", wantPass: true},
			{name: "path_open_missing", wantPass: true},
			{name: "overwrite_preopen", wantPass: true},
			{name: "file_truncation", wantPass: true},
		})
	})
}
