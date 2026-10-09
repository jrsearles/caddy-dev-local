# caddy-dev-local

A Windows tool that gives every Docker container a friendly local domain — like `https://my-app.dev.local` — with HTTPS and zero proxy config. devlocal watches Docker, registers routes in a locally running Caddy, manages your hosts file, and serves a dashboard at `https://dev.localhost`.

> **Warning**: Local development only. It uses self-signed TLS and edits the hosts file. Do not use in production.

## What you get

- Any container that publishes a port is reachable at `https://{name}.dev.local`; Compose services get `{project}.{service}.dev.local`
- Automatic HTTPS through Caddy's internal CA
- Multi-port containers are probed to find the HTTP port (prefers 80, 8080, 443, 8443)
- A live dashboard at `https://dev.localhost` with container status, search, Docker Desktop links, and the effective Caddy config
- Stopped containers stay listed for an hour, then their configuration is cleaned up

## Quick Start

You need Windows, Docker Desktop, [Caddy](https://caddyserver.com/docs/install), and the [.NET 10 SDK](https://dotnet.microsoft.com/download) with [just](https://github.com/casey/just) to build the tool.

1. **Start Caddy** with its admin API (default `http://localhost:2019`). Easiest option: run [scripts/install-caddy-service.ps1](scripts/install-caddy-service.ps1) from an Administrator prompt — it installs Caddy as an auto-start service and trusts its internal CA.

2. **Start a container that publishes a port:**

   ```bash
   docker run -d --name my-app -p 8080:80 nginx:alpine
   ```

3. **Install and start devlocal:**

   ```powershell
   just pack
   dotnet tool install --global DevLocal.Tool --add-source artifacts
   devlocal start
   ```

4. Visit **https://my-app.dev.local** — or **https://dev.localhost** for the dashboard.

Everything must run on one host: devlocal proxies to `localhost:{published-port}`. Keep Caddy in its own window (or run it as a service) — closing a shared console on Windows takes both processes down.

To remove everything devlocal created (routes, TLS policies, hosts entries):

```powershell
devlocal clean
```

## Docker Compose

No extra configuration — just publish ports:

```yaml
services:
  my-app:
    image: nginx:alpine
    ports:
      - "8080:80"
```

`docker compose up -d`, and the service is at `https://{project}.{service}.dev.local`.

## Labels

| Label | Value | Effect |
| --- | --- | --- |
| `dev.local` | `false`, `0`, `no` | Skip this container |
| `dev.local.domains` | `port:domain;port:domain` | Custom domains instead of the generated ones |
| `org.opencontainers.image.logo` | URL | Dashboard icon (takes priority over `com.docker.extension.icon`) |
| `com.docker.extension.icon` | URL | Dashboard icon |

```yaml
labels:
  - "dev.local.domains=8080:api.custom.local;8080:api.alt.local"
```

Custom domains replace the generated ones, including the `.localhost` alias.

## How domains work

- Each container gets both `{name}.dev.local` and `{name}.localhost`.
- `.localhost` works in browsers with no hosts entry and no TLS warnings, since browsers treat it as a secure context.
- devlocal writes a managed block to `C:\Windows\System32\drivers\etc\hosts` (between `# dev-local:BEGIN` and `# dev-local:END`) so `dev.local` names also resolve for curl and other tools. After a domain is removed, run `ipconfig /flushdns`.
- Containers without a published port appear on the dashboard but are not proxied.

## Configuration

| Flag | Env Var | Default | Description |
| --- | --- | --- | --- |
| `--tld` | `DEVLOCAL_TLD` | `dev.local` | Top-level domain |
| `--stale-ttl` | `DEVLOCAL_STALE_TTL` | `1h` | Keep configuration for stopped containers |
| `--probe-timeout` | `DEVLOCAL_PROBE_TIMEOUT` | `2s` | HTTP probe timeout |
| `--no-tracing` | `DEVLOCAL_TRACING=false` | tracing enabled | Disable OpenTelemetry tracing on dynamic routes |
| `--poll-interval` | `DEVLOCAL_POLL_INTERVAL` | `30s` | Periodically refresh as a safety net for missed Docker events; `0` disables |
| `--ui-port` | `DEVLOCAL_UI_PORT` | `0` | Port for the in-process dashboard host; `0` picks a random free loopback port |
| `--caddy-admin` | `DEVLOCAL_CADDY_ADMIN` | `http://localhost:2019` | Caddy admin API URL |
| `--caddy-server` | `DEVLOCAL_CADDY_SERVER` | `srv0` | Caddy HTTP server name |
| `--allow-create-server` | — | `true` | Create the target Caddy HTTP server when absent |
| `--log-level` | `DEVLOCAL_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error` |

Values come from flags, then `DEVLOCAL_*` environment variables, then defaults. Everything is enabled by default. The Docker named pipe is detected from `docker context inspect` at connect time and falls back to `docker_engine`.

### Turning pieces off

```powershell
devlocal start --no-tracing             # No OpenTelemetry tracing on routes
```

### Caddy

devlocal never starts or stops Caddy and doesn't read a Caddyfile — it talks to a running Caddy over the admin API and manages only the routes and TLS policies it owns (IDs prefixed `devlocal-`) in server `srv0`, creating that server unless `--allow-create-server=false`. Your other Caddy configuration is left untouched.

### Tracing

Dynamic routes include Caddy's [OpenTelemetry tracing handler](https://caddyserver.com/docs/modules/caddyhttp.tracing), configured through standard `OTEL_*` environment variables such as `OTEL_EXPORTER_OTLP_ENDPOINT`. The dashboard route is not traced. Disable with `--no-tracing`.

## Windows Service

From an Administrator prompt:

```powershell
devlocal service install   # Continuous controller: full reconciliation in the background
devlocal service status
devlocal service logs
```

The service starts automatically (delayed) with restart-on-failure and runs as LocalSystem. Settings, status, and logs live under `%ProgramData%\DevLocal`. Other subcommands: `upgrade`, `start`, `stop`, `restart`, `uninstall`. Uninstalling keeps settings and logs. The Docker pipe is detected from `docker context inspect` when the controller connects.

## Example

A complete demo lives in [example/](example/):

```powershell
cd example/standalone
docker compose up -d
devlocal start   # from another terminal
```

Then visit:

- `https://dev.localhost` — Dashboard
- `https://standalone.web.dev.local` — nginx web server
- `https://standalone.api.dev.local` — API server
- `https://myapp.custom.local` — Custom domain

The demo also includes non-HTTP services (PostgreSQL, Redis, SQL Server on port `1433`, `SA` / `DevLocalPass123!`), which appear on the dashboard.

## Troubleshooting

- **curl fails the HTTPS handshake** — Caddy's internal CA publishes no CRL or OCSP responder, so Windows Schannel reports `CRYPT_E_NO_REVOCATION_CHECK`. Pass `--ssl-no-revoke` to `curl.exe`; browsers are unaffected.
- **A removed domain still resolves** — flush the DNS client cache with `ipconfig /flushdns`.
- **Docker shows disconnected on the dashboard** — Docker Desktop usually starts at user login; the service connects once the pipe becomes available.
- **Certificates accumulate in `caddy-data`** — Caddy never prunes issued certificates for domains devlocal stops managing. Harmless disk growth only.

## Building from Source

Requires the [.NET 10 SDK](https://dotnet.microsoft.com/download) on Windows — the SDK in WSL does not provide a Windows `dotnet` command.

```powershell
just          # Format check, tests, and pack the tool (default)
just check    # Format check + tests
just test     # Tests only
just pack     # Pack DevLocal.Tool into artifacts/
just --list   # All recipes
```

Set `ARTIFACTS` to change the output directory and `CONFIGURATION` to build `Debug` instead of `Release`.

```powershell
dotnet tool install --global DevLocal.Tool --add-source artifacts  # Install
dotnet tool update --global DevLocal.Tool --add-source artifacts   # Upgrade
```

The package version comes from `version.json` via [Nerdbank.GitVersioning](https://dotnet.github.io/Nerdbank.GitVersioning/) — bump that file by hand for the next release, and never set `<Version>` in a project file (it wins over the generated version). See [docs/csharp-port.md](docs/csharp-port.md#versioning) for details.

## Acknowledgements

- [OrbStack](https://orbstack.dev/) — Container domain feature inspired the domain convention and automatic registration model
- [caddy-docker-proxy](https://github.com/caddy-docker/proxy) — Pioneered Caddy as a Docker reverse proxy; this project takes a more opinionated, zero-config approach
- [Caddy](https://caddyserver.com/docs/) — The web server this project configures

## License

MIT
