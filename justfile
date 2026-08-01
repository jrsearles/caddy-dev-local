set shell := ["bash", "-euco", "pipefail"]

artifacts := env_var_or_default("ARTIFACTS", "./artifacts")
plugin := "github.com/jsearles/caddy-dev-local"

# Build binaries for all platforms
build-all: check build-linux-amd64 build-linux-arm64 build-windows-amd64 build-hosts

# Run linter
lint:
    golangci-lint run ./...

# Run linter and tests
check: lint
    go test -race ./...

[private]
install-lint:
    curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.12.2

[private]
xcaddy:
    go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# Build for linux-amd64
build-linux-amd64: xcaddy
    mkdir -p {{artifacts}}/binaries/linux-amd64
    CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
        xcaddy build \
        --output {{artifacts}}/binaries/linux-amd64/caddy \
        --with {{plugin}}=$PWD

# Build for linux-arm64
build-linux-arm64: xcaddy
    mkdir -p {{artifacts}}/binaries/linux-arm64
    CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
        xcaddy build \
        --output {{artifacts}}/binaries/linux-arm64/caddy \
        --with {{plugin}}=$PWD

# Build for windows-amd64
build-windows-amd64: xcaddy
    mkdir -p {{artifacts}}/binaries/windows-amd64
    CGO_ENABLED=0 GOARCH=amd64 GOOS=windows \
        xcaddy build \
        --output {{artifacts}}/binaries/windows-amd64/caddy.exe \
        --with {{plugin}}=$PWD

# Build standalone devlocal-hosts binaries for all platforms
build-hosts: build-hosts-linux-amd64 build-hosts-linux-arm64 build-hosts-windows-amd64

[private]
build-hosts-linux-amd64:
    mkdir -p {{artifacts}}/binaries/linux-amd64
    CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
        go build -o {{artifacts}}/binaries/linux-amd64/devlocal-hosts ./cmd/devlocal-hosts

[private]
build-hosts-linux-arm64:
    mkdir -p {{artifacts}}/binaries/linux-arm64
    CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
        go build -o {{artifacts}}/binaries/linux-arm64/devlocal-hosts ./cmd/devlocal-hosts

[private]
build-hosts-windows-amd64:
    mkdir -p {{artifacts}}/binaries/windows-amd64
    CGO_ENABLED=0 GOARCH=amd64 GOOS=windows \
        go build -o {{artifacts}}/binaries/windows-amd64/devlocal-hosts.exe ./cmd/devlocal-hosts
