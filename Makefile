.PHONY: help build clean test test-e2e test-race test-race-e2e lint format cover cover-e2e

# Default target: display help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build       Build the wasm2go-run binary to ./bin"
	@echo "  clean       Remove build artifacts and temporary files"
	@echo "  test        Run unit tests (excludes -tags=e2e integration tests)"
	@echo "  test-e2e    Run full test suite including wasi-testsuite e2e"
	@echo "  test-race     Run unit tests with the race detector enabled"
	@echo "  test-race-e2e Run full suite with the race detector enabled"
	@echo "  lint        Run golangci-lint"
	@echo "  format      Run go fmt"
	@echo "  cover       Unit test coverage report (opens browser)"
	@echo "  cover-e2e   Full test coverage report including e2e tag (opens browser)"

build:
	@mkdir -p bin
	go build -o bin/wasm2go-run ./cmd/wasm2go-run

clean:
	rm -rf bin/
	rm -f coverage.out

test:
	go test ./... -cover -count=1

test-e2e:
	go test -tags=e2e ./... -cover -count=1

test-race:
	go test ./... -race -count=1

test-race-e2e:
	go test -tags=e2e ./... -race -count=1

lint:
	golangci-lint run

format:
	go fmt ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

cover-e2e:
	go test -tags=e2e -coverprofile=coverage-e2e.out ./...
	go tool cover -html=coverage-e2e.out
