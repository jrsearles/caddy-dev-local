# Windows C# port

Branch: `port/csharp`, based on Go commit `e2a80d3`. The Go implementation remains the behavior reference until the C# implementation passes the parity suite.

## Current status

The `.NET 10` tool implements `devlocal` (one synchronous reconciliation), `devlocal start` (Docker events and polling), and `devlocal clean`. It lists Docker Desktop containers over the selected named pipe, probes published HTTP ports, reconciles Caddy, renders the Go index template and CSS, and maintains the existing managed hosts block. Go is still the reference implementation while Windows takeover and reboot checks are completed.

On this Windows machine, the native tool package installed successfully and the service started as LocalSystem. The active Docker Desktop pipe and Caddy admin API both reported connected. Stop/start, in-place upgrade, uninstall/reinstall, and logs were exercised successfully. Delayed automatic start is registered with Windows, but the reboot and Docker/Caddy interruption checks below still need to be run.

`src/DevLocal.Core` contains immutable container models, discovery state, domain targets, hosts blocks and Caddy resource generation. `src/DevLocal.Tool` contains Docker SDK adapters, polling and event workers, Caddy admin reconciliation, UI rendering, hook composition, CLI and Windows service management. Shared domain, hosts, Caddy JSON and index fixtures in `tests/Fixtures` are checked in both Go and C#.

The UI embeds the existing Go HTML/CSS assets. `go-text-template` renders the markup after three nested Go expressions are replaced by equivalent C# view-model booleans; the text renderer does not provide Go `html/template`'s contextual escaping, so dynamic HTML, attributes and embedded JSON are encoded by the view-model builder. Escaping and rendered UI artifacts are tested on Windows and WSL.

The existing installed Windows service is still in connectivity-monitor mode. A newly installed service can opt into the continuous controller with `devlocal service install --controller` from Administrator PowerShell. `service upgrade` preserves the current mode; upgrading an existing monitor service does not start editing Caddy or the hosts file. To change modes, uninstall and reinstall the service with the desired option.

One-pass mode was exercised on Windows against a separate Caddy instance (`tests/Fixtures/caddy_staging.json`) on admin port 20199 and HTTP port 19080. It registered container routes and TLS policies, served the generated index page, and `clean` removed the owned Caddy resources and UI artifacts. The opted-in Windows service was then exercised with that staging Caddy and `--hosts-file=false`: starting and stopping a disposable Docker container added and removed its routes, periodic refresh repaired a deleted route, and restarting staging Caddy restored all managed resources. A Docker Desktop port bound to `127.0.0.1` required explicit proxy bypass and an IPv4 probe fallback; the event lifecycle worked after that fix. Staging Caddy and the disposable container were stopped and removed. The normal Caddy process on port 2019 and the live hosts file were not changed; the existing service was returned to monitor mode. Hosts-file takeover and reboot recovery remain to be validated.

Install the .NET 10 SDK on **Windows** first (the SDK in WSL does not provide a Windows `dotnet` command). This also installs the runtime used by the Windows Service.

Build and pack:

```powershell
dotnet test DevLocal.slnx
dotnet pack src/DevLocal.Tool/DevLocal.Tool.csproj -c Release -o artifacts
dotnet tool install --global DevLocal.Tool --add-source artifacts --version 0.1.0-preview.9
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
devlocal service install --controller --caddy-admin http://localhost:20199 --hosts-file=false --index-dir C:\ProgramData\DevLocal\staging-ui
devlocal service status
```

Stop the controller before `devlocal clean --caddy-admin http://localhost:20199 --hosts-file=false --index-dir C:\ProgramData\DevLocal\staging-ui`; otherwise the next refresh re-adds the staging resources. The controller-mode status currently reports service state and mode; discovery/hook errors are written to the Windows Application event log. Reinstall without `--controller` to return to connectivity-monitor mode.

The installer reads `docker context inspect` and persists the selected named pipe. On this development machine the `desktop-linux` context selects `dockerDesktopLinuxEngine`. If needed, override it with `--docker-pipe dockerDesktopLinuxEngine`. If the Docker CLI is unavailable at installation, the fallback is `docker_engine`. The service runs as LocalSystem, which has been verified to reach this machine's Docker Desktop pipe and Caddy admin endpoint. Caddy can run under NSSM using `caddy run` and a fixed configuration and data directory. Docker Desktop typically starts at user login, so monitor mode reports it as disconnected until it becomes available, then connected on a subsequent check (every 10 seconds).

The service is registered as `DevLocal` with delayed automatic start and restart-on-failure. Runtime files are copied to `%ProgramFiles%\DevLocal`; settings, connectivity status, and logs are stored under `%ProgramData%\DevLocal`. The service executes the copied tool through the installed .NET 10 runtime, not via the interactive user's tool shim. The service account does not inherit the user's Docker context or environment.

Manage it with `devlocal service status|logs|start|stop|restart|uninstall`. To try an updated tool, run `dotnet tool update --global DevLocal.Tool --add-source artifacts`, then run `devlocal service upgrade` from Administrator PowerShell. The upgrade stops the service, copies the new runtime files, and starts it again. Uninstalling the service leaves settings and logs for inspection. The tool package ID is provisional; the local package source avoids assuming it is published on NuGet.

To try the new controller **without writing the live hosts file**, use `devlocal --hosts-file=false --caddy-admin http://localhost:20199 --index-dir C:\ProgramData\DevLocal\staging-ui` against a separately started staging Caddy instance. `devlocal clean` with the same Caddy/UI flags removes its owned resources; stop that staging Caddy afterward. `--caddy=false --ui=false --hosts-file=false` performs a read-only Docker discovery pass. Running without those opt-outs applies Caddy, UI and hosts updates on the host.

Validate on Windows:

1. Confirm Docker and Caddy both report connected after Docker Desktop login and NSSM-managed Caddy startup.
2. Close PowerShell, reboot, sign in, and confirm the service starts and reconnects.
3. Restart Docker Desktop and Caddy independently and check status transitions and logs.
4. Exercise `service upgrade` and `service uninstall` from an elevated terminal.

The service's named-pipe and Caddy connections under LocalSystem have been verified on the target machine, as have container events and recovery after staging Caddy restarts. Reboot, live hosts-file takeover, and NSSM-managed Caddy storage/CA behavior still need validation. Do not restart the machine solely to run the prototype while other work is in progress.

## Remaining gates

1. Compare more Go-generated index cases, including escaping, icons, stopped containers and Caddy config state. Validate generated HTML in a browser under staging Caddy.
2. Run live hosts-file takeover tests after reviewing the existing managed block and service configuration. The Docker Desktop loopback probe now bypasses proxies and retries IPv4; keep periodic polling as a backstop for missed events. Probes serialize refreshes, so drift repair can take longer than `--poll-interval` when several published ports are unresponsive.
3. Activate the continuous service only after verifying current settings. Reboot/sign in, check automatic recovery, and verify Caddy started via NSSM has persistent storage and a trusted internal CA.
4. Replace Go sources and Go-specific tooling only after the C# parity and service matrix passes; update README, examples and build recipes.
