# Standalone Mode Example

Run caddy-dev-local directly on your host while containers run in Docker. The binary auto-detects standalone mode when `/.dockerenv` is absent.

## Setup

Build the binary:

```bash
cd ../..
./build.sh
```

Start the containers:

```bash
cd example/standalone
docker compose up -d
```

Run the proxy on your host:

```bash
sudo ../../artifacts/binaries/linux-amd64/caddy devlocal
```

> `sudo` is required to write to `/etc/hosts`. Use `--no-host-files` to skip hosts file management.

## Containers

| Container | Ports | Domain(s) |
|---|---|---|
| `web` | 8080:80 | `web.dev.local`, `web.localhost` |
| `api` | 9090:3000 | `api.dev.local`, `api.localhost` |
| `multi` | 3000:3000, 8081:8080 | `multi.dev.local`, `multi.localhost` (HTTP port auto-detected) |
| `ignored` | — | Skipped via `dev.local=false` label |
| `custom` | 8082:80 | `myapp.custom.local`, `myapp.alt.local` |
| `worker` | — | Shown on index page with "no ports exposed" (Redis, no published ports) |
| `mssql` | 1433:1433 | `mssql.dev.local`, `mssql.localhost` (SQL Server, no HTTP) |

## URLs

- `https://dev.local` — Index page
- `https://web.dev.local` — nginx web server
- `https://api.dev.local` — API server
- `https://multi.dev.local` — Multi-port service
- `https://myapp.custom.local` — Custom domain
- Any `.localhost` variant (e.g., `https://web.localhost`) — works without TLS warnings

## Cleanup

```bash
docker compose down
sudo ../../artifacts/binaries/linux-amd64/caddy devlocal-clean
```
