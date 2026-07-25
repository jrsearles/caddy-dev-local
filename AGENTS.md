# AGENTS.md

## Project Overview

caddy-dev-local is a Caddy plugin that auto-registers `.dev.local` domains for Docker containers. It watches Docker events, discovers containers on a shared network, and dynamically generates Caddyfile configurations for reverse proxying. See [README.md](README.md) for full documentation.

## Architecture

```
cmd.go              Caddy subcommand "devlocal", event loop, config reload
module.go           Registers CaddyDevLocal as a Caddy module
config/config.go    Config struct, defaults, env vars, standalone detection
docker/client.go    Docker client wrapper, label extraction helpers
generator/
  generator.go      Core logic: refresh, port selection, Caddyfile generation
  caddyfile.tmpl    Go template for Caddyfile output
  index.go          Generates HTML index page listing containers
  index.html.tmpl   Go template for index page
  probe.go          HTTP port probing for multi-port containers
  tls.go            TLS config for skipping verification in probes
```

## Key Concepts

- **Standalone mode**: Binary runs on the host (not in Docker). Detected by absence of `/.dockerenv`. Proxies via `localhost:{published_port}`.
- **Docker mode**: Binary runs inside a container. Proxies via `{container_name}:{private_port}` using Docker DNS.
- **Compose detection**: A container is "Compose" only if both `com.docker.compose.project` and `com.docker.compose.service` labels are present.
- **Domain patterns**:
  - Compose: `{project}.{service}.{tld}` (e.g., `myapp.web.dev.local`)
  - Standalone container: `{container-name}.{tld}` (e.g., `my-nginx.dev.local`)
  - Standalone also registers `.localhost` variants (e.g., `myapp.web.localhost`, `my-nginx.localhost`)
- **Custom domains**: Containers can override auto-registration via `dev.local.domains` label (format: `port:domain;port:domain`). When set, auto-generated domain is skipped.

## Development Commands

```bash
# Test
go test ./...

# Test with race detector
go test -race ./...

# Vet
go vet ./...

# Build locally
go build -o caddy-dev-local .

# Cross-compile (requires xcaddy)
./build.sh
```

## Code Conventions

- No comments unless requested
- Go templates (embed via `//go:embed`) for generated output (Caddyfile, HTML index)
- Mutex-protected access to shared container state
- Test file mirrors source: `generator.go` -> `generator_test.go`
- Tests use `contains()` helper for substring checks in generated output
- Config values come from flags, env vars, or defaults (in that priority)
- Always update README.md when making user-facing changes

## Dependencies

- [Caddy](https://github.com/caddyserver/caddy) (`github.com/caddyserver/caddy/v2`) — Caddy core ([docs](https://caddyserver.com/docs/))
- `github.com/moby/moby/api` and `github.com/moby/moby/client` — Docker API types and client
- `github.com/moby/moby/client` (as a client interface) — for testability with mocks

## Testing Patterns

- Mock Docker client implements `docker.Client` interface
- `makeContainer()` helper builds test container summaries
- Tests verify both presence and absence of strings in generated Caddyfile/HTML
- Standalone vs Docker mode tested via `config.Standalone` flag on config
