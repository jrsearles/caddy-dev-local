# AGENTS.md

## Project Overview

caddy-dev-local is a Caddy plugin that auto-registers `.dev.local` domains for Docker containers. It watches Docker events, discovers all containers reachable from the proxy (across any Docker network), and dynamically generates Caddyfile configurations for reverse proxying. See [README.md](README.md) for full documentation.

## Architecture

```
cmd.go              Caddy subcommand "devlocal", hosts management wiring, caddy reload driver
cmd/devlocal-hosts/ Standalone hosts-only binary (no Caddy): watches Docker, writes hosts file
caddy.go            Caddy config loading, user config adapt/load, devlocal apply, reload logic
admin.go            Admin API client: diff-based reconcile of routes/policies, server ensure
builder.go          Direct JSON config construction (routes, merged TLS policy, index route)
config/config.go    Config struct, defaults, env vars, standalone detection
config/flags.go     Shared flag registration/application and config resolution for both entry points
discovery/
  discovery.go      Caddy-free controller: event watch + poll + stale cleanup driving refresh/apply callbacks
docker/client.go    Docker client wrapper, label extraction helpers
generator/
  generator.go      Core logic: refresh, port selection, domain->target computation
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
  - All modes register `.localhost` variants (e.g., `myapp.web.localhost`, `my-nginx.localhost`)
- **Custom domains**: Containers can override auto-registration via `dev.local.domains` label (format: `port:domain;port:domain`). When set, auto-generated domain is skipped.
- **Two entry points share one discovery driver**: `cmd.go` (Caddy plugin, refresh = `gen.RefreshAndSelect`, apply = caddy reload + hosts sync) and `cmd/devlocal-hosts` (standalone hosts-only binary, refresh = `gen.Refresh` with no port probing, apply = hosts sync) both drive `discovery.Controller`; the driver is Caddy-free so the standalone binary has zero caddy dependencies

## Development Commands

```bash
just lint                  # Run vet + tests with race detector
just build-linux-amd64     # Build for linux-amd64
just build-all             # Build for all platforms (linux-amd64, linux-arm64, windows-amd64)
just build-hosts           # Build the standalone devlocal-hosts binaries for all platforms
just --list                # List all recipes
```

## Code Conventions

- No comments unless requested
- Go templates (embed via `//go:embed`) for generated output (Caddyfile, HTML index)
- Mutex-protected access to shared container state
- Test file mirrors source: `generator.go` -> `generator_test.go`
- Unit tests cover generator/host logic only; the HTML index page UI is not unit-tested
- Config values come from flags, env vars, or defaults (in that priority)
- Listen ports come from the Caddyfile `http_port`/`https_port` globals when set, otherwise default to 80/443; effective ports feed `ensureServer` (via `effectivePorts()` reading the running config) and are injected as `apps.http.http_port`/`https_port` into the loaded config only when the user config has no HTTP servers, so Caddy's auto-redirect logic targets the srv0 listener instead of creating a spurious server on port 80
- Static Caddyfile (user config) is loaded as-is via the Caddyfile adapter — site blocks, TLS policies, and other apps are preserved untouched; devlocal owns no part of the user config
- `parseCaddyfileListenPorts()` mirrors Caddy's own global-options-block parsing (`caddyfile.Parse`, first block with zero keys) because `http_port`/`https_port` globals vanish from adapted JSON when there are no site blocks; only `http_port`/`https_port` are read, other globals flow through the normal adapter
- `adaptUserConfig()` is the pure adapt helper (no `caddy.Load`): adapts the user Caddyfile to JSON as-is, injecting `http_port`/`https_port` from the parsed globals only when the adapted config has no `apps.http.servers` (a globals-only Caddyfile adapts to `{}`); unit-tested directly
- `initCaddyConfig()` adapts + `caddy.Load()`s the user config once at startup (no user config uses `{}`), so the admin endpoint always starts even with empty/comment-only configs, then applies the devlocal config via `postDevlocalViaAPI()` (admin API); the merged-startup path and `mergeConfigs()`/`loadStartupConfig()` no longer exist
- `buildDevlocalConfig()` constructs the devlocal config directly as JSON (no Caddyfile/adapter on the hot path), returning only `routes` (keyed by `@id`), `policies` (a single `devlocal-tls` policy), and an `indexRoute`
- `postDevlocalViaAPI()` applies the devlocal config via the Caddy admin API: `ensureServer`, POST the index route, then `reconcileDevlocal` for container routes/policies
- `reloadCaddyConfig()` diffs desired vs applied routes/policies and patches only changes via the admin API on Docker events
- Admin API semantics: `POST /config/.../routes/-` appends (new routes/policies), `PATCH /id/<id>` replaces in place (route/policy updates; 404 means re-add via POST), `DELETE /id/<id>` removes; `PUT` creates a key that doesn't exist and is used to autovivify the server/routes/policies skeleton, returning 409 if the key already exists; `PATCH /config/apps/tls/automation/policies` replaces the whole policies array (devlocal's policy is prepended ahead of user policies — Caddy picks the first matching policy in `getAutomationPolicyForName` and allows only one catch-all in `TLS.Validate`)
- Admin client uses a fresh connection per request plus bounded retry on transient network errors (idempotent methods only) because every config reload restarts the admin endpoint
- Always update README.md when making user-facing changes

## Dependencies

- [Caddy](https://github.com/caddyserver/caddy) (`github.com/caddyserver/caddy/v2`) — Caddy core ([docs](https://caddyserver.com/docs/))
- `github.com/moby/moby/api` and `github.com/moby/moby/client` — Docker API types and client
- `github.com/moby/moby/client` (as a client interface) — for testability with mocks

## Testing Patterns

- Mock Docker client implements `docker.Client` interface
- `makeContainer()` helper builds test container summaries
- Tests verify both presence and absence of entries in generated domain targets
- Standalone vs Docker mode tested via `config.Standalone` flag on config
- UI testing is not necessary: the generated HTML index page (`index.go`/`index.html.tmpl`) is not unit-tested
