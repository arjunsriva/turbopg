.PHONY: test lint coverage install-dev-tools integration-test

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
clean:
	rm -f coverage.out coverage.html
	go clean

# Run integration tests
integration-test:
	go test -race -tags=integration ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .
