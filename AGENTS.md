# AGENTS.md

## Project Overview

caddy-dev-local is a host application that auto-registers `.dev.local` domains for Docker containers. The `devlocal` process attaches to same-host Caddy through its admin API and also manages the hosts file and generated UI.

## Architecture

```
cmd/devlocal/       Application attached to a running Caddy admin API
caddyapi/           Caddy-free admin client and actual-state reconciliation
controller/         Discovery, enrichment cache, and built-in hook composition
hook/               Independent latest-update hook runtime
hooks/caddy/        Caddy route, TLS policy, and index file-server registration
hooks/hosts/        Hosts-file reconciliation
hooks/ui/           HTML/CSS/version rendering and Caddy config retrieval
config/config.go    Config struct, defaults, env vars
config/flags.go     Flag registration/application and config resolution
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
- **External Caddy**: Caddy runs and loads its own configuration independently. devlocal only reconciles owned resources through the admin API and never imports or embeds Caddy.
- **Refresh cycle**: event, poll, and stale refreshes are serialized. Discovery builds candidate Docker state, probes it without holding the state mutex, and atomically commits the complete enriched snapshot. Docker list errors are published without clearing retained state. An internal capacity-one channel keeps only the latest pending update because every update is a complete snapshot; consumers receive updates through the receive-only `Updates()` channel.
- **Caddy ownership**: all managed routes and policies have stable `devlocal-` IDs. Every reconcile reads actual Caddy state, adopts existing resources, removes owned orphans, and preserves unrelated configuration.

## Development Commands

```bash
just lint                  # Run the linter
just check                 # Run the linter and tests with race detector
just build-all             # Default recipe: lint, tests with race detector, integration tests with coverage, build all platforms
just build-devlocal        # Build devlocal for all supported platforms
just --list                # List all recipes
```

## Code Conventions

- No comments unless requested
- Go templates (embed via `//go:embed`) for generated HTML output
- Mutex-protected access to shared container state
- Test file mirrors source: `generator.go` -> `generator_test.go`
- Unit tests cover discovery, hook isolation, Caddy API reconciliation, generated Caddy JSON, hosts, and UI artifacts
- Config values come from flags, env vars, or defaults (in that priority)
- Caddy loads and owns its static configuration; devlocal never reads a Caddyfile or replaces the running configuration
- When the target HTTP server is absent and creation is enabled, the admin client reads effective Caddy HTTP/HTTPS ports and falls back to 80/443
- `hooks/caddy` constructs dynamic JSON directly and `caddyapi.Client` reconciles it against resources currently present in Caddy
- Admin API semantics: `POST /config/.../routes/-` appends (new routes/policies), `PATCH /id/<id>` replaces in place (route/policy updates; 404 means re-add via POST), `DELETE /id/<id>` removes; `PUT` creates a key that doesn't exist and is used to autovivify the server/routes/policies skeleton, returning 409 if the key already exists; `PATCH /config/apps/tls/automation/policies` replaces the whole policies array (devlocal's policy is prepended ahead of user policies — Caddy picks the first matching policy in `getAutomationPolicyForName` and allows only one catch-all in `TLS.Validate`)
- Admin client uses context-aware requests and a fresh connection per request
- Always update README.md when making user-facing changes

## Dependencies

- [Caddy](https://caddyserver.com/docs/) — External web server configured through its admin API; not a Go dependency
- `github.com/moby/moby/api` and `github.com/moby/moby/client` — Docker API types and client
- `github.com/moby/moby/client` (as a client interface) — for testability with mocks

## Testing Patterns

- Mock Docker client implements `docker.Client` interface
- `makeContainer()` helper builds test container summaries
- Tests verify both presence and absence of entries in generated domain targets
- All tests model the standalone-only path: containers must publish ports to be proxied, and all targets resolve to `localhost:{published_port}`
- UI artifact tests use temporary directories; template behavior remains covered by generator render tests
