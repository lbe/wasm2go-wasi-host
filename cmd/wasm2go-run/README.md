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
- `-version`: Show version information.

### Examples

Run a WASM binary with mounts and environment variables:
```bash
wasm2go-run -dir ./data:/data -env KEY=VALUE my-app.wasm -- --app-arg1
```

## How it works

1. **Transpile**: It uses `wasm2go` (must be in your PATH) to transpile the `.wasm` file into Go source code in a temporary directory.
2. **Compile**: It generates a `main.go` that wires the transpiled module to the `wasihost` implementation and compiles it using `go build`.
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

To verify the runner against the `wasi-testsuite` AssemblyScript samples, use the provided E2E script from the repository root:
```bash
./scripts/e2e-assemblyscript.sh
```

### Building

```bash
go build -o bin/wasm2go-run .
```
