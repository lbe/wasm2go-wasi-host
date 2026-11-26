# Plan: add Windows support

Scope: WASI preview1 (`wasm32-wasip1`) only. Preview 2/3 are out of scope.

## Decisions

1. **Errnos.** Windows reports the same Unix-style WASI errnos as Linux and macOS. There is one translation table, and the Rust suite runs with `ERRNO_MODE_UNIX` on every OS (already set in `e2e_helpers_test.go`).
2. **Root preopen pass-through.** On Windows, guest `/` in `WithHostDirectoryPreopen("/", "/")` maps to the root of the current drive (for example `C:\`). The root preopen and its re-absolutize code stay as they are. Windows resolves `/x` against the current drive's root, so `"/"+rel` and `hostRoot\rel` name the same file.
3. **Where tests run.** Builds are checked locally by cross-compiling (`GOOS=windows`). Tests run on GitHub Actions `windows-latest`, which is free for this public repo. Its runners run as administrator, so symlink tests can run there.

## Current state

- **First build errors:** `GOOS=windows go build ./...` stops in two packages:
  - `internal/wasi/stat.go` uses `syscall.Stat_t`.
  - `cmd/wasm2go-run/cache.go` uses `unix.Flock`.
- **Missing in `internal/wasi/sysfs`:** after stubbing those, `sysfs` lacks `rename`, `rmdir`, `unlink`, `utimens`, `futimens`, `openFile`, `inoFromFileInfo`, `withSyscallOflag`, `readFd`, `writeFd`, `preadFd`, `pwriteFd`, `fsync`, `datasync`, `setNonblock` and `poll`. These match wazero's `*_windows.go` files, which were dropped when the code was adapted.
- **wazero source:** these files are in the local module cache under `wazero@v1.12.0/internal/sysfs/` (about 1,300 lines). wazero's Windows error table is at `experimental/sys/syscall_errno_windows.go`.
- **Root package (not yet compiled for Windows, blocked by the errors above).** Known problems:
  - `setTimesAtPath` calls `unix.UtimesNanoAt`.
  - `linkSymlinkInode` and `getAtimeFromStat` exist only for linux and darwin.
  - About 15 direct `os.*`, `unix.*` and `filepath.EvalSymlinks` calls skip `sysfs`.
  - `dirNotEmptyErr` has an untested Windows branch that returns EACCES.
- **Build tags on other Unixes:**
  - `poll_unsupported.go` and `poll_unix.go` both apply on FreeBSD ("poll redeclared").
  - `futimens` and `withSyscallOflag` exist only for linux and darwin.
- **CLI (`cmd/wasm2go-run`):**
  - The cache lock uses `unix.Flock`.
  - The cache directory falls back to `$HOME`.
  - The built binary is named `runner`, without `.exe`.
- **Tests:**
  - About 20 test files use symlinks, permission bits or `syscall`.
  - `e2e_helpers_test.go` skips unless the OS is linux or darwin.
  - `go vet ./...` flags `wasi.File.Seek` because it returns `Errno` rather than `error`.

## Phases

Each phase ends with a commit. Windows CI runs start in phase 4.

| Phase | Work | Verified by | Model |
|---|---|---|---|
| 1 | Build tags: explicit linux/darwin/windows support, an ENOSYS fallback for other OSes, and the FreeBSD "poll redeclared" fix | `GOOS=windows` and `GOOS=freebsd` `go build` | Haiku |
| 1 | CLI: `os.UserCacheDir()`, a lock file per OS (`LockFileEx` on Windows), `runner.exe` | cross-compile plus existing tests | Haiku |
| 1 | CI job that cross-compiles and vets for windows and freebsd. Fix vet's complaint about `wasi.File.Seek` first. | CI | Haiku |
| 2 | Move the direct root-package OS calls (`setTimesAtPath`, `linkSymlinkInode`, `atime_*`, `os.Symlink`, `os.Link`, `os.ReadDir`, the `EvalSymlinks` checks) behind `DirFS` methods. Remove the `getAtimeFromStat` cast, since `Stat_t.Atim` already holds that value. | mac unit tests plus e2e | Opus designs the interface, Haiku moves the calls |
| 3 | Port wazero v1.12.0's `sysfs` Windows files, keeping the existing Apache 2.0 attribution | `GOOS=windows go build ./...` and `go vet` | Haiku, with Opus reviewing the diff |
| 3 | Windows error table (`ERROR_*` → WASI, Unix-style values), based on wazero's. Remove the EACCES branch in `dirNotEmptyErr`. | unit tests | Haiku drafts, Opus checks against the spec |
| 3 | Windows path handling in `DirFS` and the confinement checks: make the host root absolute at setup (`/` becomes the current drive root), compare paths case-insensitively, and handle `\\?\` prefixes and `/` vs `\` | new tests | Opus |
| 4 | Test skip helper for the symlink, permission and `syscall` test files. Let the e2e harness run on Windows. Add `windows-latest` to the CI matrix. | Windows CI | Haiku |
| 5 | Fix whatever fails on Windows CI | Windows CI | Haiku summarizes logs, Opus fixes real bugs |

### Phase 1: build tags, CLI and cross-compile CI

- **Support list:** add explicit build constraints for linux, darwin and windows, plus a fallback for every other OS that compiles and returns ENOSYS.
- **FreeBSD:** make `poll_unsupported.go` and `poll_unix.go` mutually exclusive.
- **Constraints:** give `futimens` and `withSyscallOflag` constraints that cover each supported OS, with the fallback for the rest.
- **CLI:**
  - Use `os.UserCacheDir()` for the cache directory, keeping `XDG_CACHE_HOME` first.
  - Split the cache lock into `lock_unix.go` (`Flock`) and `lock_windows.go` (`LockFileEx`).
  - Name the built binary `runner.exe` when `GOOS=windows`.
- **vet:** fix the `wasi.File.Seek` signature complaint before adding vet to CI.
- **CI:** add a job running `go build ./...` and `go vet ./...` with `GOOS=windows` and `GOOS=freebsd`.

Done when Linux and macOS tests and e2e still pass and the new CI job is green. The Windows build may still fail inside `sysfs` and the root package at this point; phases 2 and 3 fix that.

### Phase 2: route root-package file operations through `sysfs`

- **Goal:** the root package stays OS-independent, and all OS-specific code lives in `internal/wasi/sysfs`, the same way wazero is structured.
- **Interface:** add `DirFS` methods for the operations the root package does directly today. These are: link a symlink itself, set times without following, rename over an empty directory, create a symlink, hard link, list a directory, and resolve symlinks for confinement checks.
- **Remove:** `atime_darwin.go`, `atime_linux.go` and the `getAtimeFromStat` type cast.

Done when there are no `unix.*` imports in the root package, and unit tests, e2e and the go-exiftool-wasm suite pass on macOS.

### Phase 3: Windows `sysfs`, errors and paths

- **Port:** copy wazero v1.12.0's `internal/sysfs/*_windows.go` files, renaming to this package's function names and keeping the attribution headers.
- **Error table:** add a Windows table that maps `ERROR_*` codes to WASI errnos, using Unix-style values. Remove the Windows branch in `dirNotEmptyErr`.
- **Paths:**
  - Make the host root absolute when the preopen is created, so `/` becomes `C:\` (the current drive root).
  - Compare paths case-insensitively and normalize separators.
  - Handle `\\?\` and UNC prefixes.
  - Decide how to treat junctions and 8.3 short names in the `EvalSymlinks` confinement checks.

Done when `GOOS=windows go build ./...` and `go vet ./...` pass, and the new unit tests for the error table and path handling pass on macOS where they are OS-independent.

### Phase 4: tests and Windows CI

- **Skip helper:** add a test helper that skips symlink tests when symlinks are unavailable and skips Unix permission-bit tests on Windows. Apply it to the affected test files.
- **e2e:** remove the linux/darwin-only check in `e2e_helpers_test.go`, so the Go e2e harness runs on Windows.
- **CI matrix:** add `windows-latest` running the unit tests and `go test -tags=e2e ./...`.

### Phase 5: fix Windows failures

Run Windows CI, have Haiku summarize the failures into a short list, and fix real host bugs with tests. Keep each fix to one commit.

Done when unit tests and all 72 wasip1 testsuite cases pass on `windows-latest`.

## Cost controls

- **Haiku briefs:** each Haiku task gets a narrow written brief naming the exact files, the source to copy from, and its done check (usually `GOOS=windows go build` passes).
- **Review:** Opus reviews Haiku's diffs instead of re-reading whole files.
- **Fast feedback:** use local cross-compiles for compile errors; use Windows CI only from phase 4.
- **No large reviews:** no multi-agent reviews, and one review of the confinement changes at the end.
- **Biggest unknown:** runtime failures in phase 5. Phases 1–3 keep that small by making the code compile and pass vet before the first Windows run.

## How it went

Implemented on the `windows-support` branch, one or more commits per phase.

- **Phase 1:**
  - CI vet runs with `-stdmethods=false` instead of renaming `wasi.File.Seek`.
  - The Windows cross-compile entry was added in phase 4, once the Windows build passed.
- **Phase 2:**
  - Smaller than estimated. The only Unix-only calls left in the root package were `linkSymlinkInode`, `setTimesAtPath` and `getAtimeFromStat`.
  - They moved to `sysfs.LinkNoFollow` and `sysfs.UtimesNoFollow`, plus a portable `getAtimeFromStat`.
  - These are package-level functions that take host paths, not `DirFS` methods, so the root preopen's existing path mapping is unchanged.
  - The other `os.*` calls (`Symlink`, `Link`, `ReadDir`, `EvalSymlinks`) are already portable.
- **Phase 3:**
  - Containment needed less work than expected: `DirFS.join` already makes the host root absolute (`/` becomes the current drive root), and `filepath.Rel` compares case-insensitively on Windows.
  - The real gap was guest paths containing `\` or `:`. `sysfs.GuestPathOK` now rejects them on Windows.
  - Junction and 8.3 short-name handling was left to `filepath.EvalSymlinks`.
- **Phase 5 host fixes:**
  - O_DIRECTORY through a symlink to a file now returns ENOTDIR.
  - ERROR_PATH_NOT_FOUND is reported as ENOTDIR or ENOENT depending on the parent.
  - `path_symlink` onto an existing name returns EEXIST.
  - `readlink` targets use `/`.
  - `fd_readdir` reports real inode numbers.
  - The monotonic clock is positive from the first read.
- **Tests skipped on Windows:**
  - CLI tests that need a POSIX shell or Unix executables.
  - The `/dev/fd` poll test.
  - The "atime unchanged" assertion, because NTFS updates access time itself.
  - Tests that need host inode numbers from `os.Stat`.

## Out of scope

- WASI preview 2 and 3.
- Native Windows errnos (`ERRNO_MODE_WINDOWS`).
- Mapping guest `/c/...` to `C:\...` (multi-drive pass-through). This could be added later.
