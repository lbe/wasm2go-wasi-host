# Move wasi-testsuite Integration into the Submodule

## Decision

The `wasm2go-wasi-host` repository must not vendor or duplicate `wasi-testsuite` harness infrastructure.

Responsibilities are split as follows:

- `wasm2go-wasi-host` owns the Go WASI host implementation and the `cmd/wasm2go-run` executable.
- The forked `lbe/wasi-testsuite` submodule owns the wasi-testsuite runner, test inventory, manifests, fixtures, and the `wasm2go` runtime adapter.
- This repository keeps only thin convenience scripts that build/use `wasm2go-run` and invoke the submodule's runner.

The target scope is **all WASI Preview1 tests** in the submodule, not only AssemblyScript.

## Checklist

### 1. Create the fork branch and add the wasm2go adapter there

- [ ] In `wasi-testsuite/`, start from the existing fork base branch:
  ```bash
  git fetch origin
  git checkout prod/testsuite-base
  git pull --ff-only origin prod/testsuite-base
  ```
- [ ] Create the dedicated integration branch from `prod/testsuite-base`:
  ```bash
  git checkout -b prod/testsuite-with-wasm2go
  ```
- [ ] Make all wasi-testsuite-side changes on `prod/testsuite-with-wasm2go` only.
- [ ] Move the runtime adapter implementation from this repository into the submodule:
  - from `adapters/wasm2go.py`
  - to `wasi-testsuite/adapters/wasm2go.py`
- [ ] Move the adapter unit test from this repository into the submodule:
  - from `adapters/wasm2go_test.py`
  - to `wasi-testsuite/adapters/wasm2go_test.py`
- [ ] Ensure the adapter uses `WASM2GO_RUN` as an override and defaults to `wasm2go-run` on `PATH`.
- [ ] Ensure the adapter reports only WASI Preview1 support:
  - `get_wasi_versions() == ["wasm32-wasip1"]`
  - `get_wasi_worlds() == ["wasi:cli/command"]`
- [ ] Keep the adapter limited to single-test command construction. It must not discover suites, select `.wasm` files, read test manifests, or encode AssemblyScript/C/Rust inventory.
- [ ] Ensure the adapter unit test covers only adapter behavior:
  - `get_name()`
  - `get_version()`
  - `get_wasi_versions()`
  - `get_wasi_worlds()`
  - `compute_argv(...)`
  - `WASM2GO_RUN` override handling
  - args/env/dirs command-line translation
- [ ] Run the adapter unit test inside the submodule:
  ```bash
  cd wasi-testsuite
  python3 -m pytest adapters/wasm2go_test.py -q
  ```
- [ ] Commit the adapter change inside `wasi-testsuite/` on `prod/testsuite-with-wasm2go`.
- [ ] Push the branch to `https://github.com/lbe/wasi-testsuite`:
  ```bash
  git push -u origin prod/testsuite-with-wasm2go
  ```

### 2. Update this repository's submodule pointer

- [ ] Update `.gitmodules` so the submodule tracks the new fork branch explicitly:
  ```ini
  [submodule "wasi-testsuite"]
  	path = wasi-testsuite
  	url = https://github.com/lbe/wasi-testsuite
  	branch = prod/testsuite-with-wasm2go
  ```
- [ ] Update the `wasi-testsuite` submodule pointer to the `prod/testsuite-with-wasm2go` commit containing `adapters/wasm2go.py`.
- [ ] Verify a fresh submodule update obtains the adapter:
  ```bash
  git submodule sync --recursive
  git submodule update --init --recursive
  test -f wasi-testsuite/adapters/wasm2go.py
  test -f wasi-testsuite/adapters/wasm2go_test.py
  ```

### 3. Remove duplicated adapter files from this repository

- [ ] Remove this repository's root-level adapter files:
  - `adapters/wasm2go.py`
  - `adapters/wasm2go_test.py`
- [ ] Remove the root `adapters/` directory.
- [ ] Remove references to root-level `../adapters/wasm2go.py` and `adapters/wasm2go_test.py` from scripts, docs, tests, and plans.
- [ ] Remove root-level Python adapter test invocations from this repository's tests and quality gate.

### 4. Replace the AssemblyScript-only E2E script with an all-WASI-Preview1 runner

- [ ] Remove `scripts/e2e-assemblyscript.sh`.
- [ ] Add `scripts/e2e-wasip1.sh`.
- [ ] The new script must use the adapter from the submodule:
  ```text
  wasi-testsuite/adapters/wasm2go.py
  ```
- [ ] The new script must discover every `wasm32-wasip1` test suite present in the submodule with:
  ```bash
  find tests -path '*/testsuite/wasm32-wasip1' -type d | sort
  ```
- [ ] The new script must set these defaults:
  ```bash
  export WASM2GO_RUN="${WASM2GO_RUN:-$REPO_ROOT/bin/wasm2go-run}"
  export WASM2GO_WASIHOST_PATH="${WASM2GO_WASIHOST_PATH:-$REPO_ROOT}"
  ```
- [ ] The new script must print the discovered suite inventory before running.
- [ ] The new script must call the submodule's real test runner:
  ```bash
  python3 test-runner/wasi_test_runner.py \
    --runtime-adapter adapters/wasm2go.py \
    --test-suite "${suites[@]}"
  ```
- [ ] The script must exit non-zero when any WASI Preview1 test fails.
- [ ] Do not copy `wasi-testsuite` runner code, manifests, fixtures, or test discovery logic into this repository.

Expected initial discovered suites printed by `scripts/e2e-wasip1.sh` after it changes into `wasi-testsuite/`:

```text
tests/assemblyscript/testsuite/wasm32-wasip1
tests/c/testsuite/wasm32-wasip1
tests/rust/testsuite/wasm32-wasip1
```

### 5. Add an explicit all-Preview1 compliance command without making it the normal quality gate yet

All WASI Preview1 tests must be runnable immediately. Many are expected to fail until follow-up compliance work is completed.

- [ ] Treat `scripts/e2e-wasip1.sh` as the explicit red compliance inventory command for subsequent work.
- [ ] Do not require `scripts/e2e-wasip1.sh` to pass before completing this separation task.
- [ ] Do not invoke `scripts/e2e-wasip1.sh` from `TestQualityGate` in this separation task.
- [ ] Remove the old AssemblyScript-only E2E invocation from `TestQualityGate`.
- [ ] Keep `TestQualityGate` focused on checks that pass after the separation is complete:
  - Go formatting/linting checks already in scope.
  - Go unit/integration tests that are not the known-red all-Preview1 compliance run.
  - Submodule adapter unit test from `wasi-testsuite/adapters/wasm2go_test.py`.
- [ ] Add a follow-up note in docs that `scripts/e2e-wasip1.sh` is promoted into the mandatory quality gate only after all Preview1 C and Rust failures are fixed.

### 6. Update repository documentation

- [ ] Update `README.md` to state that the project targets all WASI Preview1 tests in `wasi-testsuite`.
- [ ] Update `cmd/wasm2go-run/README.md` to describe running all Preview1 suites, not only AssemblyScript samples.
- [ ] Remove these AssemblyScript-only scope phrases everywhere they appear:
  - "AssemblyScript E2E tests"
  - "AssemblyScript samples"
  - "zero failures across all 12 tests"
- [ ] Document that C and Rust Preview1 failures are expected initially until follow-up compliance tasks fix them.
- [ ] Document the exact all-Preview1 command:
  ```bash
  go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
  ./scripts/e2e-wasip1.sh
  ```

### 7. Verify the separation

- [ ] Build the runner from this repository:
  ```bash
  go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
  ```
- [ ] Run the submodule adapter unit test:
  ```bash
  cd wasi-testsuite
  python3 -m pytest adapters/wasm2go_test.py -q
  ```
- [ ] Return to this repository root:
  ```bash
  cd ..
  ```
- [ ] Run the all-Preview1 compliance command from this repository:
  ```bash
  ./scripts/e2e-wasip1.sh
  ```
- [ ] Confirm that `scripts/e2e-wasip1.sh` invokes all Preview1 suites from the submodule.
- [ ] Confirm that `scripts/e2e-wasip1.sh` exits non-zero because of current WASI compliance failures.
- [ ] Confirm that `scripts/e2e-wasip1.sh` failures are real WASI compliance failures, not adapter path or harness integration failures.
- [ ] Confirm this repository no longer contains duplicated wasi-testsuite adapter/harness infrastructure.
- [ ] Confirm `git status --short` is clean in this repository after committing the parent-repo changes.
- [ ] Confirm `git -C wasi-testsuite status --short` is clean after committing and pushing the submodule branch.

## Expected initial result

After this separation, running all Preview1 tests is expected to fail until follow-up work fixes compliance gaps.

The important correction is that the inventory is honest:

```text
all WebAssembly/wasi-testsuite wasm32-wasip1 suites are in scope and are executed
```

Subsequent tasks must use the failing C and Rust tests as the compliance backlog.
