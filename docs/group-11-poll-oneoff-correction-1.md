# Group 11 `poll_oneoff` correction plan

Corrections on **`group-11-poll-oneoff`** before merge: gaps from review (missing invalid-fd tests, bad combined unit-test layout, `poll_oneoff` sleep/clock semantics vs `poll_oneoff_stdio`, fragile tag parsing, doc/test housekeeping).

**Branch:** `group-11-poll-oneoff` — do not create a new branch.

**Implementation file:** `wasihost_misc.go` — `Xpoll_oneoff` only (see `.cursor/rules/wasihost-layout.mdc`)

**Authoritative guest behavior:** `wasi-testsuite/tests/rust/wasm32-wasip1/src/bin/poll_oneoff_stdio.rs`

**Regression gate:** `go test -run TestGroup11WasiTestsuiteInventoryPasses -count=1` and `go test ./...`

---

## Prerequisites

- Checked out on `group-11-poll-oneoff`; e2e `poll_oneoff_stdio` already passes on that branch.
- Do not move `poll_oneoff` into `wasihost.go` or reintroduce stubs in `wasihost_path.go`.

## Out of scope

- Group 12 sockets
- Real async readiness for non-stdio fds
- New wasi-testsuite cases beyond `poll_oneoff_stdio`
- Unrelated `group_a` tests

---

## Background

| Issue | Impact |
|-------|--------|
| Combined unit test uses `inPtr=100`, `outPtr=200` | Output events overwrite subscription 2; “no clock” assertion is a false negative |
| `Xpoll_oneoff` sleeps `minTimeout` before emitting fds | Adds 200ms latency; wrong order vs `test_stdout_stderr_write` |
| No rule to defer clocks when `fd_write` satisfied | Long-timeout clocks can appear in same poll as immediate `fd_write` |
| Original plan cycle 6 never landed | No `TestPollOneoffInvalidFd`; `group_a` invalid-fd case omits `nevents` |
| Tag read as `Uint32` at offset 8 | Fragile vs WASI `u8` tag + padding |
| Doc checklist, duplicate clock tests, stale comments | Housekeeping |

**Synchronous host model (unchanged):** stdio fds 0–2 are poll-ready for `fd_read` / `fd_write`; clocks use `time.Sleep` for the minimum positive timeout among subscriptions that actually fire.

**Mixed-poll rules (target semantics):**

1. Emit all ready `fd_read` / `fd_write` events first — no sleep beforehand.
2. If any **FD_WRITE** event was emitted in this call, do **not** emit **CLOCK** events with `timeout > 0` in the same call (guest drains writable in a loop; clock fires on a later `poll_oneoff`).
3. **FD_READ** does **not** suppress clocks: `clock 1ns` + `fd_read` on stdin still returns two events in one call (`test_stdin_read`).
4. Clock-only polls: sleep `minTimeout` when `> 0`, then emit clocks whose `timeout == minTimeout`.

---

## Execution order

Complete tasks in order. Run the listed `go test` commands after each task; finish with full suite + e2e.

### Task 1 — Fix combined unit-test memory layout

**Files:** `group_11_poll_test.go`

In `TestPollOneoffFdWriteStdio` subtest `combined: fd_write on stdout and stderr with clock timeout`:

- Set `outPtr` so the output region does not overlap subscriptions, e.g. `inPtr=100`, `outPtr=1000` (same as `TestPollOneoffNeventsPacked`).
- Optionally assert subscription 2 tag byte at `inPtr+2*48+8` remains `0` after `Xpoll_oneoff`.

**Verify:**

```bash
go test -run 'TestPollOneoffFdWriteStdio/combined' -count=1
```

---

### Task 2 — Strengthen combined unit-test assertions

**Files:** `group_11_poll_test.go`

After Task 1 offsets, extend the combined subtest:

- `nevents == 2`
- For each event `i` in `[0, nevents)`: byte at `outPtr + i*32 + 10` is `2` (FD_WRITE); none is `0` (CLOCK)
- Wall time for `Xpoll_oneoff` **&lt; 50ms** (must not sleep 200ms before returning)

Expect failure until Task 4 is done.

**Verify:**

```bash
go test -run 'TestPollOneoffFdWriteStdio/combined' -count=1
```

---

### Task 3 — Emit fd events before clock sleep

**Files:** `wasihost_misc.go`

Refactor `Xpoll_oneoff` so the delivery pass writes **all** signaling `fd_read` / `fd_write` subscriptions before any `time.Sleep` for clocks.

Keep existing behavior for clock-only and `fd_read`+short-clock cases (`TestPollOneoffFdReadStdin`, `TestPollOneoffNeventsPacked`, etc.).

**Verify:**

```bash
go test -run 'TestPollOneoffFdReadStdin|TestPollOneoffNeventsPacked|TestPollOneoffEventLayout' -count=1
```

Combined subtest may still fail until Task 4.

---

### Task 4 — Defer clocks when fd_write was delivered

**Files:** `wasihost_misc.go`

In the same `Xpoll_oneoff` call: if at least one **FD_WRITE** event was written, skip **CLOCK** subscriptions with `timeout > 0` (do not sleep for them in that call either, if the only clocks left are deferred).

After fd_write-only delivery, a later poll with only the clock (or remaining subs) may sleep and emit the clock — matching `poll_oneoff_stdio.rs` `test_stdout_stderr_write`.

**Must preserve:** `TestPollOneoffFdReadStdin` subtest `clock 1ns + fd_read fd=0` → `nevents == 2`, one CLOCK and one FD_READ.

**Verify:**

```bash
go test -run 'TestPollOneoffFdWriteStdio|TestPollOneoffFdReadStdin' -count=1
```

---

### Task 5 — Invalid fd packed events (missing original cycle 6)

**Files:** `group_11_poll_test.go`, `group_a_fd_test.go`

Add `TestPollOneoffInvalidFd`:

| Case | Expect |
|------|--------|
| `fd_read` fd=99 alone | `nevents==1`, errno `EBADF` (8) at `outPtr+8`, type FD_READ at `outPtr+10` |
| Clock 1ns + `fd_read` fd=99 | `nevents==2`; CLOCK SUCCESS at `outPtr+0`; EBADF FD_READ at `outPtr+32` |

Update `TestXpollOneoff` / `fd_read invalid fd`: assert `nevents == 1` in addition to errno `EBADF`.

Implementation likely already sets `errno` for bad fds; confirm packed layout.

**Verify:**

```bash
go test -run 'TestPollOneoffInvalidFd|TestXpollOneoff/fd_read_invalid' -count=1
```

---

### Task 6 — Read subscription tag as `u8`

**Files:** `wasihost_misc.go`, `group_11_poll_test.go`

- Add test (e.g. `TestPollOneoffTagU8`): `fd_read` with `mem[inPtr+8]==1` and non-zero bytes at `inPtr+9..11`; expect `nevents==1`, type FD_READ.
- In both scan passes of `Xpoll_oneoff`, dispatch on `mem[subOffset+8]` (or `uint8` read), not `Uint32` at tag.

**Verify:**

```bash
go test -run 'TestPollOneoffTagU8|TestPollOneoffEventLayout' -count=1
```

---

### Task 7 — Test and doc cleanup

**Files:** `group_a_fd_test.go`, `group_11_poll_test.go`, `group_11_poll_timeout_test.go`, `poll_oneoff_fd_read_stdin_test.go`, `docs/wasi-testsuite-correction-sequence-plan.md`

1. `TestXpollOneoff` / `clock subscription`: write timeout at `inPtr+24`, not `inPtr+16`.
2. Merge duplicate 1ms/5ms clock tests (`TestPollOneoffClockRespectsMinimumTimeout` vs `group_11_poll_timeout_test.go`) into one test (e.g. `TestPollOneoffClock`) in one file.
3. Remove stale “may not handle fd_read” comments from `poll_oneoff_fd_read_stdin_test.go`.
4. Mark Group 11 `poll_oneoff_stdio` as done (☑) in `docs/wasi-testsuite-correction-sequence-plan.md`.

If any `wasihost*.go` changed: `goimports -w wasihost*.go path_link_*.go`

**Verify:**

```bash
go test ./...
```

---

### Task 8 — E2E regression

**Verify:**

```bash
go test -run TestGroup11WasiTestsuiteInventoryPasses -count=1
go test ./...
```

No change to `e2e_helpers_test.go` `stdioOnly` unless broken.

---

## Done criteria

- [ ] Tasks 1–8 complete
- [ ] `TestPollOneoffFdWriteStdio/combined` passes with non-overlapping buffers, no CLOCK in output, fast return
- [ ] `TestPollOneoffInvalidFd` and strengthened `group_a` invalid-fd case pass
- [ ] Tag `u8` test passes
- [ ] `poll_oneoff_stdio` e2e still passes
- [ ] Group 11 row ticked in correction sequence plan

## References

- Review source: branch review vs `.pi/tdd-plans/group-11-poll-oneoff.yaml`
- Layout: `wasihost_misc.go` comments (~offset 230–260), `ARCHITECTURE.md`
- E2E: `e2e_group11_test.go`, `e2e_helpers_test.go` (`stdioOnly`)
