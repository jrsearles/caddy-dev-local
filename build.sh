#!/bin/bash
set -e

echo "== PARAMETERS =="
echo "ARTIFACTS: ${ARTIFACTS:=./artifacts}"

go vet ./...
go test -race ./...

# Ensure GOPATH/bin is in PATH for xcaddy
export PATH="$PATH:$(go env GOPATH)/bin"

go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# Linux AMD64
CGO_ENABLED=0 GOARCH=amd64 GOOS=linux \
    xcaddy build \
    --output ${ARTIFACTS}/binaries/caddy-dev-local-linux-amd64 \
    --with github.com/jsearles/caddy-dev-local=$PWD

# Linux ARM64
CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
    xcaddy build \
    --output ${ARTIFACTS}/binaries/caddy-dev-local-linux-arm64 \
    --with github.com/jsearles/caddy-dev-local=$PWD

# Windows AMD64
CGO_ENABLED=0 GOARCH=amd64 GOOS=windows \
    xcaddy build \
    --output ${ARTIFACTS}/binaries/caddy-dev-local-windows-amd64.exe \
    --with github.com/jsearles/caddy-dev-local=$PWD