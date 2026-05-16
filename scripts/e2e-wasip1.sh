#!/usr/bin/env bash
# Run the wasi-testsuite submodule using its own authoritative test discovery.
# Set WASM2GO_RUN to test a specific wasm2go-run binary; otherwise the adapter uses PATH.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT/wasi-testsuite"
exec python3 ./run-tests -r adapters/wasm2go.py "$@"
