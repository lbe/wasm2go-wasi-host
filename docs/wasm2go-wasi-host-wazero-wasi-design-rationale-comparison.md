# Analysis of `wasm2go-wasi-host` Compared to wazero WASI Design Rationale

## Overview

This document analyzes the architecture of `wasm2go-wasi-host` against the design principles and implementation rationale described in the wazero `RATIONALE.md` document.

Project under analysis:

- `wasm2go-wasi-host`
- Goal: bolt-on WASI Preview1 host for Go code transpiled via `wasm2go`
- Status: passes all wasi-testsuite Preview1 tests

The analysis focuses on:
- architectural alignment
- abstraction boundaries
- long-term maintainability
- WASI semantic correctness
- lessons learned from wazeroΓÇÖs implementation experience

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

This aligns directly with wazeroΓÇÖs core WASI philosophy:
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

This mirrors one of wazeroΓÇÖs strongest architectural lessons:

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

Your implementation benefits from wasm2goΓÇÖs direct memory exposure.

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

# 1. Over-Reliance on Go `fs.FS`

## Current Design

The implementation relies heavily on:
- `fs.FS`
- `fs.File`
- `fs.ReadDirFS`

Examples:
- `FSFileWrap`
- `DirEntriesFile`
- overlay fallback behavior
- embedded read-only mounts

## wazero Lesson

wazeroΓÇÖs rationale explains that GoΓÇÖs `fs.FS` abstraction eventually became insufficient for full WASI semantics.

The abstraction loses critical information:
- inode identity
- descriptor semantics
- mutation behavior
- hardlink behavior
- symlink policy
- OS-specific errno fidelity
- open-file lifecycle semantics

## Evidence Already Visible

The implementation is already compensating manually:
- overlay fallback logic
- direct host-path mutation bypasses
- custom offset tracking
- WriteAt fallbacks
- symlink special-casing

These are signs that abstraction impedance mismatch already exists.

## Recommendation

Introduce an internal WASI-native VFS abstraction layer.

Suggested direction:

```go
type WASIFile interface {
    ReadAt(...)
    WriteAt(...)
    Readdir(...)
    Stat(...)
    Rights()
    FileType()
    Inode()
}
```

Then adapt:
- `fs.FS`
- `os.File`
- embedded FS
- future memory FS

into that internal model.

This is likely the single most important long-term architectural improvement.

---

# 2. ΓÇ£ESUCCESS Without MutationΓÇ¥ Semantics

## Current Design

Several operations return success while performing no mutation for read-only `fs.FS` mounts.

Examples:
- `fd_filestat_set_size`
- `fd_filestat_set_times`

## Risk

This creates semantic ambiguity.

Guests may:
- assume persistence occurred
- cache incorrect metadata
- suppress retries
- mis-handle future behavior

## wazero Lesson

wazero strongly favors:
- explicit unsupported errors
- predictable failure semantics

over silent no-op success.

## Recommendation

Strongly consider returning:
- `EROFS`
- `ENOTSUP`
- `ENOTCAPABLE`

instead of silent success.

Especially for metadata mutation APIs.

This is likely the most questionable current semantic choice in the implementation.

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

# 4. poll_oneoff Evolution Risk

## Current Design

The implementation:
- correctly handles event memory layout
- supports clock subscriptions
- validates fd existence
- does not implement true readiness tracking

## Assessment

This is acceptable for the current synchronous design.

However, synchronous stubs can become permanent architectural constraints.

## Recommendation

Introduce an internal readiness abstraction now, even if stubbed.

Example:

```go
type Poller interface {
    SubscribeFD(...)
    Wait(...)
}
```

This preserves future evolution room:
- async runtimes
- socket support
- real readiness
- Preview2 evolution

without forcing large-scale refactors later.

---

# 5. WASI Preview1 Ambiguity Management

## Observation

One of wazeroΓÇÖs strongest lessons is:

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

# Strategic Recommendation

The implementation is currently positioned between:
- a thin Go stdlib adapter
and
- a true WASI-native runtime abstraction

The largest long-term risk is remaining indefinitely in the middle.

The strongest lesson from wazero is:

## Build explicit runtime abstractions early.

Particularly around:
- filesystem semantics
- descriptor lifecycle
- polling/readiness
- rights propagation
- inode identity

The current implementation already shows signs of pressure from abstraction mismatch.

Addressing this now will likely prevent significant technical debt later.

---

# Final Assessment

`wasm2go-wasi-host` is already architecturally impressive for an early-stage WASI host.

Particularly strong areas:
- capability confinement
- syscall semantic clarity
- ownership enforcement
- memory handling
- errno mapping
- explicit behavioral documentation
- wasi-testsuite compliance

The largest improvement opportunity is introducing a WASI-native internal VFS abstraction before the implementation grows substantially larger.

That is the single biggest architectural lesson to learn from wazeroΓÇÖs experience.

