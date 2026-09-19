set shell := ["bash", "-euco", "pipefail"]

artifacts := env_var_or_default("ARTIFACTS", "./artifacts")

# Default
all: check coverage build

# Build devlocal binaries for all platforms
build: build-linux-amd64 build-linux-arm64 build-windows-amd64

# Run linter
lint:
    golangci-lint run ./...

# Run linter and tests
check: lint
    go test -race ./...

# Run tests with coverage and generate HTML report (includes integration tests)
coverage:
    mkdir -p {{artifacts}}
    go test -tags=integration -count=1 -coverpkg=./... -coverprofile={{artifacts}}/coverage.out ./...
    go tool cover -html={{artifacts}}/coverage.out -o={{artifacts}}/coverage.html
    go tool cover -func={{artifacts}}/coverage.out | tail -1

[private]
build-linux-amd64:
    mkdir -p {{artifacts}}/linux-amd64
    rm -f {{artifacts}}/linux-amd64/devlocal
    CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
        go build -o {{artifacts}}/linux-amd64/devlocal ./cmd/devlocal

[private]
build-linux-arm64:
    mkdir -p {{artifacts}}/linux-arm64
    rm -f {{artifacts}}/linux-arm64/devlocal
    CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
        go build -o {{artifacts}}/linux-arm64/devlocal ./cmd/devlocal

[private]
build-windows-amd64:
    mkdir -p {{artifacts}}/windows-amd64
    rm -f {{artifacts}}/windows-amd64/devlocal.exe
    CGO_ENABLED=0 GOARCH=amd64 GOOS=windows \
        go build -o {{artifacts}}/windows-amd64/devlocal.exe ./cmd/devlocal
