# Analysis of `wasm2go-wasi-host` Compared to wazero WASI Design Rationale

> **Document status:** Living comparison — not a snapshot of pre-refactor code.
> **Last reviewed:** 2026-05-30 (post `internal/wasi` VFS migration on `wazero-alignment-cleanup`).
> **When comparing again:** Read `ARCHITECTURE.md`, `docs/design-improvements-2.md`, and `docs/path-confinement.md` first; update the [Implementation status](#implementation-status) section before citing "Current Design" bullets below.

## Overview

This document analyzes the architecture of `wasm2go-wasi-host` against the design principles and implementation rationale described in the wazero `RATIONALE.md` document.

Project under analysis:

- `wasm2go-wasi-host`
- Goal: bolt-on WASI Preview1 host for Go code transpiled via `wasm2go`
- Status: passes all wasi-testsuite Preview1 tests (`go test -tags=e2e ./...`)

The analysis focuses on:
- architectural alignment
- abstraction boundaries
- long-term maintainability
- WASI semantic correctness
- lessons learned from wazero's implementation experience

## Implementation status

| Topic | Pre-refactor (historical) | Current (2026-05) | Remaining work |
|-------|---------------------------|-------------------|----------------|
| VFS abstraction | Ad-hoc `FSFileWrap`, `osFile`, overlay fallback | `internal/wasi` + `internal/wasi/sysfs` (`DirFS`, `ReadFS`, `AdaptFS`); legacy adapters removed | Path resolution still in `wasihost_path_resolve.go` (~630 lines); split deferred |
| Config vs runtime | `Option func(*State)` mixed into syscall bodies | `ModuleConfig` → `wasi.Context` → `State.ctx` | — |
| Silent ESUCCESS on metadata mutation | `fd_filestat_set_size/times` no-op on read-only | Routed through `fe.File.Truncate` / `Utimens`; `ReadFS` returns `EROFS` | — |
| Overlay on writable mounts | Three-tier open with `fs.FS` fallback | Single VFS backend per preopen (intentional break) | Document for embedders migrating from overlay behavior |
| poll_oneoff readiness | Immediate emit for all open fds | Stdin consults `Pollable`; other fds still stub-ready | TDD plan `wazero-alignment-cleanup-2` cycles 7–8: `PollableFile` on `osFile` |
| Linux portability of ported wazero code | N/A | Darwin-only gaps in `stat.go` timespec fields and `withSyscallOflag` | TDD plan `wazero-alignment-cleanup-2` cycles 1–2, 9 |
| Confinement / symlinks | Strong lexical rules, some host bypasses | VFS-first path stat; known gaps in root-mount follow and read-only readlink | TDD plan `wazero-alignment-cleanup-2` cycles 3–6 |

Sections marked **(historical)** describe the pre-migration codebase for context. Sections marked **(current)** reflect the tree after design-improvements-2 Phases 1–6.

---

# High-Level Assessment

Architecturally, `wasm2go-wasi-host` is already significantly stronger than most minimal WASI hosts.

The implementation demonstrates:
- explicit semantic documentation
- correct capability-oriented filesystem design
- strong syscall-level reasoning
- clear ownership semantics
- correct memory-growth handling
- thoughtful errno mapping
- strong WASI Preview1 behavioral coverage

The project already resembles a serious embedded WASI subsystem rather than a thin compatibility shim.

The largest remaining architectural concern is long-term abstraction strategy around filesystem semantics and descriptor behavior.

---

# Areas Where `wasm2go-wasi-host` Aligns Strongly with wazero

---

# 1. Capability-Oriented Filesystem Design

## Current Design

The implementation centers filesystem access around:
- preopened directories
- explicit rights
- lexical confinement
- mount-root enforcement

Key APIs:

- `WithReadOnlyFS`
- `WithHostDirectoryPreopen`

Documented behaviors include:
- sandbox escape prevention
- rights masking
- read-only enforcement
- symlink policy handling

## Comparison to wazero

This aligns directly with wazero's core WASI philosophy:
- WASI is capability-oriented
- no ambient filesystem authority
- explicit resource provisioning

## Assessment

This is one of the strongest parts of the implementation.

The explicit lexical confinement behavior:
- preventing mount escape
- preserving trailing slash semantics
- handling symlink-follow rules carefully

is significantly better documented than many runtimes.

## Recommendation

Retain this design philosophy.

Consider extracting the path resolution and confinement rules into a standalone architecture document because this is one of the hardest parts of WASI correctness.

---

# 2. Centralized Runtime State

## Current Design

The `State` object owns:
- fd table
- argv/env
- stdio
- mount table
- clock behavior
- rights state
- WASI syscall implementation

## Comparison to wazero

This mirrors one of wazero's strongest architectural lessons:

Avoid scattering WASI process state across unrelated syscall handlers.

## Assessment

Correct architectural direction.

This minimizes:
- hidden coupling
- inconsistent policy handling
- duplicated resource tracking

## Recommendation

Continue treating `State` as the authoritative process boundary.

Avoid future fragmentation into subsystem-owned global state.

---

# 3. Explicit Concurrency Contract

## Current Design

The implementation explicitly states:
- one State per module
- single goroutine ownership
- non-concurrent usage

Optional enforcement:
- `WithOwnerAssertion`

## Comparison to wazero

wazero strongly emphasizes avoiding implicit concurrency assumptions.

Your implementation goes further by:
- documenting ownership clearly
- optionally enforcing it at runtime

## Assessment

Excellent design decision.

Particularly important because:
- guest callbacks can corrupt fd state
- WASI fd offsets are mutable
- transpiled Go guests may assume single-threaded execution

## Recommendation

Keep this model.

Avoid hidden locking unless true multithreaded WASM support becomes a primary goal.

---

# 4. Memory Growth Handling

## Current Design

Guest memory is retrieved dynamically on every syscall:

```go
func() []byte { return *mod.Xmemory().Slice() }
```

The callback is re-invoked every syscall to safely handle memory growth.

## Comparison to wazero

wazero internally solves the same issue through runtime-managed memory views.

Your implementation benefits from wasm2go's direct memory exposure.

## Assessment

This is one of the cleanest parts of the architecture.

It avoids:
- stale memory references
- invalid slices after growth
- hidden synchronization issues

## Recommendation

Retain this pattern.

This is likely one of the strongest architectural advantages of the wasm2go integration model.

---

# 5. proc_exit via panic/recover

## Current Design

`proc_exit` panics with `ExitError`.

Embedders recover at the module boundary.

## Comparison to wazero

wazero independently arrived at the same conclusion.

## Assessment

Correct design choice for Go-hosted WASM execution.

This provides:
- reliable unwinding
- minimal complexity
- clear process termination semantics

## Recommendation

Keep this behavior.

---

# 6. Socket Handling Strategy

## Current Design

Sockets are intentionally unsupported.

Behavior:
- `ENOSYS`
- `ENOTSOCK`
- `EBADF`

## Comparison to wazero

wazero similarly avoids pretending unsupported features exist.

## Assessment

Correct strategic choice.

Fake socket behavior creates semantic debt.

## Recommendation

Continue failing explicitly rather than partially emulating unsupported socket semantics.

---

# Major Architectural Risks and Lessons from wazero

---

# 1. Over-Reliance on Go `fs.FS` — **(historical → largely resolved)**

## Historical design (pre-`internal/wasi`)

The implementation once relied heavily on:
- `fs.FS` / `fs.File` / `fs.ReadDirFS` directly in syscall bodies
- `FSFileWrap`, `DirEntriesFile`, overlay fallback on writable mounts
- ad-hoc host-path bypasses and custom readdir snapshots

## Current design

Filesystem semantics now go through **`internal/wasi`** (`FS`, `File`) and **`internal/wasi/sysfs`** adapters:
- **`DirFS`** — writable host directory
- **`ReadFS`** — write-denying wrapper (`EROFS` at VFS boundary)
- **`AdaptFS`** — read-only `fs.FS` → `sys.FS`
- **`wasi.FSContext`** — fd table, `DirentCache`, stdio as regular entries

Legacy adapters (`FSFileWrap`, overlay fallback) are **removed**. Embedders still pass `fs.FS` for read-only preopens, but WASI methods consume `wasi.File`, not `fs.File` type switches.

Path confinement and guest-path resolution remain in **`wasihost_path_resolve.go`** (~630 lines) — the main boundary not yet folded into `internal/wasi`.

## wazero lesson

wazero's rationale explains that Go's `fs.FS` abstraction eventually became insufficient for full WASI semantics.

The abstraction loses critical information:
- inode identity
- descriptor semantics
- mutation behavior
- hardlink behavior
- symlink policy
- OS-specific errno fidelity
- open-file lifecycle semantics

## Evidence that motivated the migration (historical)

Before `internal/wasi`, the codebase compensated manually:
- overlay fallback logic
- direct host-path mutation bypasses
- custom offset tracking and readdir snapshots
- symlink special-casing in syscall bodies

Those pressures led to **design-improvements-2** and the wazero VFS port (see `ARCHITECTURE.md`).

## Recommendation — status

~~Introduce an internal WASI-native VFS abstraction layer.~~ **Done** (`internal/wasi`, `internal/wasi/sysfs`).

**Still open:**
- Extract or slim `wasihost_path_resolve.go` (maintainability; tracked outside wasi-alignment-cleanup-2).
- Keep `docs/path-confinement.md` aligned when confinement behavior changes.

---

# 2. "ESUCCESS Without Mutation" Semantics — **(historical → resolved for filestat)**

## Historical design

Several operations once returned success while performing no mutation for read-only `fs.FS` mounts, notably `fd_filestat_set_size` and `fd_filestat_set_times`.

## Current design

Metadata mutations route through **`fe.File.Truncate`** and **`fe.File.Utimens`**. **`ReadFS`** returns **`EROFS`** for writes at the VFS layer. WASI methods propagate errno via `errnoFromWasi` — no silent success for those paths.

## wazero lesson

wazero strongly favors:
- explicit unsupported errors
- predictable failure semantics

over silent no-op success.

## Recommendation — status

~~Strongly consider returning EROFS / ENOTSUP / ENOTCAPABLE instead of silent success.~~ **Done for filestat size/times** via VFS routing.

When auditing new syscalls, keep the rule: **propagate VFS errno; do not invent ESUCCESS no-ops.**

---

# 3. FD Offset Semantics

## Current Design

The implementation tracks:
- WASI-level fd offsets
- seek state
- sequential read/write positions

through internal fd-table state.

## Assessment

This is correct for single-threaded ownership.

However, descriptor semantics become difficult over time:
- renumbering
- aliasing
- dup-like behavior
- future threading
- callback reentrancy

## Recommendation

Explicitly document whether offsets belong to:
- fd numbers
or
- open file descriptions

POSIX semantics attach offsets to the open file description.

The wording in `fd_renumber` suggests the implementation is already modeling this correctly.

Document it explicitly now before the implementation grows further.

---

# 4. poll_oneoff Evolution Risk — **(current — partial)**

## Current design

The implementation:
- correctly handles event memory layout
- supports clock subscriptions
- validates fd existence
- consults **`Pollable`** for stdin (`StdinFile` / injected readers)
- **still emits immediately** for host-backed fds that do not implement **`PollableFile`** (e.g. `sysfs.osFile` today)

## Assessment

Acceptable for synchronous single-goroutine execution **only if** `fd_fdstat_set_flags` NONBLOCK and `poll_oneoff` stay consistent. Today they can diverge: guest sees NONBLOCK in fdstat while `poll_oneoff` reports ready and `fd_read` returns `EAGAIN`.

## Recommendation — status

Introduce readiness at the **`PollableFile`** boundary (wazero pattern), not a separate global `Poller` yet:

- **`sysfs.osFile`**: implement `Poll` + `SetNonblock` (plan: `wazero-alignment-cleanup-2` cycles 7–8).
- **`poll_oneoff`**: consult `PollableFile` before emitting fd_read/fd_write events (stdin path already does via `Pollable`).

A full async `Poller` subscription API remains optional future work for Preview2 / sockets.

---

# 5. WASI Preview1 Ambiguity Management

## Observation

One of wazero's strongest lessons is:

WASI Preview1 contains underspecified behaviors.

Examples include:
- symlink semantics
- prestat behavior
- directory iteration
- fd rights propagation
- errno selection

## Assessment

Your implementation already demonstrates careful thought in these areas.

Particularly strong:
- path_open behavior
- symlink handling
- trailing slash preservation
- rights clamping
- lexical confinement

## Recommendation

Continue documenting behavioral choices aggressively.

Treat explicit semantic documentation as part of the runtime API.

This will become increasingly valuable as:
- Preview2 adoption grows
- compatibility expectations increase
- edge-case behavior matters more

---

# Strategic Recommendation — **(updated)**

The project has moved from "thin stdlib adapter" toward **wazero-aligned VFS + Context consumption**. The largest remaining risk is **not** missing a VFS layer — it is **finishing portability and edge-case correctness** on top of that layer while **`wasihost_path_resolve.go` stays monolithic**.

Priority order (2026-05):

1. **Linux build parity** for ported wazero code (`stat.go`, `open_file_*` oflags).
2. **Confinement and symlink correctness** (root-mount follow, read-only filestat follow, readlink, readdir `..` inode via VFS only) — see `wazero-alignment-cleanup-2` TDD plan.
3. **`PollableFile` on host fds** so poll and NONBLOCK agree.
4. **Maintainability** — split path resolution and oversized `syscall_*_test.go` files (separate plan).

Build explicit runtime abstractions **early** — **done for FS/File**. Continue applying the same discipline to polling and path policy documentation.

---

# Final Assessment — **(updated 2026-05)**

`wasm2go-wasi-host` is a capable embedded WASI host with wazero-derived VFS internals and strong confinement documentation.

**Strengths (unchanged):**
- capability confinement and explicit path policy
- syscall semantic clarity and ownership enforcement
- memory callback on every syscall
- wasi-testsuite compliance (72 cases, `-tags=e2e`)

**Resolved since original write-up:**
- internal WASI-native VFS (`internal/wasi`, `sysfs`)
- `ModuleConfig` / `Context` decoupling
- explicit `EROFS` for read-only metadata mutation via VFS

**Active gaps (see Implementation status table and `.pi/tdd-plans/wazero-alignment-cleanup-2.yaml`):**
- Linux portability bugs in ported stat/open helpers
- confinement/symlink edge cases on root and read-only mounts
- `PollableFile` for host-backed fds

When re-running this comparison, update the **Implementation status** table first — do not treat historical bullets as current behavior.

