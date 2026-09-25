# Windows C# port

Branch: `port/csharp`, based on Go commit `e2a80d3`. The Go implementation remains the behavior reference until the C# implementation passes the parity suite.

## First milestone: tool and Windows Service prototype

The current `.NET 10` tool tests service installation ergonomics and dependency connectivity, using `Docker.DotNet.Enhanced` to list Docker containers through its named pipe. It does **not** yet reconcile Caddy, manage hosts, or render the index. Do not run the prototype alongside Go expecting domains to be registered.

On this Windows machine, the native tool package installed successfully and the service started as LocalSystem. The active Docker Desktop pipe and Caddy admin API both reported connected. Stop/start, in-place upgrade, uninstall/reinstall, and logs were exercised successfully. Delayed automatic start is registered with Windows, but the reboot and Docker/Caddy interruption checks below still need to be run.

The first application-logic slice is in `src/DevLocal.Core`: immutable container models and domain/target generation. `tests/Fixtures/domain_cases.json` is checked by both Go (`generator/parity_test.go`) and C# (`DomainParityTests.cs`) to catch behavior drift. `DockerDiscoveryClient` uses the maintained Docker SDK to list and map all containers; the installed Windows service now exercises this call instead of a simple ping. `DockerEventWatcher` contains the Go-compatible event filter and SDK subscription for the future continuous controller. The service still only monitors connectivity; event handling and C# reconciliation are not wired up yet. Live event latency and cancellation still require a Docker Desktop trial when the controller is connected.

Hosts-file block rendering is ported in `HostsBlock` and checked by shared fixtures in Go and C#. `HostsFileReconciler` and `HostsHook` reconcile and clean a specified file path; tests use temporary hosts files on Windows and Linux. The running service does not invoke this hook until Docker discovery and the Caddy reconciliation sequence are in place, so the prototype cannot replace the Go application's live hosts block with an incomplete snapshot.

Install the .NET 10 SDK on **Windows** first (the SDK in WSL does not provide a Windows `dotnet` command). This also installs the runtime used by the Windows Service.

Build and pack:

```powershell
dotnet test DevLocal.slnx
dotnet pack src/DevLocal.Tool/DevLocal.Tool.csproj -c Release -o artifacts
dotnet tool install --global DevLocal.Tool --add-source artifacts --version 0.1.0-preview.6
```

In Administrator PowerShell:

```powershell
devlocal service install --caddy-admin http://localhost:2019
devlocal service status
devlocal service logs
```

The installer reads `docker context inspect` and persists the selected named pipe. On this development machine the `desktop-linux` context selects `dockerDesktopLinuxEngine`. If needed, override it with `--docker-pipe dockerDesktopLinuxEngine`. If the Docker CLI is unavailable at installation, the fallback is `docker_engine`. The prototype runs as LocalSystem; verify this account can actually open your Docker Desktop pipe and reach Caddy before accepting this service identity. Caddy can run under NSSM using `caddy run` and a fixed configuration and data directory. Docker Desktop typically starts at user login, so the service should show Docker as disconnected until it becomes available, then show it as connected on a subsequent check (every 10 seconds).

The service is registered as `DevLocal` with delayed automatic start and restart-on-failure. Runtime files are copied to `%ProgramFiles%\DevLocal`; settings, connectivity status, and logs are stored under `%ProgramData%\DevLocal`. The service executes the copied tool through the installed .NET 10 runtime, not via the interactive user's tool shim. The service account does not inherit the user's Docker context or environment.

Manage it with `devlocal service status|logs|start|stop|restart|uninstall`. To try an updated tool, run `dotnet tool update --global DevLocal.Tool --add-source artifacts`, then run `devlocal service upgrade` from Administrator PowerShell. The upgrade stops the service, copies the new runtime files, and starts it again. Uninstalling the service leaves settings and logs for inspection. The tool package ID is provisional; the local package source avoids assuming it is published on NuGet.

Validate on Windows:

1. Confirm Docker and Caddy both report connected after Docker Desktop login and NSSM-managed Caddy startup.
2. Close PowerShell, reboot, sign in, and confirm the service starts and reconnects.
3. Restart Docker Desktop and Caddy independently and check status transitions and logs.
4. Exercise `service upgrade` and `service uninstall` from an elevated terminal.

The service's named-pipe and Caddy connections under LocalSystem have been verified on the target machine. Reboot, Docker/Caddy interruption, and NSSM-managed Caddy storage/CA behavior still need validation. Do not restart the machine solely to run the prototype while other work is in progress.

## Implementation phases after the service trial

1. Extract reusable fixtures from Go unit and integration tests: config precedence and durations; container state and selected-port cache; domain targets; Caddy routes/policies and adoption; hosts markers; generated UI and escaping.
2. Port configuration, domain generation, container models, and deterministic UI view models; compare C# fixture results to the Go reference.
3. Port Docker listing and event watching through `Docker.DotNet.Enhanced`, then serialized refreshes and polling, probing, immutable snapshots, latest-only hook delivery, and cleanup. Keep the existing `devlocal` one-pass, `start`, and `clean` CLI behavior. Verify event delivery latency and cancellation on Docker Desktop before relying on the event stream alone; keep periodic polling as a backstop.
4. Port actual-state Caddy reconciliation, index/UI generation, and hosts-file updates. Preserve stable `devlocal-` resource IDs so the new executable adopts resources created by Go.
5. Add Windows Docker Desktop + NSSM/Caddy integration tests. Verify one-pass operation, restarts, service recovery, upgrades, cleanup, and domain availability after sign-in.
6. Replace Go sources/tooling on this branch only after parity passes; update README, service instructions, examples, and build recipes.
