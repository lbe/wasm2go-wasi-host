package wasihost_test

import "testing"

// TestWasiTestsuiteCCase verifies that the shared testsuite helper
// can run a C wasi-testsuite wasm binary and report pass/fail correctly.
func TestWasiTestsuiteCCase(t *testing.T) {
	runWasiTestsuiteCases(t, "c", []wasiTestsuiteCase{
		{name: "fdopendir-with-access", wantPass: true},
	})
}
