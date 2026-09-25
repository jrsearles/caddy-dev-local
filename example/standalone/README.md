# Standalone Mode Example

Run the standalone devlocal controller on your host and attach it to a separate Caddy process on the same host.

## Setup

Build the binary:

```bash
cd ../..
just
```

Start the containers:

```bash
cd example/standalone
docker compose up -d
```

Start Caddy, then run the controller in another terminal:

```bash
../../artifacts/linux-amd64/caddy run --config /dev/null
sudo ../../artifacts/linux-amd64/devlocal start
```

> `sudo` is required to write to `/etc/hosts`. Use `--hosts-file=false` to skip hosts file management.

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
sudo ../../artifacts/linux-amd64/devlocal clean
```
