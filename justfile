set shell := ["bash", "-euco", "pipefail"]

artifacts := env_var_or_default("ARTIFACTS", "./artifacts")
plugin := "github.com/jrsearles/caddy-dev-local"

# Build both executable families for all platforms
build-all: check build-caddy build-devlocal

# Build plugin-enabled Caddy binaries for all platforms
build-caddy: build-linux-amd64 build-linux-arm64 build-windows-amd64

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
install-lint:
    curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.12.2

[private]
xcaddy:
    go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# Build for linux-amd64
build-linux-amd64: xcaddy
    mkdir -p {{artifacts}}/binaries/linux-amd64
    rm -f {{artifacts}}/binaries/linux-amd64/caddy
    CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
        xcaddy build \
        --output {{artifacts}}/binaries/linux-amd64/caddy \
        --with {{plugin}}=$PWD

# Build for linux-arm64
build-linux-arm64: xcaddy
    mkdir -p {{artifacts}}/binaries/linux-arm64
    rm -f {{artifacts}}/binaries/linux-arm64/caddy
    CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
        xcaddy build \
        --output {{artifacts}}/binaries/linux-arm64/caddy \
        --with {{plugin}}=$PWD

# Build for windows-amd64
build-windows-amd64: verify-go-windows xcaddy
    mkdir -p {{artifacts}}/binaries/windows-amd64
    rm -f {{artifacts}}/binaries/windows-amd64/caddy.exe
    CGO_ENABLED=0 GOARCH=amd64 GOOS=windows \
        xcaddy build \
        --output {{artifacts}}/binaries/windows-amd64/caddy.exe \
        --with {{plugin}}=$PWD

[private]
verify-go-windows:
    @version=$(go env GOVERSION); if ! printf '%s\n%s\n' go1.26.2 "$version" | sort -VC; then printf 'Windows builds require Go 1.26.2 or newer (found %s)\n' "$version" >&2; exit 1; fi

# Build standalone devlocal controller binaries for all platforms
build-devlocal: build-devlocal-linux-amd64 build-devlocal-linux-arm64 build-devlocal-windows-amd64

[private]
build-devlocal-linux-amd64:
    mkdir -p {{artifacts}}/binaries/linux-amd64
    rm -f {{artifacts}}/binaries/linux-amd64/devlocal
    CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
        go build -o {{artifacts}}/binaries/linux-amd64/devlocal ./cmd/devlocal

[private]
build-devlocal-linux-arm64:
    mkdir -p {{artifacts}}/binaries/linux-arm64
    rm -f {{artifacts}}/binaries/linux-arm64/devlocal
    CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
        go build -o {{artifacts}}/binaries/linux-arm64/devlocal ./cmd/devlocal

[private]
build-devlocal-windows-amd64: verify-go-windows
    mkdir -p {{artifacts}}/binaries/windows-amd64
    rm -f {{artifacts}}/binaries/windows-amd64/devlocal.exe
    CGO_ENABLED=0 GOARCH=amd64 GOOS=windows \
        go build -o {{artifacts}}/binaries/windows-amd64/devlocal.exe ./cmd/devlocal
