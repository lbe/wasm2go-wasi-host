#!/usr/bin/env bash
# E2E compliance inventory: run all WASI Preview1 suites from the wasi-testsuite submodule.
# Expected current result: the command executes every discovered wasm32-wasip1 suite;
# some C/Rust tests may fail until follow-up WASI compliance work is completed.
# Usage: run from the repo root after building wasm2go-run.
#
#   go build -o ./bin/wasm2go-run ./cmd/wasm2go-run
#   ./scripts/e2e-wasip1.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export WASM2GO_RUN="${WASM2GO_RUN:-$REPO_ROOT/bin/wasm2go-run}"
export WASM2GO_WASIHOST_PATH="${WASM2GO_WASIHOST_PATH:-$REPO_ROOT}"

cd "$REPO_ROOT/wasi-testsuite"

suites=()
while IFS= read -r suite; do
    suites+=("$suite")
done < <(find tests -path '*/testsuite/wasm32-wasip1' -type d | sort)

if [[ ${#suites[@]} -eq 0 ]]; then
    echo "No WASI Preview1 test suites found in wasi-testsuite" >&2
    exit 1
fi

echo "Running WASI Preview1 suites with wasm2go-run:"
printf '  %s\n' "${suites[@]}"

exec python3 test-runner/wasi_test_runner.py \
    --runtime-adapter adapters/wasm2go.py \
    --test-suite "${suites[@]}"
