# AGENTS.md

## Project Overview

caddy-dev-local is a Docker discovery controller and Caddy plugin that auto-registers `.dev.local` domains for containers. It can run as `caddy devlocal` or as a standalone `devlocal` process attached to same-host Caddy through its admin API.

## Architecture

```
cmd.go              Caddy subcommand "devlocal" and embedded Caddy bootstrap
cmd/devlocal/       Standalone controller attached to a running Caddy admin API
caddy_config.go     User config adaptation and embedded Caddy loading
caddyapi/           Caddy-free admin client and actual-state reconciliation
controller/         Shared discovery, enrichment cache, and built-in hook composition
hook/               Independent latest-update hook runtime
hooks/caddy/        Caddy route, TLS policy, and index file-server registration
hooks/hosts/        Hosts-file reconciliation
hooks/ui/           HTML/CSS/version rendering and Caddy config retrieval
config/config.go    Config struct, defaults, env vars
config/flags.go     Shared flag registration/application and config resolution for both entry points
discovery/
  discovery.go      Caddy-free core: owns container state, refresh loop, enriched updates
  probe.go          Published-host-port selection and HTTP probing
docker/client.go    Docker client wrapper, label extraction helpers
generator/
  generator.go      Stateless helpers: Domains/DomainTargets
  index.go          Generates HTML index page listing containers
  index.html.tmpl   Go template for index page
```

## Key Concepts

- **Standalone-only**: The app always runs directly on the host (never inside a container) and proxies via `localhost:{published_port}`. Containers must publish a port to be reachable; unpublished containers are skipped.
- **Compose detection**: A container is "Compose" only if both `com.docker.compose.project` and `com.docker.compose.service` labels are present.
- **Domain patterns**:
  - Compose: `{project}.{service}.{tld}` (e.g., `myapp.web.dev.local`)
  - Standalone container: `{container-name}.{tld}` (e.g., `my-nginx.dev.local`)
  - All containers register `.localhost` variants (e.g., `myapp.web.localhost`, `my-nginx.localhost`)
- **Custom domains**: Containers can override auto-registration via `dev.local.domains` label (format: `port:domain;port:domain`). When set, auto-generated domain is skipped.
- **Hook composition**: `controller.Run` owns one discovery instance and registers Caddy, UI, and hosts components with `hook.Runtime`. Docker events, polling, and stale cleanup trigger discovery refreshes; hooks receive the resulting authoritative `Update{Snapshot,Status}` values, not raw Docker events. Discovery probes only published host ports when Caddy or UI is enabled, caches `SelectedPort` per container ID, restores it for stopped containers, and evicts it when a container disappears. The runtime owns fan-out. Each hook has an independent worker, receives the newest complete immutable snapshot, and implements explicit cleanup used by `devlocal clean`. Caddy and UI run in an ordered `hook.Sequence` so UI config retrieval follows reconciliation.
- **Two entry points**: `caddy devlocal` loads the user Caddy config before starting the shared controller; standalone `devlocal` only attaches through the admin API and never calls `caddy.Load`.
- **Refresh cycle**: event, poll, and stale refreshes are serialized. Discovery builds candidate Docker state, probes it without holding the state mutex, and atomically commits the complete enriched snapshot. Docker list errors are published without clearing retained state. An internal capacity-one channel keeps only the latest pending update because every update is a complete snapshot; consumers receive updates through the receive-only `Updates()` channel.
- **Caddy ownership**: all managed routes and policies have stable `devlocal-` IDs. Every reconcile reads actual Caddy state, adopts existing resources, removes owned orphans, and preserves unrelated configuration.

## Development Commands

```bash
just lint                  # Run vet + tests with race detector
just build-linux-amd64     # Build for linux-amd64
just build-caddy           # Build plugin-enabled Caddy for all platforms
just build-all             # Build for all platforms (linux-amd64, linux-arm64, windows-amd64)
just build-devlocal        # Build the standalone controller for all platforms
just --list                # List all recipes
```

## Code Conventions

- No comments unless requested
- Go templates (embed via `//go:embed`) for generated output (Caddyfile, HTML index)
- Mutex-protected access to shared container state
- Test file mirrors source: `generator.go` -> `generator_test.go`
- Unit tests cover discovery, hook isolation, Caddy API reconciliation, generated Caddy JSON, hosts, and UI artifacts
- Config values come from flags, env vars, or defaults (in that priority)
- Listen ports come from the Caddyfile `http_port`/`https_port` globals when set, otherwise default to 80/443; effective ports feed new server creation and are injected as `apps.http.http_port`/`https_port` into the loaded config only when the user config has no HTTP servers, so Caddy's auto-redirect logic targets the srv0 listener instead of creating a spurious server on port 80
- Static Caddyfile (user config) is loaded as-is via the Caddyfile adapter — site blocks, TLS policies, and other apps are preserved untouched; devlocal owns no part of the user config
- `parseCaddyfileListenPorts()` mirrors Caddy's own global-options-block parsing (`caddyfile.Parse`, first block with zero keys) because `http_port`/`https_port` globals vanish from adapted JSON when there are no site blocks; only `http_port`/`https_port` are read, other globals flow through the normal adapter
- `adaptUserConfig()` is the pure adapt helper (no `caddy.Load`) and adapts the user Caddyfile to JSON as-is; `loadUserCaddyConfig()` injects parsed ports only when the adapted config has no `apps.http.servers`
- `loadUserCaddyConfig()` adapts + `caddy.Load()`s the user config once for embedded mode; standalone mode does not load or replace user configuration
- `hooks/caddy` constructs dynamic JSON directly and `caddyapi.Client` reconciles it against resources currently present in Caddy
- Admin API semantics: `POST /config/.../routes/-` appends (new routes/policies), `PATCH /id/<id>` replaces in place (route/policy updates; 404 means re-add via POST), `DELETE /id/<id>` removes; `PUT` creates a key that doesn't exist and is used to autovivify the server/routes/policies skeleton, returning 409 if the key already exists; `PATCH /config/apps/tls/automation/policies` replaces the whole policies array (devlocal's policy is prepended ahead of user policies — Caddy picks the first matching policy in `getAutomationPolicyForName` and allows only one catch-all in `TLS.Validate`)
- Admin client uses context-aware requests and a fresh connection per request
- Always update README.md when making user-facing changes

## Dependencies

- [Caddy](https://github.com/caddyserver/caddy) (`github.com/caddyserver/caddy/v2`) — Caddy core ([docs](https://caddyserver.com/docs/))
- `github.com/moby/moby/api` and `github.com/moby/moby/client` — Docker API types and client
- `github.com/moby/moby/client` (as a client interface) — for testability with mocks

## Testing Patterns

- Mock Docker client implements `docker.Client` interface
- `makeContainer()` helper builds test container summaries
- Tests verify both presence and absence of entries in generated domain targets
- All tests model the standalone-only path: containers must publish ports to be proxied, and all targets resolve to `localhost:{published_port}`
- UI artifact tests use temporary directories; template behavior remains covered by generator render tests
