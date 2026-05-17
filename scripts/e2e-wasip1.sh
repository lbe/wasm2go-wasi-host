#!/usr/bin/env bash
# Run the wasi-testsuite submodule using its own authoritative test discovery.
# Defaults to this repository's local ./bin/wasm2go-run and source tree.
# Set WASM2GO_RUN or WASM2GO_WASIHOST_PATH to override either path.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

export WASM2GO_RUN="${WASM2GO_RUN:-$REPO_ROOT/bin/wasm2go-run}"
export WASM2GO_WASIHOST_PATH="${WASM2GO_WASIHOST_PATH:-$REPO_ROOT}"

cd "$REPO_ROOT/wasi-testsuite"
exec python3 ./run-tests -r adapters/wasm2go.py "$@"
