# Windows C# port

Branch: `port/csharp`, based on Go commit `e2a80d3`. The Go implementation was the behavior reference; it has now been removed from the tree and this document is the remaining port record.

## Current status

The `.NET 10` tool implements `devlocal start` (Docker events and polling) and `devlocal clean`; a bare `devlocal` prints usage and exits, and a subcommand is always required, which diverged from Go where a bare `devlocal` ran the single pass. The one-shot `run` subcommand was removed, so the foreground mode always watches. It lists Docker Desktop containers over the selected named pipe, probes published HTTP ports, reconciles Caddy, hosts the dashboard in-process, and maintains the existing managed hosts block. The Windows takeover, service activation, and reboot checks below all passed, and the Go sources have since been removed from the tree.

On this Windows machine, the native tool package installed successfully and the service started as LocalSystem. The active Docker Desktop pipe and Caddy admin API both reported connected. Stop/start, in-place upgrade, uninstall/reinstall, and logs were exercised successfully. Delayed automatic start is registered with Windows and has now been confirmed across a real reboot, as has independent Docker/Caddy interruption recovery.

`src/DevLocal.Core` contains immutable container models, discovery state, domain targets, hosts blocks and Caddy resource generation. `src/DevLocal.Ui` contains the Blazor dashboard components, view models, embedded CSS/JS assets and the in-process Kestrel host. `src/DevLocal.Tool` contains Docker SDK adapters, polling and event workers, Caddy admin reconciliation, the UI hook, hook composition, CLI and Windows service management. The shared domain, hosts and Caddy JSON fixtures in `tests/Fixtures` are asserted by the C# tests; the Go-only index fixtures and HTML goldens were deleted with the Go sources, since the C# dashboard is no longer template-rendered.

The UI is a Blazor Interactive Server application hosted in-process by `UiServerHost`. Kestrel binds to `127.0.0.1` on a random OS-assigned port (or `--ui-port` / `DEVLOCAL_UI_PORT` to pin one) and Caddy reverse-proxies `dev.localhost` to it, replacing the static `file_server` route with the same `devlocal-index` route id and TLS policy. The dashboard keeps the existing look: it reuses `index.css` (now `src/DevLocal.Ui/Assets/index.css`, moved out of the Go `generator/` package and embedded in `DevLocal.Ui` as `DevLocal.IndexCss`), and the theme toggle, container search, compose-project grouping, details drawer, routes table and Caddy config tree are Blazor components with a small `ui.js` interop file for clipboard and theme storage. The `@pgrabovets/json-view` CDN dependency is gone — the config tab renders a native recursive JSON tree, and live updates flow over the Blazor SignalR circuit instead of the old `version.json` polling, so `version.json` and the `refreshToast` are gone too. Both `start` and controller-service mode always host the UI. The Go byte-parity fixtures (`tests/Fixtures/index_cases.json`, `tests/Fixtures/golden/index`) were deleted along with the Go sources; `UiServerHostTests` and `UiServerHookTests` cover page/CSS/framework-script serving, SignalR negotiation, rendered discovery state, discovery-error banners and the Caddy route/policy reconciliation instead.

The service installed on this machine runs as a continuous controller (`service install` from Administrator PowerShell). Controller mode is the only supported service mode: every install reconciles Docker discovery, Caddy routes, the hosts file and the UI, so installing on a new machine immediately starts editing Caddy and the hosts file; `--no-tracing` is the only piece that can be turned off.

Live hosts-file takeover was validated on Windows while the installed service stayed in monitor mode and the live Caddy on 2019 was down, using the then-available Caddy opt-out so only the hosts file changed. The existing managed block held 12 stale entries from earlier Go-era test containers; one pass removed exactly those lines and nothing else, `clean` restored the file byte for byte outside the block, and a follow-up pass rewrote the block for the only running container. Continuous `start` mode added and removed `devlocal-hosts-probe.{dev.local,localhost}` from Docker start/stop events, and deleting the whole block by hand was repaired on the next poll. Removed names keep resolving until the DNS client cache is flushed, so validation needs `ipconfig /flushdns`. Both implementations write the file as UTF-8 without a BOM, which dropped the BOM this machine's file carried; Docker's own entries and all other lines were preserved. The disposable probe container was removed and the live block was left matching the running containers.

One-pass mode was exercised on Windows against a separate Caddy instance (`tests/Fixtures/caddy_staging.json`) on admin port 20199 and HTTP port 19080. It registered container routes and TLS policies, served the generated index page, and `clean` removed the owned Caddy resources and UI artifacts. The opted-in Windows service was then exercised with that staging Caddy while hosts-file management was still opt-in: starting and stopping a disposable Docker container added and removed its routes, periodic refresh repaired a deleted route, and restarting staging Caddy restored all managed resources. A Docker Desktop port bound to `127.0.0.1` required explicit proxy bypass and an IPv4 probe fallback; the event lifecycle worked after that fix. Staging Caddy and the disposable container were stopped and removed. The normal Caddy process on port 2019 and the live hosts file were not changed during that trial.

The service was then activated as a continuous controller (`service install --controller`) against the live Caddy on 2019 with hosts-file, Caddy, and UI all enabled, and validated across a reboot: the service started 2m33s after boot via delayed automatic start, reported `Mode: continuous controller`, and logged a single `Service started successfully.` event with no errors or warnings. After sign-in it reconciled two running containers into six managed Caddy routes plus the `devlocal-index` route and the `devlocal-tls` / `devlocal-tls-ui` policies, kept the `# dev-local:BEGIN/END` hosts block in sync with exactly those six domains and no BOM, served `https://dev.local` and `https://standalone.api.dev.local` with `200` and a validating chain against the LocalMachine-trusted internal CA, reused certificates persisted in `caddy-data` from before the reboot, and hosted the Blazor dashboard in-process on a random loopback port. Hosts-file takeover and reboot recovery are both validated.

Install the .NET 10 SDK on **Windows** first (the SDK in WSL does not provide a Windows `dotnet` command). This also installs the runtime used by the Windows Service.

Build and pack:

```powershell
just pack
dotnet tool install --global DevLocal.Tool --add-source artifacts
```

`just` wraps the .NET commands (`just test`, `just format`, `just check`, `just build`, `just publish`); see the justfile for the full list.

In Administrator PowerShell:

```powershell
devlocal service install --caddy-admin http://localhost:2019
devlocal service status
devlocal service logs
```

For an isolated continuous-controller trial, first start Caddy with `tests/Fixtures/caddy_staging.json`, then reinstall the service from Administrator PowerShell:

```powershell
devlocal service uninstall
devlocal service install --caddy-admin http://localhost:20199
devlocal service status
```

Stop the controller before `devlocal clean --caddy-admin http://localhost:20199`; otherwise the next refresh re-adds the staging resources. The controller-mode status currently reports service state and mode; discovery/hook errors are written to the Windows Application event log.

The service writes `controller-status.json` and `controller.log` under `%ProgramData%\DevLocal`, refreshed every 10 seconds as a heartbeat. The status carries the mode, the UI base URL, the registered hook names, and every hook error seen so far; the log carries the UI announcement and each hook failure. `service status` and `service logs` always read these files. A stale `UpdatedAt` in `controller-status.json` means the controller stopped writing and the service needs investigating.

Discovery and hook errors are also written to the Windows Application event log under the `DevLocal` source, but treat that log as best-effort: the `Index UI served from {BaseUrl}` entry has never been observed there even though `UiServerHook` provably emits it, and only the host lifetime messages (`Service started successfully.`) reliably appear. Read `controller.log` and `controller-status.json` for authoritative controller state.

The controller detects the named pipe from `docker context inspect` each time it connects, so pipe changes such as `docker_engine` versus `dockerDesktopLinuxEngine` are picked up without reinstalling. If the Docker CLI is unavailable, the fallback is `docker_engine`. The service runs as LocalSystem, which has been verified to reach this machine's Docker Desktop pipe and Caddy admin endpoint. Caddy can run under NSSM using `caddy run` and a fixed configuration and data directory. Docker Desktop typically starts at user login, so the dashboard reports it as disconnected until it becomes available.

The service is registered as `DevLocal` with delayed automatic start and restart-on-failure. Runtime files are copied to `%ProgramFiles%\DevLocal`; settings, status, and logs are stored under `%ProgramData%\DevLocal` (`service.json`, `controller-status.json`, `controller.log`). The service executes the copied tool through the installed .NET 10 runtime, not via the interactive user's tool shim. The service account does not inherit the user's Docker context or environment.

Manage it with `devlocal service status|logs|start|stop|restart|uninstall`. To try an updated tool, run `dotnet tool update --global DevLocal.Tool --add-source artifacts`, then run `devlocal service upgrade` from Administrator PowerShell. The upgrade stops the service, copies the new runtime files, and starts it again. Uninstalling the service leaves settings and logs for inspection. The tool package ID is provisional; the local package source avoids assuming it is published on NuGet.

To try the controller against a staging Caddy, stop the live controller first (Caddy, the hosts file and the dashboard are always managed) and use `devlocal start --caddy-admin http://localhost:20199`. `devlocal clean` with the same flag removes its owned resources; stop that staging Caddy afterward. Running without opt-outs applies Caddy, UI and hosts updates on the host.

Validate on Windows:

1. Confirm Docker and Caddy both report connected after Docker Desktop login and NSSM-managed Caddy startup.
2. Close PowerShell, reboot, sign in, and confirm the service starts and reconnects.
3. Restart Docker Desktop and Caddy independently and check status transitions and logs.
4. Exercise `service upgrade` and `service uninstall` from an elevated terminal.

When smoke-testing HTTPS from the command line, pass `--ssl-no-revoke` to `curl.exe`. Caddy's internal CA publishes no CRL or OCSP responder, so Windows Schannel fails the handshake with `CRYPT_E_NO_REVOCATION_CHECK` and curl reports `http_code=000` even though the chain is trusted and the site is healthy. Browsers do not enforce revocation for these certificates, so a browser test is unaffected.

The service's named-pipe and Caddy connections under LocalSystem have been verified on the target machine, as have container events, recovery after staging Caddy restarts, live hosts-file takeover, and reboot recovery. Caddy runs as the `DevLocalCaddy` NSSM service from `C:\ProgramData\DevLocal\Caddyfile` with `file_system` storage rooted at `C:\ProgramData\DevLocal\caddy-data`; the internal CA (`CN=Caddy Local Authority - 2026 ECC Root`) is trusted in the LocalMachine root store and serves `.dev.local` over HTTPS on 443. Caddy does not prune issued certificates for domains that devlocal stops managing, so `caddy-data\certificates` accumulates certs from removed or renamed containers; this is disk growth only, since routes and policies are reconciled correctly.

## Versioning

The package version comes from `version.json` at the repository root via [Nerdbank.GitVersioning](https://dotnet.github.io/Nerdbank.GitVersioning/), which is referenced from `Directory.Build.props` so every project picks it up. Nothing is stored in the project files. `version` is the plain release number `0.1.0` — no `{height}` counter and no prerelease label — so every build from a public branch packs as `0.1.0` until the file is bumped by hand, and `dotnet tool install` / `dotnet tool update` find it without `--prerelease`.

`devlocal version` prints `devlocal 0.1.0.1 (5ad9eff)`: the fourth component is the commit height, which NB.GV appends to the informational version because the version string itself no longer carries a `{height}` token. It never reaches the package version, so the `.nupkg` is `DevLocal.Tool.0.1.0.nupkg` and `dotnet tool list --global` shows `0.1.0`.

Three things to keep in mind:

- Bump `version` by hand for the next release, e.g. to `0.2.0`. Nothing moves the number on its own, so forgetting to bump it republishes the same version instead of a higher one.
- Build from a commit when the stamped commit matters. An uncommitted `version.json` is read from the working copy with height `0`, which shows up as `0.1.0.0` in `devlocal version` and as an `AssemblyVersion` of `0.1.0.0`.
- Never set `<Version>` in a project file. It wins over the generated version and pins every build to that literal string.

`publicReleaseRefSpec` lists `main` and `port/csharp`, the two branches the tool is released from; a build from any other branch gets a `g<sha>` prerelease label appended (for example `DevLocal.Tool.0.1.0-gd4e9d48.nupkg`) so it can never be mistaken for the release. `dotnet msbuild -getProperty:PackageVersion` reports `1.0.0` because the version is computed in a target rather than during evaluation; read the packed file name instead.

## Remaining gates

Both gates are complete. The Blazor dashboard was validated in a browser, and the continuous controller service survived a reboot while reconciling Caddy, TLS, hosts, and the UI against the live Caddy on 2019. Controller-mode observability is file-backed through `controller-status.json` / `controller.log`.

1. Replaced the Go sources and Go-specific tooling. `go.mod`, `go.sum`, `.golangci.yml`, the Go package trees, the Go-only index fixtures and HTML goldens, and the in-container `example/Dockerfile` are gone. The justfile now runs the .NET commands, `.gitignore` and `.gitattributes` no longer carry Go entries, and README.md, `example/standalone/README.md` and AGENTS.md describe the C# tool and its Windows-first workflow.
2. Optional follow-up, still open: decide whether the Application event log remains a sink at all, given that application log messages are not reliably recorded there, or whether `controller.log` should become the only supported destination.
