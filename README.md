# caddy-dev-local

A Caddy plugin that automatically registers `{project}.{service}.dev.local` domains for Docker containers, with HTTP port probing, self-signed TLS, and a built-in index page — inspired by OrbStack's container domain feature.

> **Warning**: This plugin is designed for local development environments only. It uses self-signed TLS, auto-manages hosts files, and assumes trusted networks. Do not use in production.

## Features

- **Automatic domain registration** — Containers on a shared Docker network get `*.dev.local` domains
- **Compose-aware** — Uses `{project}.{service}.dev.local` for Compose services, `{container-name}.dev.local` for standalone containers
- **HTTP port probing** — For containers with multiple ports, automatically detects the HTTP port, preferring common ports (80, 8080, 443, 8443)
- **Self-signed TLS** — Zero-config HTTPS using Caddy's internal CA
- **Custom domains** — Override auto-registration with `dev.local.domains` label
- **Hosts file integration** — Automatically adds entries to `/etc/hosts` for local DNS resolution
- **Index page** — Visit `dev.local` to see all registered containers
- **Stale cleanup** — Automatically removes config for containers stopped > 1 hour

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

```bash
docker run -d \
  --name my-app \
  --network devlocal \
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
| `--ingress-network` | `DEVLOCAL_INGRESS_NETWORK` | `devlocal` | Docker network name |
| `--tld` | `DEVLOCAL_TLD` | `dev.local` | Top-level domain |
| `--stale-ttl` | `DEVLOCAL_STALE_TTL` | `1h` | Keep config for stopped containers |
| `--probe-timeout` | `DEVLOCAL_PROBE_TIMEOUT` | `2s` | HTTP probe timeout |
| `--hosts-file` | `DEVLOCAL_HOSTS_FILE` | `true` | Manage `/etc/hosts` entries for domains |
| `--config` | `DEVLOCAL_CONFIG` | (auto-detect) | Path to a static Caddyfile for global options only |

## Custom Caddyfile

caddy-dev-local auto-detects `Caddyfile`, `Caddyfile.json`, `Caddyfile.json5`, or `Caddyfile.yaml` in the working directory. Use `--config` or `DEVLOCAL_CONFIG` to specify a different path.

**The static Caddyfile is for global options only.** Site blocks (e.g., `example.com { ... }`) are ignored with a warning. Use the [admin API](https://caddyserver.com/docs/api) for site configuration — caddy-dev-local manages container routes via the API automatically.

Example global options:

```
{
    email admin@example.com
    debug
    grace_period 10s
}
```

## Building from Source

Requires [just](https://github.com/casey/just) and [golangci-lint](https://golangci-lint.run/).

```bash
just install-lint           # Install golangci-lint (one-time)
just build-linux-amd64      # Build for linux-amd64
just build-all              # Build for all platforms
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

- **No ingress network configured** — standalone mode is auto-detected by checking for `/.dockerenv`. If absent, the binary is running on the host.
- **`--ingress-network` flag or `DEVLOCAL_INGRESS_NETWORK` env var set** — assumes inside Docker, uses container DNS names and private ports.

### Standalone vs Docker Mode

| | Docker Mode | Standalone Mode |
|---|---|---|
| Proxy target | `{container}:{private_port}` | `localhost:{published_port}` |
| Port selection | Private (internal) ports | Published (host-mapped) ports |
| Unpublished containers | Included | Skipped |
| Domain suffixes | `{tld}` | `{tld}` + `.localhost` |
| Detection | `--ingress-network` set or `/.dockerenv` present | `/.dockerenv` absent, no ingress network set |

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

1. Watches Docker events for container lifecycle and network changes on the ingress network
2. Lists all containers on the configured ingress network
3. Computes domains from container labels (Compose project/service or container name)
4. For multi-port containers, probes ports to find the HTTP server (common ports 80, 8080, 443, 8443 are checked first)
5. In Docker mode, probes via the container name; in standalone mode, probes via `localhost`
6. Generates a Caddyfile with `tls internal` for self-signed HTTPS
7. Loads the config into Caddy with zero downtime

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
```

### Permissions

On Linux, writing to `/etc/hosts` requires root. If the process doesn't have write permission, caddy-dev-local logs a warning and skips hosts file updates (Caddy still works normally).

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach
- [Caddy](https://caddyserver.com/docs/) — The web server this project extends

## License

MIT
