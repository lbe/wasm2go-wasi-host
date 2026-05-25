// Command wasm2go-run transpiles a WASI Preview1 WebAssembly module to Go
// with [wasm2go], links it against this repository's wasihost implementation,
// builds a native binary, and runs it.
//
// The tool is the runner expected by the WebAssembly wasi-testsuite
// wasm2go adapter. It supports directory preopens, guest environment
// variables, tier-1 transpile caching, and the same exit semantics as a
// typical WASI CLI (guest exit codes propagate; host errors print to stderr).
//
// See the package README and repository ARCHITECTURE.md for the full pipeline.
package main
