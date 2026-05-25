# wasm2go-run

> [!WARNING]
> **Status: Alpha.** Flags and orchestration may change as the project matures.

`wasm2go-run` transpiles a WASI Preview1 `.wasm` module with [wasm2go](https://github.com/ncruces/wasm2go), generates a small `main` wrapper linked to [`wasihost`](../../), builds a native binary, and runs it. It is the runner expected by the [WebAssembly/wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite) wasm2go adapter.

## Installation

```bash
go install github.com/lbe/wasm2go-wasi-host/cmd/wasm2go-run@latest
```

## Usage

```bash
wasm2go-run [options] <wasm-file> [-- <guest-args>]
```

### Options

- `-dir <host-dir>[:<guest-dir>]`: Writable host directory preopen. Guest path defaults to the host path. Repeatable.
- `-env <key=value>`: Guest environment entry (`KEY=VALUE`). Repeatable.
- `-cache off`: Disable tier-1 transpile caching (enabled by default).
- `-version`: Print version and exit 0.

### Environment variables

Tier-1 cache stores transpiled `module.go` only; each run still compiles in a fresh temp directory.

| Variable | Effect |
|----------|--------|
| `WASM2GO_RUN_CACHE` | `0`, `false`, or `off` disables caching |
| `WASM2GO_RUN_CACHE_DIR` | Override cache root (default: `$XDG_CACHE_HOME/wasm2go-run` or `$HOME/.cache/wasm2go-run`) |
| `WASM2GO_WASIHOST_PATH` | Repo root for `replace` in generated `go.mod` when not a release build |

### Examples

```bash
wasm2go-run -dir ./data:/data -env KEY=VALUE my-app.wasm -- --app-arg1
```

## How it works

1. **Transpile**: Run `wasm2go` on the `.wasm` file. With caching on, reuse cached `module.go` when the WASM bytes and toolchain identity match.
2. **Post-process**: Deduplicate duplicate interface methods in generated Go (wasm2go quirk for repeated imports).
3. **Compile**: Write `module/module.go`, generated `main.go`, and `go.mod`; `go mod tidy` and `go build` in a temp directory.
4. **Execute**: Run the binary with process stdio; guest exit code becomes the process exit code.
5. **Cleanup**: Remove the temp build directory.

## Development

### Prerequisites

- Go 1.26.3+
- `wasm2go` on `PATH`
- Initialized `wasi-testsuite` submodule for adapter/e2e tests

```bash
git submodule update --init --recursive
go test ./cmd/wasm2go-run/...
```

### Full Preview1 run

From the repository root:

```bash
go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
./scripts/e2e-wasip1.sh
```

See the root [README.md](../../README.md) for `WASM2GO_RUN` / `WASM2GO_WASIHOST_PATH` defaults.

### Build

```bash
go build -o bin/wasm2go-run .
```
