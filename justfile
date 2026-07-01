# jig developer tasks — run `just` to list recipes

# Show available recipes
default:
    @just --list

# Install the jig binary to $GOBIN / $GOPATH/bin
install:
    go install ./cmd/jig

# Build the binary into ./bin/jig
build:
    go build -o bin/jig ./cmd/jig

# Run the test suite
test:
    go test ./...

# Run tests with verbose output and the race detector
test-race:
    go test -race -v ./...

# Report test coverage
cover:
    go test -cover ./...

# Run golangci-lint
lint:
    golangci-lint run

# Format all Go source
fmt:
    go fmt ./...

# Verify formatting and vet without modifying files
check: fmt-check vet

# Fail if any file is not gofmt-clean
fmt-check:
    @test -z "$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)

# Run go vet
vet:
    go vet ./...

# Tidy module dependencies
tidy:
    go mod tidy

# Remove build artifacts
clean:
    rm -rf bin
    go clean

# Build, lint, and test — the full pre-commit sweep
all: build lint test
