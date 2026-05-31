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

Filesystem access is capability-oriented: the guest sees only **preopened directories**—**read-only fs.FS preopens** (embedded assets) or writable host directory mounts. Each preopen uses a single backend; combine embedded and host data with **separate preopens**, not a hybrid overlay on one mount.

This repository also ships [`wasm2go-run`](./cmd/wasm2go-run/README.md), the runner used by the [WebAssembly/wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite) wasm2go adapter (via the `wasi-testsuite` git submodule). All 72 `wasm32-wasip1` tests in that inventory pass with this host.

For production-grade performance, advanced sandboxing, or broader runtime features, consider mature runtimes such as Wasmtime or wazero. See [ARCHITECTURE.md](./ARCHITECTURE.md) for design detail.

## Features

- **Full WASI Preview1**: All 40 snapshot-preview1 imports, validated against wasi-testsuite.
- **wasm2go-native**: Direct guest linear memory via a callback—no external runtime API between guest and host.
- **Capability-oriented VFS**: Multiple preopens; read-only `fs.FS` and writable host directories (`internal/wasi/sysfs`), one backend per mount.
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
    cfg := wasihost.NewModuleConfig().
        WithArgs("app-name", "arg1").
        WithEnv("PATH", "/bin").
        WithHostDirectoryPreopen("/", "./host-data").
        WithStdout(os.Stdout).
        WithStderr(os.Stderr)
    state := wasihost.New(
        func() []byte { return *mod.Xmemory().Slice() },
        cfg,
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

Configure mounts on [`NewModuleConfig`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#NewModuleConfig):

- **`WithReadOnlyFS(guestPath, root)`**: Read-only `fs.FS` preopen (`internal/wasi/sysfs.AdaptFS`); WASI rights exclude writes and path mutations.
- **`WithHostDirectoryPreopen(guestPath, hostPath)`**: Writable host directory preopen (`sysfs.DirFS`); reads and mutations go through the WASI VFS with path confinement on the host root.
- **`WithFS(fsConfig)`**: Advanced layout via [`FSConfig`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#FSConfig) (`WithDirMount`, `WithReadOnlyDirMount`, `WithFSMount`).

Use **separate preopens** when the guest needs both writable host storage and embedded read-only files (for example `/tmp` on disk and `/assets` from `embed.FS`). Writable mounts do not fall back to embedded `fs.FS` content.

Optional **`WithOsyield`** configures the yield function used by `sched_yield` (default `runtime.Gosched`).

Path matching, lexical bounds, and symlink rules: [docs/path-confinement.md](./docs/path-confinement.md).

## Concurrency and safety

- **Single-owner**: [`State`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#State) is not safe for concurrent use. One `State` per wasm2go module instance, one goroutine.

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

### Testing

See **[TESTING.md](./TESTING.md)** for tier layout, unit-vs-e2e policy, and the `e2e_cases.json` manifest.

Tests split into two tiers via the **`e2e` build tag**:

| Command | What runs | Typical time |
|---------|-----------|--------------|
| `go test ./...` or `make test` | Unit / in-process tests (`*_test.go` without tag) | ~5s |
| `go test -tags=e2e ./...` or `make test-e2e` | Everything above **plus** all 72 `e2e_*_test.go` wasi-testsuite cases (parallel subprocesses) | ~15–25s |

Integration tests load `testdata/e2e_cases.json` via `e2e_inventory_test.go` (tagged `//go:build e2e`). CI runs `go test -tags=e2e ./...`. Editor support: `.vscode/settings.json` sets gopls `buildFlags: ["-tags=e2e"]`; `.golangci.yml` includes the same tag so linters analyze e2e files.

```bash
make test       # daily dev loop
make test-e2e   # before push / matches CI
make test-race  # unit tests with -race
make test-race-e2e  # full suite with -race
```

Other **Makefile** targets: `build`, `lint`, `format`, `cover` (unit coverage), `cover-e2e` (full coverage).

### Quality gate

Requires a **clean git tree** (skipped automatically when the tree is dirty, so `go test -tags=e2e ./...` works mid-branch):

```bash
go test -tags=e2e -v -run TestQualityGate
```

Runs `gofmt`, a `wasm2go-run` build, and artifact cleanup. Lint and wasi-testsuite adapter pytest run in CI (`make lint`, workflow). Equivalent shell workflow: `./scripts/phase_guard.sh`.

### Full Preview1 compliance

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

Format before commit: `gofmt -w .` or `make format`.

## License

This project is licensed under the [MIT License](./LICENSE).

[`internal/wasi/`](./internal/wasi/) and [`internal/wasi/sysfs/`](./internal/wasi/sysfs/) include code adapted from [wazero](https://github.com/tetratelabs/wazero) (Copyright 2020–2023 The wazero Authors), licensed under the [Apache License 2.0](./licenses/APACHE-2.0.txt). See [NOTICE](./NOTICE) and [docs/design-improvements-2.md](./docs/design-improvements-2.md) for attribution and the portable code inventory.
