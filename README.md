# wasm2go-wasi-host

[![Go Reference](https://pkg.go.dev/badge/github.com/lbe/wasm2go-wasi-host.svg)](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.26.3-blue.svg)](https://go.dev/dl/)
[![Go Report Card](https://goreportcard.com/badge/github.com/lbe/wasm2go-wasi-host)](https://goreportcard.com/report/github.com/lbe/wasm2go-wasi-host)
[![CI](https://github.com/lbe/wasm2go-wasi-host/actions/workflows/ci.yml/badge.svg)](https://github.com/lbe/wasm2go-wasi-host/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lbe/wasm2go-wasi-host)](https://github.com/lbe/wasm2go-wasi-host/releases)

> [!WARNING]
> **Status: Alpha.** This project is in early development. While the WASI Preview1 specification is stable, the internal host API and the `wasm2go-run` tool are subject to change. Use with caution in production environments.

A specialized **WASI snapshot-preview1** host for Go, designed to run code transpiled by [wasm2go](https://github.com/ncruces/wasm2go).

The primary goal is a WASI implementation for wasm2go-generated packages that complies with the [WASI preview 1](https://github.com/WebAssembly/WASI/blob/wasi-0.1/preview1/docs.md) specification. The host implements all 40 WASI Preview1 functions plus the `env.call_host_function` stub required by some modules.

Filesystem access is capability-oriented: the guest sees only **preopened directories**—**read-only fs.FS preopens** (embedded assets) or writable host directory mounts.

This repository also ships [`wasm2go-run`](./cmd/wasm2go-run/README.md), the runner used by the [WebAssembly/wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite) wasm2go adapter (via the `wasi-testsuite` git submodule). All 72 `wasm32-wasip1` tests in that inventory pass with this host.

For production-grade performance, advanced sandboxing, or broader runtime features, consider mature runtimes such as Wasmtime or wazero. See [ARCHITECTURE.md](./ARCHITECTURE.md) for design detail.

## Features

- **Full WASI Preview1**: All 40 snapshot-preview1 imports, validated against wasi-testsuite.
- **wasm2go-native**: Direct guest linear memory via a callback—no external runtime API between guest and host.
- **Capability-oriented VFS**: Multiple preopens; read-only `fs.FS` and writable host directories.
- **Stdlib-first**: Go standard library plus `golang.org/x/sys` for a few OS-specific helpers (Linux and Darwin).
- **CLI runner**: [`wasm2go-run`](./cmd/wasm2go-run/README.md) transpiles, compiles, and runs `.wasm` in one step (tier-1 transpile cache included).

## Installation

```bash
go get github.com/lbe/wasm2go-wasi-host
```

## Usage

### Using as a library

```go
import (
    "os"
    wasihost "github.com/lbe/wasm2go-wasi-host"
    generated "github.com/your/generated/package" // module produced by wasm2go
)

func main() {
    var mod *generated.Module
    state := wasihost.New(
        func() []byte { return *mod.Xmemory().Slice() },
        wasihost.WithArgs("app-name", "arg1"),
        wasihost.WithEnv("PATH=/bin"),
        wasihost.WithHostDirectoryPreopen("/", "./host-data"),
        wasihost.WithStdout(os.Stdout),
        wasihost.WithStderr(os.Stderr),
    )

    mod = generated.New(state, state)

    defer func() {
        if r := recover(); r != nil {
            if e, ok := r.(wasihost.ExitError); ok {
                os.Exit(int(e.Code))
            }
            panic(r)
        }
    }()
    mod.X_start()
}
```

Package documentation: [pkg.go.dev](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host).

### Using the CLI runner

See [`cmd/wasm2go-run/README.md`](./cmd/wasm2go-run/README.md).

## File system support

- **`WithReadOnlyFS(guestPath, root)`**: Read-only `fs.FS` preopen; WASI rights exclude writes and path mutations.
- **`WithHostDirectoryPreopen(guestPath, hostPath)`**: Writable host directory preopen (`os.DirFS` for reads, host paths for mutations).

## Concurrency and safety

- **Single-owner**: [`State`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#State) is not safe for concurrent use. One `State` per wasm2go module instance, one goroutine.
- **Owner assertion**: [`WithOwnerAssertion`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#WithOwnerAssertion) panics if the host is called from another goroutine.

## Development

### Prerequisites

- **Go** 1.26.3 or later
- **wasm2go** on `PATH`
- **Python 3** for wasi-testsuite and quality-gate tests
- **golangci-lint** for linting

### Getting started

1. Clone and initialize submodules:

    ```bash
    git clone https://github.com/lbe/wasm2go-wasi-host.git
    cd wasm2go-wasi-host
    git submodule update --init --recursive
    ```

2. **Makefile** targets: `build`, `test`, `test-race`, `lint`, `format`, `cover`.

3. **Quality gate** (clean git tree required):

    ```bash
    go test -v -run TestQualityGate
    ```

4. **Full Preview1 compliance** via the submodule inventory:

    ```bash
    go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
    ./scripts/e2e-wasip1.sh
    ```

    The wrapper defaults `WASM2GO_RUN` to `./bin/wasm2go-run` and `WASM2GO_WASIHOST_PATH` to the repo root. Override either variable to use different paths.

    Direct invocation:

    ```bash
    go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
    cd wasi-testsuite
    WASM2GO_RUN="$PWD/../bin/wasm2go-run" WASM2GO_WASIHOST_PATH="$PWD/.." python3 ./run-tests -r adapters/wasm2go.py
    ```

    If `WASM2GO_RUN` is unset, the adapter uses `wasm2go-run` from `PATH`.

5. Format before commit: `gofmt -w .` or `make format`.

## License

MIT
