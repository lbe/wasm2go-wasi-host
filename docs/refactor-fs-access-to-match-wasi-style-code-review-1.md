# Code Review: refactor-fs-access-to-match-wasi-style

## Summary

This review audits the branch `refactor-fs-access-to-match-wasi-style` against the plan `.pi/tdd-plans/refactor-fs-access-to-match-wasi-style.yaml` and performs a general correctness/security/compliance pass.

The refactor successfully removes the old public `WithMount` / `WithWritableMount` APIs from `wasihost.go`, wires `wasm2go-run --dir` through `WithHostDirectoryPreopen`, and adds useful regression coverage. However, I found several blocking issues, primarily around path confinement and WASI rights correctness.

---

## Validation Note

Current `go test ./...` fails because the worktree has an untracked file:

```text
?? e2e-wasip1-test-results.log
```

`TestQualityGate` requires a clean git status, so this must be removed or ignored before the suite can pass locally.

---

## Blocking Findings

### 1. Path traversal confinement is bypassable with nested `..`

**Severity:** Blocking / Security  
**Plan impact:** Violates cycles 2 and 5: preopened directory access and mutations must be confined to the configured host root.

Relevant code:

- `wasihost.go:681-684`
- `wasihost.go:692`
- `wasihost.go:1205-1215`

Current check:

```go
cleanRel := path.Clean("/" + rel)
if strings.HasPrefix(rel, "..") || cleanRel == "/.." || strings.HasPrefix(cleanRel, "/..") {
	return "", wasiENotCap
}
```

This misses paths like:

```text
subdir/../../outside.txt
```

because:

```go
path.Clean("/" + "subdir/../../outside.txt") == "/outside.txt"
```

That no longer starts with `/..`, so the path is accepted. Then:

```go
filepath.Join(m.hostRoot, filepath.FromSlash(rel))
```

cleans the path and resolves it outside `m.hostRoot`.

This affects:

- `Xpath_open`
- `Xpath_create_directory`
- `Xpath_remove_directory`
- `Xpath_unlink_file`
- `Xpath_readlink`
- `Xpath_symlink`
- `Xpath_link`
- `Xpath_rename`
- `Xpath_filestat_set_times` via `resolvePrimary`

Suggested fix:

- Validate the mount-relative path before host joining using relative-path semantics, not `"/"+rel`.
- For example:

```go
cleanRel := path.Clean(rel)
if cleanRel == ".." || strings.HasPrefix(cleanRel, "../") || path.IsAbs(cleanRel) {
	return "", wasiENotCap
}
```

- Then still verify the final host path remains beneath the root, preferably using `filepath.Rel` or, on modern Go, an `os.Root` / open-root style API to avoid lexical-only checks.

Add tests for at least:

```text
subdir/../../outside.txt
a/b/../../../outside.txt
/data/subdir/../../outside.txt
```

---

### 2. Symlinks can escape the preopen root

**Severity:** Blocking / Security  
**Plan impact:** Violates the "confined to configured host root" guarantee.

Relevant code:

- `wasihost.go:776-783`
- `wasihost.go:1214-1232`

`Xpath_symlink` allows an arbitrary target string:

```go
target := string(s.readBytes(oldPathPtr, oldPathLen))
...
os.Symlink(target, path)
```

Then `Xpath_open` uses `os.OpenFile(hostPath, ...)`, which follows symlinks. A guest can create or use a symlink inside the preopen pointing outside the root, then open/write through it.

Example scenario:

```text
preopen root: /tmp/root
outside:      /tmp/secret.txt

guest creates symlink:
  path_symlink("../secret.txt", "escape")

guest opens:
  path_open("escape", write rights)
```

The lexical path is inside `/tmp/root`, but the kernel follows the symlink outside the preopen.

This is especially important because the plan explicitly removes unsafe root fallback / parent escape behavior and moves toward WASI-style capability confinement.

Suggested fix:

- Use a symlink-safe path resolution strategy.
- If available in the project’s Go version, prefer an `os.Root` / open-root API.
- Otherwise, each path component must be resolved carefully with no-follow semantics and checked to remain under the preopen root.
- `path_filestat_get` should also respect WASI lookup flags rather than always using `os.Stat`, which follows symlinks.

Add tests for:

- Existing symlink inside preopen pointing outside.
- Guest-created symlink inside preopen pointing outside.
- `path_open`, `path_filestat_get`, `path_rename`, and write operations through such links.

---

### 3. WASI rights constants are incorrect/incomplete

**Severity:** Blocking / Correctness / Plan compliance  
**Plan impact:** Violates cycles 1 and 6: preopens should report directory/path rights, and `fd_fdstat_get` should return actual WASI rights.

Relevant code:

- `wasihost.go:74-80`
- `wasihost.go:88-91`
- `wasihost.go:221-232`

Current constants:

```go
rightFDRead         uint64 = 1 << 1
rightFDWrite        uint64 = 1 << 6
rightFDSeek         uint64 = 1 << 2
rightFdstatGet      uint64 = 1 << 8
rightFdstatSetFlags uint64 = 1 << 9
rightFilestatGet    uint64 = 1 << 12
```

Several of these bit positions do not match WASI preview1 rights:

- `1 << 8` is `FD_ALLOCATE`, not `FD_FDSTAT_GET`.
- `1 << 9` is `PATH_CREATE_DIRECTORY`, not `FD_FDSTAT_SET_FLAGS`.
- `1 << 12` is `PATH_LINK_TARGET`, not filestat get.
- Directory/path rights like `PATH_OPEN`, `PATH_READLINK`, `PATH_RENAME_SOURCE`, `PATH_RENAME_TARGET`, `PATH_UNLINK_FILE`, `FD_READDIR`, etc. are not modeled.

This means `fd_fdstat_get` reports nonsensical capabilities to WASI guests. Any guest that actually checks rights may make incorrect decisions or fail compliance tests.

Suggested fix:

- Define the full WASI preview1 rights bitset with spec-correct names and values.
- Separate rights bundles by descriptor type:
  - regular file
  - directory preopen
  - readonly directory preopen
  - stdio/character device
- Update tests to assert spec bits directly, especially for preopen fds.

---

### 4. Rights enforcement is incomplete beyond `fd_read` / `fd_write`

**Severity:** Blocking / Correctness  
**Plan impact:** Cycle 6 says WASI fd rights determine permitted operations.

Relevant code:

- `wasihost.go:1433-1473` — `Xfd_pread`
- `wasihost.go:1483-1522` — `Xfd_pwrite`
- `wasihost.go:1639-1652` — `Xfd_filestat_set_size`
- `wasihost.go:1660+` — `Xfd_filestat_set_times`
- `wasihost.go:1183+` — `Xpath_open`

Examples:

- `Xfd_pread` does not check read rights.
- `Xfd_pwrite` does not check write rights.
- `Xfd_filestat_set_size` truncates without checking a filestat-set-size right.
- `Xpath_open` accepts requested rights without validating them against the parent/preopen inheriting rights.

This weakens the rights model added by cycle 6.

Suggested fix:

- Enforce rights in every fd/path operation that has a WASI right.
- In `path_open`, reject requested base/inheriting rights that are not allowed by the dirfd’s inheriting rights.
- Add tests for `fd_pread`, `fd_pwrite`, filestat mutation, and path mutation rights.

---

## High / Medium Findings

### 5. `O_DIRECTORY` is not enforced for host directory preopens

**Severity:** Medium / WASI compliance  
**Relevant code:** `wasihost.go:1229-1232`, `wasihost.go:1263-1281`

When `oflagDir` is set, the code changes flags to `os.O_RDONLY`, but after opening it does not reject non-directory files.

A file opened with `oflagDir` can succeed and return an fd with `fdFile`.

Suggested fix:

```go
if uint32(oflags)&oflagDir != 0 && (fi == nil || !fi.IsDir()) {
	hostFile.Close()
	return wasiENotDir
}
```

This likely relates to the untracked e2e log showing failures for path-open / dirfd-related tests.

---

### 6. `Xpath_open` collapses many host errors to `ENOENT`

**Severity:** Medium / Correctness  
**Relevant code:** `wasihost.go:1232-1263`, `wasihost.go:1285-1287`

For writable mounts:

```go
hostFile, osErr := os.OpenFile(...)
if osErr != nil {
	...
	return wasiENoEnt
}
```

This maps permission errors, `ENOTDIR`, `EISDIR`, and other host failures to `ENOENT`.

The read-only fallback also does:

```go
f, err = mount.root.Open(relPath)
if err != nil {
	return wasiENoEnt
}
```

Suggested fix:

- Use `mapOSError(osErr)` where possible.
- Only fall back to an overlay when the fallback is semantically intended.
- Preserve `EACCES`, `ENOTDIR`, `EISDIR`, etc.

---

### 7. Stale design docs still document removed APIs and unsafe behavior

**Severity:** Medium / Documentation / Plan hygiene  
**Plan impact:** Cycle 9 removed ExifTool-shaped APIs and unsafe semantics, but repo docs still teach them.

Examples:

- `docs/wasi-for-wasm2go-design-1.md:115-126`
- `docs/wasi-for-wasm2go-design-1.md:414`
- `docs/wasi-for-wasm2go-design-1.md:441-442`
- `docs/plan-improve-testing.md:123-125`
- `docs/plan-improve-testing.md:231`
- `docs/plan-improve-testing.md:298-301`

These still mention:

- `WithMount`
- `WithWritableMount`
- `mountHostPaths`
- root fallback behavior
- ExifTool-specific wiring

The Red test only scanned `README.md` and `ARCHITECTURE.md`, so this slipped through.

Suggested fix:

- Either update these docs to the new API or explicitly mark them as historical/obsolete.
- Consider extending `api_removal_test.go` to scan `docs/` if stale docs are considered part of the public repo contract.

---

## Advisory Findings

### A1. Current test suite fails because of an untracked e2e log

`go test ./...` currently fails because `TestQualityGate` checks `git status --short`, and the worktree contains:

```text
?? e2e-wasip1-test-results.log
```

Suggested action:

- Remove the file before final validation, or
- Add a pattern to `.gitignore` if this is an expected local artifact.

---

### A2. The untracked e2e log reports broad wasi-testsuite failures

The untracked `e2e-wasip1-test-results.log` reports:

```text
FAIL: 33/72 tests failed
```

Many failures are filesystem-related, including:

- `path_rename.wasm`
- `path_open_dirfd_not_dir.wasm`
- `fd_readdir.wasm`
- `nofollow_errors.wasm`
- `truncation_rights.wasm`
- `path_open_preopen.wasm`
- `interesting_paths.wasm`

The plan only required targeted compliance coverage, not full suite completion, so I am not treating this as a plan-blocker by itself. But it is a strong signal that the rights/path/symlink issues above are observable in real WASI tests.

---

### A3. `docs_compliance_test.go` is mostly a static string test

Relevant code:

- `docs_compliance_test.go:17-36`
- `docs_compliance_test.go:51-68`

The test verifies terminology and checks that `runner_test.go` mentions a mutation binary. It does not itself verify that the e2e script runs that binary or that the binary passes with `--dir`.

This was acceptable for the TDD cycle, but as a compliance regression test it is weak.

Suggested improvement:

- Keep the static docs assertions.
- Add or strengthen a targeted executable test that runs the selected wasi-testsuite binary through `wasm2go-run --dir` and asserts success.
- Or have the docs test inspect `scripts/e2e-wasip1.sh` / a manifest if that is the intended compliance path.

---

### A4. White-box test mutates internal mount state

Relevant code:

- `group_a_fd_test.go:682-685`

```go
s := New(func() []byte { return buf },
	WithHostDirectoryPreopen("/tmp", tmpDir),
)
s.mounts[len(s.mounts)-1].root = overlay
```

This is fragile because it depends on internal mount ordering and mutates unexported state. It is acceptable for package-internal testing, but it indicates the test is trying to express an overlay/read-through use case no longer supported by the public API.

Suggested options:

- Remove the overlay fallback case if it is no longer part of supported behavior.
- Or add an explicit internal helper for tests.
- Or add a real public/internal overlay option if overlay read-through remains intended.

---

### A5. Some Red-phase comments remain in committed tests

Relevant code:

- `preopen_test.go:19-24`

These comments still describe "RED PHASE" and a hypothetical constructor even though the implementation is now complete.

This is not functionally harmful, but it makes the test read like temporary scaffolding.

---

## Positive Notes

- The old public `WithMount` and `WithWritableMount` functions are removed from `wasihost.go`.
- `wasm2go-run` now generates `WithHostDirectoryPreopen(...)` for `--dir`.
- The branch added useful targeted tests for:
  - preopen fd metadata
  - readonly preopens
  - mutation confinement
  - fd rights
  - fd flags / append behavior
  - CLI `--dir` writable behavior
- The public docs now describe preopened directories and read-only `fs.FS` preopens in WASI-style terms.

---

## Recommended Next Steps

1. Remove or ignore `e2e-wasip1-test-results.log` so `go test ./...` can run cleanly.
2. Fix lexical path traversal validation for all preopen-relative paths.
3. Add symlink-safe path resolution or explicitly reject symlink traversal where required.
4. Replace the rights constants with a complete WASI preview1 rights table.
5. Enforce rights across all fd/path operations, not just `fd_read` / `fd_write`.
6. Fix `O_DIRECTORY` and host error mapping in `Xpath_open`.
7. Update or archive stale docs under `docs/`.
8. Add regression tests for nested `..`, symlink escape, and spec-correct rights bits.
