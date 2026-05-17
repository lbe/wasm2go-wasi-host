.PHONY: help build clean test test-race lint format cover

# Default target: display help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build       Build the wasm2go-run binary to ./bin"
	@echo "  clean       Remove build artifacts and temporary files"
	@echo "  test        Run all tests with coverage"
	@echo "  test-race   Run all tests with the race detector enabled"
	@echo "  lint        Run golangci-lint"
	@echo "  format      Run go fmt"
	@echo "  cover       Open the test coverage report in a browser"

build:
	@mkdir -p bin
	go build -o bin/wasm2go-run ./cmd/wasm2go-run

clean:
	rm -rf bin/
	rm -f coverage.out

test:
	go test ./... -cover -count=1

test-race:
	go test ./... -race -count=1

lint:
	golangci-lint run

format:
	go fmt ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out
