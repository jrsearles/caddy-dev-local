# caddy-dev-local

A host application that discovers Docker containers and registers `{project}.{service}.dev.local` domains with Caddy, with optional continuous watching, HTTP port probing, self-signed TLS, hosts-file management, and a built-in index page.

> **Warning**: This application is designed for local development environments only. It uses self-signed TLS, auto-manages hosts files, and assumes trusted networks. Do not use in production.

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
  - **Compose groups** — services grouped under collapsible project sections with up/down counts; project and Caddy config expansion state, config view mode, and the active tab are preserved across live reloads via `sessionStorage` (project expansion and active tab are also reflected in the URL via `?open=` and `?tab=`)
  - **Detail drawer** — click any container card header to slide open a wide side panel with full image, short container ID, networks, published port table, health status, and filtered labels (`dev.local.*`, `com.docker.compose.*`, `org.opencontainers.image.*`); includes an "Open in Docker Desktop" button
  - **Domain rows** — copy button per domain copies `host:port`; a globe icon links directly to the service in the browser
  - **Docker Desktop links** — each container card has an icon that opens Docker Desktop's Logs view filtered to that container (`docker-desktop://dashboard/logs?containerIds={id}`); the icon next to "Containers" opens the dashboard (`docker-desktop://dashboard/open`); each Compose project section header links to that project's view (`docker-desktop://dashboard/apps/{project}`)
  - **Live refresh** — polls a lightweight `/version.json` endpoint every 30 seconds and reloads only on change; scroll position is preserved across reloads; falls back to full-page hash polling if `version.json` is unavailable
  - **Discovery banner** — a dismissible error banner appears at the top when Docker event streaming fails, showing the last error and time of last successful refresh
  - **Caddy config tab** — shows the effective running Caddy config as a collapsible JSON tree (via [json-view](https://github.com/pgrabovets/json-view)) with expand/collapse-all; toggle to raw JSON; only appears if a config is available
  - **Theme** — defaults to system preference; header toggle cycles light → dark → system
- **Stale cleanup** — Stopped containers stay listed on the index page (marked stopped) until the stale TTL expires, then their config is removed
- **OpenTelemetry tracing** — Dynamic reverse proxy routes include Caddy's `tracing` handler for automatic span collection; opt out with `--no-tracing`
- **Composable hooks** — Caddy registration, UI rendering, and hosts-file updates independently reconcile complete discovery snapshots
- **Caddy integration** — Reconciles routes and TLS policies with a separately running Caddy process through its admin API

## Quick Start

### 1. Start Caddy

Install and start ordinary [Caddy](https://caddyserver.com/docs/install) on your host with its admin API enabled. Caddy's default admin endpoint is `http://localhost:2019`.

### 2. Start your containers

Start any Docker container that publishes a port:

```bash
docker run -d --name my-app -p 8080:80 nginx:alpine
```

### 3. Run devlocal

devlocal runs **directly on your host**. By default it discovers containers once, updates Caddy, generates the UI, writes the hosts file, and exits.

```bash
just build
sudo ./artifacts/linux-amd64/devlocal
```

Successful runs log each applied hook and finish with a reminder to use `devlocal start` for continuous watching.

> `sudo` is required so devlocal can write to your system hosts file (`/etc/hosts`). Pass `--hosts-file=false` to skip hosts file management.

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

Start it, then run devlocal to register `{project}.{service}.dev.local`:

```bash
docker compose up -d
sudo devlocal
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
    ports:
      - "8080:80"
    labels:
      - "dev.local.domains=8080:api.custom.local;8080:api.alt.local"
```

## Configuration

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `--tld` | `DEVLOCAL_TLD` | `dev.local` | Top-level domain |
| `--stale-ttl` | `DEVLOCAL_STALE_TTL` | `1h` | Keep config for stopped containers |
| `--probe-timeout` | `DEVLOCAL_PROBE_TIMEOUT` | `2s` | HTTP probe timeout |
| `--hosts-file` | `DEVLOCAL_HOSTS_FILE` | `true` | Manage hosts file entries for domains |
| `--no-tracing` | `DEVLOCAL_TRACING=false` | tracing enabled | Disable OpenTelemetry tracing on dynamic routes |
| `--poll-interval` | `DEVLOCAL_POLL_INTERVAL` | `30s` | In `start` mode, periodically refresh as a safety net for missed Docker events; `0` disables |
| `--caddy` | — | `true` | Register routes and TLS policies with Caddy |
| `--ui` | — | `true` | Generate and register the index UI |
| `--caddy-admin` | `DEVLOCAL_CADDY_ADMIN` | `http://localhost:2019` | Caddy admin API URL |
| `--caddy-server` | `DEVLOCAL_CADDY_SERVER` | `srv0` | Caddy HTTP server name |
| `--allow-create-server` | — | `true` | Create the target Caddy HTTP server when absent |
| `--index-dir` | `DEVLOCAL_INDEX_DIR` | user cache directory | Directory for generated UI files |
| `--log-level` | `DEVLOCAL_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error` |

Caddy, UI, and hosts-file integration are enabled by default.

> **Note:** `devlocal start` watches Docker events continuously. Its periodic poll backstops missed events and repairs Caddy configuration changed by another process. Hook workers coalesce queued work to the newest complete snapshot.

## Caddy Configuration

devlocal does not load or modify a Caddyfile. Start and configure Caddy normally, then point devlocal at its admin API with `--caddy-admin` when the endpoint differs from the default.

devlocal applies dynamic container routes, its TLS policy, and the index route through Caddy's [admin API](https://caddyserver.com/docs/api). It owns only resources with stable `devlocal-` IDs and preserves unrelated routes and policies. By default it targets `srv0` and creates that server when absent; use `--allow-create-server=false` to require an existing server.

## Building from Source

Requires [Go 1.26.2 or newer](https://go.dev/dl/), [just](https://github.com/casey/just), and [golangci-lint](https://golangci-lint.run/).

```bash
just install-lint           # Install golangci-lint (one-time)
just build                  # Build devlocal for all supported platforms
just                        # Run checks, integration tests with coverage, and build devlocal (default recipe)
just lint                   # Run linter
just check                  # Run linter + tests
```

The build produces `devlocal` (`devlocal.exe` on Windows) under `artifacts/<os>-<arch>/`. It connects to a separately installed Caddy instance through the admin API.

See `just --list` for all available recipes.

## Running

Start Caddy separately with its admin API enabled. Run one reconciliation pass after starting or changing containers:

```bash
just build
sudo ./artifacts/linux-amd64/devlocal
```

To watch Docker and reconcile automatically as containers change, run the continuous controller:

```bash
sudo ./artifacts/linux-amd64/devlocal start
```

When using `start`, run Caddy and devlocal in **separate terminals**. devlocal attaches to Caddy over its admin API and never starts or stops it, but on Windows a console close or Ctrl-C broadcasts a signal to every process attached to that console. Caddy's `caddy start` child stays attached to the terminal on Windows, so if devlocal shares that terminal, closing it shuts down both. Keep Caddy in its own window (or run it as a service) and devlocal in another; with native Caddy daemons such as systemd the two are already independent.

The default admin endpoint is `http://localhost:2019`. Use `--caddy-admin` and `--caddy-server` to select another same-host Caddy process and HTTP server. Caddy, devlocal, and Docker-published ports must be on the same host because generated upstreams use `localhost:{published_port}`.

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
just build
cd example
docker compose up -d
# Start Caddy separately, then reconcile the running containers.
sudo ../artifacts/linux-amd64/devlocal
```

Then visit:
- `https://dev.local` — Index page (also available at `https://dev.localhost`)
- `https://example.web.dev.local` — nginx web server
- `https://example.api.dev.local` — API server
- `https://myapp.custom.local` — Custom domain

Non-HTTP services like `mssql` are also registered (see it on the index page); SQL Server listens on port `1433` (`SA` / `DevLocalPass123!`).

## How It Works

1. Lists all containers via the Docker API
2. Computes domains from container labels (Compose project/service or container name)
3. Registers running containers that publish at least one port; unpublished containers are skipped
4. Probes only each running container's published host ports at `localhost:{published_port}` to find the HTTP server (common ports 80, 8080, 443, 8443 are checked first)
5. Reconciles stable, owned route and TLS policy IDs against Caddy's actual configuration, preserving unrelated resources and adopting state after restarts
6. Renders the UI after Caddy reconciliation and updates the hosts file
7. Exits after one pass, or, with `devlocal start`, watches Docker events and periodically refreshes according to `--poll-interval`

## Generated Files

caddy-dev-local writes three files to the user cache directory (`os.UserCacheDir()/caddy-dev-local`) by default. Use `--index-dir` when Caddy runs as another OS user; files are written with read permissions for the Caddy process.

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

The block is updated on each `devlocal` pass and whenever `devlocal start` refreshes configuration. The TLD (`dev.local`) and its `.localhost` alias (`dev.localhost`) always point at the index page; container `.dev.local` and `.localhost` domains are included so non-browser tools (curl, API clients, etc.) can resolve them without relying on DNS.

### Opt Out

Disable hosts file management entirely:

```bash
devlocal --hosts-file=false
# or
DEVLOCAL_HOSTS_FILE=false devlocal
```

### Cleanup

Remove all resources managed by the enabled hooks:

```bash
devlocal clean
```

`devlocal clean` runs cleanup for every enabled hook: it removes owned Caddy routes and TLS policy, generated UI files, and the managed hosts block. It accepts the normal hook and Caddy connection flags, such as `devlocal clean --caddy-admin http://localhost:2020`. Cleanup is explicit and does not run automatically when the controller stops.

### Permissions

On Linux, writing to `/etc/hosts` requires root. If the process doesn't have write permission, caddy-dev-local logs a warning and skips hosts file updates (Caddy still works normally).

## OpenTelemetry Tracing

All dynamic reverse proxy routes include Caddy's [tracing handler](https://caddyserver.com/docs/modules/caddyhttp.tracing) by default. The handler creates spans using the standard OTel SDK, which auto-configures from environment variables like `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_TRACES_EXPORTER`. The index page route is not instrumented to avoid noise from periodic polling.

### Opt Out

Disable tracing on dynamic routes:

```bash
devlocal --no-tracing
# or
DEVLOCAL_TRACING=false devlocal
```

## Hook Composition

The application enables all built-in hooks by default. Disable components independently with boolean flags:

```bash
devlocal --caddy=false --ui=false  # Hosts-file updates only
devlocal --hosts-file=false        # Caddy registration and UI only
devlocal --ui=false                # Caddy registration without the index route
```

Additional compile-time hooks implement `Name() string`, `Apply(context.Context, discovery.Update) error`, and `Cleanup(context.Context) error`, then register with `hook.Runtime`. The default command synchronously applies one authoritative snapshot to every hook. In `start` mode, Docker events, polling, and stale cleanup trigger serialized discovery refreshes; each hook has an independent worker with latest-update coalescing. Port probing is part of discovery. Ordered dependencies can use `hook.Sequence`; the default composition sequences Caddy before UI so the rendered config matches the reconciled update. Cleanup is invoked explicitly with `devlocal clean`.

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach
- [Caddy](https://caddyserver.com/docs/) — The web server this project configures

## License

MIT
