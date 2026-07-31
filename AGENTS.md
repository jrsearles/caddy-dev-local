# AGENTS.md

## Project Overview

caddy-dev-local is a Caddy plugin that auto-registers `.dev.local` domains for Docker containers. It watches Docker events, discovers containers on a shared network, and dynamically generates Caddyfile configurations for reverse proxying. See [README.md](README.md) for full documentation.

## Architecture

```
cmd.go              Caddy subcommand "devlocal", event loop, hosts management
caddy.go            Caddy config loading, adaptation, global options extraction, reload logic
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
just lint                  # Run vet + tests with race detector
just build-linux-amd64     # Build for linux-amd64
just build-all             # Build for all platforms (linux-amd64, linux-arm64, windows-amd64)
just --list                # List all recipes
```

## Code Conventions

- No comments unless requested
- Go templates (embed via `//go:embed`) for generated output (Caddyfile, HTML index)
- Mutex-protected access to shared container state
- Test file mirrors source: `generator.go` -> `generator_test.go`
- Tests use `contains()` helper for substring checks in generated output
- Config values come from flags, env vars, or defaults (in that priority)
- Static Caddyfile (user config) is restricted to global options only; site blocks are stripped with a warning
- `loadUserGlobalOptions()` adapts user Caddyfile and loads global options via `caddy.Load()`
- `loadDevlocalViaAPI()` posts devlocal routes (index + containers) via Caddy admin API
- `reloadCaddyConfig()` updates container routes incrementally via admin API on Docker events
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
