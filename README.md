# caddy-dev-local

A Docker discovery controller and Caddy plugin that automatically registers `{project}.{service}.dev.local` domains, with HTTP port probing, self-signed TLS, and a built-in index page — inspired by OrbStack's container domain feature.

> **Warning**: This plugin is designed for local development environments only. It uses self-signed TLS, auto-manages hosts files, and assumes trusted networks. Do not use in production.

## Features

- **Automatic domain registration** — Any running container that publishes at least one port gets `*.dev.local` domains, proxied via `localhost` using its published (host-mapped) ports.
- **Compose-aware** — Uses `{project}.{service}.dev.local` for Compose services, `{container-name}.dev.local` for standalone containers
- **HTTP port probing** — For containers with multiple ports, automatically detects the HTTP port, preferring common ports (80, 8080, 443, 8443)
- **Self-signed TLS** — Zero-config HTTPS using Caddy's internal CA
- **Custom domains** — Override auto-registration with `dev.local.domains` label
- **Hosts file integration** — Automatically adds entries to `/etc/hosts` for local DNS resolution
- **Index page** — Visit `dev.local` (or `dev.localhost`) to see all registered containers, with the following features:
  - **Cards** show each container's icon, image, health badge (`healthy`/`starting`/`unhealthy`), and running/stopped status with live relative timestamps ("since 3m ago")
  - **Search** — filter across name, image, project, service, and domains using the header search box; filter terms are space-separated (all must match); query is synced to the URL as `?q=` for bookmarking; matching project sections auto-expand while filtering and collapse back when the filter is cleared
  - **Compose groups** — services grouped under collapsible project sections with up/down counts; expansion state and active tab are preserved across live reloads via `sessionStorage` and reflected in the URL (`?open=`, `?tab=`)
  - **Detail drawer** — click any container card header to slide open a side panel with full image, short container ID, networks, published port table, health status, and filtered labels (`dev.local.*`, `com.docker.compose.*`, `org.opencontainers.image.*`); includes an "Open in Docker Desktop" button
  - **Domain rows** — copy button per domain copies `host:port`; a globe icon links directly to the service in the browser
  - **Docker Desktop links** — each container card has an icon that opens Docker Desktop's Logs view filtered to that container (`docker-desktop://dashboard/logs?containerIds={id}`); the icon next to "Containers" opens the dashboard (`docker-desktop://dashboard/open`); each Compose project section header links to that project's view (`docker-desktop://dashboard/apps/{project}`)
  - **Live refresh** — polls a lightweight `/version.json` endpoint every 30 seconds and reloads only on change; scroll position is preserved across reloads; falls back to full-page hash polling if `version.json` is unavailable
  - **Discovery banner** — a dismissible error banner appears at the top when Docker event streaming fails, showing the last error and time of last successful refresh
  - **Caddy config tab** — shows the effective running Caddy config as a collapsible JSON tree (via [json-view](https://github.com/pgrabovets/json-view)) with expand/collapse-all; toggle to raw JSON; only appears if a config is available
  - **Theme** — defaults to system preference; header toggle cycles light → dark → system
- **Stale cleanup** — Stopped containers stay listed on the index page (marked stopped) until the stale TTL expires, then their config is removed
- **OpenTelemetry tracing** — Dynamic reverse proxy routes include Caddy's `tracing` handler for automatic span collection; opt out with `--no-tracing`
- **Composable plugins** — Caddy registration, UI rendering, and hosts-file updates consume the same discovery stream independently
- **Standalone controller** — `devlocal` can attach to a separate Caddy process on the same host through its admin API

## Quick Start

### 1. Run the proxy

caddy-dev-local runs **directly on your host** (not inside Docker). It discovers containers via the Docker socket and proxies to them via `localhost` using their published ports.

```bash
just build-linux-amd64
sudo ./artifacts/binaries/linux-amd64/caddy devlocal
```

> `sudo` is required so the proxy can write to your system hosts file (`/etc/hosts`). Pass `--hosts-file=false` to skip hosts file management.

### 2. Start your containers

Start any Docker container that publishes a port:

```bash
docker run -d --name my-app -p 8080:80 nginx:alpine
```

Visit `https://my-app.dev.local` (hosts entry written automatically) or `https://my-app.localhost` (no hosts entry needed).

Containers must publish a port to be reachable — unpublished containers are listed on the index page but not proxied.

## Docker Compose

Just run your Compose app with published ports; no special devlocal service is needed.

```yaml
services:
  my-app:
    image: nginx:alpine
    ports:
      - "8080:80"
```

Start it and caddy-dev-local registers `{project}.{service}.dev.local` automatically:

```bash
docker compose up -d
```

## Labels

| Label | Value | Effect |
|---|---|---|
| `dev.local` | `false`, `"false"`, `0`, `no` | Skip this container |
| `dev.local.domains` | `port:domain;port:domain` | Custom domain mappings |
| `org.opencontainers.image.logo` | URL | Custom icon for the container on the index page (takes priority over `com.docker.extension.icon`) |
| `com.docker.extension.icon` | URL | Custom icon for the container on the index page |

### Custom Domains Example

```yaml
services:
  my-app:
    image: nginx:alpine
    networks:
      - devlocal
    labels:
      - "dev.local.domains=80:api.custom.local;80:api.alt.local"
```

## Configuration

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `--tld` | `DEVLOCAL_TLD` | `dev.local` | Top-level domain |
| `--stale-ttl` | `DEVLOCAL_STALE_TTL` | `1h` | Keep config for stopped containers |
| `--probe-timeout` | `DEVLOCAL_PROBE_TIMEOUT` | `2s` | HTTP probe timeout |
| `--hosts-file` | `DEVLOCAL_HOSTS_FILE` | `true` | Manage hosts file entries for domains |
| `--no-tracing` | `DEVLOCAL_TRACING=false` | tracing enabled | Disable OpenTelemetry tracing on dynamic routes |
| `--poll-interval` | `DEVLOCAL_POLL_INTERVAL` | `30s` | Periodic full refresh as a safety net for missed Docker events; `0` disables |
| `--config` | `DEVLOCAL_CONFIG` | (auto-detect) | Path to a static Caddyfile loaded as-is |

The standalone `devlocal` command also supports `--caddy`, `--ui`, `--caddy-admin`, `--caddy-server`, `--allow-create-server`, and `--index-dir`. Caddy, UI, and hosts plugins are enabled by default.

> **Note:** The periodic poll backstops missed Docker events and repairs Caddy configuration changed by another process. Plugin workers coalesce queued work to the newest complete snapshot.

## Custom Caddyfile

caddy-dev-local auto-detects `Caddyfile`, `Caddyfile.json`, `Caddyfile.json5`, or `Caddyfile.yaml` in the working directory. Use `--config` or `DEVLOCAL_CONFIG` to specify a different path.

**Your Caddyfile is loaded as-is.** Site blocks, TLS automation policies, logging, etc. are honored exactly as written. caddy-dev-local applies its own dynamic container routes, TLS policy, and index page through Caddy's [admin API](https://caddyserver.com/docs/api) — it never rewrites or merges your config. The two live side by side on the same HTTP server.

## Building from Source

Requires [just](https://github.com/casey/just) and [golangci-lint](https://golangci-lint.run/).

```bash
just install-lint           # Install golangci-lint (one-time)
just build-linux-amd64      # Build for linux-amd64
just build-all              # Build for all platforms
just build-devlocal         # Build the standalone controller binaries
just lint                   # Run linter
just check                  # Run linter + tests
```

See `just --list` for all available recipes.

## Building the Docker Image

The root [`Dockerfile`](Dockerfile) builds caddy with the devlocal plugin via the official [`caddy:builder`](https://hub.docker.com/_/caddy#adding-custom-caddy-modules) image (using `xcaddy`), then overlays the built binary onto the regular `caddy` image. The base tags are pinned to the Caddy version in `go.mod` — bump them together when upgrading.

```bash
docker build -t caddy-dev-local .
```

The image runs `caddy devlocal`, so it expects the same mounts as the prebuilt image in the [Quick Start](#quick-start).

## Standalone Controller

The `devlocal` executable runs discovery and all built-in plugins without embedding Caddy. Start Caddy separately with its admin API enabled, then run:

```bash
just build-devlocal
sudo ./artifacts/binaries/linux-amd64/devlocal
```

The default admin endpoint is `http://localhost:2019`. Use `--caddy-admin` and `--caddy-server` to select another same-host Caddy process and HTTP server. The initial implementation assumes Caddy and Docker-published ports are on the same host because generated upstreams use `localhost:{published_port}`.

```bash
docker run -d --name my-app -p 8080:80 nginx:alpine
# Available at https://my-app.dev.local → localhost:8080
# Also available at https://my-app.localhost → localhost:8080
```

### `.localhost` Domains

Each container also gets a `.localhost` domain in addition to the configured TLD. Browsers treat `.localhost` as a secure context without needing a certificate, so these URLs work without any TLS warnings.

- Compose services: `{project}.{service}.localhost`
- Other containers: `{container-name}.localhost`

Since the proxy runs on the host, `.localhost` reaches the published ports directly — no hosts entry required.

These domains are not generated when custom `dev.local.domains` labels are set.

## Example

See the [example directory](example/) for a complete demo with multiple containers.

```bash
cd example
docker compose up -d
```

Then visit:
- `https://dev.local` — Index page (also available at `https://dev.localhost`)
- `https://example.web.dev.local` — nginx web server
- `https://example.api.dev.local` — API server
- `https://myapp.custom.local` — Custom domain

Non-HTTP services like `mssql` are also registered (see it on the index page); SQL Server listens on port `1433` (`SA` / `DevLocalPass123!`).

## How It Works

1. Watches Docker events for container lifecycle changes across all networks
2. Lists all containers via the Docker API
3. Computes domains from container labels (Compose project/service or container name)
4. Registers running containers that publish at least one port; unpublished containers are skipped
5. For multi-port containers, probes `localhost:{published_port}` to find the HTTP server (common ports 80, 8080, 443, 8443 are checked first)
6. Publishes each immutable discovery update to independent Caddy, UI, and hosts-file plugin workers
7. Reconciles stable, owned route and TLS policy IDs against Caddy's actual configuration, preserving unrelated resources and adopting state after restarts
8. Renders the UI files independently and registers their directory with Caddy's file server
9. Polls Docker every `--poll-interval` (default 30s) as a safety net for missed events and external Caddy changes

## Generated Files

caddy-dev-local writes three files to the user cache directory (`os.UserCacheDir()/caddy-dev-local`) by default. Use `--index-dir` with the standalone controller when Caddy runs as another OS user; files are written with read permissions for the Caddy process.

| File | Purpose |
|---|---|
| `index.html` | Served at the TLD and its `.localhost` alias (e.g. `http://dev.local` / `http://dev.localhost`) as a status page listing discovered containers. Includes an expandable "Caddy Config" panel showing the effective running config (user config + devlocal routes/policies, fetched from the admin API after each reload) |
| `index.css` | Styles for the generated index page |
| `version.json` | Content fingerprint used by the page's live-refresh polling |

## Hosts File

caddy-dev-local automatically manages entries in your system hosts file (`/etc/hosts` on Linux, `C:\Windows\System32\drivers\etc\hosts` on Windows) so domains resolve locally without configuring DNS. Since the proxy runs on your host, it can write the hosts file directly.

Entries are written inside a managed block with searchable markers:

```
# dev-local:BEGIN
# Managed by caddy-dev-local — do not edit.
127.0.0.1    dev.local
127.0.0.1    dev.localhost
127.0.0.1    myapp.web.dev.local
127.0.0.1    myapp.web.localhost
127.0.0.1    my-nginx.dev.local
127.0.0.1    my-nginx.localhost
# dev-local:END
```

The block is updated on every config reload — added when containers start, removed when they stop. The TLD (`dev.local`) and its `.localhost` alias (`dev.localhost`) always point at the index page; container `.dev.local` and `.localhost` domains are included so non-browser tools (curl, API clients, etc.) can resolve them without relying on DNS.

### Opt Out

Disable hosts file management entirely:

```bash
caddy devlocal --hosts-file=false
# or
DEVLOCAL_HOSTS_FILE=false caddy devlocal
```

### Cleanup

Remove all resources managed by the enabled plugins:

```bash
caddy devlocal-clean
# or
devlocal clean
```

`devlocal clean` runs cleanup for every enabled plugin: it removes owned Caddy routes and TLS policy, generated UI files, and the managed hosts block. It accepts the normal plugin and Caddy connection flags, such as `devlocal clean --caddy-admin http://localhost:2020`. Cleanup is explicit and does not run automatically when the controller stops.

`caddy devlocal-clean --index-dir /custom/path` removes the local generated UI files and hosts block; embedded Caddy routes disappear with that Caddy process.

### Permissions

On Linux, writing to `/etc/hosts` requires root. If the process doesn't have write permission, caddy-dev-local logs a warning and skips hosts file updates (Caddy still works normally).

## OpenTelemetry Tracing

All dynamic reverse proxy routes include Caddy's [tracing handler](https://caddyserver.com/docs/modules/caddyhttp.tracing) by default. The handler creates spans using the standard OTel SDK, which auto-configures from environment variables like `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_TRACES_EXPORTER`. The index page route is not instrumented to avoid noise from periodic polling.

### Opt Out

Disable tracing on dynamic routes:

```bash
caddy devlocal --no-tracing
# or
DEVLOCAL_TRACING=false caddy devlocal
```

## Plugin Composition

The standalone controller enables all built-in plugins by default. Disable components independently with boolean flags:

```bash
devlocal --caddy=false --ui=false  # Hosts-file updates only
devlocal --hosts-file=false        # Caddy registration and UI only
devlocal --ui=false                # Caddy registration without the index route
```

Additional compile-time plugins implement `Name() string`, `Apply(context.Context, discovery.Delta) error`, and `Cleanup(context.Context) error`, then register with `plugin.Runtime`. Each plugin has an independent latest-update worker, so a slow or failed component does not block discovery or other plugins. Ordered dependencies can use `plugin.Sequence`; the default composition sequences Caddy before UI so the rendered config matches the reconciled update. Cleanup is invoked explicitly with `devlocal clean`.

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach
- [Caddy](https://caddyserver.com/docs/) — The web server this project extends

## License

MIT
