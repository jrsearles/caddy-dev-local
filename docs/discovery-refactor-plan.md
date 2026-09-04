# Discovery Refactor Plan

## Goal

Turn the `docker` + `discovery` + stateful `generator` trio into a **reusable
discovery core** that owns container state and publishes batched
add/remove/update deltas. The three consumers (hosts file, Caddy integration,
UI) become **subscribers** instead of pulling full snapshots through the
`Generator`.

Decisions locked in:

- Notification model: **batched delta per refresh cycle** (preserves the current
  100ms event coalescing / apply semantics).
- Port probing: **pluggable enricher inside the discovery core** (Caddy injects
  it; the hosts binary does not).
- Scope: **full refactor** — discovery owns state, generator becomes stateless
  helpers.
- `docker` package: **kept as an internal dependency** of discovery (consumers
  import only `discovery`).
- `generator` package: **keep the name**; it becomes stateless helper functions.
- Port probing stays **under the state lock** for behavioral parity with today's
  `selectPortsLocked` (no probing-outside-lock change in this pass).

## Target architecture

```
docker.Client (unchanged, internal to discovery)
      │
      ▼
discovery.Discovery  ── owns container state, diff loop, enricher hook, subscribers
      │  Refresh → diff → enrich → Delta{Added,Updated,Removed,Snapshot,Status}
      ▼  (batched, coalesced, in-order dispatch)
subscribers:
   • hosts   → hosts.Sync(generator.Domains(cfg, snapshot))
   • caddy   → reconcile admin API (generator.DomainTargets) + write index
   • UI      → part of caddy subscriber (GenerateIndexPage)
```

Import direction is inverted: today `discovery → generator`; after, `generator →
discovery` (generator becomes stateless helpers over `discovery.ContainerInfo`).
This removes the coupling and makes discovery the reusable core.

## New `discovery` package API

```go
type ContainerInfo struct { ... }   // moved from generator (unchanged fields)
type CustomDomain struct { ... }     // moved from generator
type TargetKind int                  // exported: TargetUnreachable/TargetDNS/TargetGateway

type Status struct { LastError string; LastErrorAt, LastRefresh time.Time }

type Delta struct {
    Added, Updated, Removed []*ContainerInfo
    Snapshot                []*ContainerInfo
    Status                  Status
}
type Subscriber func(Delta)

func New(cfg *config.Config, d docker.Client, logger *zap.Logger) *Discovery
func (d *Discovery) SetEnricher(fn func(*ContainerInfo)) // caddy injects probe; hosts injects none
func (d *Discovery) Subscribe(fn Subscriber)             // register before Run
func (d *Discovery) Refresh(ctx context.Context) error   // list+diff+enrich; dispatches if running
func (d *Discovery) Snapshot() []*ContainerInfo          // sorted copy
func (d *Discovery) Status() Status
func (d *Discovery) Run(ctx context.Context)             // watch + poll + stale loops
```

- The two opaque callbacks (`refresh`/`apply`) and `SetApply` are removed. Each
  loop cycle (event-throttled 100ms / poll / stale) internally refreshes,
  computes a `Delta`, and dispatches to all subscribers **in order,
  synchronously** — preserving today's coalescing/apply semantics.
- `refreshLocked` is reworked to **return** the diff (Added = not previously
  present; Updated = present and changed via a field compare; Removed = deleted
  per running/stale rules) instead of silently mutating.
- The enricher (port probe) runs under lock during refresh for running/changed
  containers — same behavior as today's `selectPortsLocked`, just injected.

## File-by-file changes

**`discovery/discovery.go`** — absorb container state + diff logic:

- Add `ContainerInfo`, `CustomDomain`, `TargetKind` (exported), `selfInfo`.
- Move from `generator.go`: `refreshLocked`, `buildContainerInfo`,
  `discoverSelf`, `isSelfContainer`, `reachability`, `firstNetworkIP`,
  `extractPorts`, `shouldSkip`, `parseCustomDomains`, plus the `containers` map +
  `mu`.
- Replace `Controller` with `Discovery` (state owner + subscribers + enricher).
  Keep `watchEvents`/`streamEvents`/`shouldRefresh`/`staleCleanup`/`pollLoop`,
  but their tail becomes `refresh + dispatchDelta` instead of
  `runRefresh + apply`.
- `New` signature drops `gen`/`refresh`/`apply`, gains just cfg/docker/logger.

**`generator/generator.go`** — becomes stateless helpers:

- Delete `Generator` struct, `NewGenerator`, the map/mutex,
  `Refresh`/`RefreshAndSelect`/`refreshLocked`/`StaleCleanup`, and everything
  moved to discovery.
- Convert to free functions taking `cfg` + `[]*discovery.ContainerInfo`:
  `Domains(cfg, containers)`, `DomainTargets(cfg, containers)`, plus helpers
  `hostFor`, `effectivePort`, `domainForContainer(Localhost)`,
  `hasReachablePort`.
- Add `PortSelector(cfg, probeFn) func(*discovery.ContainerInfo)` — the enricher
  wrapping today's per-container `selectPortsLocked` body. Keep `ProbeHTTPPort`,
  `probe.go`, `tls.go`, `icons.go`.

**`generator/index.go`** — `GenerateIndexPage` already takes
`[]*ContainerInfo`; retype to `discovery.ContainerInfo`. `TLDLocalhost` stays.

**`cmd/devlocal-hosts/main.go`**:

```go
disc := discovery.New(cfg, dockerClient, logger)     // no enricher
apply := func(){ hosts.Sync(cfg.TLD, generator.Domains(cfg, disc.Snapshot())) }
disc.Subscribe(func(discovery.Delta){ apply() })
disc.Refresh(ctx); apply()                            // initial
disc.Run(ctx)
```

**`cmd.go`**:

```go
disc := discovery.New(cfg, dockerClient, logger)
disc.SetEnricher(generator.PortSelector(cfg, generator.ProbeHTTPPort))
disc.Refresh(ctx)                                     // initial state (with probe)
initCaddyConfig(cfg, indexDir, api, configPath, disc.Snapshot(), disc.Status())
syncHosts(...)
disc.Subscribe(func(discovery.Delta){ applyDevlocal(cfg, indexDir, api, hostsOK, disc) })
disc.Run(ctx)
```

- `applyDevlocal`, `reloadCaddyConfig`, `syncHosts`, `initCaddyConfig` swap their
  `*generator.Generator` + `statusFn` params for `*discovery.Discovery` (or a
  `Snapshot()`/`Status()` view). `reloadCaddyConfig` calls
  `generator.DomainTargets(cfg, disc.Snapshot())` and
  `generator.GenerateIndexPage(..., disc.Snapshot(), ...)`. The `tryBeginApply`
  guard stays (dispatch is in-order but reconcile can still overlap the initial
  call).

**`caddy.go` / `admin.go` / `builder.go`** — update signatures/types from
`*generator.Generator` → snapshot/discovery; `discovery.Status` type usages
unchanged in shape.

## Tests

- **`discovery/discovery_test.go`** (currently 64 lines) grows: move the
  refresh/diff/reachability/build/stale tests here from `generator_test.go`, plus
  new tests for delta computation (added/updated/removed), subscriber dispatch,
  and enricher invocation. The mock `docker.Client` and `makeContainer()` helper
  move/duplicate here.
- **`generator/generator_test.go`** (1341 lines) shrinks to naming/targets
  tests, rewritten to call the new stateless funcs with
  `discovery.ContainerInfo` inputs.
- **`generator/render_sanity_test.go`**, **`icons_test.go`** — retype
  `ContainerInfo` → `discovery.ContainerInfo`; logic unchanged.

## Docs

- Update **AGENTS.md** (Architecture map, "Two entry points share one discovery
  driver" → "subscribe to one discovery hub", generator/discovery
  responsibilities) and **README.md** for any user-facing wording. No new
  labels/flags/behavior — the `.dev.local` behavior, ports, and modes are
  preserved.

## Verification

- `just lint` (vet + race tests) after each package compiles.
- Behavior parity checks: domain generation, standalone vs docker targets, stale
  cleanup, and probe-based port selection unchanged; hosts binary still has zero
  Caddy imports (`generator` must not import Caddy — it doesn't today, and
  discovery won't either).

## Risks / notes

- **Biggest churn is test migration** (~1400 lines split across packages) —
  mechanical but large.
- **Enricher under lock**: probing still happens while holding the state lock
  (parity with today). Moving probing outside the lock is a possible follow-up,
  not part of this pass.
- `generator` package keeps its name but is now stateless helpers.
