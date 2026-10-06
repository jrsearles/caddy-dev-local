# caddy-dev-local

A Windows host application that discovers Docker containers and registers `{project}.{service}.dev.local` domains with Caddy, with continuous watching, HTTP port probing, self-signed TLS, hosts-file management, and a built-in Blazor dashboard.

> **Warning**: This application is designed for local development environments only. It uses self-signed TLS, auto-manages the hosts file, and assumes trusted networks. Do not use in production.

## Features

- **Automatic domain registration** — Any running container that publishes at least one port gets `*.dev.local` domains, proxied via `localhost` using its published (host-mapped) ports.
- **Compose-aware** — Uses `{project}.{service}.dev.local` for Compose services, `{container-name}.dev.local` for standalone containers
- **HTTP port probing** — For containers with multiple ports, automatically detects the HTTP port, preferring common ports (80, 8080, 443, 8443)
- **Self-signed TLS** — Zero-config HTTPS using Caddy's internal CA
- **Custom domains** — Override auto-registration with the `dev.local.domains` label
- **Hosts file integration** — Automatically adds entries to `C:\Windows\System32\drivers\etc\hosts` for local DNS resolution
- **Dashboard** — Visit `https://dev.local` (or `dev.localhost`) for a Blazor dashboard listing every discovered container:
  - **Cards** show each container's icon, image, health badge (`healthy`/`starting`/`unhealthy`), and running/stopped status with relative timestamps ("since 3m ago")
  - **Search** — filter across name, image, project, service, and domains from the header search box; filter terms are space-separated (all must match); the query is synced to the URL as `?q=` for bookmarking
  - **Compose groups** — services grouped under collapsible project sections with up/down counts; expansion state, config view mode, and the active tab are preserved across live updates via `sessionStorage` (project expansion and active tab are also reflected in the URL via `?open=` and `?tab=`)
  - **Detail drawer** — click a container card header to slide open a side panel with full image, short container ID, networks, published port table, health status, and filtered labels (`dev.local.*`, `com.docker.compose.*`, `org.opencontainers.image.*`); includes an "Open in Docker Desktop" button
  - **Domain rows** — a copy button per domain copies `host:port`; a globe icon links to the service in the browser
  - **Docker Desktop links** — each card opens Docker Desktop's Logs view filtered to that container (`docker-desktop://dashboard/logs?containerIds={id}`); project section headers link to that project's view
  - **Live updates** — state streams over the Blazor SignalR circuit, so the page updates without a reload
  - **Discovery banner** — a dismissible error banner appears when Docker discovery fails, showing the last error and time of last successful refresh
  - **Caddy config tab** — shows the effective running Caddy config as a collapsible JSON tree with expand/collapse-all, and a raw JSON toggle
  - **Theme** — defaults to system preference; the header toggle cycles light → dark → system
- **Stale cleanup** — Stopped containers stay listed (marked stopped) until the stale TTL expires, then their configuration is removed
- **OpenTelemetry tracing** — Dynamic reverse proxy routes include Caddy's `tracing` handler for automatic span collection; opt out with `--no-tracing`
- **Composable hooks** — Caddy registration, dashboard hosting, and hosts-file updates independently reconcile complete discovery snapshots
- **Caddy integration** — Reconciles routes and TLS policies with a separately running Caddy process through its admin API
- **Windows service** — Install as a service that reports Docker and Caddy connectivity, or as a continuous controller that keeps reconciling

## Quick Start

### 1. Start Caddy

Install and start ordinary [Caddy](https://caddyserver.com/docs/install) on your host with its admin API enabled. Caddy's default admin endpoint is `http://localhost:2019`. The repository ships [scripts/windows/install-caddy-service.ps1](scripts/windows/install-caddy-service.ps1) to register Caddy under NSSM with delayed automatic start and the internal CA trusted in the LocalMachine root store.

### 2. Start your containers

Start any Docker container that publishes a port:

```bash
docker run -d --name my-app -p 8080:80 nginx:alpine
```

### 3. Run devlocal

devlocal runs **directly on your host** and attaches to a running Caddy over its admin API. A subcommand is always required:

```powershell
just pack
devlocal tool install --global DevLocal.Tool --add-source artifacts
devlocal start
```

`devlocal start` reconciles immediately, then watches Docker events and polls as a safety net. Visit `https://my-app.dev.local` (hosts entry written automatically) or `https://my-app.localhost` (no hosts entry needed).

Containers must publish a port to be reachable — unpublished containers are listed on the dashboard but not proxied.

To remove everything devlocal manages:

```powershell
devlocal clean
```

`devlocal` with no arguments prints usage and exits, as does `devlocal version`.

## Docker Compose

Just run your Compose app with published ports; no special devlocal service is needed.

```yaml
services:
  my-app:
    image: nginx:alpine
    ports:
      - "8080:80"
```

Start it, then run `devlocal start` to register `{project}.{service}.dev.local`.

## Labels

| Label | Value | Effect |
|---|---|---|
| `dev.local` | `false`, `"false"`, `0`, `no` | Skip this container |
| `dev.local.domains` | `port:domain;port:domain` | Custom domain mappings |
| `org.opencontainers.image.logo` | URL | Custom icon for the container on the dashboard (takes priority over `com.docker.extension.icon`) |
| `com.docker.extension.icon` | URL | Custom icon for the container on the dashboard |

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
| `--stale-ttl` | `DEVLOCAL_STALE_TTL` | `1h` | Keep configuration for stopped containers |
| `--probe-timeout` | `DEVLOCAL_PROBE_TIMEOUT` | `2s` | HTTP probe timeout |
| `--hosts-file` | `DEVLOCAL_HOSTS_FILE` | `true` | Manage hosts file entries for domains |
| `--no-tracing` | `DEVLOCAL_TRACING=false` | tracing enabled | Disable OpenTelemetry tracing on dynamic routes |
| `--poll-interval` | `DEVLOCAL_POLL_INTERVAL` | `30s` | Periodically refresh as a safety net for missed Docker events; `0` disables |
| `--caddy` | — | `true` | Register routes and TLS policies with Caddy |
| `--ui` | — | `true` | Host the dashboard and register its route |
| `--ui-port` | `DEVLOCAL_UI_PORT` | `0` | Port for the in-process dashboard host; `0` picks a random free loopback port |
| `--caddy-admin` | `DEVLOCAL_CADDY_ADMIN` | `http://localhost:2019` | Caddy admin API URL |
| `--caddy-server` | `DEVLOCAL_CADDY_SERVER` | `srv0` | Caddy HTTP server name |
| `--allow-create-server` | — | `true` | Create the target Caddy HTTP server when absent |
| `--docker-pipe` | — | from `docker context inspect` | Docker named pipe to connect to, such as `docker_engine` or `dockerDesktopLinuxEngine` |
| `--log-level` | `DEVLOCAL_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error` |

Values come from flags, then `DEVLOCAL_*` environment variables, then defaults. Caddy, dashboard, and hosts-file integration are enabled by default.

> **Note:** Docker events, polling, and stale cleanup all trigger serialized discovery refreshes. Each hook runs on an independent worker and receives the newest complete snapshot, coalescing queued work.

## Caddy Configuration

devlocal does not load or modify a Caddyfile. Start and configure Caddy normally, then point devlocal at its admin API with `--caddy-admin` when the endpoint differs from the default.

devlocal applies dynamic container routes, its TLS policy, and the dashboard route through Caddy's [admin API](https://caddyserver.com/docs/api). It owns only resources with stable `devlocal-` IDs and preserves unrelated routes and policies. Every reconcile reads actual Caddy state, adopts existing resources, removes owned orphans, and targets `srv0` by default, creating that server when absent; pass `--allow-create-server=false` to require an existing server.

## Building from Source

Requires the [.NET 10 SDK](https://dotnet.microsoft.com/download) on **Windows**. A Windows `dotnet` command is needed — the SDK in WSL does not provide one. Build with:

```powershell
just                        # Format check, tests, and pack the tool (default recipe)
just check                  # Format check + tests
just test                   # Tests only
just format                 # dotnet format verification
just build                  # Build the solution
just pack                   # Pack DevLocal.Tool into artifacts/
just publish                # Publish a runnable tool into artifacts/publish/
just clean                  # Clean build output
```

Set `ARTIFACTS` to change the output directory and `CONFIGURATION` to build `Debug` instead of the default `Release`. See `just --list` for all recipes.

To install the packed tool:

```powershell
just pack
dotnet tool install --global DevLocal.Tool --add-source artifacts
```

To try a newer build:

```powershell
just pack
dotnet tool update --global DevLocal.Tool --add-source artifacts
```

The package version comes from `version.json` at the repository root via [Nerdbank.GitVersioning](https://dotnet.github.io/Nerdbank.GitVersioning/), so bump that file by hand for the next release. Never set `<Version>` in a project file; it wins over the generated version. See [docs/csharp-port.md](docs/csharp-port.md#versioning) for the details.

## Running

Run Caddy separately with its admin API enabled, then start devlocal in its own terminal:

```powershell
devlocal start
```

devlocal attaches to Caddy over its admin API and never starts or stops it. Keep Caddy in its own window (or run it as a service) so a console close does not take both down: on Windows a console close or Ctrl-C broadcasts a signal to every process attached to that console.

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

## Windows Service

`devlocal service` manages an installed `DevLocal` Windows service. Run these from an Administrator PowerShell session.

```powershell
devlocal service install --caddy-admin http://localhost:2019
devlocal service status
devlocal service logs
```

The service is registered with delayed automatic start and restart-on-failure, runs as LocalSystem, executes the copied tool through the installed .NET 10 runtime, and reports Docker and Caddy connectivity. By default it runs in **connectivity-monitor mode** and only checks that Docker and Caddy are reachable.

Pass `--controller` to install it as a **continuous controller** that reconciles Caddy, the hosts file, and the dashboard:

```powershell
devlocal service install --controller --caddy-admin http://localhost:2019
```

`service upgrade` stops the service, copies new runtime files, and starts it again, preserving the current mode. Uninstalling leaves settings and logs behind for inspection. To switch modes, uninstall and reinstall with the desired option.

Other subcommands: `start`, `stop`, `restart`, `uninstall`.

The service reads the Docker named pipe from `docker context inspect` at install time and persists it; override with `--docker-pipe`. Runtime files are copied to `%ProgramFiles%\DevLocal`; settings, status, and logs live under `%ProgramData%\DevLocal` (`service.json`, then `status.json`/`service.log` in monitor mode or `controller-status.json`/`controller.log` in controller mode). Controller mode refreshes its heartbeat file every 10 seconds, so a stale `UpdatedAt` means the controller stopped writing.

## Example

See the [example directory](example/) for a complete demo with multiple containers.

```powershell
cd example/standalone
docker compose up -d
# Then, from another terminal:
devlocal start
```

Then visit:
- `https://dev.local` — Dashboard (also available at `https://dev.localhost`)
- `https://standalone.web.dev.local` — nginx web server
- `https://standalone.api.dev.local` — API server
- `https://standalone.multi.dev.local` — Multi-port service
- `https://myapp.custom.local` — Custom domain

Non-HTTP services like `mssql` are also registered (see them on the dashboard); SQL Server listens on port `1433` (`SA` / `DevLocalPass123!`).

## How It Works

1. Lists all containers over the selected Docker named pipe
2. Computes domains from container labels (Compose project/service or container name)
3. Registers running containers that publish at least one port; unpublished containers are skipped
4. Probes only each running container's published host ports at `localhost:{published_port}` to find the HTTP server (common ports 80, 8080, 443, 8443 are checked first)
5. Reconciles stable, owned route and TLS policy IDs against Caddy's actual configuration, preserving unrelated resources and adopting state after restarts
6. Hosts the dashboard in-process on a loopback port and registers its route with Caddy, then updates the hosts file
7. Watches Docker events and refreshes periodically according to `--poll-interval`

## Hosts File

devlocal automatically manages entries in `C:\Windows\System32\drivers\etc\hosts` so domains resolve locally without configuring DNS. Since the proxy runs on your host, it can write the hosts file directly.

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

The block is rewritten on every refresh. The TLD (`dev.local`) and its `.localhost` alias (`dev.localhost`) always point at the dashboard; container `.dev.local` and `.localhost` domains are included so non-browser tools (curl, API clients, etc.) can resolve them without relying on DNS. The file is written as UTF-8 without a BOM. Removed names keep resolving until the DNS client cache is flushed, so run `ipconfig /flushdns` after a removal.

### Opt Out

Disable hosts file management entirely:

```powershell
devlocal start --hosts-file=false
# or
$env:DEVLOCAL_HOSTS_FILE = "false"; devlocal start
```

### Cleanup

Remove all resources managed by the enabled hooks:

```powershell
devlocal clean
```

`devlocal clean` runs cleanup for every enabled hook: it removes owned Caddy routes and TLS policies, stops the dashboard host, and removes the managed hosts block. It accepts the normal hook and Caddy connection flags. Cleanup is explicit and does not run automatically when the controller stops.

## OpenTelemetry Tracing

All dynamic reverse proxy routes include Caddy's [tracing handler](https://caddyserver.com/docs/modules/caddyhttp.tracing) by default. The handler creates spans using the standard OTel SDK, which auto-configures from environment variables like `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_TRACES_EXPORTER`. The dashboard route is not instrumented to avoid noise from periodic refreshes.

### Opt Out

Disable tracing on dynamic routes:

```powershell
devlocal start --no-tracing
# or
$env:DEVLOCAL_TRACING = "false"; devlocal start
```

## Hook Composition

The application enables all built-in hooks by default. Disable components independently with boolean flags:

```powershell
devlocal start --caddy=false --ui=false  # Hosts-file updates only
devlocal start --hosts-file=false        # Caddy registration and dashboard only
devlocal start --ui=false                # Caddy registration without the dashboard route
```

Hooks implement an apply method receiving the current snapshot and a cleanup method, then register with the hook runtime. Caddy and dashboard run in an ordered sequence so the retrieved config matches the reconciled update. Cleanup is invoked explicitly with `devlocal clean`.

## Troubleshooting

- **Windows Schannel fails HTTPS** — Caddy's internal CA publishes no CRL or OCSP responder, so command-line clients fail the handshake with `CRYPT_E_NO_REVOCATION_CHECK` even though the chain is trusted. Pass `--ssl-no-revoke` to `curl.exe`; browsers are unaffected.
- **Docker reports disconnected in monitor mode** — Docker Desktop usually starts at user login, so the service reports a disconnected state until the pipe becomes available, then connects on a later check.
- **A domain still resolves after removal** — flush the DNS client cache with `ipconfig /flushdns`.
- **Certificates accumulate in `caddy-data`** — Caddy does not prune issued certificates for domains devlocal stops managing. This is disk growth only; routes and policies reconcile correctly.

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach
- [Caddy](https://caddyserver.com/docs/) — The web server this project configures

## License

MIT