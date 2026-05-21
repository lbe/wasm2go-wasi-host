# Review: TDD plan `fix_fd_readdir`

**Plan:** `.pi/tdd-plans/fix_fd_readdir.yaml`  
**Reviewed against:** `docs/wasi-testsuite-correction-sequence-plan.md` (Group 1), current `wasihost.go` / tests, `wasi-testsuite` sources  
**Verdict:** Directionally sound; revise before execution.

---

## Overall assessment

The plan targets the right failure modes for Group 1: synthetic `.` and `..`, `d_ino`, cookie-stable pagination, and writable directory listing. It aligns with `wasi-testsuite/tests/rust/wasm32-wasip1/src/bin/fd_readdir.rs` for the main behaviors.

It has **ordering inconsistencies**, **gaps around `fd_filestat_get` / `writeFilestat` ino**, **overlap with existing tests**, and **under-specified writable-preopen mechanics**. Address those in the YAML before starting TDD orchestration.

---

## Strengths

- **Authoritative behavior** — Matches `fd_readdir.rs`: empty dir → two entries (`.` / `..`); after creating `file` → three entries; cookie resume; `d_ino == fd_filestat_get(...).ino` for `.` and the file.
- **Assumptions block** — Documents that host `ReadDir` omits `.` / `..`, EOF via `bufUsed < bufLen`, and that the C test uses host `open()` (os-backed path).
- **Right integration points** — `Xfd_readdir`, `DirEntriesFile`, `fdEntry.dirFile`, `newWMState`, `group_a_fd_test.go` (`TestFdReaddir`).
- **E2e inventory** — Rust `fd_readdir` plus C `fdopendir-with-access` matches the correction sequence plan.
- **Dependency order** — Group 1 before later path/readdir-dependent groups is correct.

---

## Issues

### 1. Cycle ordering rationale does not match cycle definitions (high)

The header says cycles **1–2** establish **caching**. In the YAML, caching/pagination is **cycle 3**. Cycles 1–2 are synthetic `.` / `..` (fs.FS vs writable host).

**Risk:** Implementers cache before injection, or validate the wrong behavior per cycle.

**Recommendation:** Rewrite the rationale to match cycle IDs 1–5, or renumber the cycles.

---

### 2. `d_ino` / `fd_filestat_get` dependency is missing as explicit work (high)

`fd_readdir.rs` requires:

```rust
assert_eq!(dir.dirent.d_ino, stat.ino);  // for "." and "file"
```

Today `Xfd_readdir` always writes `d_ino = 0` (dirent offset +8), and `writeFilestat` always sets `ino = 0` (`wasihost.go` ~576–579).

Cycle **2** acceptance ties `.`’s `d_ino` to `fd_filestat_get(fd).ino`, but **no cycle** calls out updating `writeFilestat` / `Xfd_filestat_get` (and likely path filestat for consistency). The old rationale labeled cycle 3 as `d_ino`; in the YAML, cycle 3 is caching.

**Risk:** Unit tests pass on buffer layout while e2e fails; or cycle 2 is blocked until ino work is discovered late.

**Recommendation:** Add an explicit sub-task (cycle 2 or 2b): populate `dev`/`ino` in filestat and dirent from host stat where available.

---

### 3. Cycle 2 “writable host preopen” vs actual code path (medium–high)

Writable preopens use `os.DirFS(hostPath)` as `mount.root` (`WithHostDirectoryPreopen`). First `fd_readdir` on preopen fd 3 loads via `ReadDirFS` → `DirEntriesFile`, **not** `osFile.Readdir` as the plan rationale states.

That path can still satisfy `d_ino` if `DirEntry.Info()` / `Sys()` is used, but:

- It is not the same as the C e2e path (`open` / `fdopendir` → libc inodes).
- The real split is **ReadDirFS-backed preopen** vs **`path_open`’d `osFile` directory fd**, not fs.FS vs writable host per se.

**Recommendation:** Name both paths in assumptions/context; add a `path_open` directory subtest for the osFile path the C test exercises.

---

### 4. Cycle 3 acceptance criteria are brittle (medium)

- **“bufUsed == bufLen (full buffer)”** with five entries depends on buffer size and name lengths; layout changes break the test.
- **“cookie = d_next of the third entry → exactly 2 remaining”** assumes fixed enumeration order and a first call that fits exactly three dirents; injection of `.` / `..` makes order sensitive (typically `.`, `..`, then files).
- **“Entries are not re-read from the host”** is the right requirement; current code calls `ReadDir(-1)` on every invocation and resets `DirEntriesFile.idx` — hard to assert without a test seam.

**Recommendation:** Assert cookie continuity and entry names across calls, not exact `bufUsed == bufLen`. Document injection order (`.` / `..` first).

---

### 5. Cycle 4 largely duplicates existing tests (low–medium)

`group_a_fd_test.go` `TestFdReaddir` already covers:

- `bufLen=1` → `bufUsed=0`
- Cookie continuation on preopen
- `EBADF` for fd 999

Cycle 4 adds “cookie past last → bufUsed=0” (not clearly covered today) but does not mention **`ENOTDIR`** for non-directory fds (current behavior).

**Recommendation:** Narrow cycle 4 to net-new cases, or mark it as “extend `TestFdReaddir`” to avoid duplicate red commits.

---

### 6. E2e scale not reflected in unit cycles (medium)

`fd_readdir.rs` also runs `test_fd_readdir_lots` (1000 files, cookie walk, expect 1002 entries). No unit cycle exercises pagination at that scale; only cycle 5 e2e would catch regressions there.

**Recommendation:** Note in cycle 5 that `test_fd_readdir_lots` is part of `fd_readdir.wasm`, or add a lighter stress subtest (e.g. 50 files).

---

### 7. C test requirements vs plan emphasis (low)

`fdopendir-with-access.c` skips names starting with `.` and only checks `d_ino` for real files vs `fstatat`. It does **not** require synthetic `.` / `..`.

Plan weight on `.` / `..` is driven by the Rust test. Cycle 5 should still pass once `d_ino` is correct for real entries on an os-backed directory fd.

---

### 8. Cycle 5 structure / naming (low)

- References `e2e_group2_test.go` pattern; Group 1 should get its own file (e.g. `e2e_group1_test.go`), as Group 6a did with `e2e_group6a_test.go`.
- `test_level: integration` — repo convention for wasm2go-run tests is **e2e** (`e2e_helpers_test.go`).
- No mention of reusing `runWasiTestsuiteCases` if already on `main` from the fstflags work.

---

### 9. Missing “out of scope” section (low)

Unlike `fix-fstflags_validate.yaml`, there is no explicit boundary. Suggested out of scope:

- Full Group 6 `dev`/`ino` everywhere
- `RIGHT_FD_READDIR` / `ENOTCAP` enforcement on `fd_readdir`
- Symlink / trailing-slash groups
- Rewriting all of `group_a` readdir tests

---

### 10. Model assignments / branch naming (low)

- All phases assigned `sonnet` vs `fix-fstflags_validate` using `composer-2-fast` for red/green — cost/latency tradeoff only.
- `feature_branch: fix_fd_readdir` (underscore) vs `fix-fstflags_validate` (hyphen) — align with repo convention.

---

## Cycle-by-cycle traceability

| Cycle | Plan intent | vs testsuite / codebase |
|-------|-------------|-------------------------|
| **1** | fs.FS dir via `path_open`, synthetic `.`/`..`, `d_ino=0` | Valid for read-only mounts; **not** the path `fd_readdir.wasm` uses (writable scratch dir). Still worth having. |
| **2** | Writable preopen, `d_ino` from stat | **Blocked** until filestat ino is implemented; listing may be `DirEntriesFile` not `osFile`; `.` ino must match `fd_filestat_get`. |
| **3** | Cache + cookies | Correct target; current impl re-reads each call; acceptance wording should be tightened. |
| **4** | Small buf, EOF cookie, EBADF | Mostly redundant with `TestFdReaddir`; EOF cookie is the main addition. |
| **5** | E2e both wasms + smokes | Correct; carries full Rust test including `test_fd_readdir_lots`. |

---

## Alignment with correction sequence plan

Group 1 items (synthetic entries, `d_ino`, cookie cache, writable listing) are all represented. The plan does not mention updating the Group 1 checklist row in `docs/wasi-testsuite-correction-sequence-plan.md` when done (the fstflags plan did tick Group 6).

---

## Recommended plan edits

1. Fix cycle ordering rationale to match cycle IDs.
2. Add explicit **ino/filestat** work tied to cycle 2 (or new cycle 2b).
3. Clarify **ReadDirFS preopen** vs **`osFile` dir fd** in assumptions and tests.
4. Soften cycle 3 numeric assertions; specify `.` / `..` / host entry order.
5. Slim cycle 4 to net-new cases; reference extending `TestFdReaddir`.
6. Cycle 5: `e2e_group1_test.go`, `test_level: e2e`, note `test_fd_readdir_lots`.
7. Add an **out of scope** block.
8. Tick Group 1 checklist when e2e passes.

---

## Current implementation snapshot (review baseline)

Relevant gaps in `Xfd_readdir` today (`wasihost.go` ~1253–1331):

- No synthetic `.` / `..`
- `d_ino` always 0 in dirent buffer
- `ReadDir(-1)` on every call; cookie uses slice index, not a stable cached list with injection
- Writable preopen: lazy load via `ReadDirFS` on `os.DirFS`, not raw `*os.File` readdir

`writeFilestat` sets `dev` and `ino` to 0 for all filestat writes.

---

## Follow-up review (corrected plan)

**Plan revision:** `.pi/tdd-plans/fix_fd_readdir.yaml` (6 cycles, `feature_branch: fix-fd-readdir`)  
**Verdict:** **Ready to execute**, with one semantic gap to clarify in cycle 4 (cache vs `cookie=0` after directory mutation).

The corrected plan addresses every high-priority item from the initial review above. Cycle ordering, ino/filestat work, path split documentation, pagination tests, e2e scope, out-of-scope boundaries, and branch naming are all coherent.

---

### Resolved from initial review

| Prior issue | Status |
|-------------|--------|
| Rationale vs cycle IDs mismatched | Fixed — cycles 1–6 rationale matches definitions |
| Missing `writeFilestat` / ino work | Fixed — cycle 1 is explicit prerequisite |
| ReadDirFS preopen vs `osFile` path unclear | Fixed — assumptions §9–12; cycle 3 `path_open` subtest |
| Brittle pagination acceptance | Improved — order `.`, `..`, `f1…f3`; buffer sized for 3 dirents; delete-after-read cache proof |
| Duplicate edge-case cycle | Fixed — cycle 5 extends `TestFdReaddir` |
| `test_fd_readdir_lots` not in plan | Fixed — cycle 6 acceptance |
| Wrong e2e file / test level | Fixed — `e2e_group1_test.go`, `test_level: e2e` |
| No out of scope | Fixed — plan lines 18–24 |
| Branch naming | Fixed — `fix-fd-readdir` (hyphen) |

---

### Remaining issues

#### 1. Cache refresh on `cookie=0` after directory changes (high — clarify before cycle 4 green)

`fd_readdir.rs` calls `exec_fd_readdir(dir_fd, 0)` twice on the same fd: once on an empty dir, again after creating `file`. The second call must see three entries.

Cycle 4 only proves the cache is **stable across continuation cookies** (delete file, then read with `f1.d_next`). It does not state what happens on **`cookie=0`** after the directory mutates.

If the cache is built once on first `fd_readdir` and never refreshed, the post-create `cookie=0` read can still return two entries and **cycle 6 will fail**.

**Recommendation:** Add to cycle 4 acceptance or assumptions:

> `cookie=0` re-builds the directory entry list from the host (or invalidates the cache). Continuation cookies (`>0`) use the cached snapshot from that build.

#### 2. Cycle 1: preopen stat source (medium)

Cycle 1 uses a writable host preopen and expects non-zero `ino`. Today `Xfd_filestat_get` on preopen uses `fs.Stat(mount.root, ".")` (`os.DirFS`), not `os.Stat(mount.hostRoot)`.

`fs.FileInfo` from `DirFS` may not expose `Sys().(*syscall.Stat_t)` the same way as a direct host `os.Stat`. Platform helpers in `atime_*.go` assume `Stat_t` is present.

**Recommendation:** In cycle 1 context, specify that writable mounts should stat via `mount.hostRoot` (or equivalent) when populating `dev`/`ino`, with `fs.FS` mounts remaining 0.

#### 3. Cycle 3: `DirEntry.Sys()` may require `Info()` (medium)

For file entries on `os.DirFS`, `d_ino` from `DirEntry.Sys()` is often **nil until `Info()` is called**. Cycle 3 acceptance assumes `Sys()` works; implementation should call `Info()` (or stat by path under `hostRoot`) when filling dirents.

Worth one line in cycle 3 context, not a separate cycle.

#### 4. `model_assignments` removed (low)

The fstflags plan had explicit red/green/refactor models; this plan has none. Fine if orchestrator defaults apply.

#### 5. Checklist / doc hygiene (low)

Still no step to mark Group 1 complete in `docs/wasi-testsuite-correction-sequence-plan.md` after cycle 6 (fstflags plan ticked Group 6). Optional housekeeping.

#### 6. Cycle 1 context vs acceptance scope (low)

Context mentions `Xpath_filestat_get`; acceptance only tests `fd_filestat_get`. Green phase should not require path tests to pass cycle 1.

---

### Corrected plan — cycle readiness

| Cycle | Ready? | Notes |
|-------|--------|-------|
| **1** | Yes, with `hostRoot` note | Blocks cycles 3 and 6 |
| **2** | Yes | fs.FS-only; `d_ino=0` |
| **3** | Yes | Preopen + `path_open` subtest; depends on 1 |
| **4** | Yes after cache/`cookie=0` rule | Delete-file proof is strong |
| **5** | Yes | Thin extension of existing tests |
| **6** | Yes | Full Rust + C + `test_fd_readdir_lots` + smokes |

---

### Corrected plan — testsuite alignment

- **Rust `fd_readdir`:** Empty dir (2), create file (3), `.`/`..` names, `d_ino` vs filestat, cookie resume, 1000-file walk — covered across cycles 2–4 and 6.
- **C `fdopendir-with-access`:** Real-file `d_ino` vs `fstatat`; cycle 3 `path_open` / osFile path; no `.`/`..` requirement — covered.
- **`group_a` `TestFdReaddir`:** Out of scope for rewrite; cycle 5 extends — consistent.

---

### Bottom line (corrected plan)

Execution-ready after clarifying **`cookie=0` refreshes the listing** (required for the Rust test’s second `cookie=0` read). Add `hostRoot` / `Info()` notes to reduce risk in cycles 1 and 3.
