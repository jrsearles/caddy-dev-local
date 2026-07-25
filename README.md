# caddy-dev-local

A Caddy plugin that automatically registers `{project}.{service}.dev.local` domains for Docker containers, with HTTP port probing, self-signed TLS, and a built-in index page — inspired by OrbStack's container domain feature.

## Features

- **Automatic domain registration** — Containers on a shared Docker network get `*.dev.local` domains
- **Compose-aware** — Uses `{project}.{service}.dev.local` for Compose services, `{container-name}.dev.local` for standalone containers
- **HTTP port probing** — For containers with multiple ports, automatically detects the HTTP port
- **Self-signed TLS** — Zero-config HTTPS using Caddy's internal CA
- **Custom domains** — Override auto-registration with `dev.local.domains` label
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
| `--poll-interval` | `DEVLOCAL_POLL_INTERVAL` | `30s` | Fallback polling interval |
| `--probe-timeout` | `DEVLOCAL_PROBE_TIMEOUT` | `2s` | HTTP probe timeout |

## Building from Source

```bash
go build -o caddy-dev-local .
./caddy-dev-local devlocal
```

Or with Docker:

```bash
docker build -t caddy-dev-local .
```

## Standalone Mode

When running the binary directly on your host (not inside Docker), caddy-dev-local auto-detects standalone mode and proxies to containers via `localhost` using their published (host-mapped) ports instead of Docker DNS names.

### Quick Start

```bash
go build -o caddy-dev-local .
./caddy-dev-local devlocal
```

Containers must publish ports to be reachable in standalone mode. Containers without published ports are automatically skipped.

```bash
docker run -d --name my-app -p 8080:80 nginx:alpine
# Available at https://my-app.dev.local → localhost:8080
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
| Detection | `--ingress-network` set or `/.dockerenv` present | `/.dockerenv` absent, no ingress network set |

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

1. Watches Docker events for container start/stop
2. Lists all containers on the configured ingress network
3. Computes domains from container labels (Compose project/service or container name)
4. For multi-port containers, probes ports in order to find the HTTP server
5. Generates a Caddyfile with `tls internal` for self-signed HTTPS
6. Loads the config into Caddy with zero downtime

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach

## License

MIT
