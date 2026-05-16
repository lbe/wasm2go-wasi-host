# Bug: Duplicate interface method generated for wasm modules that import the same host function twice

## Summary

When a WebAssembly module imports the same host function more than once (which is valid per the
WebAssembly specification), `wasm2go` emits duplicate method signatures in the generated Go
interface type, producing invalid Go code that fails to compile.

## Steps to Reproduce

```bash
wasm2go wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/fd_write-to-stdout.wasm
```

## Generated Output (invalid Go)

```go
type Xwasi_snapshot_preview1 = interface {
    Xfd_write(v0, v1, v2, v3 int32) int32   // ← first occurrence
    Xproc_exit(v0 int32)
    Xfd_write(v0, v1, v2, v3 int32) int32   // ← duplicate — invalid Go
}
```

Attempting to compile this output fails:

```
./module.go:72:2: duplicate method Xfd_write
        module.go:70:2: other declaration of method Xfd_write
```

## Root Cause

The `fd_write-to-stdout.wasm` binary contains two import table entries for `wasi_snapshot_preview1::fd_write`
(functions 0 and 2 in the import section). The WASM specification permits a module to import the
same function under different import indices. `wasm2go` generates one interface method per import
entry without deduplicating, producing repeated method signatures.

The generated `fn0` and `fn2` both delegate to `m._wasi_snapshot_preview1.Xfd_write(...)`,
which is semantically correct — both import slots resolve to the same host function. The interface
definition, however, must only declare the method once.

## Expected Behaviour

`wasm2go` should deduplicate method signatures within each generated interface type so that a host
function imported multiple times appears only once in the interface. All internal `fn*` dispatch
functions that reference that import slot continue to call the single deduplicated method normally.

## Workaround

As a temporary workaround, callers can post-process the `wasm2go` output and strip duplicate
method declarations from each interface block before passing the source to the Go compiler.
This workaround should be removed once this issue is fixed upstream in `wasm2go`.

## Environment

- `wasm2go` version: `wasm2go version` output
- Go version: go1.26.3 darwin/arm64
- Affected fixture: `wasi-testsuite/tests/assemblyscript/testsuite/wasm32-wasip1/fd_write-to-stdout.wasm`
