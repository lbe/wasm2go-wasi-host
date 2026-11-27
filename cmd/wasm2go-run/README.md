# wasm2go-run

> [!WARNING]
> **Status: Alpha.** Flags and behavior may change.

`wasm2go-run` transpiles a WASI Preview1 `.wasm` module with [wasm2go](https://github.com/ncruces/wasm2go), generates a `main` wrapper linked to [`wasihost`](../../), builds a native binary, and runs it. The [WebAssembly/wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite) wasm2go adapter uses it as its runner. It runs on Linux, macOS, Windows and FreeBSD.

## Installation

```bash
go install github.com/lbe/wasm2go-wasi-host/cmd/wasm2go-run@latest
```

## Usage

```bash
wasm2go-run [options] <wasm-file> [-- <guest-args>]
```

### Options

- `-dir <host-dir>::<guest-dir>` or `-dir <host-dir>:<guest-dir>` adds a writable host directory preopen. Both paths are required. Use `::` when the host path contains `:`, such as a Windows path with a drive letter: `-dir C:\data::/data`. Repeatable.
- `-env <key=value>` adds a guest environment entry. Repeatable.
- `-cache off` turns off the tier-1 transpile cache, which is on by default.
- `-version` prints the version and exits with status 0.

### Environment variables

The tier-1 cache stores the transpiled `module.go` only. Each run still compiles in a new temporary directory.

| Variable | Effect |
|----------|--------|
| `WASM2GO_RUN_CACHE` | `0`, `false` or `off` turns off the cache |
| `WASM2GO_RUN_CACHE_DIR` | Sets the cache root |
| `WASM2GO_WASIHOST_PATH` | Sets the repository root for the `replace` directive in the generated `go.mod`, in builds that are not releases |

Without `WASM2GO_RUN_CACHE_DIR`, the cache root is the first of these that applies:

1. `$XDG_CACHE_HOME/wasm2go-run`, when `XDG_CACHE_HOME` is set
2. `$HOME/.cache/wasm2go-run`, when `HOME` is set
3. `wasm2go-run` under `os.UserCacheDir()`, for example `%LocalAppData%\wasm2go-run` on Windows
4. `wasm2go-run` under `os.TempDir()`

### Examples

```bash
wasm2go-run -dir ./data:/data -env KEY=VALUE my-app.wasm -- --app-arg1
```

## How it works

1. Transpile: run `wasm2go` on the `.wasm` file. With the cache on, reuse the cached `module.go` when the WASM bytes and toolchain identity match.
2. Post-process: remove duplicate interface methods from the generated Go. wasm2go emits them for repeated imports.
3. Compile: write `module/module.go`, the generated `main.go` and `go.mod` to a temporary directory, then run `go mod tidy` and `go build` there.
4. Execute: run the binary with the process stdio. The guest exit code becomes the process exit code.
5. Clean up: remove the temporary build directory.

## Development

### Prerequisites

- Go 1.26.3 or later
- `wasm2go` on `PATH`
- The `wasi-testsuite` submodule, initialized, for the adapter and e2e tests

```bash
git submodule update --init --recursive
```

### Running tests

```bash
# wasm2go-run package only, 5 s
go test ./cmd/wasm2go-run/...

# All unit tests in the repository, 5 s
go test ./...

# Unit tests plus wasi-testsuite integration, from the repository root
go test -tags=e2e ./...
```

`runner_test.go` compiles selected wasi-testsuite WASM fixtures in-process. The submodule inventory e2e tests are in the root package as `e2e_*_test.go` and need `-tags=e2e`. On Windows, tests that need a POSIX shell and Unix executables are skipped.

### Full Preview1 run

From the repository root:

```bash
go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
./scripts/e2e-wasip1.sh
```

See the root [README.md](../../README.md) for the `WASM2GO_RUN` and `WASM2GO_WASIHOST_PATH` defaults.

### Build

```bash
go build -o bin/wasm2go-run .
```
