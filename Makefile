.PHONY: test lint coverage install-dev-tools integration-test official-python-correctness

# Default target
all: fmt test lint

# Install development tools
install-dev-tools:
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	pip install pre-commit
	pre-commit install

# Run tests
test:
	go test -race ./...

# Run linter
lint:
	golangci-lint run

# Run tests with coverage
coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Clean up
clean: clean-server
	rm -f coverage.out coverage.html
	go clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS = -X main.Version=$(VERSION) -X main.Commit=$(COMMIT)

## build-server: build the turbopg-server binary
build-server:
	@echo "Building turbopg-server..."
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/turbopg-server ./cmd/turbopg-server

## run-server: build and run the turbopg-server
run-server: build-server
	@echo "Starting turbopg-server..."
	TURBOPG_ALLOW_INSECURE_API_KEY=1 ./bin/turbopg-server

## clean-server: remove the turbopg-server binary
clean-server:
	@echo "Cleaning turbopg-server..."
	rm -f bin/turbopg-server

# Run official tpuf-benchmark smoke against a local server (DATABASE_URL required).
benchmark-smoke: build-server
	@echo "Use CI or run tpufbench against a started turbopg-server. See .github/workflows/benchmark_test.yml"

# Official turbopuffer-python custom tests (allowlisted). Requires a running
# turbopg-server and TURBOPUFFER_BASE_URL / TURBOPUFFER_API_KEY.
official-python-correctness:
	bash tests/official-python/run.sh

# Run integration tests
integration-test:
	go test -race -tags=integration ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .
