#!/usr/bin/env bash
# Phase Guard / Quality Gate script
# Performs comprehensive quality checks before tests or commits.
# This script mirrors the functionality of TestQualityGate in quality_gate_test.go.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== Phase Guard Quality Check ==="
echo "Repository: $REPO_ROOT"

# Function to check git status is clean
check_git_clean() {
    local phase="$1"
    echo "Checking git status (phase: $phase)..."
    if ! git status --short | grep -q .; then
        echo "✓ Git status is clean"
        return 0
    else
        echo "✗ Git status is not clean:"
        git status --short
        return 1
    fi
}

# Function to run gofmt check
check_gofmt() {
    echo "Checking gofmt formatting..."
    local unformatted=$(gofmt -l .)
    if [ -z "$unformatted" ]; then
        echo "✓ All Go files are properly formatted"
        return 0
    else
        echo "✗ Files not formatted correctly:"
        echo "$unformatted"
        return 1
    fi
}

# Function to run golangci-lint
check_golangci_lint() {
    echo "Running golangci-lint..."
    if golangci-lint run "./..." >/dev/null 2>&1; then
        echo "✓ Linting passed"
        return 0
    else
        echo "✗ Linting failed"
        return 1
    fi
}

# Function to run Python adapter tests
check_python_adapter_tests() {
    echo "Running Python adapter tests..."
    local test_dir="$REPO_ROOT/wasi-testsuite"
    if [ -d "$test_dir" ]; then
        cd "$test_dir"
        if python3 -m pytest adapters/wasm2go_test.py -q >/dev/null 2>&1; then
            echo "✓ Python adapter tests passed"
            return 0
        else
            echo "✗ Python adapter tests failed"
            return 1
        fi
    else
        echo "⚠ Skipping Python adapter tests (wasi-testsuite not found)"
        return 0
    fi
}

# Function to build wasm2go-run
check_build() {
    echo "Building wasm2go-run..."
    local bin_dir="$REPO_ROOT/bin"
    mkdir -p "$bin_dir"
    cd "$REPO_ROOT"
    if go build -o ./bin/wasm2go-run ./cmd/wasm2go-run >/dev/null 2>&1; then
        echo "✓ Build succeeded"
        return 0
    else
        echo "✗ Build failed"
        return 1
    fi
}

# Function to cleanup artifacts
cleanup_artifacts() {
    echo "Cleaning up artifacts..."
    local artifacts=(
        ".pytest_cache"
        "wasi-testsuite/.pytest_cache"
        "wasi-testsuite/adapters/__pycache__"
    )
    for artifact in "${artifacts[@]}"; do
        if [ -e "$artifact" ] || [ -d "$artifact" ]; then
            rm -rf "$artifact" || true
        fi
    done
    echo "✓ Cleanup complete"
}

# Main execution
main() {
    # Ensure git is clean before starting quality checks
    if ! check_git_clean "before quality checks"; then
        echo "Error: Git must be clean before running phase guard."
        exit 1
    fi

    local failures=0

    # Run all checks
    check_gofmt || ((failures++))
    check_golangci_lint || ((failures++))
    check_python_adapter_tests || ((failures++))
    check_build || ((failures++))

    # Cleanup
    cleanup_artifacts

    # Final git status check
    if ! check_git_clean "after cleanup"; then
        echo "Warning: Git status not clean after cleanup"
    fi

    # Summary
    if [ $failures -gt 0 ]; then
        echo "=== Phase Guard Summary: $failures check(s) failed ==="
        exit 1
    else
        echo "=== Phase Guard Summary: All checks passed ✓ ==="
        exit 0
    fi
}

main "$@"