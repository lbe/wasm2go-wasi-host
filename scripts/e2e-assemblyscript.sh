#!/usr/bin/env bash
# E2E test: run the full wasi-testsuite assemblyscript suite against wasm2go-run.
# Expected result: 0 failures across all 12 tests.
# Usage: run from the repo root after building wasm2go-run.
#
#   go build -o ./wasm2go-run ./cmd/wasm2go-run
#   ./scripts/e2e-assemblyscript.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export WASM2GO_RUN="${WASM2GO_RUN:-$REPO_ROOT/wasm2go-run}"
export WASM2GO_WASIHOST_PATH="${WASM2GO_WASIHOST_PATH:-$REPO_ROOT}"

cd "$REPO_ROOT/wasi-testsuite"
exec python3 test-runner/wasi_test_runner.py \
    --runtime "$REPO_ROOT/adapters/wasm2go.py" \
    --test-suite tests/assemblyscript/testsuite/wasm32-wasip1
