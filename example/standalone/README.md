# Standalone Mode Example

Run devlocal on your Windows host and attach it to a Caddy process on the same host.

## Setup

Install the tool from a local package build:

```powershell
cd ../..
just pack
dotnet tool install --global DevLocal.Tool --add-source artifacts
```

Start Caddy with its admin API enabled (the default is `http://localhost:2019`), then start the containers:

```powershell
cd example/standalone
docker compose up -d
```

Run the continuous controller in another terminal:

```powershell
devlocal start
```

> devlocal writes the hosts file, which needs an Administrator PowerShell session. Use `--hosts-file=false` to skip hosts file management.

## Containers

| Container | Ports | Domain(s) |
|---|---|---|
| `web` | 8080:80 | `standalone.web.dev.local`, `standalone.web.localhost` |
| `api` | 9090:3000 | `standalone.api.dev.local`, `standalone.api.localhost` |
| `multi` | 3000:3000, 8081:8080 | `standalone.multi.dev.local`, `standalone.multi.localhost` (HTTP port auto-detected) |
| `ignored` | - | Skipped via `dev.local=false` label |
| `custom` | 8082:80 | `myapp.custom.local`, `myapp.alt.local` |
| `postgres` | 5432:5432 | `standalone.postgres.dev.local`, `standalone.postgres.localhost` (no HTTP) |
| `worker` | - | Listed on the dashboard with no ports exposed |
| `mssql` | 1433:1433 | `standalone.mssql.dev.local`, `standalone.mssql.localhost` (no HTTP) |

## URLs

- `https://dev.localhost` — Dashboard
- `https://standalone.web.dev.local` — nginx web server
- `https://standalone.api.dev.local` — API server
- `https://standalone.multi.dev.local` — Multi-port service
- `https://myapp.custom.local` — Custom domain
- Any `.localhost` variant (e.g. `https://standalone.web.localhost`) — works without TLS warnings

## Cleanup

```powershell
docker compose down
devlocal clean
ipconfig /flushdns
```

`devlocal clean` removes the owned Caddy routes and TLS policies and the managed hosts block. Flush the DNS cache afterwards so removed names stop resolving immediately.