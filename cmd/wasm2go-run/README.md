# wasm2go-run

> [!WARNING]
> **Status: Alpha.** This tool is in early development. The command-line interface and internal orchestration logic may change as the project matures.

`wasm2go-run` is a command-line tool that automates the process of transpiling a WebAssembly file to Go using [wasm2go](https://github.com/ncruces/wasm2go), compiling the resulting Go code, and executing it with the `wasihost` WASI preview1 implementation.

It is designed to be used as a runner for the [WebAssembly/wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite).

## Installation

```bash
go install github.com/lbe/wasm2go-wasi-host/cmd/wasm2go-run@latest
```

## Usage

```bash
wasm2go-run [options] <wasm-file> [-- <guest-args>]
```

### Options

- `-dir <host-dir>[:<guest-dir>]`: Mount a host directory into the WASM guest. If guest path is omitted, it defaults to the host path. Multiple `-dir` flags are supported.
- `-env <key=value>`: Set an environment variable for the guest. Multiple `-env` flags are supported.
- `-cache off`: Disable tier-1 transpile caching (enabled by default).
- `-version`: Show version information.

### Environment variables

Tier-1 transpile cache (transpiled `module.go` only; compile still uses a per-run temp directory):

- `WASM2GO_RUN_CACHE`: Set to `0`, `false`, or `off` (case-insensitive) to disable caching.
- `WASM2GO_RUN_CACHE_DIR`: Override the cache directory (default: `$XDG_CACHE_HOME/wasm2go-run` or `$HOME/.cache/wasm2go-run`).

### Examples

Run a WASM binary with mounts and environment variables:

```bash
wasm2go-run -dir ./data:/data -env KEY=VALUE my-app.wasm -- --app-arg1
```

## How it works

1. **Transpile**: It uses `wasm2go` (must be in your PATH) to transpile the `.wasm` file into Go source code. When tier-1 caching is enabled, the transpiled `module.go` is stored under the cache directory (keyed by WASM contents and toolchain identity) so repeat runs can skip calling `wasm2go`.
2. **Compile**: It generates a `main.go` that wires the transpiled module to the `wasihost` implementation and compiles it using `go build` in a temporary directory.
3. **Execute**: It runs the resulting binary with the specified mounts, environment variables, and arguments.
4. **Cleanup**: Temporary files are removed after execution.

## Development

### Prerequisites

- **Go**: 1.26.3 or later.
- **wasm2go**: Required for the transpilation step.

### Testing the Runner

The runner depends on the `wasi-testsuite` submodule being populated. If you haven't already:

```bash
git submodule update --init --recursive
```

You can run the tests for the runner package:

```bash
go test ./...
```

To execute every WASI Preview1 suite during development, build `wasm2go-run` and run the wrapper. The wrapper defaults `WASM2GO_RUN` to this repository's `./bin/wasm2go-run`, defaults `WASM2GO_WASIHOST_PATH` to this repository root, and delegates test discovery to the `wasi-testsuite` submodule's authoritative runner:

```bash
go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
./scripts/e2e-wasip1.sh
```

Equivalent direct invocation from the submodule:

```bash
go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
cd wasi-testsuite
WASM2GO_RUN="$PWD/../bin/wasm2go-run" WASM2GO_WASIHOST_PATH="$PWD/.." python3 ./run-tests -r adapters/wasm2go.py
```

If `WASM2GO_RUN` is unset during direct `wasi-testsuite` use, the adapter uses `wasm2go-run` from `PATH`.

C and Rust Preview1 failures are expected until follow-up compliance work fixes them. This all-Preview1 command becomes part of the mandatory quality gate only after those failures are fixed.

### Building

```bash
go build -o bin/wasm2go-run .
```
