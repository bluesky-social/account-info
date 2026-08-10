set shell := ["bash", "-cu"]
set dotenv-load

# Run the linter and tests.
default: lint test

# Enter the pinned Nix development shell.
dev *ARGS="":
    exec ./dev.sh {{ARGS}}

# Build the account-info binary.
build:
    CGO_ENABLED=0 go build -trimpath -o account-info ./cmd/account-info

# Run account-info.
run *ARGS:
    go run ./cmd/account-info {{ARGS}}

# Run account-info with the race detector enabled.
run-race *ARGS:
    go run -race ./cmd/account-info {{ARGS}}

# Run all tests.
test *ARGS="./...":
    go test -count=1 {{ARGS}}

# Run all tests with the race detector enabled.
test-race *ARGS="./...":
    go test -count=1 -race {{ARGS}}

# Run the linter.
lint:
    golangci-lint run --timeout 5m ./...

# Format Go code.
format:
    golangci-lint fmt ./...

# Remove build artifacts.
clean:
    rm -f account-info
