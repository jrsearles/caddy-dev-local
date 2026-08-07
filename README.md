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
- **Index page** — Visit `dev.local` to see all registered containers; standalone containers (no Compose project) list at the top, Compose containers are grouped under collapsible project sections (collapsed by default); multi-port containers are listed as one row per port (HTTP port first, then ordered by port and domain), and each domain has a copy-to-clipboard button that copies the full URL for HTTP(S) services and `domain:port` for non-HTTP services. Containers whose image matches a well-known project show a brand icon next to the image name (sourced from [Simple Icons](https://simpleicons.org/), or the project's official brand assets where available); override it per-container with the open-standard labels below.
- **Stale cleanup** — Stopped containers stay listed on the index page (marked stopped) until the stale TTL expires, then their config is removed
- **Standalone hosts binary** — `devlocal-hosts` watches Docker and maintains hosts entries without running a proxy

## Quick Start

### 1. Create the Docker network

```bash
docker network create devlocal
```

### 2. Run the proxy

```bash
docker run -d \
  --name devlocal \
  -p 80:80 \
  -p 443:443 \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v caddy_data:/data \
  --network devlocal \
  ghcr.io/jsearles/caddy-devlocal:latest
```

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
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - caddy_data:/data
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
| `--hosts-file` | `DEVLOCAL_HOSTS_FILE` | `true` | Manage `/etc/hosts` entries for domains |
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

## Standalone Mode

When running the binary directly on your host (not inside Docker), caddy-dev-local auto-detects standalone mode and proxies to containers via `localhost` using their published (host-mapped) ports instead of Docker DNS names.

### Quick Start

```bash
just build-linux-amd64
./artifacts/binaries/linux-amd64/caddy devlocal
```

Containers must publish ports to be reachable in standalone mode. Containers without published ports are automatically skipped.

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
| Domain suffixes | `{tld}` | `{tld}` + `.localhost` |
| Detection | `/.dockerenv` present | `/.dockerenv` absent |

### `.localhost` Domains

In standalone mode, each container also gets a `.localhost` domain in addition to the configured TLD. Browsers treat `.localhost` as a secure context without needing a certificate, so these URLs work without any TLS warnings.

- Compose services: `{project}.{service}.localhost`
- Standalone containers: `{container-name}.localhost`

These domains are not generated when custom `dev.local.domains` labels are set.

## Example

See the [example directory](example/) for a complete demo with multiple containers.

```bash
cd example
docker compose up -d
```

Then visit:
- `https://dev.local` — Index page
- `https://example.web.dev.local` — nginx web server
- `https://example.api.dev.local` — API server
- `https://myapp.custom.local` — Custom domain

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
| `index.html` | Served at the TLD (e.g. `http://dev.local`) as a status page listing discovered containers |
| `devlocal.json` | The last successfully applied devlocal config (routes, TLS policy, index route) — useful for debugging; only rewritten when the config actually changes |

## Hosts File

caddy-dev-local automatically manages entries in your system hosts file (`/etc/hosts` on Linux, `C:\Windows\System32\drivers\etc\hosts` on Windows) so domains resolve locally without configuring DNS.

Entries are written inside a managed block with searchable markers:

```
# dev-local:BEGIN
# Managed by caddy-dev-local — do not edit.
127.0.0.1    myapp.web.dev.local
127.0.0.1    myapp.web.localhost
127.0.0.1    my-nginx.dev.local
127.0.0.1    my-nginx.localhost
# dev-local:END
```

The block is updated on every config reload — added when containers start, removed when they stop. Both `.dev.local` and `.localhost` domains are included so non-browser tools (curl, API clients, etc.) can resolve them without relying on DNS.

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
- **Standalone detection** — auto-detected exactly like the plugin (`/.dockerenv` absent → standalone mode, which also registers `.localhost` variants).
- **Permissions** — requires root to write `/etc/hosts`; exits with an error if the hosts file isn't writable (unlike the plugin, which warns and continues).
- **Index page** — not generated; this binary only maintains the hosts file.

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach
- [Caddy](https://caddyserver.com/docs/) — The web server this project extends

## License

MIT
