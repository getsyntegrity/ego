# Design — Deterministic in-memory runtime and `compose/inmem` (EGO-RUNTIME-005, #105 IMPL-6)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` |
| Date | 2026-09-27 |
| Phase | `sdd-design` |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148), parent [`#11`](https://github.com/getsyntegrity/ego/issues/11) |
| Inputs | [`proposal.md`](./proposal.md); ego-runtime-001 [`design.md`](../ego-runtime-001/design.md) §2, D1–D10; ego-arch-003 [`design.md`](../ego-arch-003/design.md) §D1–§D8, §5.2, §6; ego-arch-004 [`design.md`](../ego-arch-004/design.md) §D4, §D6, F-E; ego-arch-002-s3 [`design.md`](../ego-arch-002-s3/design.md); ego-arch-006 [`design.md`](../ego-arch-006/design.md) D1–D8, F1; ego-arch-001 [`design.md`](../ego-arch-001/design.md) §3, §10 |
| Baseline | `main` at `57c4b11` (every `file:line` below was read there) |

## 1. Summary and vocabulary

Ego can run a behavior only on GoAkt today. This design adds a second runtime that keeps everything in the process, with no actor system: a map of entities, each with its own serialized mailbox. It implements the same interface consumer code already uses (`port/runtime.Runtime`), persists through the same stores, and emits the same event-stream messages. A second composition root, `compose/inmem`, builds it from the same `compose.Spec` that `compose/goakt` takes, validates it with the same rules, and starts and stops it in the same order. A test then runs one behavior, through one consumer function, on both roots and checks that what can be observed is the same. That test is the criterion #105 has been waiting for.

The runtime lives in an internal package, so the only new public API is `compose/inmem`. It changes nothing in package `ego`.

Terms used below:

- **Runtime**: the component that hosts entities and sagas and delivers commands to them. The GoAkt runtime is `*ego.Engine`; the new one is `inmemruntime.Runtime`.
- **Composition root**: the package that validates a `compose.Spec`, builds a runtime from it, and owns its `Start`/`Stop` (`compose/goakt` today, `compose/inmem` here).
- **Mailbox**: a first-in, first-out queue of work for one entity, processed by at most one goroutine at a time, so an entity never runs two commands at once.
- **Observable**: what a consumer can see through public contracts: `port/runtime` return values and errors, the stores' contents (`persistence`), the event stream (`Subscribe`), and what publishers receive. Timing, goroutines, log lines and telemetry are not observable in this sense.
- **Observable-equivalent**: two runtimes are observable-equivalent for a scenario when the observations listed in §D9 are equal after normalization (timestamps and shard numbers removed).
- **Condition-based wait**: waiting for a fact (a channel receive, a status value) bounded by a context deadline, as opposed to a fixed sleep. The repository removed fixed waits in #112; this design adds none.
- **archcheck**: `internal/cmd/archcheck`, the import-rule checker CI runs (`docs/ci.md`, "Architecture boundary check").
- **Closure test**: a test that runs `go list -deps` on a package and fails when a forbidden package appears anywhere in the dependency tree, direct or not (`internal/runtimeconsumer/closure_test.go` is the model).

## 2. What the GoAkt runtime does today

The in-memory runtime must match this behavior, so it is recorded first. Every item was read on `57c4b11`.

### 2.1 Event-sourced entities

- **Spawn.** `Engine.spawnEventSourced` (`engine.go:758`) checks the declared family (`requireFamily`, `engine.go:831`, error `ErrEntityFamilyNotDeclared`), resolves the spawn tenant (`spawnTenantScope`, `engine.go:875`: `WithTenant`, else the resolver's fixed tenant, else `ErrSpawnTenantUndetermined`), and spawns the actor. Spawning an ID that is alive is idempotent under the same tenant and fails with `ErrSpawnTenantMismatch` under another.
- **Recovery.** On start the actor pings its stores, loads the latest snapshot if a snapshot store exists, then replays events after it through `HandleEvent`, decrypting and running the event adapters first (`EventSourcedActor.recover`, `event_sourced_actor.go:559`).
- **Commands.** The actor prefers `HandleEnvelope` when the behavior implements `behavior.EventSourcedEnvelope` and metadata is present, else `HandleCommand` (`dispatchToBehavior`, `event_sourced_actor.go:772`). It checks the deadline before the handler and again before persisting (`deadline_gate.go:60`). Each event is applied with `HandleEvent` and wrapped in an `egopb.Event` with persistence ID, sequence number, timestamp, shard, encryption fields and, in tenant-aware mode, tenant metadata (`marshalEvent`, `event_sourced_actor.go:1196`).
- **No events.** When the handler returns no events, the actor replies with the **current state** (`event_sourced_actor.go:991-993`), and `resultFromReply` turns that into `OutcomeSuccess` with the current revision (`engine.go:1866-1873`). So `SendCommand` returns the unchanged state, not nil. See §2.6.
- **Revision** is the sequence number of the last persisted event: 1 after the first event.
- **Expected revision.** A command's metadata may carry an expected revision, mapped to `persistence.ExpectGenesis`, `ExpectRevision(n)` or `Unconditional`. A conflict becomes `OutcomeRejected` with `command.CodeConcurrencyConflict`, with the `*persistence.ConflictError` recoverable through `errors.As` (`reply_classification.go:80-90`). After a conflict the actor stops and is restarted into recovery unless it can prove it is still in sync (`shouldStayAliveAfterConflict`, `event_sourced_actor.go:830`).
- **Write-side settings.** `WithSnapshotInterval`, `WithRetentionPolicy`, `WithBatchThreshold` and `WithBatchFlushWindow` are GoAkt adapter settings (ego-runtime-001 D3): only `*ego.Engine` reads them.

### 2.2 Durable-state entities

The command handler must return exactly the prior version plus one (`checkPreconditions`, `durable_state_actor.go:575`). `commitState` (`durable_state_actor.go:689`) writes the `egopb.DurableState`, then updates memory, then publishes. Durable state is never encrypted. When the actor stops, `PostStop` (`durable_state_actor.go:240`) writes the committed state once more, unconditionally, and publishes it, unless it is a tenant-aware entity that never committed.

### 2.3 Event stream and publishers

- Events go to topic `topic.events` (`event_sourced_actor.go:58`) as `*egopb.Event`, states to `topic.states` (`durable_state_actor.go:53`) as `*egopb.DurableState`, each only after its store write succeeded.
- `Engine.Subscribe` (`engine.go:706`) returns a subscriber already subscribed to both topics.
- Each publisher gets its own subscriber and its own goroutine (`AddEventPublishers`, `engine.go:1391`; `sendEvent`, `engine.go:1966`).
- **Ordering is not guaranteed on the stream, even for one entity.** `EventsStream.publishToTopic` delivers each message to each subscriber in a new goroutine (`go sub.signal(message)`, `eventstream/stream.go:155`), so two messages published in order can be enqueued in either order. This affects every `Subscribe` caller and every publisher on both runtimes, since both use package `eventstream`.

### 2.4 Sagas

- A saga subscribes to `topic.events` (`saga_actor.go:199`) and reacts to each event with `HandleEvent`. It persists its own events to the events store (not to the stream) and applies them with `ApplyEvent`.
- It sends commands with the actor system's `SendSync` (`sendCommand`, `saga_actor.go:758`; default timeout 5 s), not through `Engine.Dispatch`, and calls `HandleResult` or `HandleError` with the answer.
- `Compensate` runs every compensation command in one go; the status becomes `SagaCompleted` when all succeed and `SagaFailed` otherwise (`saga_actor.go:806-829`). A non-zero timeout schedules a message that starts compensation (`saga_actor.go:222-237`).
- Since #163, `SagaStatus` reports the lifecycle status (`engine.go:1650`). `SagaCompensating` is never seen by a caller, because compensation runs inside one message (odd/tasks/saga-status-153.md, "Behavior notes"). The status is not persisted.

### 2.5 Other operations

- `Dispatch` (`engine.go:1123`) rejects an empty ID (`ErrUndefinedEntityID`), an internal control payload (`ErrNotACommand`), a nil payload (`command.ErrInvalidEnvelope`) and metadata that does not round-trip; returns a timed-out or canceled `Result` when the context is already done; and bounds the command by the earliest of the context deadline, the metadata deadline and now plus `timeout`. It never spawns: an unknown ID returns GoAkt's "actor not found" error.
- `SendCommand` (`engine.go:1313`) derives metadata from the context, calls `Dispatch`, and maps the `Result` back (`resultToLegacy`, `engine.go:1893`).
- `EntityExists` (`engine.go:988`) is a liveness probe: it never spawns or recovers.
- `EraseEntity` (`engine.go:1659`) scopes erasure to the caller's tenant in tenant-aware mode and, with `full`, deletes events and snapshots up to the latest sequence number. It touches no actor.
- Projections run in `projection_runner.go` and `projection_actor.go`, both in package `ego` and importing GoAkt. The runner's pull loop uses GoAkt at two points only (a `*goakt.PID` field and one `goakt.Tell`, `projection_runner.go:168`, `:413`), but it lives in package `ego`, so another package cannot import it.

### 2.6 Two mismatches between documentation and code

Found while reading, not changed by this design (Q7):

1. `port/runtime.Entities.SendCommand` says "When the command causes no state change, resultingState is nil" (`port/runtime/runtime.go`), but the GoAkt runtime returns the current state (§2.1).
2. `EraseEntity`'s comment says it deletes the encryption key when a key store backs the encryptor (`engine.go:1656-1658`). The function never calls the key store; `full == false` does nothing.

### 2.7 What is GoAkt-free and importable

The contracts a new package needs are all GoAkt-free and outside package `ego`: `command`, `egopb`, `encryption`, `eventadapter`, `eventstream`, `persistence`, `offsetstore`, `projection` (types only), `tenancy`, `port/behavior`, `port/publishing`, `port/runtime`, and `internal/syncmap`, `internal/queue`. The small pure rules the actors apply (`deadline_gate.go`, `reply_classification.go`, `command_context.go`, `tenant_binding.go`, the topic names) import no GoAkt, but they are in package `ego`, so a GoAkt-free package cannot import them.

## 3. Scope boundary

| Concern | Owner | This design |
|---|---|---|
| The in-memory runtime and `compose/inmem` | #148 (here) | Designs them |
| The interface they implement | #147, ego-runtime-001 | Uses it unchanged |
| Placement, supervision, passivation contracts | RUNTIME-003 | Recommends in-memory behavior, leaves the contract to RUNTIME-003 (Q2) |
| Capability negotiation, a runtime `Descriptor` | RUNTIME-006, ego-arch-004 F-E | Declares nothing; call-time `ErrUnsupported` only |
| Public conformance suite | RUNTIME-007 | Builds an internal table RUNTIME-007 can lift (§D10) |
| Drain and shutdown policy | #24 | Mirrors today's GoAkt order, decides no policy |
| Moving the engine, removing aliases | #124 | Nothing |
| Write-side options | #12 | In-memory ignores them, as every non-GoAkt runtime does (ego-runtime-001 D3) |

## 4. Decisions (D1–D10)

### D1 — Package placement and names

| Package | Module | Visibility | Role |
|---|---|---|---|
| `internal/inmemruntime` (package `inmemruntime`) | root | internal | The runtime: `Config`, `New`, `*Runtime` with `Start`, `Stop`, `AddEventPublishers`, `AddStatePublishers` and the `port/runtime` methods |
| `compose/inmem` (package `inmem`) | root | public | The composition root: `New`, `App`, `Option`, step names |
| `internal/runtimeconsumer` (exists) | root | internal | Gains the neutrality scenarios and their test (§D9, §D10) |

```go
// internal/inmemruntime
type Config struct {
	Families       Family // its own bit set; compose.Family is mapped by compose/inmem
	EventsStore    persistence.EventsStore
	StateStore     persistence.StateStore
	SnapshotStore  persistence.SnapshotStore
	EventAdapters  []eventadapter.EventAdapter
	Encryptor      encryption.Encryptor
	TenantResolver tenancy.TenantResolver
	Stream         eventstream.Stream // allocated by the composition root
	Clock          func() time.Time   // event and state timestamps; nil means time.Now
	Logger         kitlog.Logger      // github.com/pablogore/kit-logger; nil means the global logger
}

func New(cfg Config) (*Runtime, error)

var _ runtimeport.Runtime = (*Runtime)(nil) // the #148 compile-time assertion
```

**Why internal.** Consumers only need the runtime through `compose/inmem.App.Runtime()`, which returns `runtimeport.Runtime`. A public `inmemruntime.New` would be a second, unmanaged construction path, the same position `ego.NewEngine` is in next to `compose/goakt` (ego-arch-003 §D1), and it would be public v4 API that cannot be removed before #124. An internal package can be promoted later with an additive change. Rejected alternative: a public `runtime/inmem`. It would also add a top-level `runtime/` directory next to `port/runtime`, two packages named after the same idea. This choice is open question Q1.

**Why `inmemruntime` and not `inmem`.** `compose/inmem` imports it, and two packages named `inmem` in one file would need an alias every time.

**Why the root module.** The runtime needs `command`, `persistence`, `tenancy` and the other root-level contracts. A nested module would have to require the root module to get them, so its module graph would still contain GoAkt, and ego-arch-006 D6 accepts a module only for module-graph pruning, a toolchain difference or release cadence, none of which applies. A module would also trigger ego-arch-006 F1 (moving the root-level contracts out early). Rejected alternative: a nested `inmem` module.

**Why the runtime does not take a `compose.Spec`.** `internal/inmemruntime` sits outside `compose/`, so `composition-leaf` forbids it to import `compose`. `compose/inmem` translates the `Spec` into a `Config`, exactly as `compose/goakt` translates it into `ego.Option`s (`compose/goakt/option.go`, `egoOptions`).

### D2 — archcheck classification

**Which existing rules apply, unchanged.**

- `composition-leaf` applies to `internal/inmemruntime` (a root-module production package outside `compose/`): it must not import `compose/...`. It does not, by D1.
- `compose/inmem` is under `compose/`, so `composition-leaf` exempts it: it may import `compose`, `compose/internal/lifecycle` and `compose/internal/adapters`, as `compose/goakt` does.
- `composition-no-runtime` does **not** cover `compose/inmem`: its layer is `compose` plus `compose/internal/...` only (`internal/cmd/archcheck/rules/layers.go`, `CompositionLayer`), which deliberately leaves out runtime-specific roots such as `compose/goakt`.
- `no-cross-module-internal`: both packages are in the root module and import only root-module internals.

**One rule is added, `inmem-no-runtime`.** Without it nothing in archcheck stops either package from importing package `ego` or GoAkt directly.

```go
// internal/cmd/archcheck/rules/layers.go
// InMemoryRuntimeLayer: internal/inmemruntime (and subpackages) and compose/inmem (and subpackages).
func InMemoryRuntimeLayer(rootModulePath string) Layer

// internal/cmd/archcheck/rules/rules.go, appended to DefaultRules
{
	ID:          "inmem-no-runtime",
	Description: "the in-memory runtime (internal/inmemruntime) and its composition root (compose/inmem) must not import the root package ego, internal/extensions, the GoAkt runtime or compose/goakt",
	Source:      "ego-runtime-002/design.md §D2",
	Layer:       InMemoryRuntimeLayer(rootModulePath),
	Semantics:   Denylist,
	Forbids:     func(p string) bool { return forbidsRuntime(rootModulePath, p) || hasPathOrSubpath(p, rootModulePath+"/compose/goakt") },
	Reason:      /* runtimeReason, plus a compose/goakt case */,
}
```

It reuses the denylist `application-no-runtime` and `composition-no-runtime` share (`forbidsRuntime`, `rules.go`), plus `compose/goakt`, which would bring GoAkt in through the back door. Its layer matches `internal/inmemruntime` from spec 1, so the rule never matches zero packages (archcheck fails a rule that matches nothing, `docs/ci.md`).

**Why a new rule and not a wider `CompositionLayer`.** Adding `compose/inmem` to `CompositionLayer` would cover the root but not `internal/inmemruntime`, and a violation would be reported as "runtime-neutral composition packages", a layer that by its own definition excludes runtime-specific roots. Rejected alternative: widen `CompositionLayer`.

**Transitive check.** archcheck checks direct edges only. Both packages also get a closure test, modeled on `internal/runtimeconsumer/closure_test.go`, over `go list -deps` **and** `go list -deps -test`: neither GoAkt nor the root package may appear. `compose/inmem`'s test closure must stay GoAkt-free too, which is why the neutrality test does not live there (§D9).

**What changes in the rule table.** One layer and one rule are added, with tests in `internal/cmd/archcheck/rules/evaluate_test.go` (one graph that breaks it for each of the four forbidden targets, one that satisfies it) and one row in `docs/ci.md`'s rule table. No rule is relaxed, no baseline entry is added, and the baseline stays as it is.

### D3 — Lifecycle, unsupported operations, errors

- `New` validates nothing a `compose.Spec` already validated and starts nothing. `Start` marks the runtime started. `Stop` is described in §D8.
- Before `Start` and after `Stop`, every method returns `runtimeport.ErrEngineNotStarted` — except the unsupported ones, which return `*runtimeport.UnsupportedError` in every state, as `port/runtime/runtime.go` requires ("ErrUnsupported comes first").
- Unsupported in this change: the five `Projections` methods. Each returns `&runtimeport.UnsupportedError{Runtime: "inmem", Operation: "StartProjection"}` (and `StopProjection`, `IsProjectionRunning`, `RebuildProjection`, `ProjectionLag`), before any side effect. This is open question Q3.
- The runtime's name in errors is the constant `"inmem"`.
- Every sentinel it returns is the `port/runtime` value (`ErrUndefinedEntityID`, `ErrNotACommand`, `ErrEventsStoreRequired`, `ErrDurableStateStoreRequired`, `ErrEntityFamilyNotDeclared`, the three tenant spawn errors), so `errors.Is` holds under either name.
- **Unknown entity.** `SendCommand`/`Dispatch` to an ID that is not alive never spawns, like GoAkt, and returns an error from the runtime. There is no neutral sentinel for it in `port/runtime` today; GoAkt returns its own `ErrActorNotFound`. This design does not add one (Q5).

**Capabilities are declaration-only.** The runtime implements every method, including the unsupported ones, so its method set says nothing about what it supports. It declares no `adapter.Descriptor` in this change: a runtime port and runtime capability constants are RUNTIME-006's (ego-arch-004 F-E), and when they arrive V8b must treat them as declaration-only (`compose/spec.go`, `capabilityCheck.declarationOnly`). No runtime is a `Spec` slot today, so V8 is not involved.

### D4 — Entities

**Structure.** The runtime holds a map from entity ID to an entity record: its behavior, family, tenant binding, current state, revision and mailbox. The map is guarded by one mutex; each record's mailbox is a FIFO queue drained by at most one goroutine at a time (a goroutine is started when work arrives and exits when the queue is empty, so an idle runtime has no goroutines of its own).

**Spawn.** `SpawnEventSourced` and `SpawnDurableState`, in this order, return:

1. `ErrEngineNotStarted` when not started;
2. `ErrEventsStoreRequired` / `ErrDurableStateStoreRequired` when the store is missing;
3. an error wrapping `ErrEntityFamilyNotDeclared` when `Config.Families` does not contain the family (same message shape as `engine.go:835`);
4. the tenant spawn errors when a resolver is configured (§D6);
5. `nil` without change when the ID is alive under the same tenant (idempotent), after resolving `ResolveSpawnOptions(opts...)`.

Otherwise the runtime recovers the entity synchronously before `Spawn` returns: event-sourced entities from the latest snapshot (when a snapshot store is set) plus the events after it, decrypted and adapted, through `HandleEvent`; durable-state entities from `GetLatestState`. A recovery error fails the spawn and registers nothing. Pinging the stores at spawn, as the GoAkt actor does, is kept.

**Commands on an event-sourced entity** run inside the entity's mailbox, in this order: deadline check; `HandleEnvelope` or `HandleCommand` (same preference as §2.1); if no events, the result is `OutcomeSuccess` with the current state and revision (§2.1, not nil; §2.6); `HandleEvent` for each event; deadline check; one `WriteEvents` call with the precondition from the expected revision; on success, update memory, publish each `egopb.Event` to `topic.events` in sequence order, enqueue it to every live saga (§D5), and reply. On a conflict, the result is `OutcomeRejected` with `CodeConcurrencyConflict` and the `*persistence.ConflictError` as cause; the entity then re-hydrates from the store in place, which leaves it in the state the GoAkt restart would have. A handler panic is recovered and becomes `OutcomeFailed`; the entity keeps its last persisted state, which is what a GoAkt restart would recover.

**Commands on a durable-state entity** follow §2.2: the new version must be the prior version plus one, else `OutcomeFailed`; one `WriteState`; then memory; then publish the `egopb.DurableState` to `topic.states`.

**Envelope fields.** Persistence ID, sequence or version number, the payload packed in `anypb.Any` (encrypted when an encryptor is set, events only), `Timestamp` from `Config.Clock`, encryption key ID and flag, tenant metadata in tenant-aware mode. `Shard` is computed by a documented deterministic function of the persistence ID. It cannot equal GoAkt's, which comes from the actor system's partitioner, so the neutrality proof excludes it (§D9). Shard numbers matter to projections, which the in-memory runtime does not run.

**Results.** The runtime builds `command.Result` values directly, with the same outcome and failure code the GoAkt runtime produces for the same cause: canceled, timed out, concurrency conflict, failed. It does not reproduce GoAkt's wire round-trip or its message strings; failure messages are not part of the observable contract (§D9).

**`EntityExists`** reports whether the ID is in the map. Since the in-memory runtime never passivates (§D7), an entity stays alive until `Stop` or a `StopDirective`.

**Write-side settings.** Adapter settings of package `ego` (snapshot interval, retention, batching) are invisible to other runtimes by construction (unexported keys, ego-runtime-001 D3), so the in-memory runtime never writes snapshots, applies no retention, and writes one batch per command. Neutral consumer code cannot request those settings without importing package `ego`, so this does not break neutrality.

### D5 — Sagas

- `SpawnSaga` checks lifecycle, the events store, the saga family and tenancy like an entity spawn, recovers the saga's own events through `ApplyEvent`, and registers the saga as an event recipient. `WithTenant` is the only spawn option it reads, as in GoAkt (`engine.go`, `spawnSaga` doc).
- **Event delivery.** When an entity persists events, the runtime enqueues each one to every live saga's mailbox, in persist order, before the entity replies to the command. Sagas do not read the event stream. This is stronger than GoAkt, where a saga reads its own stream subscriber and inherits §2.3's lack of ordering. A behavior that is correct under the weaker guarantee is correct under the stronger one, and the stronger one is what makes saga tests deterministic.
- In its mailbox, the saga runs `HandleEvent`, persists the action's events to the events store (not to the stream, as `saga_actor.go` does), applies them with `ApplyEvent`, then sends each command through the runtime's internal dispatch with the saga's tenant and metadata derived from its root metadata (the rule of `saga_actor.go:740-749`), default timeout 5 s, and calls `HandleResult` or `HandleError`.
- A saga command to an entity waits for that entity's reply while the saga's own mailbox is busy. That cannot deadlock: an entity never waits on a saga, and delivery to a saga's mailbox never blocks (the queue is unbounded).
- `Complete` sets `SagaCompleted`. `Compensate` runs `behavior.Compensate` and every compensation command in the same mailbox turn, then sets `SagaCompleted` or `SagaFailed` exactly as `saga_actor.go:806-829`. So, as in GoAkt, `SagaCompensating` is never returned by `SagaStatus`.
- **Timeout.** A non-zero timeout starts a timer that enqueues a timeout item to the saga's mailbox; the timeout path compensates as in GoAkt. This is the only timer in the runtime. It is real time, because a deadline is real time; tests that exercise it wait on `SagaStatus` with a context deadline, never with a sleep.
- **`SagaStatus`** is answered from inside the saga's mailbox, so it sees every item queued before it: `SagaInfo{ID, Status, State}` with the status of #153. The status is not persisted, as in GoAkt.

### D6 — Tenancy, erasure, the event stream and publishers

- **Tenancy** follows GoAkt: the spawn tenant is `WithTenant`, else the resolver's fixed tenant (`tenancy.FixedTenantOf`), else `ErrSpawnTenantUndetermined`; the resolver's `Resolve` is called at `Dispatch`, never at spawn; a command whose resolved tenant differs from the entity's binding is refused; persistence uses `persistence.NewTenantScope`. In memory there is no need to query a binding over the wire, so `ErrSpawnTenantUnverified` is never returned. Without a resolver the runtime uses `persistence.Unscoped()`.
- **`EraseEntity`** does what the GoAkt code does (§2.5), not what its comment says (§2.6): tenant scoping, then, when `full`, delete events and snapshots up to the latest sequence number.
- **`Subscribe`** adds a subscriber to `Config.Stream` subscribed to `topic.events` and `topic.states`. The two topic names are repeated as constants in `internal/inmemruntime`; the shared table (§D10) checks they match what GoAkt publishes.
- **Publishers.** `AddEventPublishers`/`AddStatePublishers` are methods of `*inmemruntime.Runtime`, not of `port/runtime` (they are wiring, ego-runtime-001 D1). Like `engine.go:1391`, each publisher gets its own stream subscriber and goroutine and receives only messages published after it was attached; a duplicate ID is rejected. Ordering is what §2.3 describes for GoAkt, because both use package `eventstream`. Making the stream ordered would be a change to `eventstream` for both runtimes, follow-up FU-C.

### D7 — Concurrency model and guarantees

| Guarantee | In-memory runtime | GoAkt today |
|---|---|---|
| One entity runs one command at a time, in arrival order | yes (mailbox) | yes (actor mailbox) |
| A command's events are persisted before its reply | yes | yes |
| After `SendCommand` returns, the command's events are in the store and queued to every live saga | yes | store yes; saga delivery asynchronous |
| A saga sees one entity's events in persist order | yes | no (§2.3) |
| `Subscribe` and publishers see one entity's events in order | no (package `eventstream`, §2.3) | no |
| Order across entities | none beyond the caller's own sequencing | none |
| Entity passivates when idle | never | with `WithPassivateAfter` |
| Background goroutines when idle | none (publishers' goroutines excepted) | the actor system's |

**Determinism.** For one caller that sends commands one after another, the in-memory runtime's final states, revisions, journals and saga statuses are a pure function of the behaviors, the commands and the store contents. Concurrent callers get a serialization per entity, but which caller goes first is the Go scheduler's choice, as in any runtime.

**Rejected alternative: a single-threaded scheduler** that runs every cascade (an event, the saga it wakes, the commands the saga sends) to completion before `SendCommand` returns. It is fully deterministic but blocks callers on work they did not ask for, differs from GoAkt enough to hide real concurrency bugs, and makes a command sent by a saga re-enter a call that is still on the stack.

**Rejected alternative: one long-lived goroutine per entity.** It gives the same ordering but leaves goroutines behind for every entity ever spawned, which `Stop` must then join. The start-on-demand drain loop has the same guarantee with no idle goroutines.

### D8 — `compose/inmem`

```go
package inmem

const (
	StepProbeStores      = "probe stores"      // same text as compose/goakt
	StepStartRuntime     = "start runtime"
	StepAttachPublishers = "attach publishers" // same text as compose/goakt
)

var ErrNotStartable = errors.New("compose/inmem: app cannot be started: an App is single-use")

var ErrProjectionsUnsupported // see M1 below; wrapped in a *compose.ValidationError

type App struct{ /* spec, options, lifecycle sequence, runtime, stream */ }
type Option func(*options)

func New(spec compose.Spec, opts ...Option) (*App, error)
func (a *App) Start(ctx context.Context) error
func (a *App) Stop(ctx context.Context) error
func (a *App) Runtime() runtimeport.Runtime // nil before a successful Start and for good after a failed one

func WithLogger(logger kitlog.Logger) Option
func WithClock(now func() time.Time) Option // event and state timestamps; tests pin it
```

**Validation.** `New` runs `spec.Validate()` (V1–V8, unchanged and shared), then its own rule, and joins every problem as `compose/goakt.New` does:

- **M1** — `Spec.Projections` must be empty, reported as `&compose.ValidationError{Rule: "M1", Field: "Projections", ...}`, because the runtime returns `ErrUnsupported` for projections (D3). Failing at `New` follows #105's "an invalid graph fails at construction, not at first command". Rejected alternative: accept the `Spec` and fail at `Start`. If Q3 is answered by supporting projections, M1 is dropped and a "start projections" step is added.
- `Spec.Name` has no rule in `compose/inmem`: GoAkt's G2 is GoAkt's naming rule, and copying it would make the in-memory root GoAkt-shaped. A `Spec` that passes `compose/goakt` also passes `compose/inmem`, so the neutrality proof can share it. The reverse does not hold for an empty or non-GoAkt name, which is stated in the package documentation.
- `Spec.OffsetStore` is accepted and pinged but unused, since V4 only requires it with projections, which M1 forbids; a `Spec` that sets it without projections is valid on both roots.

**compose/goakt options with no in-memory equivalent.** `WithCluster` (and rule G1), `WithActorSystemOptions` and `WithTelemetry` have none. Options are typed per package, so a consumer cannot pass them to `compose/inmem`; there is nothing to validate. `WithTelemetry` in particular takes an `*ego.Telemetry`, which `compose/inmem` cannot name without importing package `ego`; telemetry for any runtime waits for #31. `WithLogger` exists in both, with the same kit-logger type.

**Start.** Three steps through `compose/internal/lifecycle`, with `Spec.ShutdownTimeout` as the cleanup bound:

| Step | Action | Undo on later failure |
|---|---|---|
| 1 `probe stores` | Ping every configured store through `adapter.PingerOf`, same code shape as `compose/goakt/app.go` `probeStores` | nothing |
| 2 `start runtime` | Allocate the event stream (`eventstream.New`), `inmemruntime.New`, `Start` | `runtime.Stop`, which closes the stream |
| 3 `attach publishers` | `adapters.StartAndProbe` over the owned publishers, then `AddEventPublishers`/`AddStatePublishers` | attached publishers are closed by step 2's undo |
| — | any failure | release every publisher never attached, then return `*compose.StartError` naming the step |

GoAkt's steps 2 and 3 ("start actor system", "start engine") build two objects; the in-memory runtime is one object, so they become one step. GoAkt's step 5 ("start projections") has nothing to do after M1. The order of what remains, the rollback, publisher ownership (transferred at `New`, ego-arch-003 §D5), the single-use state machine and `Stop` idempotence are the same, because the same `lifecycle.Sequence` and the same `adapters.StartAndProbe` run them. Rejected alternative: keep five step names with two no-op steps, which would give `StartError.Step` names that describe nothing.

**Stop.** `lifecycle.Sequence.Stop` undoes the steps in reverse: publishers attached in step 3 have no undo of their own, and step 2's undo is `Runtime.Stop`, which (1) marks the runtime stopped, so new calls get `ErrEngineNotStarted`; (2) closes the publishers and the stream, like `Engine.Stop`; (3) stops every entity, and durable-state entities write their state once more unconditionally, as `PostStop` does in GoAkt. The final state write therefore happens after publishers are closed, which is the order `compose/goakt`'s `TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown` records for GoAkt; the drain policy stays #24's (LIFE-004). Items still queued in a mailbox when `Stop` runs are answered with `ErrEngineNotStarted` rather than run.

**`Runtime()`** returns the runtime as `runtimeport.Runtime` with the same nil rule as `compose/goakt` (ego-runtime-001 D6): an untyped nil before `Start` succeeds and after a failed `Start`, the stopped runtime after `Stop`.

**Tests carry over in shape.** The IMPL-2/IMPL-3 test shapes apply unchanged: `compose/spec_test.go` and `compose/internal/lifecycle` are shared code and are not touched. `compose/inmem/app_test.go` mirrors `compose/goakt/app_test.go` one for one where the step exists: valid `Spec` runs a runtime, missing dependency fails at `New` with nothing started, every problem reported at once, failure injected at each step releases everything, cancelled context starts nothing, `Stop` before `Start` closes publishers, `Stop` twice is a no-op, stop order, undeclared family returns the typed error, publisher failure at *k*, V8 rejects a lying publisher, plus M1. Failure injection uses the same `hooks.afterStep` pattern as `compose/goakt/app.go`.

### D9 — The neutrality proof

**What runs.** `internal/runtimeconsumer` (production code, closure already free of `ego` and GoAkt) gains one exported scenario function per family, beside the existing `Run`:

```go
// Observe drives r through the scenario and returns what a consumer can see.
func Observe(ctx context.Context, r runtimeport.Runtime, stores Stores) (Trace, error)
```

It uses the existing account behavior plus one durable-state behavior and one saga, all on `test/data/testpb` messages. It subscribes before the first command, spawns, sends a fixed command sequence (including one command that emits no event, one rejected by an expected revision, and one that triggers the saga), waits for the saga's final status with `SagaStatus` under a context deadline, and reads the journal and latest states back from the stores.

**What is compared.** `Trace` holds, after normalization:

- each `SendCommand`/`Dispatch` result: outcome, failure code, revision, and the state (compared with `proto.Equal`);
- the journal from `EventsStore.ReplayEvents` per persistence ID: sequence number, payload, `IsDeleted`, tenant metadata;
- the latest durable state from `StateStore.GetLatestState`: version and payload;
- the stream messages received through `Subscribe`: topic, persistence ID and sequence or version number, **as a set**, since §2.3 gives no order;
- the saga's final `SagaInfo.Status` and state;
- `EntityExists` for each ID.

Removed before comparison: timestamps, `Shard`, encryption key IDs, failure message text.

**Where the test lives.** `internal/runtimeconsumer/neutrality_test.go`, external test package `runtimeconsumer_test`. It builds one `compose.Spec` value per run (fresh `testkit` stores for each root, so the second run cannot see the first run's journal), starts `compose/goakt` and `compose/inmem` from it, calls `Observe` on each `App.Runtime()`, and requires equal traces. Why here:

- Test files may import both roots: `composition-leaf` and every other archcheck rule look at production imports only.
- The package's production closure test (`go list -deps .`) is unaffected by test imports, so `Observe` itself stays provably free of `ego` and GoAkt.
- Rejected: `compose/inmem/neutrality_test.go`. It would put GoAkt in `compose/inmem`'s test closure, which its own closure test forbids (D2).
- Rejected: `compose/neutrality_test.go`. It would put both runtimes in the test closure of the runtime-neutral `compose` package, and every GoAkt change would then select the `compose` tests.
- Rejected: the `test/compat` module. It checks compatibility against released versions (ego-arch-006 S1), a different question.

`internal/cmd/ciselect` needs no change: a test's direct test imports are part of its "affected" expansion (`docs/ci.md`, "Reverse-dependency expansion"), so a change in `compose/goakt`, `compose/inmem`, `internal/inmemruntime` or the root package selects this test.

### D10 — Testing strategy and the shared table

- **Deterministic.** No `time.Sleep`, no `pause.For`, no polling loops with fixed intervals. The in-memory runtime's guarantees (D7) let unit tests assert right after `SendCommand` returns. Asynchronous facts (stream messages, saga completion on GoAkt) are awaited by receiving on a channel or re-querying under a context deadline. `WithClock` pins timestamps where a test compares envelopes.
- **No `-race` locally, no workbench.** CI remains the race gate; `internal/inmemruntime` must be race-clean there.
- **Unit tests** in `internal/inmemruntime` (package-internal, to reach the mailbox) cover each rule of D3–D7 with `testkit` stores and the `mocks/` stores for failure injection.
- **The shared table.** `internal/runtimeconsumer` holds `Scenarios`, a slice of `{Name string; Run func(ctx, runtimeport.Runtime, Stores) (Trace, error)}`. `Observe` is its first entry; others cover the rules that were duplicated from package `ego` instead of imported (D4 "Results", deadline gates, the no-event reply, the topic names, the conflict mapping). The neutrality test runs every scenario on both roots. This is how drift in duplicated logic is caught.
- **Not RUNTIME-007.** The table is internal, compares two runtimes with each other rather than against written expectations, and has no public API. RUNTIME-007 can lift it into a public package such as `port/runtime/runtimetest` later; nothing here fixes that package's name or shape.

## 5. Open questions for the maintainer

Each has a recommendation; none is decided here.

**Q1 — Public or internal runtime package.**
(a) `internal/inmemruntime`, reached only through `compose/inmem`. (b) Public `runtime/inmem` with its own `New`.
*Recommendation: (a).* It keeps the new public API to `compose/inmem`, avoids a second construction path, and can be promoted additively later; (b) cannot be withdrawn inside v4.

**Q2 — Spawn settings the in-memory runtime cannot honor.** ego-runtime-001 D4 rule 3 leaves this to RUNTIME-003 and notes that #148's criterion ("capabilities it does not support, for example cluster placement, return an explicit typed error") may need amending.
(a) Ignore `WithPlacement` and `WithRelocation` as a no-op, exactly as single-node GoAkt does (`port/runtime/spawn.go:35`: "only relevant when cluster mode is enabled"); ignore `WithPassivateAfter` (never passivate); honor `WithSupervisorDirective` (restart re-hydrates from the store, stop removes the entity). #148's typed-error criterion is met by the operations of D3 (projections).
(b) Fail the spawn with `&runtimeport.UnsupportedError{Runtime: "inmem", Operation: "SpawnEventSourced: WithRelocation(true)"}` (and similarly for a non-default placement and a non-zero passivation), before anything is spawned.
(c) Honor passivation with a real timer.
*Recommendation: (a).* A default placement (`RoundRobin`) cannot be told apart from an explicit one in `SpawnSettings`, so (b) could only reject non-default values, and the same consumer code would succeed on single-node GoAkt and fail in memory, which breaks the neutrality #105 asks for. (c) brings the only background timer into the command path and makes `EntityExists` time-dependent. Whatever is chosen stays local to one function in the spawn path, so RUNTIME-003 can change it later.

**Q3 — Projections.**
(a) Return `ErrUnsupported` now; `compose/inmem` rejects a `Spec` with projections (M1); follow-up FU-A extracts the projection runner. (b) Include a projection runner in this chain.
*Recommendation: (a).* The runner is in package `ego` (§2.5), so (b) either duplicates about 870 lines of pull loop, backoff and offset logic, or moves them out of package `ego`, which touches the hot root package this chain otherwise leaves alone. The runner also polls on tickers, which conflicts with D7's "no timers in the command path". FU-A can replace the runner's one `goakt.Tell` with a callback, move it to an internal package, and let both runtimes use it; M1 is then dropped.

**Q4 — Duplicate or extract the small pure rules of package `ego`.**
(a) Re-implement them in `internal/inmemruntime` (deadline gate, result building, metadata derivation, event envelope, topic names), with the shared table (D10) as the drift check. (b) First move them from package `ego` into internal GoAkt-free packages that both runtimes import.
*Recommendation: (a) for this chain, (b) as a follow-up (FU-B) aligned with #124.* (b) is the better end state, but it edits `engine.go`, `event_sourced_actor.go`, `durable_state_actor.go`, `saga_actor.go` and `reply_classification.go`, which have several concurrent writers, and it changes the GoAkt adapter while its behavior is the reference this chain measures against.

**Q5 — A neutral "entity not found" error.**
(a) Nothing in this chain: the in-memory runtime returns a descriptive error that wraps no sentinel, and the neutrality scenarios do not cover the case. (b) Add `runtimeport.ErrEntityNotFound` and make both runtimes wrap it; GoAkt would wrap `ErrActorNotFound` so both `errors.Is` checks hold.
*Recommendation: (a) now, (b) as a small follow-up under RUNTIME-002 or #29,* because (b) edits `engine.go` and changes the error GoAkt callers see (wrapping keeps `errors.Is`, but not `==`).

**Q6 — A public quiescence helper.** Should `compose/inmem.App` export something like `WaitIdle(ctx) error` (all mailboxes empty) for consumer tests?
*Recommendation: no, not in this chain.* D7 already makes most facts true when `SendCommand` returns, and a public helper would be in-memory-only API that consumer tests then depend on, so the same test could not run on GoAkt. The runtime keeps an internal version for its own tests.

**Q7 — The two mismatches of §2.6.** Should the in-memory runtime follow the code (current state on a no-event command; no key deletion on erase) or the documentation?
*Recommendation: follow the code, and open two issues* to fix the `port/runtime.SendCommand` comment and to decide whether `EraseEntity` should crypto-shred. Following the documentation would make the two runtimes differ, which is exactly what the neutrality proof must not show.

## 6. Compatibility and apidiff

| Package | Spec 1 | Spec 2 | Spec 3 | Spec 4 | Spec 5 |
|---|---|---|---|---|---|
| `internal/inmemruntime` (new, internal) | new | extended | extended | — | — |
| `compose/inmem` (new, public) | — | — | — | new: additions only | — |
| `internal/runtimeconsumer` (internal) | — | — | — | — | extended |
| `internal/cmd/archcheck/rules` (internal) | one layer, one rule | — | — | — | — |
| `ego`, `port/runtime`, `port/behavior`, `compose`, `compose/goakt`, `compose/internal/...`, `eventstream` | unchanged | unchanged | unchanged | unchanged | unchanged |

apidiff: only spec 4 produces a report, and it must list additions only (a new package). Every other spec must show no report for any public package. SemVer: a minor release. `CHANGELOG.md` gains one entry in spec 4 (the new package) and one line in spec 5 (the neutrality proof, #105's criterion met); specs 1–3 add none, since they change no public API. No `Deprecated:` marker is added anywhere (ego-arch-001 §10 as corrected by ego-runtime-001 Q1).

## 7. Risks

- **Drift between two implementations of the same rules.** Mitigated by the shared table (D10) and by FU-B. Residual risk: a rule no scenario exercises.
- **The reference moves.** A GoAkt change (for example #24's drain policy or a RUNTIME-003 decision) makes the neutrality test fail on the in-memory side. That is the test working; the cost is that such changes now need a matching in-memory change.
- **Ordering expectations.** A consumer who tests on the in-memory runtime may come to rely on D7's stronger saga ordering. The package documentation states which guarantees are in-memory-only.
- **Test-only use in production.** The in-memory runtime has no clustering and keeps entities forever. Its documentation says it is for tests and local development (#11's own wording).
- **Goroutine leaks on `Stop`.** Publisher goroutines and saga timers must be joined or stopped; spec 2 and spec 3 each carry a leak check (`runtime.NumGoroutine` comparison around `Stop`, condition-waited).

## 8. Chain of specs and file ownership

| # | Spec | Tasks | Depends on | Owns |
|---|---|---|---|---|
| 1 | `inmem-runtime-core` | 5 | this design | `internal/inmemruntime/**` (new); `internal/cmd/archcheck/rules/layers.go`, `rules.go`, `evaluate_test.go`; `docs/ci.md` (rule table row) |
| 2 | `inmem-runtime-state` | 5 | spec 1 | `internal/inmemruntime/**` (durable state, publishers, tenancy, erase files) |
| 3 | `inmem-runtime-sagas` | 4 | spec 1; spec 2 for the tenant-aware saga case | `internal/inmemruntime/**` (saga files) |
| 4 | `compose-inmem` | 5 | specs 1, 2 | `compose/inmem/**` (new); `CHANGELOG.md` |
| 5 | `runtime-neutrality` | 4 | specs 3, 4 | `internal/runtimeconsumer/**`; `CHANGELOG.md`; `openspec/changes/ego-arch-003/design.md` §6/§7 (IMPL-6 row), `openspec/changes/ego-arch-001/design.md` §4 (map row) |

Specs 2 and 3 both write `internal/inmemruntime`, in different files; run them one after the other, or in parallel only on separate files agreed in advance. Spec 4 can start after spec 2, in parallel with spec 3.

**Hot spots.** No spec touches `engine.go`, `option.go`, any other root-package file, `.github/workflows/*` or `internal/cmd/ciselect/**`. The only shared tool file is archcheck's rule table, edited by spec 1 alone; rebase it onto any open archcheck change. `CHANGELOG.md` is edited by specs 4 and 5.

**Follow-ups named here (outside the chain):** FU-A projection runner for both runtimes (Q3); FU-B extract shared pure rules from package `ego` (Q4); FU-C ordered delivery in `eventstream` (§2.3, D6); FU-D neutral entity-not-found error (Q5); FU-E the two documentation mismatches (Q7).

## 9. Evidence and reproduction

| Claim | How it was obtained |
|---|---|
| Every `file:line` in §2 | Read on `57c4b11` with `rg -n` and the files themselves |
| Stream delivery is unordered | `eventstream/stream.go:145-157`: `go sub.signal(message)` per subscriber and message |
| No-event command returns the current state | `event_sourced_actor.go:991-993`; `engine.go:1866-1873`; `command.NewSuccessNoState` has no production caller (`rg -n NewSuccessNoState` outside `command/`) |
| `EraseEntity` never deletes a key | `engine.go:1659-1716`: no `KeyStore` or encryptor call |
| Contracts the runtime needs are GoAkt-free | `go list -f '{{.Imports}}'` on `testkit`, `projection`, `eventadapter`, `encryption`; `rg -l tochemey/goakt` finds no hit in `command`, `egopb`, `encryption`, `eventadapter`, `eventstream`, `persistence`, `offsetstore`, `projection`, `tenancy`, `port/*` |
| Projection runner's GoAkt use | `projection_runner.go:168`, `:413` |
| `CompositionLayer` excludes runtime-specific roots | `internal/cmd/archcheck/rules/layers.go`, `CompositionLayer` doc and `Match` |
| ciselect follows test imports | `docs/ci.md`, "Reverse-dependency expansion" |

No spike was run for this design; the chain's specs each start with a RED test (strict TDD).

## 10. Alternatives rejected (summary)

- Running GoAkt with `testkit` stores as "the in-memory composition": still the GoAkt runtime (ego-arch-003 §5.2 rules it out).
- A public runtime package (D1, Q1); a nested module (D1).
- Widening `CompositionLayer` instead of a new rule (D2).
- A single-threaded run-to-completion scheduler; one permanent goroutine per entity (D7).
- Failing at `Start` instead of `New` for projections (D8, M1).
- Five step names with no-op steps (D8).
- Copying GoAkt's naming rule G2 (D8).
- The neutrality test in `compose/inmem`, `compose` or `test/compat` (D9).
- Extracting the shared rules from package `ego` inside this chain (Q4).
