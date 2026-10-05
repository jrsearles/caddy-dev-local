# Windows C# port

Branch: `port/csharp`, based on Go commit `e2a80d3`. The Go implementation remains the behavior reference until the C# implementation passes the parity suite.

## Current status

The `.NET 10` tool implements `devlocal start` (Docker events and polling) and `devlocal clean`; a bare `devlocal` prints usage and exits, and a subcommand is always required, which diverges from Go where a bare `devlocal` runs the single pass. The one-shot `run` subcommand has been removed, so the foreground mode always watches. It lists Docker Desktop containers over the selected named pipe, probes published HTTP ports, reconciles Caddy, hosts the index UI in-process, and maintains the existing managed hosts block. Go is still the reference implementation while Windows takeover and reboot checks are completed.

On this Windows machine, the native tool package installed successfully and the service started as LocalSystem. The active Docker Desktop pipe and Caddy admin API both reported connected. Stop/start, in-place upgrade, uninstall/reinstall, and logs were exercised successfully. Delayed automatic start is registered with Windows, but the reboot and Docker/Caddy interruption checks below still need to be run.

`src/DevLocal.Core` contains immutable container models, discovery state, domain targets, hosts blocks and Caddy resource generation. `src/DevLocal.Ui` contains the Blazor dashboard components, view models, embedded CSS/JS assets and the in-process Kestrel host. `src/DevLocal.Tool` contains Docker SDK adapters, polling and event workers, Caddy admin reconciliation, the UI hook, hook composition, CLI and Windows service management. Shared domain, hosts and Caddy JSON fixtures in `tests/Fixtures` are checked in both Go and C#; the index fixtures and goldens are Go-only now that the C# UI is no longer template-rendered.

The UI is a Blazor Interactive Server application hosted in-process by `UiServerHost`. Kestrel binds to `127.0.0.1` on a random OS-assigned port (or `--ui-port` / `DEVLOCAL_UI_PORT` to pin one) and Caddy reverse-proxies `dev.local` / `dev.localhost` to it, replacing the static `file_server` route with the same `devlocal-index` route id and TLS policy. The dashboard keeps the existing look: it reuses `generator/index.css` (embedded in `DevLocal.Ui`), and the theme toggle, container search, compose-project grouping, details drawer, routes table and Caddy config tree are Blazor components with a small `ui.js` interop file for clipboard and theme storage. The `@pgrabovets/json-view` CDN dependency is gone — the config tab renders a native recursive JSON tree, and live updates flow over the Blazor SignalR circuit instead of the old `version.json` polling, so `version.json` and the `refreshToast` are gone too. `start` and controller-service mode host the UI; pass `--ui=false` to skip hosting it. The Go byte-parity fixtures (`tests/Fixtures/index_cases.json`, `tests/Fixtures/golden/index`) no longer drive C# tests; `UiServerHostTests` and `UiServerHookTests` cover page/CSS/framework-script serving, SignalR negotiation, rendered discovery state, discovery-error banners and the Caddy route/policy reconciliation instead.

The existing installed Windows service is still in connectivity-monitor mode. A newly installed service can opt into the continuous controller with `devlocal service install --controller` from Administrator PowerShell. `service upgrade` preserves the current mode; upgrading an existing monitor service does not start editing Caddy or the hosts file. To change modes, uninstall and reinstall the service with the desired option.

Live hosts-file takeover was validated on Windows while the installed service stayed in monitor mode and the live Caddy on 2019 was down, using `--caddy=false --ui=false` so only the hosts file changed. The existing managed block held 12 stale entries from earlier Go-era test containers; one pass removed exactly those lines and nothing else, `clean` restored the file byte for byte outside the block, and a follow-up pass rewrote the block for the only running container. Continuous `start` mode added and removed `devlocal-hosts-probe.{dev.local,localhost}` from Docker start/stop events, and deleting the whole block by hand was repaired on the next poll. Removed names keep resolving until the DNS client cache is flushed, so validation needs `ipconfig /flushdns`. Both implementations write the file as UTF-8 without a BOM, which dropped the BOM this machine's file carried; Docker's own entries and all other lines were preserved. The disposable probe container was removed and the live block was left matching the running containers.

One-pass mode was exercised on Windows against a separate Caddy instance (`tests/Fixtures/caddy_staging.json`) on admin port 20199 and HTTP port 19080. It registered container routes and TLS policies, served the generated index page, and `clean` removed the owned Caddy resources and UI artifacts. The opted-in Windows service was then exercised with that staging Caddy and `--hosts-file=false`: starting and stopping a disposable Docker container added and removed its routes, periodic refresh repaired a deleted route, and restarting staging Caddy restored all managed resources. A Docker Desktop port bound to `127.0.0.1` required explicit proxy bypass and an IPv4 probe fallback; the event lifecycle worked after that fix. Staging Caddy and the disposable container were stopped and removed. The normal Caddy process on port 2019 and the live hosts file were not changed; the existing service was returned to monitor mode. Hosts-file takeover and reboot recovery remain to be validated.

Install the .NET 10 SDK on **Windows** first (the SDK in WSL does not provide a Windows `dotnet` command). This also installs the runtime used by the Windows Service.

Build and pack:

```powershell
dotnet test DevLocal.slnx
dotnet pack src/DevLocal.Tool/DevLocal.Tool.csproj -c Release -o artifacts
dotnet tool install --global DevLocal.Tool --add-source artifacts
```

In Administrator PowerShell:

```powershell
devlocal service install --caddy-admin http://localhost:2019
devlocal service status
devlocal service logs
```

For an isolated continuous-controller trial, first start Caddy with `tests/Fixtures/caddy_staging.json`, then replace the monitor service from Administrator PowerShell:

```powershell
devlocal service uninstall
devlocal service install --controller --caddy-admin http://localhost:20199 --hosts-file=false
devlocal service status
```

Stop the controller before `devlocal clean --caddy-admin http://localhost:20199 --hosts-file=false`; otherwise the next refresh re-adds the staging resources. The controller-mode status currently reports service state and mode; discovery/hook errors are written to the Windows Application event log. Reinstall without `--controller` to return to connectivity-monitor mode.

The installer reads `docker context inspect` and persists the selected named pipe. On this development machine the `desktop-linux` context selects `dockerDesktopLinuxEngine`. If needed, override it with `--docker-pipe dockerDesktopLinuxEngine`. If the Docker CLI is unavailable at installation, the fallback is `docker_engine`. The service runs as LocalSystem, which has been verified to reach this machine's Docker Desktop pipe and Caddy admin endpoint. Caddy can run under NSSM using `caddy run` and a fixed configuration and data directory. Docker Desktop typically starts at user login, so monitor mode reports it as disconnected until it becomes available, then connected on a subsequent check (every 10 seconds).

The service is registered as `DevLocal` with delayed automatic start and restart-on-failure. Runtime files are copied to `%ProgramFiles%\DevLocal`; settings, connectivity status, and logs are stored under `%ProgramData%\DevLocal`. The service executes the copied tool through the installed .NET 10 runtime, not via the interactive user's tool shim. The service account does not inherit the user's Docker context or environment.

Manage it with `devlocal service status|logs|start|stop|restart|uninstall`. To try an updated tool, run `dotnet tool update --global DevLocal.Tool --add-source artifacts`, then run `devlocal service upgrade` from Administrator PowerShell. The upgrade stops the service, copies the new runtime files, and starts it again. Uninstalling the service leaves settings and logs for inspection. The tool package ID is provisional; the local package source avoids assuming it is published on NuGet.

To try the new controller **without writing the live hosts file**, use `devlocal start --hosts-file=false --caddy-admin http://localhost:20199` against a separately started staging Caddy instance. `devlocal clean` with the same Caddy/UI flags removes its owned resources; stop that staging Caddy afterward. `devlocal start --caddy=false --ui=false --hosts-file=false` performs a read-only Docker discovery pass without touching the host. Running without those opt-outs applies Caddy, UI and hosts updates on the host.

Validate on Windows:

1. Confirm Docker and Caddy both report connected after Docker Desktop login and NSSM-managed Caddy startup.
2. Close PowerShell, reboot, sign in, and confirm the service starts and reconnects.
3. Restart Docker Desktop and Caddy independently and check status transitions and logs.
4. Exercise `service upgrade` and `service uninstall` from an elevated terminal.

The service's named-pipe and Caddy connections under LocalSystem have been verified on the target machine, as have container events, recovery after staging Caddy restarts, and live hosts-file takeover. Caddy now runs as the `DevLocalCaddy` NSSM service from `C:\ProgramData\DevLocal\Caddyfile` with `file_system` storage; the internal CA is trusted in the LocalMachine root store and serves `.dev.local` over HTTPS on 443. Reboot recovery is the remaining unvalidated step. Do not restart the machine solely to run the prototype while other work is in progress.

## Versioning

The package version comes from `version.json` at the repository root via [Nerdbank.GitVersioning](https://dotnet.github.io/Nerdbank.GitVersioning/), which is referenced from `Directory.Build.props` so every project picks it up. Nothing is stored in the project files: `{height}` counts the commits since `version` last changed, and `versionHeightOffset` shifts that count. The sequence continued the previously hand-maintained numbers, so the commit that introduced `version.json` packs as `0.1.0-preview.12` and each later commit adds one.

Three things to keep in mind:

- Build from a commit, not a dirty tree. An uncommitted `version.json` is read as a working copy, which yields height `0` and silently drops `versionHeightOffset`.
- Never set `<Version>` in a project file. It wins over the generated version and pins every build to that literal string.
- To start the next minor version, change `version` to `0.2.0-preview.{height}` and leave `versionHeightOffset` and `versionHeightOffsetAppliesTo` alone. The offset then stops matching, so it is ignored and the sequence restarts at `preview.1`.

`publicReleaseRefSpec` lists `main` and `port/csharp`, the two branches the tool is released from; a build from any other branch appends `-g<sha>` to the prerelease label to mark it as non-public. `nugetPackageVersion.semVer` is `2` so the prerelease number is not zero-padded, which keeps the `.nupkg` name identical to what `devlocal version` prints. `dotnet msbuild -getProperty:PackageVersion` reports `1.0.0` because the version is computed in a target rather than during evaluation; read the packed file name instead.

## Remaining gates

1. Validate the hosted dashboard in a browser under staging Caddy (theme toggle, search filter, project grouping, drawer, routes and config tabs, live SignalR updates while containers start/stop); the Go byte-parity harness no longer applies to the Blazor UI.
2. Activate the continuous service only after verifying current settings. Reboot/sign in, check automatic recovery, and verify Caddy started via NSSM has persistent storage and a trusted internal CA.
3. Replace Go sources and Go-specific tooling only after the C# parity and service matrix passes; update README, examples and build recipes.
