# wasm2go-wasi-host

[![Go Reference](https://pkg.go.dev/badge/github.com/lbe/wasm2go-wasi-host.svg)](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.26.3-blue.svg)](https://go.dev/dl/)
[![Go Report Card](https://goreportcard.com/badge/github.com/lbe/wasm2go-wasi-host)](https://goreportcard.com/report/github.com/lbe/wasm2go-wasi-host)
[![CI](https://github.com/lbe/wasm2go-wasi-host/actions/workflows/ci.yml/badge.svg)](https://github.com/lbe/wasm2go-wasi-host/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lbe/wasm2go-wasi-host)](https://github.com/lbe/wasm2go-wasi-host/releases)

> [!WARNING]
> **Status: Beta.** This project is in mid-development. The WASI Preview1 API is stable. At this time, I do not anticipate any breaking changes. Use with caution in production environments.
>
> The current work on this project is porting existing Go packages that use the wazero wasip1 to this package. The following have been ported:
> - [go-wasi](https://github.com/lbe/go-wasi)
> - go-wasm
> - [resvg-go](https://github.com/lbe/resvg-go)
> - [qjs](https://github.com/lbe/qjs)
> - [go-graphviz](https://github.com/lbe/go-graphviz)

A WASI snapshot-preview1 host for Go code transpiled by [wasm2go](https://github.com/ncruces/wasm2go).

The host implements the [WASI preview 1](https://github.com/WebAssembly/WASI/blob/wasi-0.1/preview1/docs.md) specification for wasm2go-generated packages. It implements all 40 WASI Preview1 functions plus the `env.call_host_function` stub that some modules import.

The guest sees only preopened directories. Each preopen is one of two kinds: read-only fs.FS preopens, such as embedded assets, or writable host directory mounts. Each preopen has one backend. To give a guest both embedded and host data, configure separate preopens.

This repository also ships [`wasm2go-run`](./cmd/wasm2go-run/README.md). The [WebAssembly/wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite) wasm2go adapter uses it to run tests, through the `wasi-testsuite` git submodule. All 72 `wasm32-wasip1` tests in that inventory pass with this host.

For higher performance, stronger sandboxing, or other WASI versions, use a runtime such as Wasmtime or wazero. See [ARCHITECTURE.md](./ARCHITECTURE.md) for the design.

## Features

- Implements all 40 snapshot-preview1 imports. The wasi-testsuite inventory checks them.
- Reads and writes guest linear memory through a callback. No runtime API sits between guest and host.
- Supports several preopens per guest. Each one is a read-only `fs.FS` or a writable host directory, backed by `internal/wasi/sysfs`.
- Depends only on the Go standard library and `golang.org/x/sys`.
- Includes the [`wasm2go-run`](./cmd/wasm2go-run/README.md) CLI. It transpiles, compiles and runs a `.wasm` file in one step, and caches transpiled output.

## Supported platforms

The host and `wasm2go-run` run on Linux, macOS, Windows and FreeBSD. CI runs the unit tests and all 72 wasi-testsuite `wasm32-wasip1` cases on each of these:

| OS | Tested in CI |
|----|--------------|
| Linux | amd64, arm64 |
| macOS | arm64 |
| Windows | amd64 |
| FreeBSD | 15.1 on amd64, in a QEMU virtual machine |

CI jobs for Linux on riscv64, s390x and ppc64le exist. They run only when the repository variable `CI_LINUX_OTHER_ARCH` is `true`.

### Limitations on every OS

- The guest always receives Linux errno values, on every host OS.
- `sock_accept`, `sock_recv` and `sock_send` return `ENOSYS`. `sock_shutdown` returns `ENOTSOCK` for every open fd.
- `path_rename` between two preopens on different host filesystems fails with `EXDEV`. The host does not copy files across filesystems.
- `poll_oneoff` reports stdin ready for reading at once, even when no input is waiting.

### Linux and macOS

No limitations beyond those listed for every OS.

### Windows

- A host directory preopen of `/` maps to the root of the current drive, for example `C:\`. The guest cannot reach other drives through it. Preopen a directory on another drive to give the guest access to it.
- The host rejects guest paths that contain `\` or `:`.
- `path_symlink` needs Administrator rights or Windows Developer Mode. Without them it fails.
- NTFS updates access times itself. An access time set by `fd_filestat_set_times` or `path_filestat_set_times` can change when the file is read later.
- `poll_oneoff` on a file opened from a host preopen:
  - An `fd_write` subscription returns its event with `ENOTSUP`.
  - The host checks a pipe for data every 100 ms, so a read event on a pipe can arrive up to 100 ms late.
- In `wasm2go-run`, a host path with a drive letter contains `:`. Separate the host and guest paths with `::`, for example `-dir C:\data::/data`.

Unknown behavior on Windows:

- `path_rename` of a directory over an empty directory that sits in a different preopen. The Linux and macOS code removes the empty target first. The Windows code does not, and no test covers this case.
- `path_rename` of a directory onto a path inside a non-empty directory, for example `target/file`. Linux, macOS and FreeBSD return `ENOTDIR`. The Windows test is skipped.
- `path_rename` of a directory over a non-empty directory, or of a file over a directory. The Windows tests check only that the call fails, not which errno it returns.
- `path_open` of a directory with the `FD_WRITE` right. Linux, macOS and FreeBSD return `EISDIR`. The Windows test does not check the result.
- `path_open` of a file whose host permissions deny the host process access. Linux and macOS return `EACCES`. The Windows test is skipped.

### FreeBSD

Unknown behavior on FreeBSD:

- `path_rename` of a directory over an empty directory that sits in a different preopen. This case uses the same code as Windows and no test covers it.

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

### Porting from wazero

To move an existing wazero WASI embedding to a wasm2go guest, see [docs/porting-guidance-from-wazero.md](./docs/porting-guidance-from-wazero.md).

## File system support

Configure mounts on [`NewModuleConfig`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#NewModuleConfig):

- `WithReadOnlyFS(guestPath, root)` adds a read-only `fs.FS` preopen through `internal/wasi/sysfs.AdaptFS`. Its WASI rights exclude writes and path mutations.
- `WithHostDirectoryPreopen(guestPath, hostPath)` adds a writable host directory preopen through `sysfs.DirFS`. Reads and mutations go through the WASI VFS, confined to the host root.
- `WithFS(fsConfig)` takes an [`FSConfig`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#FSConfig) built with `WithDirMount`, `WithReadOnlyDirMount` and `WithFSMount`.

Use separate preopens when the guest needs both writable host storage and embedded read-only files, for example `/tmp` on disk and `/assets` from `embed.FS`. A writable mount does not fall back to embedded `fs.FS` content.

`WithOsyield` sets the function that `sched_yield` calls. The default is `runtime.Gosched`.

Path matching, lexical bounds and symlink rules are in [docs/path-confinement.md](./docs/path-confinement.md).

## Concurrency

[`State`](https://pkg.go.dev/github.com/lbe/wasm2go-wasi-host#State) is not safe for concurrent use. Use one `State` per wasm2go module instance, from one goroutine.

## Development

### Prerequisites

- Go 1.26.3 or later
- `wasm2go` on `PATH`
- Python 3 for the wasi-testsuite and quality gate tests
- golangci-lint for linting

### Getting started

Clone the repository and initialize its submodules:

```bash
git clone https://github.com/lbe/wasm2go-wasi-host.git
cd wasm2go-wasi-host
git submodule update --init --recursive
```

### Testing

[TESTING.md](./TESTING.md) describes the test tiers, the unit and e2e policy, and the `e2e_cases.json` manifest.

The `e2e` build tag splits the tests into two tiers:

| Command | What runs | Typical time |
|---------|-----------|--------------|
| `go test ./...` or `make test` | Unit and in-process tests, which have no build tag | 5 s |
| `go test -tags=e2e ./...` or `make test-e2e` | The unit tests plus all 72 wasi-testsuite cases in `e2e_*_test.go`, run as parallel subprocesses | 15 to 25 s |

`e2e_inventory_test.go` has the `e2e` build tag and loads `testdata/e2e_cases.json`. CI runs `go test -tags=e2e ./...`. `.vscode/settings.json` sets the gopls `buildFlags` to `["-tags=e2e"]`, and `.golangci.yml` sets the same tag, so editors and linters analyze the e2e files.

```bash
make test           # unit tests
make test-e2e       # full suite, as CI runs it
make test-race      # unit tests with -race
make test-race-e2e  # full suite with -race
```

Other Makefile targets: `build`, `lint`, `format`, `cover` for unit coverage, and `cover-e2e` for full coverage.

### Quality gate

The quality gate needs a clean git tree. It skips itself when the tree is dirty, so `go test -tags=e2e ./...` still passes during development.

```bash
go test -tags=e2e -v -run TestQualityGate
```

It runs `gofmt`, builds `wasm2go-run`, and removes build artifacts. CI also runs lint and the wasi-testsuite adapter's pytest. `./scripts/phase_guard.sh` runs the same checks from the shell.

### Full Preview1 compliance

```bash
go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
./scripts/e2e-wasip1.sh
```

The script sets `WASM2GO_RUN` to `./bin/wasm2go-run` and `WASM2GO_WASIHOST_PATH` to the repository root unless they are already set.

To run the testsuite directly:

```bash
go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
cd wasi-testsuite
WASM2GO_RUN="$PWD/../bin/wasm2go-run" WASM2GO_WASIHOST_PATH="$PWD/.." python3 ./run-tests -r adapters/wasm2go.py
```

If `WASM2GO_RUN` is unset, the adapter uses `wasm2go-run` from `PATH`.

Format code before committing with `gofmt -w .` or `make format`.

## License

This project is licensed under the [MIT License](./LICENSE).

[`internal/wasi/`](./internal/wasi/) and [`internal/wasi/sysfs/`](./internal/wasi/sysfs/) include code adapted from [wazero](https://github.com/tetratelabs/wazero), Copyright 2020-2023 The wazero Authors, licensed under the [Apache License 2.0](./licenses/APACHE-2.0.txt). See [NOTICE](./NOTICE) for attribution, and [docs/design-improvements-2.md](./docs/design-improvements-2.md) for the list of adapted code.
