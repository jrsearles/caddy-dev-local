# caddy-dev-local

A Caddy plugin that automatically registers `{project}.{service}.dev.local` domains for Docker containers, with HTTP port probing, self-signed TLS, and a built-in index page — inspired by OrbStack's container domain feature.

> **Warning**: This plugin is designed for local development environments only. It uses self-signed TLS, auto-manages hosts files, and assumes trusted networks. Do not use in production.

## Features

- **Automatic domain registration** — Any container the proxy can reach gets `*.dev.local` domains. Containers on a network shared with the proxy are reached by container name via Docker DNS; containers on other networks are discovered too and proxied via their published ports through the host gateway
- **Compose-aware** — Uses `{project}.{service}.dev.local` for Compose services, `{container-name}.dev.local` for standalone containers
- **HTTP port probing** — For containers with multiple ports, automatically detects the HTTP port, preferring common ports (80, 8080, 443, 8443)
- **Self-signed TLS** — Zero-config HTTPS using Caddy's internal CA
- **Custom domains** — Override auto-registration with `dev.local.domains` label
- **Hosts file integration** — Automatically adds entries to `/etc/hosts` for local DNS resolution
- **Index page** — Visit `dev.local` (or `dev.localhost`) to see all registered containers, with the following features:
  - **Cards** show each container's icon, image, IP, health badge (`healthy`/`starting`/`unhealthy`), and running/stopped status with live relative timestamps ("since 3m ago")
  - **Search** — filter across name, image, project, service, and domains using the header search box; filter terms are space-separated (all must match); query is synced to the URL as `?q=` for bookmarking; matching project sections auto-expand while filtering and collapse back when the filter is cleared
  - **Compose groups** — services grouped under collapsible project sections with up/down counts; expansion state and active tab are preserved across live reloads via `sessionStorage` and reflected in the URL (`?open=`, `?tab=`)
  - **Detail drawer** — click any container card header to slide open a side panel with full image, short container ID, IP, networks, published port table, health status, and filtered labels (`dev.local.*`, `com.docker.compose.*`, `org.opencontainers.image.*`); includes an "Open in Docker Desktop" button
  - **Domain rows** — copy button per domain copies `host:port`; a globe icon links directly to the service in the browser
  - **Docker Desktop links** — the icon next to "Containers" opens Docker Desktop's dashboard (`docker-desktop://dashboard/open`); each Compose project section header links to that project's view (`docker-desktop://dashboard/apps/{project}`)
  - **Live refresh** — polls a lightweight `/version.json` endpoint every 5 seconds and reloads only on change; scroll position is preserved across reloads; falls back to full-page hash polling if `version.json` is unavailable
  - **Discovery banner** — a dismissible error banner appears at the top when Docker event streaming fails, showing the last error and time of last successful refresh
  - **Caddy config tab** — shows the effective running Caddy config as a collapsible JSON tree (via [json-view](https://github.com/pgrabovets/json-view)) with expand/collapse-all; toggle to raw JSON; only appears if a config is available
  - **Theme** — defaults to system preference; header toggle cycles light → dark → system
- **Stale cleanup** — Stopped containers stay listed on the index page (marked stopped) until the stale TTL expires, then their config is removed
- **OpenTelemetry tracing** — Dynamic reverse proxy routes include Caddy's `tracing` handler for automatic span collection; opt out with `--no-tracing`
- **Standalone hosts binary** — `devlocal-hosts` watches Docker and maintains hosts entries without running a proxy

## Quick Start

### 1. Create the Docker network

```bash
docker network create devlocal
```

### 2. Run the proxy

Beyond the basics, the container needs a few extra mounts to work well from your host:

- **Docker socket** (`/var/run/docker.sock`, read-only) — so it can watch and discover containers.
- **Your Caddyfile** (`/etc/caddy/Caddyfile`, read-only) — loaded as-is on top of the auto-generated container routes. Point the proxy at it with `DEVLOCAL_CONFIG=/etc/caddy/Caddyfile`. Omit the mount if you have no Caddyfile.
- **Hosts file** — in Docker mode the proxy runs inside a container and can't write your host's hosts file (see the note below); run the [standalone hosts binary](#standalone-hosts-binary) on your host if you need `*.dev.local` to resolve there. Pass `--hosts-file=false` if you don't want the proxy managing hosts entries at all.
- **Host network access** — containers on other networks are reached via the host gateway. On Linux add `--add-host host.docker.internal:host-gateway`; Docker Desktop for Windows/macOS provides `host.docker.internal` automatically.

#### Linux

```bash
docker run -d \
  --name devlocal \
  -p 80:80 \
  -p 443:443 \
  -p 2019:2019 \
  -e DEVLOCAL_CONFIG=/etc/caddy/Caddyfile \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v caddy_data:/data \
  -v "$PWD/Caddyfile":/etc/caddy/Caddyfile:ro \
  --add-host host.docker.internal:host-gateway \
  --network devlocal \
  ghcr.io/jsearles/caddy-dev-local:latest
```

#### Windows (Docker Desktop, PowerShell)

```powershell
docker run -d `
  --name devlocal `
  -p 80:80 `
  -p 443:443 `
  -p 2019:2019 `
  -e DEVLOCAL_CONFIG=/etc/caddy/Caddyfile `
  -v "//var/run/docker.sock:/var/run/docker.sock:ro" `
  -v caddy_data:/data `
  -v "${PWD}\Caddyfile:/etc/caddy/Caddyfile:ro" `
  --network devlocal `
  ghcr.io/jsearles/caddy-dev-local:latest
```

Notes:

- The `2019:2019` mapping publishes Caddy's [admin API](https://caddyserver.com/docs/api) so you can inspect the live config from your machine (e.g., `curl http://localhost:2019/config/`). Caddy's admin endpoint binds to `localhost:2019` inside the container by default, so the published port only works when your Caddyfile points it at a reachable address in its global options:

```caddyfile
{
	admin 0.0.0.0:2019
}
```

  The admin API is unauthenticated, so keep that in mind on shared networks; omit the mapping if you don't need it.
- In **Docker mode** the proxy runs inside a container, so any hosts entries it writes land in the container's own `/etc/hosts` and don't affect your host machine. For host-side DNS resolution run the [standalone hosts binary](#standalone-hosts-binary) on your host, or pass `--hosts-file=false` if you don't want the proxy touching its container hosts file at all.
- In **Git Bash** for Windows, keep the `//` prefix on the socket path and use forward slashes (MSYS would otherwise rewrite `/var/run/docker.sock`).
- Running from **WSL2**? Run the [standalone hosts binary](#standalone-hosts-binary) inside your WSL distro — it writes to the distro's `/etc/hosts`, which WSL keeps in sync with the Windows hosts file.

### 3. Start your containers

Containers are discovered across **all** Docker networks. Two routing modes:

- **Shared network (preferred)** — attach the container to the `devlocal` network so the proxy reaches it by name over Docker DNS:

```bash
docker run -d \
  --name my-app \
  --network devlocal \
  nginx:alpine
```

- **Other networks** — containers on *any* other network are still discovered and registered. If the container publishes a port, the proxy routes to it via the host gateway (`{gateway}:{published_port}`):

```bash
docker run -d \
  --name my-app \
  -p 8080:80 \
  nginx:alpine
```

Visit `https://my-app.dev.local`

## Docker Compose

```yaml
services:
  devlocal:
    image: ghcr.io/jsearles/caddy-devlocal:latest
    ports:
      - "80:80"
      - "443:443"
      - "2019:2019"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - caddy_data:/data
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
    environment:
      - DEVLOCAL_CONFIG=/etc/caddy/Caddyfile
    extra_hosts:
      - "host.docker.internal:host-gateway"
    networks:
      - devlocal
    restart: unless-stopped

  my-app:
    image: nginx:alpine
    networks:
      - devlocal

networks:
  devlocal:
    name: devlocal
    driver: bridge

volumes:
  caddy_data:
```

The `extra_hosts` entry is a no-op on Docker Desktop (which already resolves `host.docker.internal`) but required on Linux for host-network reachability.

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

> **Note:** The periodic poll backstops any reloads skipped while another reload is in flight. Disabling it (`--poll-interval=0`) is not recommended when running with heavy container churn, since concurrent Docker events can then drop an apply with no scheduled re-check.

## Custom Caddyfile

caddy-dev-local auto-detects `Caddyfile`, `Caddyfile.json`, `Caddyfile.json5`, or `Caddyfile.yaml` in the working directory. Use `--config` or `DEVLOCAL_CONFIG` to specify a different path.

**Your Caddyfile is loaded as-is.** Site blocks, TLS automation policies, logging, etc. are honored exactly as written. caddy-dev-local applies its own dynamic container routes, TLS policy, and index page through Caddy's [admin API](https://caddyserver.com/docs/api) — it never rewrites or merges your config. The two live side by side on the same HTTP server.

## Building from Source

Requires [just](https://github.com/casey/just) and [golangci-lint](https://golangci-lint.run/).

```bash
just install-lint           # Install golangci-lint (one-time)
just build-linux-amd64      # Build for linux-amd64
just build-all              # Build for all platforms
just build-hosts            # Build the standalone devlocal-hosts binaries
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

## Standalone Mode

When running the binary directly on your host (not inside Docker), caddy-dev-local auto-detects standalone mode and proxies to containers via `localhost` using their published (host-mapped) ports instead of Docker DNS names.

### Quick Start

```bash
just build-linux-amd64
./artifacts/binaries/linux-amd64/caddy devlocal
```

Containers must publish ports to be reachable in standalone mode; unpublished containers are skipped.

```bash
docker run -d --name my-app -p 8080:80 nginx:alpine
# Available at https://my-app.dev.local → localhost:8080
# Also available at https://my-app.localhost → localhost:8080
```

### How Detection Works

- **Inside Docker** (`/.dockerenv` present) — Docker mode: containers on a shared network are reached by Docker DNS name, others via published ports through the host gateway. The proxy identifies its own container via the `HOSTNAME` environment variable to compute shared networks.
- **On the host** (`/.dockerenv` absent) — standalone mode: proxies to containers via `localhost` using their published ports.

### Standalone vs Docker Mode

| | Docker Mode | Standalone Mode |
|---|---|---|
| Proxy target | `{container}:{private_port}` (shared network) or `{gateway}:{published_port}` (other networks) | `localhost:{published_port}` |
| Port selection | Private ports (shared network) or published ports (other networks) | Published (host-mapped) ports |
| Unpublished containers | Included only when reachable by Docker DNS | Skipped |
| Detection | `/.dockerenv` present | `/.dockerenv` absent |

Both modes register `.localhost` domain variants (see below).

### `.localhost` Domains

Each container also gets a `.localhost` domain in addition to the configured TLD. Browsers treat `.localhost` as a secure context without needing a certificate, so these URLs work without any TLS warnings.

- Compose services: `{project}.{service}.localhost`
- Other containers: `{container-name}.localhost`

In standalone mode the proxy runs on the host, so `.localhost` reaches the published ports directly. In Docker mode `.localhost` resolves to the client machine's own loopback, so browsing from the host reaches the proxy's published ports just like the `{tld}` domains — without needing a hosts entry.

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

1. Watches Docker events for container lifecycle and network connect/disconnect changes across all networks
2. Lists all containers via the Docker API; identifies the proxy's own container (via `HOSTNAME`) and its network memberships, then excludes itself
3. Computes domains from container labels (Compose project/service or container name)
4. Classifies each container by reachability: shared network → Docker DNS; no shared network but published ports → host gateway; otherwise skipped
5. For multi-port containers, probes ports to find the HTTP server (common ports 80, 8080, 443, 8443 are checked first) — via the container name on shared networks, via the host gateway over published ports otherwise
6. Builds the devlocal config directly as JSON — one `reverse_proxy` route per domain, a single merged `tls internal` policy, and an index page route for the TLD
7. Loads the user Caddyfile as-is with `caddy.Load`, then applies the devlocal routes and TLS policy through Caddy's [admin API](https://caddyserver.com/docs/api) using diff-based patching — only added, removed, or changed routes/policies are touched, so reloads are incremental with zero downtime
8. Polls Docker every `--poll-interval` (default 30s) as a safety net for missed events; if nothing changed, the reload is skipped entirely via a fingerprint of the current domains

## Generated Files

caddy-dev-local writes two files to the user cache directory (`os.UserCacheDir()/caddy-dev-local`):

| File | Purpose |
|---|---|
| `index.html` | Served at the TLD and its `.localhost` alias (e.g. `http://dev.local` / `http://dev.localhost`) as a status page listing discovered containers. Includes an expandable "Caddy Config" panel showing the effective running config (user config + devlocal routes/policies, fetched from the admin API after each reload) |
| `devlocal.json` | The last successfully applied devlocal config (routes, TLS policy, index route) — useful for debugging; only rewritten when the config actually changes |

## Hosts File

caddy-dev-local automatically manages entries in your system hosts file (`/etc/hosts` on Linux, `C:\Windows\System32\drivers\etc\hosts` on Windows) so domains resolve locally without configuring DNS. When running in Docker mode the proxy can't write your host's hosts file from inside the container — use the [standalone hosts binary](#standalone-hosts-binary) on your host instead.

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

Remove all devlocal entries from the hosts file:

```bash
caddy devlocal-clean
# or
devlocal-hosts clean
```

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

## Standalone Hosts Binary

`devlocal-hosts` is a standalone executable that watches Docker and maintains hosts file entries **without running Caddy or any proxy**. It shares the exact same discovery logic, labels, and domain conventions as the Caddy plugin, so the hostnames it registers always match what the proxy would serve. It's useful when you only want DNS resolution for your containers and don't need a reverse proxy.

### Build

```bash
just build-hosts
./artifacts/binaries/linux-amd64/devlocal-hosts
```

### Usage

```bash
devlocal-hosts            # Run in the foreground, watching Docker events
devlocal-hosts clean      # Remove all devlocal entries from the hosts file
devlocal-hosts --help
```

Flags mirror the Caddy plugin's shared options (same env vars, defaults, and precedence):

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `--tld` | `DEVLOCAL_TLD` | `dev.local` | Top-level domain |
| `--stale-ttl` | `DEVLOCAL_STALE_TTL` | `1h` | Keep entries for stopped containers |
| `--poll-interval` | `DEVLOCAL_POLL_INTERVAL` | `30s` | Periodic full refresh as a safety net for missed events; `0` disables |
| `--probe-timeout` | `DEVLOCAL_PROBE_TIMEOUT` | `2s` | Accepted for flag parity; probing is skipped (see below) |

Notes:

- **No port probing** — the domain set is identical to the proxy's, but no HTTP requests are made; port probing only exists to pick a proxy target port.
- **Standalone detection** — auto-detected exactly like the plugin (`/.dockerenv` absent → standalone mode, routing via `localhost` instead of Docker DNS).
- **Permissions** — requires root to write `/etc/hosts`; exits with an error if the hosts file isn't writable (unlike the plugin, which warns and continues).
- **Index page** — not generated; this binary only maintains the hosts file (the proxy's index page and its config panel require Caddy).

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach
- [Caddy](https://caddyserver.com/docs/) — The web server this project extends

## License

MIT
