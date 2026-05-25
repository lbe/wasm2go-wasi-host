package wasihost_test

import "testing"

// TestGroup8WasiTestsuiteSeekAllocateLseekPwrite verifies that the Group 8
// wasi-testsuite fixes for fd_read, fd_write, fd_seek, and fd_allocate pass
// end-to-end via the built wasm2go-run binary.
//
// These cover five WASM binaries that were previously failing:
//   - file_seek_tell.wasm — was returning EINVAL on seek -50 WHENCE_CUR
//   - fd_advise.wasm — was returning size=100 instead of 200 after allocate
//   - file_allocate.wasm — was returning size=0 instead of 100 after allocate
//   - lseek.wasm — lseek SEEK_CUR returned 0 instead of 4
//   - pwrite-with-append.wasm — lseek SEEK_CUR returned 0 instead of 4
func TestGroup8WasiTestsuiteSeekAllocateLseekPwrite(t *testing.T) {
	runWasiTestsuiteCases(t, "rust", []wasiTestsuiteCase{
		{name: "file_seek_tell", wantPass: true},
		{name: "fd_advise", wantPass: true},
		{name: "file_allocate", wantPass: true},
	})
	runWasiTestsuiteCases(t, "c", []wasiTestsuiteCase{
		{name: "lseek", wantPass: true},
		{name: "pwrite-with-append", wantPass: true},
	})
}
