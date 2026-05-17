# ToDo

Follow-ups and recommendations from the audit of **refactor-fs-access-to-match-wasi-style-correction-2** (branch `refactor-fs-access-to-match-wasi-style`).

Correction-2 is **ready to merge** for its stated scope. Items below are optional hardening, test parity, or future corrections.

---

## Test coverage gaps

### Cycle 1 — filestat escape paths

`TestNestedParentSegmentPathsPathFilestatCannotEscapeWritableHostPreopen` covers:

- `subdir/../../outside`
- `a/b/../../../outside`

It does **not** cover the absolute guest path `/data/subdir/../../outside`, which is already tested for `path_open` and other mutations in `TestNestedParentSegmentPathsCannotEscapeWritableHostPreopen`. Implementation should handle it (same `resolveDirfdPath` raw branch + `preopenDirfdLexicallyEscapes`), but filestat syscalls are untested for that case.

**Recommendation:** Add `/data/subdir/../../outside` to the filestat confinement test matrix.

### Cycle 4 — fs.FS overlay fallback in `path_open`

Both `mount.root.Open` failure paths in `Xpath_open` now call `mapOSError(err)`:

- Read-only fs.FS preopen (~1514) — **tested** via `TestPathOpenDistinguishesPermissionDeniedFromMissingOnReadOnlyFS` and `errorFS`.
- Writable-mount overlay fallback after host `ErrNotExist` (~1459) — **fixed in code, not directly tested**.

**Recommendation:** Add an integration test where the host file is absent, overlay `Open` returns `fs.ErrPermission`, and `path_open` returns `EACCES` (not `ENOENT`).

---

## Behavior / errno parity (out of correction-2 scope)

### `path_filestat_get` stat errors still collapse to `ENOENT`

On writable host-backed mounts, when `statHostPathOrOverlay` fails, the syscall returns `wasiENoEnt` instead of `mapOSError(err)`. The read-only mount branch does the same for `fs.Stat` failures.

**Recommendation:** Consider a future correction cycle to route stat failures through `mapOSError` where the underlying `fs.FS` or `os` error provides a more specific errno (e.g. `EACCES`).

---

## Symlink semantics (advisory)

### No-follow `path_open` on in-root symlinks

With `lookupflags=0`, any existing symlink leaf (including confined symlinks under the preopen) returns `ELOOP` via `joinWritableHostPathForLookup`. This matches O_NOFOLLOW-style behavior. Tests only assert the escape-symlink case; in-root symlinks with no-follow are not explicitly covered.

**Recommendation:** Optional test: symlink `root/link → legit.txt` inside preopen, `path_open` with `lookupflags=0` → `ELOOP`; with `SYMLINK_FOLLOW` → `ESUCCESS`.

---

## Process / housekeeping

- Plan file: `.pi/tdd-plans/refactor-fs-access-to-match-wasi-style-correction-2.yaml`
- Work landed on branch `refactor-fs-access-to-match-wasi-style` (not a separate `correction-2` branch name) — intentional per user direction during orchestration.

---

## Completed in correction-2 (no action)

| Original review finding | Resolution |
|-------------------------|------------|
| `resolvePrimary` escape in `path_filestat_set_times` | Switched to `resolveWritable` |
| `Xpath_filestat_get` missing lexical escape check | `preopenDirfdLexicallyEscapes` |
| No-follow `path_open` followed symlinks at OS level | `os.Lstat` + `wasiELoop` in `joinWritableHostPathForLookup` |
| Missing no-follow symlink tests | `symlink_confinement_test.go` cycles 2–3 |
| `path_open` fs.FS errors always `ENOENT` | `mapOSError(err)` at both `Open` callsites |
