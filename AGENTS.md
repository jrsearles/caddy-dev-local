# AGENTS.md

## Project Overview

caddy-dev-local is a Windows host application that auto-registers `.dev.local` domains for Docker containers. The `devlocal` process attaches to same-host Caddy through its admin API and also manages the hosts file and hosts the dashboard.

`src/DevLocal.Core` holds immutable container/discovery models, domain target generation, hosts blocks and Caddy resource generation. `src/DevLocal.Ui` holds the Blazor Interactive Server dashboard (components, view models, embedded CSS/JS, in-process Kestrel host on a loopback port). `src/DevLocal.Tool` holds the Windows service, Docker adapter, Caddy admin client, hosts reconciler, discovery runner, hook runtime, UI hook and controller. `tests/DevLocal.Tool.Tests` checks these components, and shared JSON fixtures under `tests/Fixtures` verify domain, hosts and Caddy resource output.

The foreground CLI runs start/clean modes. Installed services run `MonitorWorker` by default and use `ControllerWorker` only when installed with `service install --controller`. A subcommand is always required: bare `devlocal` prints usage and exits. Go is no longer part of the tree; `docs/csharp-port.md` records the port history and any remaining follow-ups.

## Architecture

```
src/DevLocal.Core/
  ContainerInfo.cs         Immutable container/discovery models
  DiscoveryState.cs        Container state, refresh loop, enriched updates
  DiscoveryUpdate.cs       Complete immutable snapshot passed to hooks
  DomainGenerator.cs       Stateless helpers: Domains/DomainTargets
  CaddyResources.cs        Generated Caddy route and TLS policy JSON
  HostsBlock.cs            Managed hosts block rendering
src/DevLocal.Ui/
  Dashboard.cs             Blazor Interactive Server root component
  Components/              Header, cards, drawer, routes, JSON tree, pages
  Assets/index.css         Embedded stylesheet (DevLocal.IndexCss)
  Assets/ui.js             Clipboard and theme interop
  UiServerHost.cs          In-process Kestrel host on 127.0.0.1
  UiState.cs/UiSession.cs  Live state over the SignalR circuit
src/DevLocal.Tool/
  Program.cs               CLI entry point and service dispatch
  CommandLine.cs           AppOptions, flag/env parsing, duration parsing
  Controller.cs            Continuous discovery plus hook composition
  DiscoveryRunner.cs       Docker event watcher and serialized refreshes
  HttpPortProbe.cs         Published-host-port selection and HTTP probing
  CaddyApiClient.cs        Admin client and actual-state reconciliation
  CaddyHook.cs             Caddy route, TLS policy, and dashboard registration
  HostsFileReconciler.cs   Managed hosts block read/replace
  HostsHook.cs             Hosts-file reconciliation hook
  UiServerHook.cs          Dashboard host lifecycle and reverse-proxy route
  HookRuntime.cs           IUpdateHook, HookSequence, independent workers
  ServiceManager.cs        install/upgrade/uninstall/start/stop/status/logs
  MonitorWorker.cs         Connectivity-monitor service mode
  ControllerWorker.cs      Continuous-controller service mode
```

## Key Concepts

- **Standalone-only**: The app always runs directly on the host (never inside a container) and proxies via `localhost:{published_port}`. Containers must publish a port to be reachable; unpublished containers are skipped.
- **Compose detection**: A container is "Compose" only if both `com.docker.compose.project` and `com.docker.compose.service` labels are present.
- **Domain patterns**:
  - Compose: `{project}.{service}.{tld}` (e.g., `myapp.web.dev.local`)
  - Standalone container: `{container-name}.{tld}` (e.g., `my-nginx.dev.local`)
  - All containers register `.localhost` variants (e.g., `myapp.web.localhost`, `my-nginx.localhost`)
- **Custom domains**: Containers can override auto-registration via `dev.local.domains` label (format: `port:domain;port:domain`). When set, auto-generated domain is skipped.
- **Execution modes**: `devlocal` with no arguments prints usage and exits; `devlocal start` reconciles once then continuously watches Docker events and polls; `devlocal clean` removes managed resources. Flags without a subcommand are rejected.
- **Hook composition**: `Controller` owns continuous discovery and registers Caddy, dashboard, and hosts hooks with `HookRuntime`; Docker events, polling, and stale cleanup trigger refreshes. `CleanupAsync` runs every registered hook's cleanup for `devlocal clean`. Discovery probes only published host ports when Caddy or the dashboard is enabled, caches `SelectedPort` per container ID, restores it for stopped containers, and evicts it when a container disappears. Continuous hooks have independent workers receiving the newest complete immutable snapshot. Caddy and the dashboard run in an ordered `HookSequence` so config retrieval follows reconciliation.
- **External Caddy**: Caddy runs and loads its own configuration independently. devlocal only reconciles owned resources through the admin API and never imports or embeds Caddy.
- **Refresh cycle**: event, poll, and stale refreshes are serialized. Discovery builds candidate Docker state, probes it without holding the state mutex, and atomically commits the complete enriched snapshot. Docker list errors are published without clearing retained state. An internal capacity-one channel keeps only the latest pending update because every update is a complete snapshot.
- **Caddy ownership**: all managed routes and policies have stable `devlocal-` IDs. Every reconcile reads actual Caddy state, adopts existing resources, removes owned orphans, and preserves unrelated configuration.
- **Service modes**: a newly installed service is a connectivity monitor by default; `--controller` opts into continuous reconciliation. `service upgrade` preserves the mode. Controller mode writes a 10-second heartbeat to `controller-status.json` plus `controller.log` under `%ProgramData%\DevLocal`; monitor mode uses `status.json`/`service.log`.

## Development Commands

```powershell
just                        # Default recipe: format check, tests, pack the tool
just check                  # Format check + tests
just test                   # Tests only
just format                 # dotnet format verification
just build                  # Build the solution
just pack                   # Pack DevLocal.Tool into artifacts/
just publish                # Publish a runnable tool into artifacts/publish/
just --list                 # List all recipes
dotnet test DevLocal.slnx -c Release
dotnet format DevLocal.slnx --verify-no-changes
```

## Code Conventions

- No comments unless requested
- `DevLocal.Core` stays free of I/O and hosting dependencies so it can be unit tested without Docker or Caddy
- Lock-protected access to shared container state
- Test file mirrors source: `CaddyHook.cs` -> `CaddyHookTests.cs`
- Unit tests cover discovery, hook isolation, Caddy API reconciliation, generated Caddy JSON, hosts, command-line parsing, the service manager, and the hosted dashboard server
- Config values come from flags, env vars, or defaults (in that priority)
- Caddy loads and owns its static configuration; devlocal never reads a Caddyfile or replaces the running configuration
- When the target HTTP server is absent and creation is enabled, the admin client reads effective Caddy HTTP/HTTPS ports and falls back to 80/443
- `CaddyResources` builds the dynamic JSON and `CaddyApiClient` reconciles it against resources currently present in Caddy
- Admin API semantics: `POST /config/.../routes/-` appends (new routes/policies), `PATCH /id/<id>` replaces in place (route/policy updates; 404 means re-add via POST), `DELETE /id/<id>` removes; `PUT` creates a key that doesn't exist and is used to autovivify the server/routes/policies skeleton, returning 409 if the key already exists; `PATCH /config/apps/tls/automation/policies` replaces the whole policies array (devlocal's policy is prepended ahead of user policies — Caddy picks the first matching policy in `getAutomationPolicyForName` and allows only one catch-all in `TLS.Validate`)
- Admin client uses context-aware requests and a fresh connection per request
- Always update README.md when making user-facing changes

## Dependencies

- [Caddy](https://caddyserver.com/docs/) — External web server configured through its admin API
- `Docker.DotNet` — Docker API client over named pipes
- `Nerdbank.GitVersioning` — Package version from `version.json`; never set `<Version>` in a project file
- ASP.NET Core (`Microsoft.AspNetCore.App`) — Hosts the Blazor dashboard
- [xunit](https://xunit.net/) — Test framework

## Testing Patterns

- Fakes implement the narrow interface under test: `ICaddyConfigClient`, `IContainerSnapshotSource`, `IPortProbe`, `IDockerEventSource`, `IDependencyProbe`
- Fake Docker clients return `ContainerInfo` snapshots built by a helper
- Tests verify both presence and absence of entries in generated domain targets
- All tests model the standalone-only path: containers must publish ports to be proxied, and all targets resolve to `localhost:{published_port}`
- The dashboard is a Blazor Interactive Server app hosted by `UiServerHost` on `127.0.0.1` (`--ui-port`/`DEVLOCAL_UI_PORT`, default `0` = random); Caddy reverse-proxies `dev.local`/`dev.localhost` to it with the existing `devlocal-index` route id and TLS policy. `DevLocal.Ui` embeds `Assets/index.css` as `DevLocal.IndexCss` and ships its own `Assets/ui.js`; the `blazor.web.js`/`blazor.server.js` scripts resolve from `wwwroot` or the static-assets manifest, and `ServiceManager.CopyRuntimeFiles` copies `wwwroot/**` for installed services. There is no `version.json` — live updates flow over the SignalR circuit
- `UiServerHostTests` (page/CSS/framework-script serving, SignalR negotiation, rendered discovery state, error banner) and `UiServerHookTests` (reverse-proxy route/TLS reconciliation) cover the dashboard
- `tests/Fixtures/{domain,hosts,caddy}_cases.json` are shared fixtures asserted directly by C# tests; they are copied to output via explicit `Content Include` items in the test csproj