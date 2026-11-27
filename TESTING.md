# Testing

This document describes how the tests are organized, when to run each tier, and how the unit tests relate to the wasi-testsuite e2e tests.

## Tiers

| Tier | Command | Time | Checks |
|------|---------|------|--------|
| Unit | `make test` or `go test ./...` | 5 s | Syscall contract: errno, rights, guest memory layout, confinement |
| `wasm2go-run` | `go test ./cmd/wasm2go-run/...` | 5 s | Transpile, cache, compile and execute, on selected fixtures |
| E2e smoke | `go test -tags=e2e -run TestWasiTestsuiteSmoke ./...` | 3 s | Seven Rust binaries through the `wasm2go-run` CLI |
| E2e full | `make test-e2e` or `go test -tags=e2e ./...` | 25 s | All 72 wasi-testsuite `wasm32-wasip1` cases and `TestQualityGate` |
| Shell inventory | `./scripts/e2e-wasip1.sh` | 11 s | The same 72 cases through the Python adapter, as a second check |

## Platforms

CI runs `go test -tags=e2e ./...` on Linux amd64, Linux arm64, macOS arm64, Windows amd64, and FreeBSD 15.1 amd64 in a QEMU virtual machine. It also cross-compiles and vets for FreeBSD and Windows.

Some tests are skipped on some OSes:

| Test | Skipped on | Reason |
|------|------------|--------|
| `wasm2go-run` tests that call `skipWithoutPOSIXShell` | Windows | They need a POSIX shell and Unix executables |
| `TestPollOneoffFdReadNonblockHostPipe` | Windows, FreeBSD | It needs `/dev/fd/N` for any fd. Windows has no `/dev/fd`, and FreeBSD exposes only fds 0 to 2 without fdescfs |
| `path_rename` of a directory onto `target/file`, expecting `ENOTDIR` | Windows | The test is written for Unix only |
| `path_open` `EACCES` on a mode 000 file | Windows, and any OS when run as root | `chmod` does not reliably produce `EACCES` there |
| Access time unchanged after `fd_filestat_set_times` | Windows | NTFS updates access times itself |
| `path_open` of a directory with `FD_WRITE`, expecting `EISDIR` | Windows | The test does not check the result on Windows |
| Tests that compare host inode or device numbers | Hosts where `os.Stat` reports zero | Nothing to compare |

On Windows, the `path_rename` tests for a directory over a non-empty directory and for a file over a directory check only that the call fails, not which errno it returns.

## Layout

Unit tests in the root `wasihost` package:

- `syscall_*_test.go` test WASI syscall behavior, grouped by area: path, fd, symlink, filestat, poll, sock, fs and foundation.
- `test_helpers_test.go` has shared fixtures: `newTestState`, preopen helpers and stub files.
- `wasihost_*_test.go` test `ModuleConfig`, `State`, adapters and `FSConfig`.
- `guardrails_test.go` checks the docs, removed APIs, the template and the e2e manifest.

Unit tests in `internal/wasi/**` test the VFS, sysfs, errno mapping, and the low-level file and fd table code.

E2e tests, with the `//go:build e2e` tag:

- `testdata/e2e_cases.json` is the manifest of 72 cases.
- `e2e_inventory_test.go` has `TestWasiTestsuite{Smoke,Rust,C,AssemblyScript}`.
- `e2e_helpers_test.go` has the shared runner, `runWasiTestsuiteCases`.
- `e2e_quality_gate_test.go` runs gofmt, the build and artifact cleanup, on a clean tree only.

## Unit and e2e policy

Unit tests and e2e tests cover the same areas but check different things. Keep both.

Unit tests:

- Fail fast with exact assertions, such as `EACCES` against `ENOENT`, rights masks and buffer contents.
- Cover security and edge cases that wasi-testsuite does not: confinement, symlink escape, wazero errno propagation and poll struct layout.
- Run on every save and in every CI job without building guest binaries.

E2e tests:

- Run the full pipeline: `wasm2go-run`, transpile, `go build`, run.
- Run real guest code in Rust, C and AssemblyScript, not in-process host stubs.
- Apply the wasi-testsuite JSON fields `root`, `dirs`, `args` and `env`, and check its expectations as `run-tests` does. The exit code must match `exit_code`, which defaults to 0. Each stream must start with the declared `stdout` or `stderr`.
- Run the Rust suite with `ERRNO_MODE_UNIX`, so each case asserts one errno instead of accepting any platform's errno. The host reports Linux errnos on every OS, so Unix mode applies on all of them.
- Are the compliance check for the Preview1 inventory.

Do not delete a unit test because an e2e case exists for the same syscall. Do not run fewer than the 72 e2e cases in CI unless a separate nightly job runs all of them. The full run takes 25 s.

### Overlap with unit tests

Each entry in `testdata/e2e_cases.json` has a `unitMirror` array that names related unit or `wasm2go-run` tests:

| `unitMirror` | Meaning |
|--------------|---------|
| 2 or more entries | Strong mirror. Unit tests check the same wasi-testsuite spec in-process. The e2e case is still needed to test the pipeline and the guest. |
| 1 entry | Partial mirror. Related unit tests exist. The e2e case is the main test of guest-specific behavior. |
| `[]` | E2e only. No unit test mirrors the case, for example `isatty`, `path_exists`, `big_random_buf`, and C-only libc paths. |

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

AssemblyScript cases can name `cmd/wasm2go-run` tests, such as `TestProcExit` and `TestStdoutAndEnv`, as well as root `syscall_*` tests. The `cmd/wasm2go-run` tests are in the `main` package.

`TestGuardrails_E2EManifestUnitMirrorsValid` checks that every `unitMirror` name is a real `Test*` function.

## Daily workflow

```bash
make test                                            # unit tests, 5 s
go test -tags=e2e -run TestWasiTestsuiteSmoke ./...  # smoke e2e, 3 s
make test-e2e                                        # full suite as CI runs it, 25 s
```

## Adding or changing tests

New wasi-testsuite binary:

1. Add an entry to `testdata/e2e_cases.json` with `suite`, `name`, `stdioOnly`, `smoke` and `unitMirror`. The expected exit code and output come from the test's own `<name>.json` in the submodule.
2. To test the same spec in-process, add or extend a `syscall_*_test.go` test.
3. Run `make test-e2e`.

New behavior covered only by unit tests, such as confinement or errno mapping:

1. Add tests to the matching `syscall_*_test.go` or `internal/wasi/*_test.go` file.
2. Change the e2e manifest only if a wasi-testsuite case exists for the behavior.

## Related docs

- [README.md](./README.md): installation, supported platforms and Makefile targets
- [ARCHITECTURE.md](./ARCHITECTURE.md): design and compliance
- [cmd/wasm2go-run/README.md](./cmd/wasm2go-run/README.md): runner and cache tests
