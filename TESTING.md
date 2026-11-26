# Testing

This document describes how tests are organized, when to run each tier, and how unit tests relate to wasi-testsuite e2e coverage.

## Tiers

| Tier | Command | Time | Proves |
|------|---------|------|--------|
| Unit | `make test` / `go test ./...` | ~5s | Syscall contract: errno, rights, guest memory layout, confinement |
| `wasm2go-run` | `go test ./cmd/wasm2go-run/...` | ~5s | Transpile, cache, compile, execute (selected fixtures) |
| E2e smoke | `go test -tags=e2e -run TestWasiTestsuiteSmoke ./...` | ~3s | Seven high-value rust binaries via the `wasm2go-run` CLI |
| E2e full | `make test-e2e` / `go test -tags=e2e ./...` | ~25s | All 72 wasi-testsuite `wasm32-wasip1` cases + `TestQualityGate` |
| Shell inventory | `./scripts/e2e-wasip1.sh` | ~11s | Same 72 cases through the Python adapter (backup / release check) |

CI runs `go test -tags=e2e ./...` on Linux and macOS.

## Layout

**Unit tests (root `wasihost` package)**

- `syscall_*_test.go` — WASI syscall behavior grouped by domain (path, fd, symlink, filestat, poll, sock, fs, foundation).
- `test_helpers_test.go` — shared fixtures (`newTestState`, preopen helpers, stub files).
- `wasihost_*_test.go` — `ModuleConfig`, `State`, adapters, `FSConfig`.
- `guardrails_test.go` — docs compliance, API removal guards, template checks, e2e manifest validation.

**Unit tests (`internal/wasi/**`)**

- VFS, sysfs, errno mapping, and low-level file/table primitives.

**E2e (`//go:build e2e`)**

- `testdata/e2e_cases.json` — inventory manifest (72 cases).
- `e2e_inventory_test.go` — `TestWasiTestsuite{Smoke,Rust,C,AssemblyScript}`.
- `e2e_helpers_test.go` — shared runner (`runWasiTestsuiteCases`).
- `e2e_quality_gate_test.go` — gofmt, build, artifact cleanup (clean tree only).

## Unit vs e2e policy

Unit tests and e2e tests overlap in *behavior area* but not in *what they prove*. Keep both.

**Unit tests**

- Fail fast with precise assertions (`EACCES` vs `ENOENT`, rights masks, buffer contents).
- Cover security and edge cases **not** in wasi-testsuite (confinement, symlink escape, wazero errno propagation, poll struct layout).
- Run on every save / CI job without building guest binaries.

**E2e tests**

- Exercise the full pipeline: `wasm2go-run` → transpile → `go build` → run.
- Run real guest code (Rust, C, AssemblyScript), not in-process host stubs.
- Honor wasi-testsuite JSON (`root`, `dirs`, `args`, `env`) and check its expectations the way `run-tests` does: exact `exit_code` (default 0), and declared `stdout` / `stderr` must be what the stream starts with.
- Run the Rust suite with `ERRNO_MODE_UNIX` so it asserts one errno per case instead of accepting any platform's errno. The host reports Linux errnos on macOS too, so Unix mode applies on both.
- Are the compliance signal for Preview1 inventory coverage.

**Do not** delete unit tests because an e2e case exists for the same syscall. **Do not** shrink CI e2e below 72 cases unless a separate nightly full run replaces it—the runtime cost (~25s) is small relative to the integration value.

### Overlap tiers (documented in the manifest)

Each entry in `testdata/e2e_cases.json` has a `unitMirror` array naming related unit/`wasm2go-run` tests:

| `unitMirror` | Meaning |
|--------------|---------|
| 2+ entries | **Strong mirror** — same wasi-testsuite spec exercised in-process; e2e still required for pipeline + guest proof. |
| 1 entry | **Partial mirror** — related unit coverage; e2e is primary for guest-specific behavior. |
| `[]` | **E2e-primary** — no dedicated unit mirror (e.g. `isatty`, `path_exists`, `big_random_buf`, C-only libc paths). |

Examples:

```json
{
  "name": "path_link",
  "unitMirror": ["TestPathLinkSyscall"]
}
```

```json
{
  "name": "isatty",
  "unitMirror": []
}
```

AssemblyScript cases may mirror `cmd/wasm2go-run` tests (e.g. `TestProcExit`, `TestStdoutAndEnv`) in addition to root `syscall_*` tests. Those live in the `main` package under `cmd/wasm2go-run/`.

`TestGuardrails_E2EManifestUnitMirrorsValid` verifies every `unitMirror` name resolves to a real `Test*` function.

## Daily workflow

```bash
make test                              # unit loop (~5s)
go test -tags=e2e -run TestWasiTestsuiteSmoke ./...   # quick integration (~3s)
make test-e2e                          # before push; matches CI (~25s)
```

## Adding or changing coverage

**New wasi-testsuite binary**

1. Add an entry to `testdata/e2e_cases.json` (`suite`, `name`, `stdioOnly`, `smoke`, `unitMirror`). Expected exit code and output come from the test's own `<name>.json` in the submodule.
2. Add or extend `syscall_*_test.go` if you want fast in-process coverage of the same spec.
3. Run `make test-e2e`.

**New unit-only behavior** (confinement, errno mapping, etc.)

1. Add tests to the appropriate `syscall_*_test.go` or `internal/wasi/*_test.go`.
2. No e2e manifest change required unless a wasi-testsuite case exists.

## Related docs

- [README.md](./README.md) — install and Makefile targets
- [ARCHITECTURE.md](./ARCHITECTURE.md) — design and compliance overview
- [cmd/wasm2go-run/README.md](./cmd/wasm2go-run/README.md) — runner and cache tests
