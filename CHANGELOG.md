# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### 💥 Breaking Changes

- **`migration.New` now returns `(*Migrator, error)`, and the `Migrator` walks one explicit scope.** Once store calls carry a `persistence.Scope`, the legacy `Migrator` listed, replayed, and wrote snapshots only under `persistence.Unscoped()` while its documentation claimed it walked every tenant, so after tenant adoption it reported success without migrating anything in a tenant scope. The new `migration.WithScope(scope)` selects the scope used for listing, replay, and the snapshot write alike; the default stays `persistence.Unscoped()`, so a caller that passes no scope keeps today's behavior. `New` rejects an invalid (zero-value) scope with `persistence.ErrInvalidScope`, so every `Migrator` it returns is ready to run. It does not sweep all tenants automatically — the SPI has no way to enumerate scopes — so run one `Migrator` per tenant scope. Update callers from `m := migration.New(...)` to `m, err := migration.New(...)` and handle the error.

- **Module path changed to `github.com/pablogore/ego/v4`.** `github.com/pablogore/ego` is a fork of [tochemey/ego](https://github.com/Tochemey/ego), maintained independently since 2026-09. The module, every internal import, the generated protobuf `go_package` options, and this repo's own CI, badges and docs now refer to the fork's own path instead of upstream's. Update `go.mod` and every import: `github.com/tochemey/ego/v4` → `github.com/pablogore/ego/v4`. Dependencies genuinely owned by the original author (`tochemey/goakt`, `tochemey/ego-contrib`, `tochemey/olric`) are unaffected. No tag has been cut for the fork yet, so the satellite modules (`benchmark`, `example/cluster`, `publisher/*`) carry a local `replace` directive back to the monorepo root until a first `pablogore/ego` release exists.

- **eGo logs through [kit-logger](https://github.com/pablogore/kit-logger).** The `ego.Logger` seam and its optional capability interfaces (`LeveledLogger`, `EnabledLogger`, `ContextLogger`, `FieldLogger`), the `WithFields` helper and the `DefaultLogger` variable are gone. Every logging surface of the framework now takes a `github.com/pablogore/kit-logger/pkg/logger.Logger`:

  ```go
  import kitlog "github.com/pablogore/kit-logger/pkg/logger"

  logger := kitlog.New(kitlog.Config{Level: kitlog.LevelInfo, Format: kitlog.FormatJSON})
  cfg := ego.NewConfig(store, ego.WithLogger(logger))
  ```

  - `ego.WithLogger`, `migration.WithLogger` and `kafka.Config.Logger` take a kit-logger `Logger`. A custom `ego.Logger` implementation no longer compiles; wrap the backend as a kit-logger `Logger` instead, or hand kit-logger a `slog.Handler` through `Config.Sink`.
  - `ego.DefaultLogger` is now a function. It returns kit-logger's process-wide logger (`logger.L()`), so an application that calls `logger.SetGlobal` before building the engine gets eGo's records through it without passing `WithLogger` at all. When nothing is configured, kit-logger's default writes text to stdout at `info`.
  - `ego.DiscardLogger` is a kit-logger `Logger` whose sink drops every record and whose level gate reports every level as disabled, so it keeps working wherever it was used.
  - `ego.ResolveLogger` keeps its role — a nil or typed-nil logger resolves to `DefaultLogger()` — with the kit-logger type.

  The same logger is adapted into the GoAkt actor system, so the actor runtime, eGo's internals and the application log through one backend. The adapter forwards the engine's `context.Context` verbatim, builds child loggers with kit-logger's own `With`, answers GoAkt's level checks from the logger's live level, so a runtime `SetLevel` is honored on the next record, and skips its own frame through kit-logger's `CallerSkipper`, so a record written with `Config.AddSource` names GoAkt's call site rather than eGo's adapter.

- **eGo's own records are structured.** Every record the engine, the projection runner, the saga actor, the snapshot and retention actors and the migrator write is now a fixed message with snake_case fields (`publisher`, `topic`, `persistence_id`, `sequence_number`, `version`, `projection`, `attempt`, `retry_in`, `saga_id`, `entity_id`, `entities`, `error`) instead of a message rendered with `fmt.Sprintf`. Log-based alerts or dashboards that matched the old message text need updating; nothing about *when* a record is written or at which level has changed.

  Records written with a context now go through the `*Context` methods, so a kit-logger configured with its OpenTelemetry decorator (`pkg/logger/otel`) stamps `trace_id`/`span_id` on eGo's records for free.

- **`persistence.EventsStore`, `persistence.StateStore` and `persistence.SnapshotStore` now key every record by the pair `(persistence.Scope, persistence_id)`, not `persistence_id` alone (EGO-TENANT-003).** Every record-addressing method gained a required `scope persistence.Scope` parameter, immediately after `ctx`:

  ```go
  // EventsStore
  WriteEvents(ctx, events []*egopb.Event, precondition WritePrecondition) error ->
  WriteEvents(ctx, scope persistence.Scope, events []*egopb.Event, precondition WritePrecondition) error

  DeleteEvents(ctx, persistenceID string, toSequenceNumber uint64) error ->
  DeleteEvents(ctx, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error

  ReplayEvents(ctx, persistenceID string, fromSequenceNumber, toSequenceNumber uint64, limit uint64) ([]*egopb.Event, error) ->
  ReplayEvents(ctx, scope persistence.Scope, persistenceID string, fromSequenceNumber, toSequenceNumber uint64, limit uint64) ([]*egopb.Event, error)

  GetLatestEvent(ctx, persistenceID string) (*egopb.Event, error) ->
  GetLatestEvent(ctx, scope persistence.Scope, persistenceID string) (*egopb.Event, error)

  PersistenceIDs(ctx, pageSize uint64, pageToken string) ([]string, string, error) ->
  PersistenceIDs(ctx, scope persistence.Scope, pageSize uint64, pageToken string) ([]string, string, error)

  // StateStore
  WriteState(ctx, state *egopb.DurableState, precondition WritePrecondition) error ->
  WriteState(ctx, scope persistence.Scope, state *egopb.DurableState, precondition WritePrecondition) error

  GetLatestState(ctx, persistenceID string) (*egopb.DurableState, error) ->
  GetLatestState(ctx, scope persistence.Scope, persistenceID string) (*egopb.DurableState, error)

  // SnapshotStore
  WriteSnapshot(ctx, snapshot *egopb.Snapshot) error ->
  WriteSnapshot(ctx, scope persistence.Scope, snapshot *egopb.Snapshot) error

  GetLatestSnapshot(ctx, persistenceID string) (*egopb.Snapshot, error) ->
  GetLatestSnapshot(ctx, scope persistence.Scope, persistenceID string) (*egopb.Snapshot, error)

  DeleteSnapshots(ctx, persistenceID string, toSequenceNumber uint64) error ->
  DeleteSnapshots(ctx, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error
  ```

  `Connect`, `Disconnect` and `Ping` on all three interfaces are deliberately unchanged: connection lifecycle is not record-addressing, so it carries no tenant boundary. `EventsStore.GetShardEvents` and `EventsStore.ShardOffsets` are also unchanged, for a different reason: both are shard-level projection reads, not `(scope, persistence_id)`-addressed record reads, and read-side/projection isolation across tenants is EGO-TENANT-004's scope — explicitly not decided by this change.

  `persistence.ConflictError` now carries a `Scope` — `NewConflictError(scope, persistenceID, expected, opts...)` requires it, and `(*ConflictError).Scope()` recovers it — and its canonical wire grammar is now versioned, carries the scope, and quotes both identifiers:

  ```
  ego: concurrency conflict: grammar=v1, scope=<unscoped|tenant:"<id>">, persistence_id="<id>", expected=<unconditional|genesis|N>, actual=<M|unknown>
  ```

  The tenant id and the persistence id are rendered with `strconv.Quote`, so an identifier containing commas, equals signs, quotes, the grammar's own field separators, or non-ASCII text can no longer make the message ambiguous; `persistence.ParseConflictError(err.Error())` is an exact inverse for every valid scope and persistence id, and rejects any non-canonical rendering. Only `grammar=v1` is parsed back: a message in the previous unversioned `persistence_id=<id>` grammar (for example from a node not yet upgraded) still classifies as `concurrency_conflict` by its unchanged `ego: concurrency conflict` prefix, but no `*ConflictError` cause is reconstructed for it. Anything else that parses `(*ConflictError).Error()`'s text directly, instead of using `errors.As` or `ParseConflictError`, must be updated.

  **Upgrade recipe for an external store adapter:** accept the new `scope persistence.Scope` parameter on every method listed above, and fold it into the record key STRUCTURALLY — for a SQL-backed store that means a real tenant column that participates in the primary key and in every `WHERE` clause, not a string concatenated onto the existing `persistence_id` column. `persistence.Scope.String()` (`"unscoped"`, `"tenant:<id>"`) is a diagnostic rendering only and must never become a storage key: nothing at the string level stops a tenant literally named `"unscoped"` from rendering as `"tenant:unscoped"`, so a store that reduces `Scope` to its string before keying loses the structural guarantee `Scope.Equal` provides. Key on the `Scope` value itself (or its kind and `TenantID()`), never on `String()`.

  **Zero-migration guarantee:** an adapter that maps `persistence.Unscoped()` onto its existing key layout unchanged needs no data migration and no backfill for a deployment that never activates tenancy (no `tenancy.TenantResolver` configured) — every call already carries `Unscoped()` today, before and after the adapter is upgraded. A deployment that *adopts* tenancy on existing data does need a migration: every row written before this change was written under `Unscoped()`, which is a real, distinct `Scope` value, not a wildcard that matches every tenant. Adopting tenancy does not retroactively assign those rows to a tenant — an operator must deliberately decide, per existing `persistence_id`, which tenant (if any) it now belongs to, and either keep serving it under `Unscoped()` or re-key it under a chosen tenant `Scope`.

  **`migration.TenantAdopter` is that migration tool.** It copies an aggregate's events, snapshot, and durable state from a source scope (normally `Unscoped()`) into a per-aggregate target tenant scope:

  ```go
  adopter, err := migration.NewTenantAdopter(
      func(ctx context.Context, persistenceID string) (tenancy.TenantID, bool, error) {
          // Business decision only the operator holds — the framework
          // cannot infer which tenant an existing aggregate belongs to.
          return lookupTenantFor(persistenceID)
      },
      migration.WithEventsStore(eventsStore),
      migration.WithSnapshotStore(snapshotStore),
      migration.WithStateStore(stateStore),
      migration.WithWriteEnabled(), // required opt-in; the default is dry-run
      migration.WithAdoptionFence(fence), // required whenever writes are enabled
  )
  report, err := adopter.Run(ctx)
  ```

  The `TenantAssignment` function is a required constructor argument, never an option, and never defaulted: assigning an existing aggregate to a tenant is a business decision the framework has no way to make on its own. `TenantAdopter` defaults to **dry-run** — it plans and reports but writes nothing until `WithWriteEnabled` is passed — and it **never deletes source data** unless `WithSourceDeletion` is also set, and even then only after that aggregate's copy has been written to the target scope, read back, and matched **byte-for-byte against the exact record this tool intended to write** — the source record with `tenant_metadata` replaced by the target tenant's plus its adoption receipt, compared via `proto.Equal`; for events, matched by `SequenceNumber` rather than by slice position or count. A failed verification never deletes. (A count-only, sequence-number-only, or version-number-only check — an earlier version of this text described exactly that — cannot detect a truncated write or a write that dropped the payload, `tenant_metadata`, or the encryption envelope while still matching on count/sequence/version alone; the full-record match added in the same change that fixed the `PersistenceIDs` pagination issue below closes that gap.) Every write into the target scope uses `persistence.ExpectGenesis()` (events, durable state) or a target pre-read (snapshots, which have no write precondition in the SPI), so an existing target record is never silently overwritten. It is reported as `already_present` only when it is proven to be this adoption, and otherwise fails closed with the aggregate named in the report; tenant ownership or existence alone is never enough. While the source still exists, proof is exact comparison: an events target must contain every source event (`proto.Equal`, by `SequenceNumber`) and may only append later events, and a snapshot or durable-state target must be the identical record at the same position — a later snapshot or state proves nothing, because a single latest record keeps no lineage. Once `WithSourceDeletion` removed the source, proof is the **adoption receipt** every adopted record carries in its `tenant_metadata` under `ego.adoption.receipt` (`v1:` plus a SHA-256 over the source scope and the record's deterministic protobuf encoding; an event's receipt also records, under the digest, the sequence of the adopted event before it — `0` for the first — so the adopted events form a chain), which tenant-bound actors never write. The chain lets a re-run accept a legitimately sparse sequence while still detecting a missing first or middle adopted event, or an altered link; it cannot detect a missing last adopted event, since no later receipt points back at it. A re-run right after a deleting run is therefore a no-op; a re-run after the actor has rewritten a snapshot or durable state fails closed, since that record carries no receipt. An id held by neither the source nor the target still fails as missing. The report's per-aggregate counters are not mutually exclusive: record kinds run in order (events, snapshot, durable state), so an aggregate that fails on a later kind still counts toward `Copied` and `SourceDeleted` for an earlier kind that already wrote its target or deleted its source — those side effects are irreversible and never hidden — while `Verified` counts only aggregates that completed without a failure. **A write-enabled run requires a `migration.AdoptionFence`** (`WithAdoptionFence`, otherwise `NewTenantAdopter` returns `migration.ErrAdoptionFenceRequired`). The application supplies it, because only the application knows how its writers are coordinated: while `Acquire(ctx, scope, persistenceID)` is held, no other writer may create, modify, or delete any record of that aggregate in that scope. For every aggregate the run acquires it for the source and the target scope — in a fixed order (Unscoped first, then tenant scopes by id), before its first read of the aggregate — and holds both through the target check, the write, the read-back, and any source deletion, releasing them on every exit path including an error or a panic. This is what makes adoption safe against concurrent writers: the snapshot SPI has no write precondition, so without it another writer could create a target snapshot between the adopter's existence check and its write and have it overwritten; and the SPI offers no atomic read-verify-delete for `WithSourceDeletion`. (Events and durable state are already protected on the target by `ExpectGenesis`.) Under the fence, deletion re-reads the source and deletes only if it is still exactly the verified records — a newer record, or one rewritten at the same sequence number, blocks it — and afterwards requires the source to hold nothing for that aggregate: a newer record, or one not removed or recreated at the same or a lower sequence, reports the aggregate as failed, never `source_deleted`. `WithSourceDeletion` also deletes the source of a target an earlier run already adopted, once that target is proven exact. A target equal to the source scope is rejected. `WithScanPageSize(0)` is rejected with `migration.ErrInvalidScanPageSize` instead of silently scanning nothing, and an invalid (zero-value) `WithSourceScope` with `persistence.ErrInvalidScope`, both at construction before any store is touched. Every copied record's `tenant_metadata` is stamped via `tenancy.MarshalMetadata` of a `tenancy.TenantContext` built for the target tenant — the same way `EventSourcedActor`/`DurableStateActor` stamp it — which matters because T4 (above) turned recovered `tenant_metadata` into a cross-check against the actor's spawn-bound tenant: a copy with missing or stale `tenant_metadata` would otherwise recover successfully into the wrong scope and then fail every subsequent tenant-bound recovery.

  **Durable-state enumeration limitation:** `persistence.EventsStore.PersistenceIDs` can enumerate a scope's ids, but neither `persistence.SnapshotStore` nor `persistence.StateStore` has an equivalent method. A durable-state-only or snapshot-only deployment (no events store configured) cannot be discovered automatically — the operator must supply the ids explicitly via `migration.WithPersistenceIDs`. Separately, `persistence.StateStore` has no delete method at all in the SPI, so `WithSourceDeletion` can never remove a durable-state source copy, regardless of the option. Both are gaps in today's persistence SPI, documented rather than papered over with an invented API.

  **`persistence/conformance` is the acceptance test.** An adapter author wires it into their own test package:

  ```go
  func TestPostgresEventsStoreConformance(t *testing.T) {
      conformance.RunEventsStoreConformance(t, func(t *testing.T) persistence.EventsStore {
          return newPostgresEventsStore(t) // a fresh, empty store per subtest
      })
  }
  ```

  `conformance.RunStateStoreConformance` and `conformance.RunSnapshotStoreConformance` cover the other two interfaces the same way (see `testkit/conformance_test.go` for the exact wiring against this repo's own stores). Passing the applicable suites is the evidence of EGO-TENANT-003 compliance.

  **Known limitation:** a GoAkt actor's name is still the caller-supplied `entityID`/`sagaID`, not tenant-qualified, so two tenants that happen to use the same entity id contend for one actor. This is fail-closed and leak-free — the actor binds to whichever tenant's spawn wins the race, the other tenant's spawn fails with `ego.ErrSpawnTenantMismatch` (a same-tenant re-spawn stays an idempotent success; a binding the engine cannot read back — e.g. the node owning a remote actor did not answer — fails closed with `ego.ErrSpawnTenantUnverified`, which asserts no conflict and is safe to retry), and every command from the other tenant is rejected before any store is ever touched — but the losing tenant simply cannot use that entity id until a follow-up gives actors tenant-qualified identity.

  **`Engine.Entity`, `Engine.DurableStateEntity`, and `Engine.Saga` bind a spawned actor to a tenant via a new `ego.WithTenant(id tenancy.TenantID)` spawn option, not by resolving one.** A `tenancy.TenantResolver` MUST be invoked exactly once, at the command trust boundary (`Engine.Dispatch`/`SendCommand`, `Engine.SagaStatus`, `Engine.EraseEntity`) — never at spawn. An earlier draft of this change called `Resolve` at spawn too, which CI caught as a violation of that rule (`TestSendCommandResolverSwapIdenticalSequence` observed the resolver invoked twice for one spawn-plus-command sequence). The application now declares which tenant an entity/durable-state entity/saga belongs to explicitly, with `ego.WithTenant`, at the same call that spawns it:

  ```go
  err := engine.Entity(ctx, behavior, ego.WithTenant(tenancy.TenantID("acme")))
  ```

  `tenancy.WithSingleTenant`'s resolver additionally implements a small new capability interface, `tenancy.FixedTenantResolver` (`FixedTenant() (TenantID, bool)`), so a single-tenant deployment still needs no `WithTenant` at all — the engine reads the resolver's one fixed tenant instead, with no `Resolve` call. When tenancy is active, the registered resolver exposes no fixed tenant, and the caller passed no `WithTenant`, the spawn fails closed with the new `ErrSpawnTenantUndetermined` rather than silently falling back to `persistence.Unscoped()`. `Engine.Saga` gained a trailing `opts ...SpawnOption` parameter (additive, not breaking) solely to carry `WithTenant`; every other spawn option has no effect on a saga. Legacy mode (no resolver registered at all) is unchanged: no `WithTenant` is required and every store call still carries `persistence.Unscoped()`.

### ✨ Features

- **Runtime-neutral behavior contracts in `port/behavior`** ([#123](https://github.com/getsyntegrity/ego/issues/123), slice S3-1 of the ego-arch-002-s3 design). The new package `github.com/pablogore/ego/v4/port/behavior` declares `EventSourced`, `EventSourcedEnvelope`, `DurableState`, `DurableStateEnvelope` and `Saga`, together with `SagaAction`, `SagaCommand` and the `Command`/`Event`/`State` aliases of `proto.Message`. A behavior written against them needs only `ID()` and its domain methods: no `MarshalBinary`/`UnmarshalBinary` and no GoAkt type. The package depends only on the standard library, the protobuf runtime and the `command` contract. The engine does not accept these contracts yet; the spawn entry points that do (`Engine.SpawnEventSourced`, `SpawnDurableState`, `SpawnSaga`) follow in later slices of #123.

  Package `ego` is unchanged for callers. `ego.EventSourcedBehavior`, `ego.DurableStateBehavior` and `ego.SagaBehavior` are now declared as the neutral contract plus GoAkt's `extension.Dependency`, with exactly the method sets they had before, so existing behaviors, and code that uses a behavior as an `extension.Dependency`, keep compiling. `ego.SagaAction` and `ego.SagaCommand` are now aliases of `behavior.SagaAction` and `behavior.SagaCommand`, so both names denote the same type. The only observable difference is reflective: `reflect` and `%T` now name those two structs `behavior.SagaAction` and `behavior.SagaCommand`. `apidiff` reports the two aliased structs and the four `SagaBehavior` methods that mention them as changed; this is the tool's known limitation with aliases to another package, the same one recorded for `port/publishing`, and a consumer program built against the previous API compiles and behaves the same. Nothing is deprecated in this slice.

- **Behaviors that GoAkt cannot serialize get a typed error in cluster mode, and value-type behaviors no longer panic on a single node** ([#123](https://github.com/getsyntegrity/ego/issues/123), slice S3-2 of the ego-arch-002-s3 design). `Engine.Entity`, `DurableStateEntity` and `Saga` now decide, at the spawn site, what carries the behavior to GoAkt. A behavior that is a non-nil pointer with `MarshalBinary`/`UnmarshalBinary` reaches GoAkt exactly as before: the same pointer, registered with `Inject`, with the same type name and wire bytes, so mixed-version clusters keep decoding each other's spawns and relocations. Any other behavior (a value type, or, once the neutral entry points land, one with only its domain methods) runs on the local node through an internal wrapper that GoAkt never registers or serializes. In cluster mode, where GoAkt serializes every spawn, such a behavior is rejected before anything is spawned with the new `*ego.BehaviorPlacementError`, whose `Err` is the new `ego.ErrBehaviorNotSerializable` (no serialization methods) or `ego.ErrBehaviorNotPointer` (serialization methods on a non-pointer); test with `errors.Is` and read `Kind` and `EntityID` with `errors.As`. A nil behavior, or a typed-nil pointer, is rejected in every mode with the same error wrapping `ErrBehaviorNotPointer` (and an empty `EntityID`) instead of panicking when the spawn reads its ID. Before this change a value-type behavior made `Entity` panic inside GoAkt's type registry while it held the actor-system lock, even on a single node, and the actor system could no longer be stopped; it now spawns and answers commands on a single node and gets `ErrBehaviorNotPointer` in cluster mode. `apidiff` reports only the three additions. The public spawn methods that accept the `port/behavior` contracts directly follow in S3-3.

- **`Engine.SpawnEventSourced`, `SpawnDurableState` and `SpawnSaga` accept the `port/behavior` contracts** ([#123](https://github.com/getsyntegrity/ego/issues/123), slice S3-3 of the ego-arch-002-s3 design). A behavior that implements only `ID()` and its domain methods (`behavior.EventSourced`, `behavior.DurableState` or `behavior.Saga`) can now be spawned directly: `engine.SpawnEventSourced(ctx, b, opts...)`, `engine.SpawnDurableState(ctx, b, opts...)` and `engine.SpawnSaga(ctx, b, timeout, opts...)`. They take the same options as `Entity`, `DurableStateEntity` and `Saga` and share their spawn path, so an envelope-capable behavior still receives `HandleEnvelope`. On a single node no serialization methods are needed. In cluster mode a behavior must still be a non-nil pointer with `MarshalBinary`/`UnmarshalBinary`, registered with `WithEntityKinds` on every node that may host it; any other behavior is rejected before anything is spawned with `*ego.BehaviorPlacementError` wrapping `ErrBehaviorNotSerializable` or `ErrBehaviorNotPointer`. A new two-node test covers remote placement of a serializable behavior spawned through `SpawnEventSourced`, and the rejection of domain-only and value-type behaviors in cluster mode. `Entity`, `DurableStateEntity` and `Saga` are unchanged. `apidiff` reports only the three added methods.

- **`ego.BehaviorKind` and `ego.WithBehaviorKinds` register behavior kinds without naming a GoAkt type, and `NewEngine` rejects an unregistrable kind instead of panicking** ([#123](https://github.com/getsyntegrity/ego/issues/123), slice S3-4 of the ego-arch-002-s3 design). `BehaviorKind` is an interface with the same method set as `EntityKind` (`ID() string` plus `encoding.BinaryMarshaler` and `encoding.BinaryUnmarshaler`), spelled with the standard library only, so any `EntityKind` value is a `BehaviorKind` and the reverse. `WithBehaviorKinds(kinds ...BehaviorKind)` and the existing `WithEntityKinds` append to the same registration list: they can be mixed on one node, and a cluster where some nodes use one option and some the other places spawns in both directions, because the registered type names and wire bytes are unchanged. Migrating is a rename at the call site; a `[]EntityKind` slice cannot be spread into `WithBehaviorKinds(...)` (Go does not convert slice element types), so keep `WithEntityKinds(kinds...)` for it or change the slice's type. `NewEngine` now checks every registered kind before registering any: an untyped nil kind or a value type returns `*ego.BehaviorPlacementError` wrapping `ErrBehaviorNotPointer` (with the kind's Go type in `Kind` and an empty `EntityID`), in single-node and cluster mode alike. Before this change such a kind panicked inside GoAkt's type registry while it held the actor-system lock, after which the actor system could not be stopped. A typed-nil pointer such as `(*AccountBehavior)(nil)` keeps working as before: it registers the same type as `new(AccountBehavior)`. The `ErrBehaviorNotPointer` message now states each rule: a behavior must be non-nil to be spawned in any mode and a pointer to be spawned in cluster mode, and a registered kind must be a pointer type, a typed nil allowed (the variable and `errors.Is` checks are unchanged). `EntityKind` and `WithEntityKinds` keep their signatures and are not deprecated yet. `apidiff` reports only the two additions.

- **`compose.Spec`: a runtime-neutral description of a deployment, validated before anything starts** ([#105](https://github.com/getsyntegrity/ego/issues/105), slice IMPL-2 of the ego-arch-003 design). The new package `github.com/pablogore/ego/v4/compose` holds `Spec`, a plain struct of already-constructed dependencies (stores, projections, event adapters, encryptor, tenant resolver, publishers) plus the entity families in use (`compose.EventSourced`, `DurableState`, `Saga`), a name and a shutdown timeout. `Spec.Validate` checks it with no I/O and returns every problem at once, joined with `errors.Join`, each a `*compose.ValidationError` naming its rule and the offending field: at least one family (V1); an events store when event-sourced entities, sagas or projections are used (V2); a state store for durable state (V3); an offset store and a handler for every projection (V4); no typed-nil value — a nil pointer inside a non-nil interface — in any interface field, required or optional, which is the bug class behind the `eventsStore.Ping` panic (V5); and non-nil publishers with unique IDs per kind (V6). `compose.StartError` names the start step that failed, its error and any rollback error. Nothing consumes `Spec` yet: the GoAkt composition root `compose/goakt` that builds and starts an engine from it follows in IMPL-3 and IMPL-4. The existing manual path (`NewConfig`, `NewEngine`) is unchanged.

- **archcheck guards the composition root** ([#105](https://github.com/getsyntegrity/ego/issues/105), ego-arch-003 design §D8). Two new rules: `composition-no-runtime` keeps `compose` and `compose/internal/...` from importing package `ego`, `internal/extensions` or GoAkt (the same denylist as `application-no-runtime`, which still covers only `migration`), and `composition-leaf` allows only packages under `compose/`, `main` packages, examples and tests to import `compose` or anything under it, so no library package can pass a `Spec` around as a hidden service locator. The loader now records each package's name to tell `main` packages apart. No baseline entry was added.

- **Ordered start/stop with rollback for the composition root** ([#105](https://github.com/getsyntegrity/ego/issues/105), slice IMPL-3 of the ego-arch-003 design, §D6/§D7). The new internal package `compose/internal/lifecycle` runs named steps in order; when one fails, it stops the steps that already started in reverse order, then releases resources no step owns yet, attempting every cleanup call, and returns a `*compose.StartError` naming the failed step, its error and every rollback error. `Stop` undoes every started step in reverse order and joins all errors instead of stopping at the first one; on a sequence that never started it only releases; after `Stop` or a failed `Start` it is a no-op. A sequence is single-use (`New → Starting → Running → Stopping → Stopped`, or `Failed`), and `Start`/`Stop` are serialized. Rollback and `Stop` run under `context.WithoutCancel` of the caller's context bounded by the shutdown timeout (30s when zero — the design's suggested default, still open), so a cancelled caller context does not abort cleanup. The package imports no runtime and is internal to `compose/...`; `compose/goakt` (IMPL-4) is its first user, so nothing changes for callers yet.

- **`compose/goakt`: a GoAkt composition root that builds, starts and stops a whole deployment from a `compose.Spec`** ([#105](https://github.com/getsyntegrity/ego/issues/105), slice IMPL-4 of the ego-arch-003 design). `egoakt.New(spec, opts...)` runs `Spec.Validate` plus two GoAkt rules — `WithCluster` needs a cluster configuration and at least one behavior kind (G1, `ErrClusterConfigRequired`/`ErrClusterKindsRequired`), and `Spec.Name` must be a valid GoAkt actor-system name (G2) — and reports every problem at once, without starting anything. `App.Start` then probes every store, starts the actor system, starts the engine, attaches the publishers and starts every projection, in that order; when a step fails, the steps already run are undone in reverse and every publisher not yet attached is closed, and a `*compose.StartError` names the step (`egoakt.StepProbeStores` … `egoakt.StepStartProjections`). `App.Stop` stops the projections, then the engine (publishers and the event stream), then the actor system, attempting every step; it is idempotent, and on an `App` that never started it only closes the publishers, which the `App` owns from the moment `New` succeeds. Stores stay the caller's. Options: `WithLogger`, `WithTelemetry`, `WithCluster(cfg, kinds ...ego.BehaviorKind)` and `WithActorSystemOptions`. `App.Engine()` returns the running `*ego.Engine`. The manual path (`NewConfig`, `NewEngine`) is unchanged. Known and recorded, not decided: state a durable-state actor flushes while the actor system stops is written to the store but no longer reaches state publishers, which closed first (design §D7; the flush/drain policy belongs to #24).

- **`ego.WithEntityFamilies` and `ego.WithEventStream`** (additive, #105 IMPL-4). `WithEntityFamilies(ego.EventSourcedFamily|…)` declares the entity families an engine hosts; a spawn of any other family, through `SpawnEventSourced`/`SpawnDurableState`/`SpawnSaga` or their deprecated predecessors, returns an error wrapping `ErrEntityFamilyNotDeclared` before anything is spawned. Without the option every family spawns as before. `WithEventStream` hands the engine an event stream instead of the one `NewConfig` allocates; `Engine.Stop` closes it as before.

- **`port/adapter`: how an adapter says which ports it serves and which optional capabilities it has** ([#106](https://github.com/getsyntegrity/ego/issues/106), ego-arch-004 spec 1, slice SPI-1). The new standard-library-only package `github.com/pablogore/ego/v4/port/adapter` declares `Port`, `Capability`, `Descriptor{Ports, Name, Capabilities}` with `Declares` and `Serves`, the optional interfaces `Describer`, `Starter` and `Pinger`, the lifecycle capabilities `CapStart` and `CapReady`, and the accessors `Describe`, `StarterOf` and `PingerOf`, which are the only places allowed to type-assert those three interfaces. A value that implements none of them, including a nil or typed-nil value, is reported as `(zero, false)`. `port/publishing`, `persistence`, `offsetstore`, `tenancy` and `encryption` gain untyped port-name constants (`publishing.PortEventPublisher`, `PortStatePublisher`, `persistence.PortEventsStore`, `PortStateStore`, `PortSnapshotStore`, `offsetstore.PortOffsetStore`, `tenancy.PortTenantResolver`, `encryption.PortEncryptor`), so none of them imports `port/adapter`. Nothing in core uses the package yet; adapters that do not declare a descriptor keep working unchanged. `apidiff` reports additions only.

- **archcheck forbids adapter modules from importing the composition root** ([#106](https://github.com/getsyntegrity/ego/issues/106), ego-arch-004 design §D7, slice SPI-2). The new rule `external-adapter-no-composition` rejects any package of a nested module under `publisher/` that imports `compose` or anything under it, with no exemption for `main` packages or examples inside the adapter module. The four publishers' closure tests reject the same packages in `go list -deps -test ./...`, since archcheck does not read test files. No baseline entry was added; the repository has no such import.

- **Adapter conformance suites: `port/adapter/adaptertest` and `port/publishing/publishingtest`** ([#106](https://github.com/getsyntegrity/ego/issues/106), ego-arch-004 spec 2, slices SPI-3 and SPI-4). An adapter's own tests call `adaptertest.Run(t, adaptertest.Target{Port, Ownership, New, FailStart, Stall, Capabilities})`, which checks that a declared descriptor is stable, serves the target port and matches the implemented `Starter`/`Pinger` and any port-specific capability in both directions (AT-1; a declared capability with no entry in `Target.Capabilities` fails with "no check supplied"), and the lifecycle rules: a failed acquire reports its error (AT-2), release is idempotent and safe without or after a failed acquire (AT-3), release honors its deadline while the backend is stalled (AT-4), and `Ping` succeeds after acquire (AT-5). `publishingtest.RunEvents`/`RunState` check that `Publish` after `Close` returns `publishing.ErrPublisherNotStarted` (PT-1), that `ID()` is non-empty and stable (PT-2) and that a published message reaches a caller-supplied observer (PT-3). A check is skipped only when a factory returns an error matching `adaptertest.ErrUnreachable`; a check the suite cannot run (no hook, an adapter that dials in its constructor, no `Ping`) is logged as "not exercised", never as passed. Both packages import only the standard library and their contract (`port/adapter`; `port/publishing` and `egopb`). The `publisher/websocket` publishers and the `testkit` `EventStore`, `DurableStore` and `OffsetStore` now implement `Describe` and run the suites in CI with nothing skipped. `apidiff` reports additions only.

- **`compose.Spec.Validate` rejects a negative `ShutdownTimeout` (V7)**, so a composition root fails at `New` instead of when it builds its lifecycle. **`compose/internal/lifecycle` checks the context before each start step**: a context already done fails the step about to run, without calling it, and rolls back the steps already started.

- **`port/runtime`: the runtime-neutral types, spawn options and errors of the runtime SPI** ([#147](https://github.com/getsyntegrity/ego/issues/147), slice S4-2 of the ego-runtime-001 design). The new contract package `github.com/pablogore/ego/v4/port/runtime` (package clause `runtime`; Ego imports it as `runtimeport`) now declares the types a runtime other than GoAkt needs to host Ego entities and sagas. Package `ego` keeps every old name as an alias, so each pair below is the same type, constant or error value, and nothing breaks:

  | Old name (still valid) | New name |
  |---|---|
  | `ego.EntitiesPlacement`, `ego.RoundRobin`, `ego.Random`, `ego.Local`, `ego.LeastLoad` | `runtime.EntitiesPlacement`, `runtime.RoundRobin`, `runtime.Random`, `runtime.Local`, `runtime.LeastLoad` |
  | `ego.SupervisorDirective`, `ego.StopDirective`, `ego.RestartDirective` | `runtime.SupervisorDirective`, `runtime.StopDirective`, `runtime.RestartDirective` |
  | `ego.SagaStatus` (and `String`), `ego.SagaRunning`, `ego.SagaCompleted`, `ego.SagaCompensating`, `ego.SagaFailed` | `runtime.SagaStatus`, `runtime.SagaRunning`, `runtime.SagaCompleted`, `runtime.SagaCompensating`, `runtime.SagaFailed` |
  | `ego.SagaInfo` | `runtime.SagaInfo` (its `State` field is `behavior.State`, which is `proto.Message`) |
  | `ego.SpawnOption` | `runtime.SpawnOption` |
  | `ego.ErrEngineNotStarted`, `ErrUndefinedEntityID`, `ErrDurableStateStoreRequired`, `ErrEventsStoreRequired`, `ErrProjectionNotRegistered`, `ErrSpawnTenantUndetermined`, `ErrSpawnTenantMismatch`, `ErrSpawnTenantUnverified`, `ErrNotACommand`, `ErrEntityFamilyNotDeclared` | the same names in `runtime`, with the same messages; `errors.Is` matches either name |

  `SpawnOption` stays sealed: only the `With*` functions build one. A runtime reads the options a caller passed with `runtime.ResolveSpawnOptions(opts...)`, which returns a read-only `runtime.SpawnSettings` (`PassivateAfter`, `Relocation`, `SupervisorDirective`, `Placement`, `Tenant`, `AdapterSetting`). `runtime.WithPassivateAfter`, `WithRelocation`, `WithSupervisorDirective`, `WithPlacement` and `WithTenant` are the neutral options; the `ego.With*` functions of the same names are unchanged one-line wrappers around them. `runtime.WithAdapterSetting(key, value)` carries a setting only one runtime adapter reads, under a key type only that adapter can name, as `context.WithValue` does; it panics when the option is built if the key is nil or not comparable. The write-side options `ego.WithSnapshotInterval`, `WithRetentionPolicy`, `WithBatchThreshold` and `WithBatchFlushWindow` keep their signatures and travel as GoAkt adapter settings, so other runtimes ignore them; whether they belong in the runtime contract is [#12](https://github.com/getsyntegrity/ego/issues/12)'s decision. `runtime.ErrUnsupported` (which wraps `errors.ErrUnsupported`) and `*runtime.UnsupportedError` are what a runtime returns for an operation it does not provide; `*ego.Engine` supports every operation and never returns them. `runtime.WithRelocation` documents the actual default, relocation disabled unless `WithRelocation(true)` is passed; whether that default changes is [#154](https://github.com/getsyntegrity/ego/issues/154). The runtime interfaces themselves follow in S4-3.

  Two observable differences, neither an API break. **A nil `SpawnOption` is now skipped** instead of panicking inside the spawn (a value that embeds a nil `SpawnOption` still panics, as before). **`%T` and `reflect` print the new package** for the moved types: `fmt.Sprintf("%T", ego.SagaInfo{})` now prints `runtime.SagaInfo`, and a moved error's `%T` is unchanged (`*errors.errorString`). Code that compares type names as strings must update; code that uses the types does not. No name is marked `Deprecated:`: the aliases and the `ego.With*` wrappers stay unmarked until the major release of [#124](https://github.com/getsyntegrity/ego/issues/124) removes them. `apidiff` of package `ego` reports only the 31 known false positives of a cross-package alias (16 "changed from X to X" for the seven `Engine` methods that take `...SpawnOption` or return `*SagaInfo` and the nine `With*` spawn options, 15 "changed from X to …/port/runtime.X" for the five moved types and their ten constants); a consumer program written against the old names builds, vets and prints identical output against the previous `main` and this change. SemVer: minor.

  **The runtime interfaces** (slice S4-3): `port/runtime` now declares one interface per capability, `Entities` (`SpawnEventSourced`, `SpawnDurableState`, `EntityExists`, `SendCommand`, `Dispatch`, `EraseEntity`), `Sagas` (`SpawnSaga`, `SagaStatus`), `Projections` (`StartProjection`, `StopProjection`, `IsProjectionRunning`, `RebuildProjection`, `ProjectionLag`) and `Events` (`Subscribe`), and the composite `Runtime`. Each method has the signature of the `*ego.Engine` method of the same name, and `*ego.Engine` implements `Runtime` unchanged (a compile-time assertion in `engine_runtime.go`); a runtime that lacks an operation returns a `runtime.ErrUnsupported` error before any side effect. `SagaStatus`'s documentation records a known gap: `*ego.Engine` always reports `SagaRunning` ([#153](https://github.com/getsyntegrity/ego/issues/153)). A GoAkt-free test double in `port/runtime`'s tests implements `Runtime`, and an architecture test keeps GoAkt and package `ego` out of that test build. Additions only; package `ego`'s API is unchanged.

  **The composition accessor** (slice S4-4): `compose/goakt.App` gains `Runtime() runtime.Runtime`, which hands out the running application through the neutral contract, so consumer code can spawn entities, send commands and read state without naming `*ego.Engine` (`var entities runtime.Entities = app.Runtime()`). It follows `Engine()`'s documented states: an untyped nil before `Start` (so `app.Runtime() == nil` holds) and for good after a failed `Start`; after `Stop` the stopped engine, which refuses work with `ErrEngineNotStarted`; otherwise the same engine `Engine()` returns. `Engine()` is unchanged and not deprecated; its fate is #124's. The package documentation example now spawns through `app.Runtime()`. A consumer package whose production build contains neither package `ego` nor GoAkt (`internal/runtimeconsumer`, test evidence only, guarded by a `go list -deps` closure test) drives a real `compose/goakt` application end to end through the accessor. `apidiff`: `compose/goakt` gains one method; nothing else changes. With this slice the runtime SPI of #147 is complete: it lives in `port/runtime`, and moving the GoAkt adapter out of package `ego` is #124's (see `openspec/changes/ego-runtime-001/design.md` §D10).

### 🗑️ Deprecated

- **The GoAkt-bound behavior contracts and their spawn/registration entry points are deprecated in favor of `port/behavior`** ([#123](https://github.com/getsyntegrity/ego/issues/123), slice S3-5 of the ego-arch-002-s3 design). Every name below keeps its exact signature and behavior — nothing breaks inside v4 — and carries a `Deprecated:` godoc comment. All of them are removed at the major release introduced by [#124](https://github.com/getsyntegrity/ego/issues/124), together with every other alias `#124` collects (`EntityKind` was already going; the S1 publisher aliases are unaffected by this slice).

  | Deprecated | Replacement |
  |---|---|
  | `ego.EventSourcedBehavior` | [`behaviorport.EventSourced`](https://pkg.go.dev/github.com/pablogore/ego/v4/port/behavior#EventSourced) + `Engine.SpawnEventSourced` |
  | `ego.EventSourcedEnvelopeBehavior` | `behaviorport.EventSourcedEnvelope` + `Engine.SpawnEventSourced` |
  | `ego.DurableStateBehavior` | `behaviorport.DurableState` + `Engine.SpawnDurableState` |
  | `ego.DurableStateEnvelopeBehavior` | `behaviorport.DurableStateEnvelope` + `Engine.SpawnDurableState` |
  | `ego.SagaBehavior` | `behaviorport.Saga` + `Engine.SpawnSaga` |
  | `Engine.Entity` | `Engine.SpawnEventSourced` |
  | `Engine.DurableStateEntity` | `Engine.SpawnDurableState` |
  | `Engine.Saga` | `Engine.SpawnSaga` |
  | `ego.EntityKind` | `ego.BehaviorKind` |
  | `ego.WithEntityKinds` | `ego.WithBehaviorKinds` |

  `ego.SagaAction` and `ego.SagaCommand` (aliases of `behaviorport.SagaAction`/`SagaCommand` since S3-1) are **not** deprecated: they stay the ordinary names for those two types for the rest of v4.

  The non-cluster examples (`example/eventssourced`, `example/durablestate`, `example/saga`) now implement the `port/behavior` contracts directly, spawn with `Engine.SpawnEventSourced`/`SpawnDurableState`/`SpawnSaga`, and no longer carry `MarshalBinary`/`UnmarshalBinary` — they never needed GoAkt serialization, since none of them runs in cluster mode. `example/cluster` keeps its serialization methods (`ego.BehaviorKind`, required for cluster placement), switches its interface assertion and spawn call to the new names, and now registers `AccountBehavior` with `ego.WithBehaviorKinds` so every node can decode a spawn a peer places on it under `RoundRobin` placement — a gap fixed by review before merge. `apidiff` of package `ego` between this slice and `origin/main` reports no changes at all: a `Deprecated:` godoc paragraph is not an API change. A consumer program written against the API builds, vets and runs identically against `origin/main` and this slice's head.

### 🐛 Bug Fixes

- **`WithRelocation`'s documentation now matches its actual default: entities are NOT relocated by default (#154).** `ego.WithRelocation` and `runtime.WithRelocation`'s comments claimed "entities are relocatable by default to ensure system resilience and high availability", but `newSpawnConfig` (`spawn_config.go`) and `runtime.ResolveSpawnOptions` always resolved relocation to `false` unless `WithRelocation(true)` was passed, and `engine.go`'s `buildSpawnOptionsFromConfig` disabled relocation accordingly — so a caller trusting the doc believed an entity redeployed itself after its node died, when it did not. The maintainer chose to keep today's behavior for v4 (option (a) in the issue) rather than change the default: both doc comments now state plainly that relocation is disabled unless `WithRelocation(true)` is passed, and that `WithRelocation(true)` makes an entity eligible for relocation to a healthy node when its host node goes down. No behavior changed; `apidiff` reports no differences. A new test, `TestRelocationDisabledByDefault`, pins the default at both the `ego` and `port/runtime` layers so doc and code cannot silently diverge again. Whether the default itself should change is left open for RUNTIME-003 or a later major version.

- **`publisher/websocket`: `Close` is idempotent** ([#106](https://github.com/getsyntegrity/ego/issues/106), ego-arch-004 spec 2). A second `Close` on `EventsPublisher` or `DurableStatePublisher` returned the error of closing an already-closed connection; it now returns nil, as lifecycle rule L2 requires, because the composition root may close a publisher on more than one path.

- **`Engine.Stop` no longer leaks publishers and the event stream when a publisher fails to close (#126).** `Stop` marked the engine stopped and then returned on the first `Close` error, skipping the remaining event publishers, every state publisher, the event stream and the actor-system detach; a second `Stop` then returned `nil` at once, so that cleanup never ran. `Stop` now attempts every step and returns the `Close` errors joined with `errors.Join`, each wrapped with the publisher's ID.

- **`Engine.Entity` and `Engine.Saga` return `ErrEventsStoreRequired` instead of crashing the process when no events store is configured (#126).** `NewConfig(nil, ...)` is valid for a durable-state-only deployment, but spawning an event-sourced entity or a saga on such an engine reached a nil-interface `Ping` in the actor's `PreStart`; because GoAkt runs `PreStart` under `singleflight`, the panic crashed the whole process. Both spawn paths now return the new `ErrEventsStoreRequired` before anything is spawned, matching `ErrDurableStateStoreRequired` for `DurableStateEntity`.

- **`AddEventPublishers` and `AddStatePublishers` reject duplicate publisher IDs with `ErrDuplicatePublisherID` instead of orphaning a publisher (#126).** Publishers are keyed by `ID()` per kind, so a second publisher with the same ID overwrote the first, whose goroutine and event-stream subscriber were never signalled or closed. Both methods now check the whole call first — IDs repeated within it and IDs already registered for that kind — and return an error wrapping the new `ErrDuplicatePublisherID` that names the duplicates. On that error nothing from the call is registered, subscribed or started. The same ID may still be used once for an events publisher and once for a state publisher.

- **`snapshotsWriterActor.PreStart` no longer panics when the snapshot store or encryptor extension is registered with an unexpected type (#99).** PR #100 fixed every *required* extension lookup (`ctx.Extension(id).(*T)` with no nil check) by returning `ErrMissingRequiredExtensions` instead of panicking, but explicitly left the *optional*, nil-checked lookups in `snapshotsWriterActor.PreStart` unchanged, since a missing extension there was already handled safely. What was not handled was a *present* extension registered under the expected ID but with the wrong concrete type: `ext.(*extensions.SnapshotStoreExt)` and `ext.(*extensions.EncryptorExtension)` were still unchecked single-value assertions, so a mismatched registration still panicked with an unrecoverable "interface conversion" error — and because `PreStart` runs on a goroutine that `goakt` drives through a `golang.org/x/sync/singleflight.Group`, that panic is deliberately re-panicked by `singleflight` on a fresh, unrecoverable goroutine and crashes the whole process, not just the one `Spawn` call. A new `optionalExtension[T]` helper in `extension_lookup.go` keeps the "missing is fine" behavior but returns a descriptive error (wrapping `ErrMissingRequiredExtensions`, consistent with `requireExtension`'s own mismatch branch) instead of panicking when the type doesn't match. A companion audit of every remaining `ctx.Extension(id)` call in the root package (see `odd/tasks/issue-99-prestart-assertions.md`) found the same nil-checked-but-unchecked-assertion pattern in `projection_actor.go`, `event_sourced_actor.go`, `events_janitor_actor.go` and `durable_state_actor.go`; those are tracked as a follow-up slice, not fixed by this change.

- **The same unchecked-assertion panic is now fixed in every remaining `PreStart` path (#99 follow-up).** The four files the audit above left open all had the identical defect: a nil-checked but otherwise unchecked `ext.(*T)` assertion on an optional extension, still able to crash the whole process (via the same `singleflight` re-panic) if the extension ID ever held a mismatched concrete type. `ProjectionActor.PreStart` (`EventAdaptersExtensionID`, `EventsStreamExtensionID`, `EncryptorExtensionID`, `TelemetryExtensionID`), `EventSourcedActor.loadOptionalExtensions` (`SnapshotStoreExtensionID`, `EventAdaptersExtensionID`, `EncryptorExtensionID`, `TelemetryExtensionID`; `loadOptionalExtensions` now returns an error that `PreStart` propagates instead of returning `void`), `eventsJanitorActor.PreStart` (`SnapshotStoreExtensionID`) and `DurableStateActor.PreStart` (`TelemetryExtensionID`) now all use the existing `optionalExtension[T]` helper and return its error instead of letting the runtime panic. No optional-vs-required semantics changed: a missing registration still resolves to the zero value with no error, exactly as before. Re-running the audit (`rg -n '\.Extension\(' --type go`) after this change finds no remaining unchecked single-value assertion on an extension anywhere in the module.

- **`migration.WithLogger(nil)` no longer panics.** The migrator stored whatever the option supplied, so a nil — or a typed-nil such as `(*myLogger)(nil)` — replaced the default and the first log call inside `Run` dereferenced it. `migration.New` now resolves the logger after applying every option, so a nil or typed-nil logger falls back to `ego.DefaultLogger()`, the same semantics the engine already applied to `ego.WithLogger`. The `ego.ResolveLogger` helper exposes that single rule to packages outside the root instead of each one re-implementing typed-nil detection.

- **`example/cluster`'s `PostgresEventStore` now implements the scoped `persistence.EventsStore` contract (#115).** This example is a separate Go module that root CI does not build, so it had silently fallen behind EGO-TENANT-003/EGO-WRITE-004: `WriteEvents` still took no `persistence.WritePrecondition`, and no record-addressing method took a `persistence.Scope`, so `go build ./...` inside `example/cluster` failed against the current interface. `events_store` gains a `tenant_id VARCHAR(255) DEFAULT '' NOT NULL` column (`''` means `Unscoped()`; existing rows read back unchanged, see `example/cluster/README.md`'s migration note), the primary key becomes `(tenant_id, persistence_id, sequence_number)`, and every record-addressing method now filters by it. `WriteEvents` validates scope then precondition before touching the database, and conditional writes compare against a new `events_store_revisions` table that keeps each record's highest committed sequence number, so `DeleteEvents` retention never lowers it (deleting events cannot reopen `ExpectGenesis()` or let a stale `ExpectRevision` win; the schema script backfills existing databases). Every write, conditional or not, locks that revision row before touching `events_store` (multi-id unconditional batches in sorted order, so they cannot deadlock), so the revision check and the insert commit as one atomic step, returning `*persistence.ConflictError` on a failed compare exactly like `testkit`'s in-memory store. `PersistenceIDs` with `pageSize == 0` returns an empty page and an empty token instead of panicking. `DeleteEvents` now takes part in that same lock too: it row-locks the revision row (if one exists) before its `DELETE`, so a delete can no longer commit between a concurrent write's revision check and its insert (or vice versa); it never claims the lock on a persistence id that has never been written, since doing so would make that id look established and a later `ExpectGenesis()` would wrongly conflict. Three deterministic Postgres tests pin the previously timing-dependent races: an unconditional write racing a conditional write over distinct and identical sequence numbers, and a delete racing a conditional write over the same revision row.

- **`example/cluster`'s `PostgresEventStore` now persists `egopb.Event.TenantMetadata` (#115).** `insertEvent` dropped this field on every write path, and `scanEvents` — shared by `ReplayEvents`, `GetLatestEvent`, and `GetShardEvents` — never rebuilt it, so a tenant-aware `EventSourcedActor` recovered its own events with no tenant metadata and rejected them via `tenancy.UnmarshalMetadata` (`event_sourced_actor.go:572`) after a restart. `events_store` gains a nullable `tenant_metadata JSONB` column: an event written with a nil or empty map stores `NULL` rather than an empty JSON object, since proto3 cannot tell the two apart on the wire, and both read back as a nil map — the same shape a pre-existing legacy row (which never had the column at all) reads back as. The migration (`ALTER TABLE events_store ADD COLUMN IF NOT EXISTS tenant_metadata JSONB`, in `eventsStoreSchemaDDL`, `k8s/postgres.yaml`, and `example/cluster/README.md`) does not backfill or invent a tenant identity for any existing row.

- **`persistence.EventsStore.PersistenceIDs` pagination no longer silently skips one id at every page boundary.** `testkit/eventstore.go`'s `EventStore.PersistenceIDs` returned `keys[endIndex]` — the first key NOT yet returned — as `nextPageToken`, while the following call resumed strictly AFTER that same token (`key > pageToken`). Together, the key handed back as the token was never itself returned by any page: at the default adopter page size of 500, the 501st, 1002nd, … persistence id was dropped from every enumeration. This predates this change (the identical code is on upstream `main`), but became a real data-loss risk here because `migration.TenantAdopter.collectPersistenceIDs` drives the tenant-adoption scan off this exact enumeration — a run could report success while leaving some aggregates unadopted. Fixed by making `nextPageToken` the last key actually RETURNED on the page (a cursor over what the caller has consumed) rather than the first key held back for later; the `>` comparison on the following page is unchanged, so the two now agree. `persistence.EventsStore.PersistenceIDs`'s doc comment now states the pagination contract normatively (opaque token, no skip, no duplicate, empty token means done) for every implementation, in this repo or external, and `persistence/conformance`'s `Enumeration` group gained `PersistenceIDsPaginationCoversEveryIDExactlyOnce`, which forces several pages at a small page size and asserts exact, duplicate-free coverage — so this defect class is now pinned for every conforming adapter, not just this repo's in-memory store.

### ⬆️ Dependencies

- Add `github.com/pablogore/kit-logger`.

### 🧹 Improvements

- **`example/eventssourced` now builds and runs through `compose/goakt` instead of assembling `ego.NewConfig`/`goakt.NewActorSystem`/`ego.NewEngine` by hand** ([#105](https://github.com/getsyntegrity/ego/issues/105), slice IMPL-5 of the ego-arch-003 design). This example was the concrete evidence behind the two leaks `design.md` §2.2/§5.1 describe: `NewEngine` failing after the actor system had already started left it running under the example's `os.Exit(1)`, and `_ = engine.Start(ctx)` discarded the one error `Start` can return. `main` is now a thin wrapper around a `run(ctx, logger) error` function that registers `defer app.Stop(ctx)` and `defer eventStore.Disconnect(ctx)` right after each resource is acquired, so `os.Exit` — called only in `main`, once, after `run` returns — can never run ahead of cleanup; every error `run` can receive, including `App.Start`'s, is checked and returned instead of discarded. Verified end to end: the migrated example builds, vets clean, and a real run sent `SIGINT` prints the expected account balances (500, then 750) and shuts down with exit code 0 and no leaked actor system. `readme.md`'s two links to this example's directory needed no text change; its Quick Start section demonstrates the still-supported manual composition path (design §D1) and was left as is. No other example and no production package changed.
- **Pull requests test only the packages a change affects, with native coverage** ([#108](https://github.com/getsyntegrity/ego/issues/108)). A new `internal/cmd/ciselect` program reads the module's package graph and selects the changed packages plus every package that (transitively, including through tests) depends on them, falling back to the full suite for `go.mod`, CI config, the root package or any path it cannot classify. `pull_request.yml` runs this fast lane; `build.yml` keeps testing everything on every push to `main`. Coverage now comes from native `go test -covermode=atomic`, replacing `go-acc`, whose `--ignore test` matched by substring and silently dropped the real `./testkit` package from both test runs and coverage; the new exclusion list matches whole path segments instead, so `testkit` is tested and covered for the first time. `-p 1` (no longer justified) is gone and `-timeout 30m` replaces no timeout at all; `-race` stays on in both lanes. See `docs/ci.md` for the full policy and the measurements behind it.
- **Publisher contracts moved to the runtime-neutral package `port/publishing`** ([#103](https://github.com/getsyntegrity/ego/issues/103), slice S1a of the ego-arch-001 ADR). `EventPublisher`, `StatePublisher` and `ErrPublisherNotStarted` are now declared in `github.com/pablogore/ego/v4/port/publishing`, which depends only on the standard library, `egopb` and the protobuf runtime, so a publisher can be written without importing the GoAkt runtime. Package `ego` keeps type aliases and the same sentinel variable, so existing code compiles unchanged and `errors.Is(err, ego.ErrPublisherNotStarted)` still matches. The only observable difference is reflective: `reflect`/`%T` now name the interfaces `publishing.EventPublisher` and `publishing.StatePublisher`. The aliases are not deprecated yet; the publisher modules switch to the new import path after nested modules are verified in CI (#111).
- **CI enforces architectural layer boundaries** ([#107](https://github.com/getsyntegrity/ego/issues/107), slice S2 of the ego-arch-001 ADR). A new `internal/cmd/archcheck` tool checks every import edge of the root module and the nested modules against the layer rules of `openspec/changes/ego-arch-001/design.md` §3, and both workflows run it before the linter. A violation names the importer, the forbidden import and the rule. Known violations live in an explicit baseline where every entry has an owner, a justification and a removal criterion, and an entry that stops matching a violation fails the check, so the baseline can only shrink. See `docs/ci.md`, "Architecture boundary check".
- **Nested modules are built, vetted, linted and tested in CI** ([#111](https://github.com/getsyntegrity/ego/issues/111)). Before this change, a pull request confined to `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `benchmark` or `example/cluster` classified as `Satellite` and went green without building the module at all. `ciselect` now selects nested modules independently of the root package lane — a changed file under a module, a root change reaching a package the module directly imports, or a full-gate path (`-all`, root `go.mod`/`go.sum`, `.github/`, `scripts/ci/`, `internal/cmd/ciselect/`) — and writes the decision to `modules.json`. `pull_request.yml` and `build.yml` fan out one matrix job per selected module, running the new `scripts/ci/verify-module.sh` (build, vet, lint against the root `.golangci.yml`, and test only when the module has tests). `release.yml` discovers publishers from `publisher/*/go.mod` instead of a hardcoded list, and the new `scripts/ci/verify-published.sh` proves each publisher builds against its just-published root version with the local `replace` dropped, failing clearly if that version is not actually on the proxy. See `docs/ci.md`, "Nested module CI (#111)".
- **CI selects modules from the repository's module graph** ([#102](https://github.com/getsyntegrity/ego/issues/102), slice S0 of the ego-arch-006 design). `ciselect` reads every module's `go.mod` with `go mod edit -json` (offline, with `GOWORK=off` on every `go` subprocess) and treats an in-repository requirement resolved through a local `replace` as an edge; a requirement pinned to a published version is reported but not followed. A change selects the modules that contain it plus every module that transitively requires one of them, and records the chain that reached each (`it ← adapter/a ← port`). An edge is followed only if the consumer's parsed imports (tests included, build tags ignored) name an affected package of the module it requires, unless that module's `go.mod`/`go.sum` changed, it had a boundary change, or it is the root with a `full` lane; for the root this keeps #111's rule, so a root change no nested module imports still selects no module. `go.work`, `go.work.sum`, `.golangci.yml`, `Makefile`, `Dockerfile.ci`, `buf.yaml`, `buf.gen.yaml`, `.github/`, `scripts/ci/`, `internal/cmd/ciselect/` and `protos/` force the full gate. A nested `go.mod` that was added or removed fully changes its parent module; the new `-base <rev>` flag tells that apart from an edit, which only marks the module's manifest changed, and `pull_request.yml` passes the pull request's merge-base (without `-base`, every changed nested `go.mod` counts as a boundary change). A module the root requires seeds the root package lane from the root packages that import it. `modules.json` keeps its shape; the new `plan.json` lists every module with its selection and reason, and `summary.md` gains a "module | selected | why" table. On today's repository the selection differs from before only for `go.work`. See `docs/ci.md`, "Module selection rules (module graph, #102)".
- **The publisher modules import `port/publishing` instead of package `ego`** ([#103](https://github.com/getsyntegrity/ego/issues/103), slice S1b of the ego-arch-001 ADR). The production build of `publisher/kafka`, `publisher/nats`, `publisher/pulsar` and `publisher/websocket` no longer imports the GoAkt runtime: `go list -deps ./...` in each lists no `github.com/tochemey/goakt/v4` package. This is not yet a smaller test build or module graph: each module's tests still import package `ego` and compile 45 GoAkt packages (`go list -deps -test ./...`), and each `go.mod` keeps `github.com/tochemey/goakt/v4` as an indirect requirement because the root module requires it. Removing GoAkt from the test closures and measuring the effect is tracked in [#122](https://github.com/getsyntegrity/ego/issues/122). Their public API is unchanged, and they still satisfy `ego.EventPublisher` and `ego.StatePublisher` through the aliases; each module gained a test that pins those assertions and checks that `errors.Is` matches the error a stopped publisher returns against both `ego.ErrPublisherNotStarted` and `publishing.ErrPublisherNotStarted`. The four publisher entries are gone from the archcheck baseline. A publisher release must require a root version that already contains `port/publishing`, which `release.yml`'s `verify-published.sh` step enforces.
- **The publisher modules' default unit-test closure no longer compiles GoAkt or the root package** ([#122](https://github.com/getsyntegrity/ego/issues/122), following up on S1b above). Each publisher's historical alias/sentinel check against package `ego` (`compat_test.go`) now sits behind a `compat` build tag, so `go list -deps -test ./...` with no `-tags` — the command a contributor's plain `go test ./...` actually compiles — contains neither `github.com/tochemey/goakt/v4` nor the root package `github.com/pablogore/ego/v4` any more (was 45 GoAkt packages plus the root package; now 0 of either). A new `TestUnitTestClosureExcludesRuntimeAndRoot` test in every publisher module guards the regression by shelling out to `go list -deps -test ./...` itself. The historical checks still run, unchanged, in a separate "compatibility lane": `scripts/ci/verify-module.sh` now also runs `go vet`, `golangci-lint` and `go test` with `-tags compat` whenever a module has a `compat`-tagged file, and `internal/cmd/ciselect` already selects every publisher module on any root-package change (it parses test files' imports regardless of build tags), so the lane runs on exactly the PRs that could break an alias. `go.mod`/`go.sum` are unchanged in every publisher (`go mod tidy -diff`: no diff) — `go mod tidy` has no `-tags` flag and conservatively keeps requirements for every custom-tag build the module could produce, including the compat one; the win here is the test-compilation closure, not the module graph. Clean-`GOCACHE` `go test -count=1 ./...` wall time dropped roughly in half for all four modules (kafka 30.1s → 13.3s, nats 34.0s → 15.4s, pulsar 55.8s → 29.1s, websocket 33.8s → 18.2s; one machine, Go 1.27.1, no `-race`). See `docs/ci.md`, "Compatibility lane (#122)".
- **`go mod tidy -diff` is now a real CI gate for every nested module** ([#122](https://github.com/getsyntegrity/ego/issues/122) follow-up). #122's own acceptance criteria named `go mod tidy -diff` as something the change would check, but `scripts/ci/verify-module.sh` never actually ran it, so an untidy `go.mod`/`go.sum` in `benchmark`, `example/cluster` or any of the four publishers would have gone green. `verify-module.sh` now runs `go mod tidy -diff` right after `go mod download` and before `go build`, and fails the job with the printed diff when one exists. `go mod tidy` has no `-tags` flag, so it already accounts for the `compat`-tagged files the compatibility lane above added — no separate `-tags compat` tidiness pass was needed. `GOFLAGS` is now cleared alongside the script's existing `GOWORK=off`, so an inherited `-mod=vendor` (nested modules keep no `vendor/`) can no longer make `go mod tidy`, or any other step, fail for a reason unrelated to the module itself. See `docs/ci.md`, "`scripts/ci/verify-module.sh`: what runs for one selected module".
- **The publisher compatibility checks moved into a new, unreleased `test/compat` module** ([#102](https://github.com/getsyntegrity/ego/issues/102), slice S1 of the ego-arch-006 design). The eight compile-time assertions that the publishers satisfy `ego.EventPublisher` and `ego.StatePublisher` now live in `test/compat`, which requires the root module and the four publishers through local `replace` directives. It is the repository's first module that depends on another nested module. It is never released, and no released module may require it. The runtime sentinel check is split: each publisher's own contract test checks that `Publish` before `Start` returns `publishing.ErrPublisherNotStarted`, and `test/compat` checks that `ego.ErrPublisherNotStarted` is that same error value, which together prove the original check. The four `compat_test.go` files, the `compat` build tag and `verify-module.sh`'s tag lane are gone, so no publisher file imports package `ego` in any build any more. `go mod tidy` dropped the indirect GoAkt, Olric and OpenTelemetry requirements from every publisher's `go.mod`, and added indirect lines that pin already-resolved versions in `publisher/kafka` (1) and `publisher/pulsar` (12); the module versions each publisher actually builds and tests with are identical to before. The publishers still require the root module for `egopb` and `port/publishing` until slice S3. `internal/cmd/archcheck` gains a module table (read with `go mod edit -json`) and two module-aware checks: the new `no-module-cycle` fails on any in-repository `go.mod` requirement cycle, and `no-cross-module-internal` now rejects an `internal/` import between any two in-repository modules, not only from a nested module into the root. No baseline entry was added. See `docs/ci.md`, "Compatibility checks: the `test/compat` module".
- **Codecov removed.** The Codecov upload, `codecov.yml`, and the README badge are gone — the maintainers do not use it, and it was inherited from the upstream project this repo was forked from (`tochemey/ego`). Both workflows now publish the native coverage total (`go tool cover -func`) to the job summary instead.
- **The last archcheck baseline entry is gone: `migration` no longer imports package `ego`** ([#147](https://github.com/getsyntegrity/ego/issues/147), slice S4-1 of the ego-arch-001 ADR). `migration`'s only production use of package `ego` was `ego.ResolveLogger`, resolving the kit-logger logger it falls back to when none is configured (via `ego.DefaultLogger()`) whenever a caller's logger is nil or a typed-nil pointer. That logic — `DefaultLogger`, the typed-nil detection and `ResolveLogger` — now lives in a new runtime-free internal package, `internal/logging`, which imports only `kit-logger` and the standard library `reflect` package. `ego.DefaultLogger` and `ego.ResolveLogger` keep their exact signature, behavior and identity (a test comparing `ego.DefaultLogger()` with `assert.Same` still sees the same `kitlog.L()` instance) and delegate to `internal/logging`; `migration` now imports `internal/logging` directly instead of package `ego`. No public API changed. `go list -deps github.com/pablogore/ego/v4/migration` no longer contains the root package or `github.com/tochemey/goakt/v4`. The `migration -> ego` baseline entry (`internal/cmd/archcheck/baseline.go`, rule `application-no-runtime`) is removed; `repoBaseline` is now empty, and archcheck reports `0 baselined, 0 violation(s), 0 stale entries`. See `openspec/changes/ego-arch-001/design.md` §2–§4.

## [v4.4.3] - 2026-08-15

### 💥 Breaking Changes

- **`testkit.EventSourcedScenario.Given` takes a prior state instead of prior events.** Existing calls that passed events — `Given(accountCreated, accountCredited)` — no longer compile. Rename them to `GivenEvents` to keep the same arrangement, or pass the single state those events fold into. Nothing outside the testkit is affected, and `DurableStateScenario.Given(state, version)` is unchanged.

### ✨ Features

- **Event-sourced scenarios can be arranged from state or from history.** `testkit.EventSourcedScenario` now offers both ways to put an entity into the state a command is handled against, so a test can say what the entity *is* or what has *happened* to it, whichever reads better:

  ```go
  // from state — handed to HandleCommand verbatim, the way EventSourcedActor
  // hands over the state it recovered
  testkit.ForEventSourcedBehavior(behavior).
      Given(&pb.Account{AccountId: "acc-1", AccountBalance: 125}).
      When(&pb.CreditAccount{AccountId: "acc-1", Balance: 75}).
      ThenState(t, &pb.Account{AccountId: "acc-1", AccountBalance: 200})

  // from history — replayed in order through HandleEvent, the way the engine
  // replays a journal
  testkit.ForEventSourcedBehavior(behavior).
      GivenEvents(
          &pb.AccountCreated{AccountId: "acc-1", AccountBalance: 100},
          &pb.AccountCredited{AccountId: "acc-1", AccountBalance: 25},
      ).
      When(&pb.CreditAccount{AccountId: "acc-1", Balance: 75}).
      ThenState(t, &pb.Account{AccountId: "acc-1", AccountBalance: 200})
  ```

  `Given(priorState)` keeps the arrangement independent of `HandleEvent`, which matters when the command handler is what the test is actually about: the handler under test no longer builds its own fixture. `GivenEvents(events...)` keeps the event-based arrangement for tests where the history is the clearer statement, and deliberately exercises `HandleEvent` as part of the scenario. The two compose — `Given(snapshotState).GivenEvents(subsequentEvents...)` mirrors an entity recovered from a snapshot and then replayed from that point. Omitting both starts from `InitialState()`.

  Either way, the events the command produced are applied in order to derive the resulting state, the same derivation the actor performs before persisting, and that result is what `ThenState` asserts.

- **A broken scenario arrangement can no longer pass as a command outcome.** An event `GivenEvents` cannot replay is recorded as an arrangement failure, distinct from the failure of the command under test, and **every** assertion — `ThenEvents`, `ThenState`, `ThenNoEvents`, and `ThenError` alike — reports it as `given events could not be applied: ...`. A replay failure previously came back as the scenario's own error, indistinguishable from a rejected command, so a test whose *setup* was broken could satisfy `ThenError` and read as a command correctly refused — eGo's own testkit suite contained exactly such a test.

### 🐛 Bug Fixes

- **The testkit scenarios now accept real behaviors.** `testkit.ForEventSourcedBehavior` and `testkit.ForDurableStateBehavior` are declared against small interfaces the testkit defines with `proto.Message` parameters, which any ego behavior is meant to satisfy structurally — but `ego.Command`, `ego.Event`, and `ego.State` were *defined* types, not synonyms of `proto.Message`, so the signatures never matched. Any behavior written against `ego.EventSourcedBehavior` — including every example in this repository — failed to compile when passed to a scenario, and the only workaround was to write a second, `proto.Message`-shaped copy of the behavior and test that copy instead of the code the engine runs.

  `ego.Command`, `ego.Event`, and `ego.State` are now aliases for `proto.Message`. An alias is the same type under another name, so existing application code compiles unchanged — and every behavior the engine accepts now satisfies the testkit's interfaces, exactly as intended:

  ```go
  // the behavior you run in production is the behavior you test — no adapter
  testkit.ForEventSourcedBehavior(&AccountBehavior{}).
      Given(&pb.Account{AccountId: "acc-1", AccountBalance: 125}).
      When(&pb.CreditAccount{AccountId: "acc-1", Balance: 75}).
      ThenState(t, &pb.Account{AccountId: "acc-1", AccountBalance: 200})
  ```

  A compile-time assertion in the ego package now pins this compatibility, so the two can never drift apart unnoticed again.

### ⬆️ Dependencies

- `github.com/tochemey/goakt/v4` v4.4.3 → v4.5.0
- `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/metric`, `go.opentelemetry.io/otel/sdk`, and `go.opentelemetry.io/otel/trace` v1.44.0 → v1.45.0
- `google.golang.org/protobuf` → v1.36.12

  Go-Akt v4.5.0 introduces a multiplexed remoting protocol — per-peer lane connections, chunked large messages, and credit-based flow control. eGo adopts it without a code or configuration change: remoting belongs to the application rather than to eGo, and the same `remote.NewConfig(host, remotingPort)` negotiates the new protocol on its own. Entity placement and death-watch now travel on a dedicated control lane, isolated from user command traffic.

  Clusters roll node by node. The default `auto` protocol pin negotiates the multiplexed wire with upgraded peers and falls back to the legacy wire for peers still on v4.4.x, so no flag day is required. Deployments with heavy inter-node command traffic can shard it across connections with `remote.WithOrdinaryLanes(n)` — safe at any count, because eGo's ordering guarantee is per entity and Go-Akt pins each receiver to a lane by a stable hash of its address. See [Remoting](./readme.md#remoting).

## [v4.4.2] - 2026-07-24

### 🐛 Bug Fixes

- **Projections no longer freeze silently on store errors** ([#318](https://github.com/Tochemey/ego/issues/318)). A failed events/offsets store round trip used to stop the processing loop permanently while the projection actor stayed alive and healthy-looking, leaving the read model dead until a node restart. The runner now retries the pull pass in place with exponential backoff (1s doubling to a 30s cap, reset on the first clean pass) and resumes from committed offsets once the store recovers — in both standalone and cluster mode, with no actor restart involved.
- **Unprocessable events now stop the projection visibly.** An event that cannot be processed — a handler error under the `Fail`/`RetryAndFail` recovery policies, a failed decryption, or a failed event adaptation — stops the runner and escalates to the projection actor, which fails through supervision with a stop directive instead of dying silently behind a healthy-looking actor. The failure is observable via `Engine.IsProjectionRunning`. The projection supervisor now also applies to cluster singletons via Go-Akt's `WithSingletonSupervisor`.

## [v4.4.0] - 2026-07-17

This release makes projections first-class named components: each projection is registered under its own name with its **own handler** and runtime options, aligning eGo with how Akka/Pekko Projections and Axon event processors bind one handler per projection. It removes two footguns of the previous engine-wide design — a single handler silently shared by every projection, and a second `WithProjection` call silently overwriting the first — and renames the projection lifecycle methods to match what they actually do.

### 💥 Breaking Changes

- **Each projection now has its own handler: `WithProjection` takes a name and is repeatable.** Previously the engine held a single engine-wide `projection.Options`, so every projection started with `AddProjection` shared the same handler instance — forcing a multiplexing handler that branched on event type. Calling `WithProjection` twice simply overwrote the earlier configuration. `WithProjection` now registers a *named* projection with its own handler and runtime options, and is called once per projection:

  ```go
  // before — one handler shared by every projection
  cfg := ego.NewConfig(eventsStore,
      ego.WithOffsetStore(offsetStore),
      ego.WithProjection(&projection.Options{Handler: handler, ...}),
  )
  engine.AddProjection(ctx, "account-balances")
  engine.AddProjection(ctx, "audit-log") // same handler as above

  // after — one handler per projection
  cfg := ego.NewConfig(eventsStore,
      ego.WithOffsetStore(offsetStore),
      ego.WithProjection("account-balances", &projection.Options{Handler: balanceHandler, ...}),
      ego.WithProjection("audit-log", &projection.Options{Handler: auditHandler, ...}),
  )
  engine.StartProjection(ctx, "account-balances")
  engine.StartProjection(ctx, "audit-log")
  ```

  The name passed to `WithProjection` is the projection's unique identifier: the same name is passed to `Engine.StartProjection`, and it keys the projection's committed offsets in the offset store. Each projection still gets its own runner, its own offsets, and its own recovery/dead-letter configuration — those are now simply carried per name instead of engine-wide. Registering the same name twice keeps the last registration; a nil options pointer is ignored.

  In cluster mode every node must register the same projections: a projection runs as a cluster singleton that can be (re)spawned on any node, and the hosting node resolves the handler from its **own** registration — the same contract `WithEntityKinds` establishes for entity behaviors. This is also why registration stays on the `Config` rather than moving to `StartProjection`: handlers are not serializable, so a handler passed at start time on one node could never reach the peer that ends up hosting the singleton.

- **`Engine.AddProjection` is renamed to `StartProjection`; `Engine.RemoveProjection` is renamed to `StopProjection`.** With registration moved to `WithProjection`, these methods purely start and stop a previously registered projection, and the new names mirror `Engine.Start`/`Engine.Stop`. `Engine.RebuildProjection` and `Engine.IsProjectionRunning` keep their names; `RebuildProjection` now performs the `StopProjection` → reset offset → `StartProjection` sequence under the new names. Behavior is unchanged apart from the fail-fast unknown-name check described below.

### ✨ Features

- **Multiple projections per engine.** Registering N projections and starting them all is now a first-class flow: each polls the events store independently on its own `PullInterval`, commits its own offsets, and invokes only its own handler. As before, handlers must be goroutine-safe: even within a single projection, pending shards are processed concurrently by the runner's worker pool, so `Handle` can be invoked from several goroutines at once — only events belonging to the same shard are handled sequentially, in order.

- **`ego.ErrProjectionNotRegistered`.** `StartProjection` on a name that was never registered fails immediately with this sentinel error (wrapped with the offending name), instead of spawning a projection actor whose `PreStart` would fail and be retried five times by the actor system:

  ```go
  if err := engine.StartProjection(ctx, "typo-name"); errors.Is(err, ego.ErrProjectionNotRegistered) {
      // registration and start-up are out of sync
  }
  ```

### 🛠 Improvements

- **Clear errors instead of panics on projection misconfiguration.** The projection actor's `PreStart` previously type-asserted the projection extension unconditionally and would panic if a projection actor was spawned on a system without one. It now returns descriptive errors — one when the registry extension is absent on the node, and one naming the projection when its registration is missing (pointing at `ego.WithProjection` as the fix).

- **Recovery defaulting no longer touches caller-owned options.** When `Options.Recovery` is nil, the default recovery strategy is applied to an internal copy of the options rather than the struct the caller passed in.

### 📖 Migration

1. **Name every `WithProjection` call** — use the same string you already pass to `AddProjection`/`StartProjection`. Offsets are keyed by that name, so existing committed offsets carry over unchanged; no store migration is needed.
2. **Rename the lifecycle calls** — `AddProjection` → `StartProjection`, `RemoveProjection` → `StopProjection`. Signatures are unchanged.
3. **Split multiplexing handlers (optional but recommended)** — a handler that branched on event type to serve several logical read models can now be split into one focused handler per named projection.
4. **Cluster deployments** — make sure every node's `Config` registers the same set of projections, just as every node already lists the same `WithEntityKinds`.

## [v4.3.0] - 2026-07-16

### 💥 Breaking Changes

- **`EventsStore.ShardNumbers` is replaced by `ShardOffsets`.** `ShardNumbers(ctx) ([]uint64, error)` returned only the distinct shard numbers, forcing the projection runner to interrogate every shard on every pull to find out which ones had new events. The new method

  ```go
  ShardOffsets(ctx context.Context) (map[uint64]int64, error)
  ```

  returns every distinct shard mapped to the offset (timestamp) of its most recent event, so a single round trip answers both "which shards exist" and "which shards are behind".

  **Migration for store implementors** — replace the `ShardNumbers` query with an aggregate:

  ```sql
  -- before
  SELECT DISTINCT shard_number FROM events_store
  -- after
  SELECT shard_number, MAX(timestamp) FROM events_store GROUP BY shard_number
  ```

  An index on `(shard_number, timestamp)` keeps the aggregate cheap. No schema change is required.

### ⚡ Performance Improvements

- **Projection pulls are now O(active shards) instead of O(all shards × 2 + events).** Previously each pull issued one `ShardNumbers` query, then one `GetCurrentOffset` **and** one `GetShardEvents` per shard in the journal — even for shards with nothing new — plus one `WriteOffset` per event. With hundreds of shards over a few milliseconds of database round-trip time, a single pull stretched into hundreds of milliseconds and compounded into multi-second projection latency under write bursts. Each pull now costs one `ShardOffsets` query when the projection is caught up, plus one `GetShardEvents` and one `WriteOffset` per shard that actually has pending events:

  - committed offsets are cached in memory (the projection runs as a cluster singleton, so the runner is the sole writer of its offsets) and compared against `ShardOffsets` to skip caught-up shards;
  - the offset is committed once per processed batch instead of once per event;
  - events persisted on the projection's node trigger an immediate pull through the in-process events stream instead of waiting for the next `pullInterval` tick (peer-node writes still ride the ticker);
  - a full event buffer triggers an immediate follow-up pull, draining backlogs without idling between ticks.

  `Engine.ProjectionLag` benefits as well: it previously scanned up to 10,000 events per shard to locate the newest timestamp; it now reads it straight from `ShardOffsets`.

### 🐛 Bug Fixes

- **A projection crash mid-batch no longer skips the rest of the batch.** The runner committed the batch's `nextOffset` after **each** event, so the very first commit already pointed past every event in the batch; a crash between two events of the same batch silently dropped the remaining ones on restart. The offset is now committed once, after the whole batch has been handled, restoring at-least-once delivery within a batch.

- **Remote entity spawns no longer fail with `dependency type is not registered` in cluster mode.** `Engine.Entity`, `Engine.DurableStateEntity`, and `Engine.Saga` registered the behavior's dependency type only on the calling node, at spawn time. With the default `RoundRobin` placement, `SpawnOn` routes most spawns to a peer, and the receiving node deserializes the spawn request's dependencies (the behavior plus eGo's internal spawn-configuration types) against **its own** registry — failing unless that node had already spawned the same kind itself. On a fresh N-node cluster, the first spawn of any entity kind failed roughly (N−1)/N of the time.

  `NewEngine` now registers eGo's internal spawn-configuration dependency types (which application code cannot reach) on every node, together with the behavior kinds supplied via the new `WithEntityKinds` option:

  - `ego.EntityKind` — alias for goakt's `extension.Dependency`; every `EventSourcedBehavior`, `DurableStateBehavior`, and `SagaBehavior` value is an `EntityKind`.
  - `ego.WithEntityKinds(kinds ...EntityKind) Option` — lists the behavior types this node can host. Pass one value per behavior type (a zero value is fine; only the concrete type is registered).

  **Cluster deployments must now build every node's `Config` with `WithEntityKinds`, listing every entity, durable-state, and saga behavior the cluster hosts** — the same contract `ClusterKinds()` already establishes for actor kinds:

  ```go
  cfg := ego.NewConfig(eventsStore,
      ego.WithEntityKinds(new(AccountBehavior), new(OrderBehavior)),
      // ...
  )
  ```

  Single-node deployments are unaffected and may omit the option: the lazy registration done by `Entity`, `DurableStateEntity`, and `Saga` remains as a local-node fallback.

## [v4.2.1] - 2026-06-20

### 🐛 Bug Fixes

- **Publisher and saga event-stream loops no longer busy-spin a CPU core when idle** (`6789f66`, #292). `eventstream.Subscriber.Iterator()` returns a closed snapshot channel whenever the queue is empty, so the engine's `sendEvent`/`sendState` publisher loops and the saga actor's receive loop — which `select`ed on `Iterator()` directly — spun at 100% CPU per loop while waiting for messages. The `Subscriber` interface gained a `Ready() <-chan struct{}` signal that fires when messages are enqueued or the subscriber shuts down; the loops now block on `Ready()` while idle and only drain the `Iterator()` snapshot once woken. If you implement `eventstream.Subscriber` yourself, you must add the `Ready()` method.

### 🧹 Improvements

- **Logger adapter refactor** (`8f0a73d`, `491c480`). The goakt logger adapter avoids `fmt.Sprint` reflection on the common single-string-message path, delegates the `*Context` variants to their non-context counterparts instead of duplicating their bodies, and replaces ad-hoc level strings with named constants. Dead code and redundant tests were removed alongside.

## [v4.2.0] - 2026-05-17

### 💥 Breaking Changes

- **eGo Is Now a Meta-Framework on Top of Go-Akt** — eGo no longer constructs, starts, or stops the underlying `goakt.ActorSystem`. Cluster discovery, TLS, remoting, partitioning, supervisors, and any other actor-runtime concern are now configured directly through Go-Akt's APIs. eGo contributes its event-sourcing, durable-state, projection, and saga primitives as Go-Akt extensions and plugs into an actor system the developer has built.

  **New surface:**

  - `ego.Config` — captures every option an engine needs (events store, plus state store, offset store, projection, snapshot store, event adapters, telemetry, encryptor, logger). Built once with `ego.NewConfig` and reused twice: once to build the actor system, once to plug in the engine.
  - `ego.NewConfig(eventsStore, opts ...Option) *Config` — constructs the config. Pass `nil` for `eventsStore` in durable-state-only deployments.
  - `(*Config).GoaktOptions() []goakt.Option` — returns the Go-Akt options eGo needs at actor-system construction: engine extensions (events store, event stream, plus state store, offset store, projection, snapshot store, event adapters, telemetry, encryptor whenever those are configured), pubsub, the logger adapter, and the default supervisor. Pass to `goakt.NewActorSystem(...)`.
  - `ego.ClusterKinds() []goakt.Actor` — returns the four actor kinds eGo needs registered in the cluster config (`EventSourcedActor`, `DurableStateActor`, `SagaActor`, `ProjectionActor`). Pass to `goakt.NewClusterConfig().WithKinds(...)` for relocation to work in cluster mode.
  - `ego.NewEngine(sys goakt.ActorSystem, cfg *Config) (*Engine, error)` — plugs eGo into an already-running actor system. Returns an error if `sys` is not running or if a required extension is missing from `sys`. The engine name is taken from `sys.Name()` — there is no separate name parameter. `Engine.Stop` does not call `sys.Stop`; the caller owns the actor-system lifecycle.

  **Typical bootstrap (any deployment shape):**

  ```go
  cfg := ego.NewConfig(eventsStore, opts...)

  sys, _ := goakt.NewActorSystem("MyApp", cfg.GoaktOptions()...)
  sys.Start(ctx)
  defer sys.Stop(ctx)

  engine, _ := ego.NewEngine(sys, cfg)
  engine.Start(ctx)
  defer engine.Stop(ctx)
  ```

  The same four-line shape works for local development and cluster mode; cluster users append Go-Akt's `WithCluster(...)` (with `ego.ClusterKinds()` registered) and `WithRemote(...)` to `cfg.GoaktOptions()` when calling `NewActorSystem`.

  **Removed:**

  - `WithCluster`, `WithClusterOption`, `ClusterOption`, `WithClusterConfigurator`, `ClusterProvider` (cluster configuration is now done directly with `goakt.NewClusterConfig(...)`).
  - `WithTLS`, `TLS` (use `goakt.WithTLS(...)` directly).
  - `WithRoles` (use `goakt.NewClusterConfig().WithRoles(...)` directly).
  - `WithRemoteOptions`, `WithActorSystemOptions` (compose Go-Akt options directly when calling `goakt.NewActorSystem`).
  - `WithActorSystemBuilder`, `ActorSystemBuilder`, `BuildActorSystem` (no longer needed — the actor system is always built by the caller).
  - `Engine` constructor argument: the `name string` parameter is removed; the engine name is derived from `sys.Name()`.

  **Why the change.** The previous design tried to be both a Go-Akt wrapper and a Go-Akt collaborator at the same time. That created precedence and cluster-coordination problems whenever a caller needed control over the runtime, and it hid Go-Akt capabilities developers legitimately need (custom cluster config, custom discovery, additional actors on the same system, TLS specifics). Treating eGo as a meta-framework on top of Go-Akt — analogous to how `database/sql` sits on top of a driver, or `net/http` sits on top of a `Listener` — resolves both concerns by exposing the primitives eGo needs and letting the developer compose the runtime.

  See [option.go](option.go), [engine.go](engine.go), and the rewritten [readme.md](./readme.md) for the full bootstrap pattern.

### 📝 Migration Notes

Upgrading from `v4.1.x` to this release requires reshaping the engine bootstrap:

1. Replace `ego.NewEngine("myapp", eventsStore, opts...)` with:
   ```go
   cfg := ego.NewConfig(eventsStore, opts...)
   sys, _ := goakt.NewActorSystem("myapp", cfg.GoaktOptions()...)
   sys.Start(ctx)
   engine, _ := ego.NewEngine(sys, cfg)
   engine.Start(ctx)
   ```
2. Move cluster configuration from `ego.WithClusterOption(...)` to a direct `goakt.NewClusterConfig(...).WithKinds(ego.ClusterKinds()...)` call passed to `goakt.NewActorSystem` via `goakt.WithCluster(...)`. Discovery providers come from the `discovery` packages of Go-Akt.
3. Move TLS from `ego.WithTLS(...)` to `goakt.WithTLS(...)`.
4. Drop `ego.WithRoles(...)`; use `goakt.NewClusterConfig(...).WithRoles(...)` instead.
5. Stop calling `Engine.Stop` for actor-system shutdown — call `sys.Stop(ctx)` yourself after `engine.Stop(ctx)`.
6. Check the `error` returned by the new `ego.NewEngine` signature; it now reports missing required extensions and unstarted actor systems instead of deferring those errors to `Engine.Start`.

### 🐛 Bug Fixes & Internal Changes

- **Event timestamps now use nanosecond resolution (was: seconds).** The `Timestamp` field on `egopb.Event`, `egopb.DurableState`, `egopb.Snapshot`, and `egopb.StateReply` is now populated via `time.Now().UnixNano()` instead of `time.Now().Unix()`. The underlying type stays `int64`, so wire compatibility is preserved, but the **semantic units change**.
  - Anything that interprets these timestamps as seconds (custom event publishers, external dashboards reading the raw column, ad-hoc SQL using `to_timestamp(timestamp / 1.0)`, etc.) must be updated to treat them as nanoseconds — e.g. `to_timestamp(timestamp / 1e9)` in Postgres.
  - `Engine.ProjectionLag(...)` continues to return a `time.Duration`; the value is now correct at nanosecond resolution instead of being quantised to whole seconds.
  - **Why.** Pre-fix, the projection runner's offset cursor was an event timestamp at 1-second resolution, and the poll predicate (`WHERE timestamp > committed_offset`) would silently skip any event written with the same second as the previously committed offset between two polls. Under sub-second burst load, events at the second boundary were dropped (reproduced by `make test` in `example/cluster`: 1000 + 30×10 − 10×5 expected = 1250, observed = 1270 because four debits were skipped). Bumping the timestamp to nanoseconds makes co-timestamp collisions across polls astronomically improbable and eliminates the race for any realistic workload.

- **In-process pub/sub topics collapsed to a single events topic and a single states topic.** Entity actors previously published to `topic.events.<shard>` / `topic.states.<shard>` and subscribers (engine publishers, sagas, `Engine.Subscribe()` consumers) had to enumerate every per-shard topic. With goakt's default 271 partitions hardcoded into the subscribe loop, any deployment configured with `goakt.NewClusterConfig().WithPartitionCount(N)` where `N > 271` would silently drop events for entities mapped to shards ≥ 271.
  - Now all entity events publish to a single `topic.events` (and durable state to `topic.states`). The shard each event belongs to travels in the payload (`egopb.Event.Shard`, `egopb.DurableState.Shard`), so downstream consumers can still filter by shard.
  - The internal `Engine.publisherTopicsPartitionCount` helper and the per-shard `generateTopics` helper have been removed. This is internal-only, but anyone who reached into the eventstream extension directly and subscribed to `topic.events.<n>` should switch to the single topic.

- **Sagas now observe events from every shard.** `SagaActor.PreStart` previously subscribed to topics `topic.events.0` … `topic.events.<ownShard>` (only as many topics as the saga's own shard index), so a saga placed in shard 5 missed events from shards 6 and above. Folded into the single-topic change above: sagas now subscribe to `topic.events` once and see every event.

- **`example/cluster/projection.go`** — fixed `AccountDebited` upsert that emitted `VALUES ($1, -$2, $3)`, which Postgres rejected with "operator is not unique: - unknown (SQLSTATE 42725)" because the prefix `-` could not pick an overload against an untyped placeholder. The handler now pre-negates the delta in Go and uses `EXCLUDED.balance` symmetrically with the credit branch.

- **`example/cluster/Makefile`** — the load-distribution probe now hits `/accounts/{id}` instead of `/healthz`. Kind's NGINX Ingress Controller exposes its own `/healthz` on the data port (80) and answers it before the request reaches the app, so the previous probe never received the app's `X-Served-By` header and the assertion was a no-op.

## [v4.1.2] - 2026-04-26

### 🚀 New Features

- **`Engine.ActorSystem()` Accessor** — Added `Engine.ActorSystem()` to expose the underlying `goakt.ActorSystem` powering the engine. Returns `nil` before `Start` and after `Stop`. Intended for callers that need direct access to actor-system primitives — for example, spawning auxiliary actors alongside ego entities or inspecting cluster topology — without having to wire the actor system separately. The returned reference is a snapshot and should not be retained across engine restarts. See [engine.go](engine.go).

### 🧹 Improvements

- **Lock-Free Hot Read Paths** — To make the new accessor safe for concurrent use, the underlying `goakt.ActorSystem` and its `NoSender` PID are now bundled in a single `atomic.Pointer[actorSystemRef]` and published atomically by `Start`. All hot read paths (`AddProjection`, `RemoveProjection`, `IsProjectionRunning`, `Entity`, `EntityExists`, `DurableStateEntity`, `SendCommand`, `Saga`, `SagaStatus`) now load the reference with a single atomic load instead of taking a mutex. `Stop` swaps the reference to `nil` so callers racing with shutdown fail fast with `ErrEngineNotStarted` rather than operating on a system in mid-teardown.
- **`RWMutex` for Remaining Snapshot Reads** — The engine mutex has been promoted from `sync.Mutex` to `sync.RWMutex`. The remaining field-snapshot reads (`offsetStore`, `eventStream`, `stateStore`, `eventsStore`, `snapshotStore`) acquire it via `RLock`, allowing concurrent readers. Writers (`AddEventPublishers`, `AddStatePublishers`) keep the exclusive `Lock`.
- **Test Coverage** — Added `TestActorSystem` covering the unstarted, started, stopped, and concurrent-reader cases (the latter exercised under `-race`), and `TestActorSystemRefGuard` exercising the defensive `ErrEngineNotStarted` branch on every hot read path.

## [v4.1.1] - 2026-04-18

### 🚀 New Features

- **Entity Existence Probe** — Added `Engine.EntityExists(ctx, entityID)` to check whether an entity is currently alive in the cluster without spawning it or replaying its journal. Works for both event-sourced and durable state entities, and is intended as a lightweight liveness check for callers that need to branch on entity presence without paying the cost of materialization. See [engine.go](engine.go).

### 🐛 Bug Fixes

- **Projection Lag Computation** — Fixed `Engine.ProjectionLag` to correctly compute the per-shard delta between the newest persisted event and the projection's committed offset. The result is now clamped at zero so a projection running ahead of the probe window is no longer reported as negative, and the implementation documents the unit (seconds) and bounded-scan trade-off used to locate the latest event in a shard. See [engine.go](engine.go).

### 🧹 Improvements

- Added `SECURITY.md` describing the project's security analysis and disclosure process.

## [v4.1.0] - 2026-04-02

### 🚀 New Features

- **Event Batching** — Accumulate events from multiple commands and flush them in a single store write, amortizing persistence cost under concurrent load. Two new spawn options control batching:
  - `WithBatchThreshold(n)` — flush after `n` accumulated events (0 disables batching, which is the default)
  - `WithBatchFlushWindow(d)` — flush after duration `d`, whichever comes first While a batch is being written, the actor stashes incoming commands and replays them after the write completes.

- **Benchmark Suite** — Added a comprehensive benchmark suite (`benchmark/`) measuring throughput, latency percentiles (p50/p90/p95/p99), heap usage, and GC cycles across sequential, parallel, batched, and unbatched workloads with simulated I/O latencies.

### ⚡ Performance Improvements

- **Async Persistence Pipeline** — Restructured event-sourced entity persistence into three specialized child actors:
  - `EventsWriterActor` — synchronous (Ask) event persistence and publishing; correctness requires write confirmation
  - `SnapshotsWriterActor` — asynchronous (Tell) snapshot persistence with encryption and exponential-backoff retry
  - `EventsJanitorActor` — asynchronous (Tell) retention policy enforcement after snapshot writes Snapshots and retention no longer block command processing, eliminating the primary synchronous bottleneck.

- **Batch Event Tracking** — Batch threshold now counts accumulated events rather than commands, correctly handling commands that produce multiple events.

### 🧹 Improvements

- Added retry utility (`retry.go`) with exponential backoff and jitter (up to 3 retries, capped at 2s) for async persistence operations
- Added Performance Tuning section to README covering batch threshold selection, snapshot configuration, retention policies, allocation optimization, and horizontal scaling guidance
- Added Persistence Stores section to README documenting ego-contrib store implementations (Postgres, MongoDB)
- Refreshed README header layout and removed emojis from section headings
- Comprehensive test coverage for events writer, snapshots writer, and events janitor actors
- Added batch trace assertion tests
- Excluded benchmark tests from code coverage metrics
- Updated publisher modules to ego v4.0.0

### ⬆️ Dependencies

- `github.com/tochemey/goakt/v4` v4.1.0 → v4.2.0
- `github.com/jackc/pgx/v5` → v5.9.1
- `github.com/klauspost/compress` v1.18.4 → v1.18.5
- `github.com/fxamacker/cbor/v2` v2.9.0 → v2.9.1
- `github.com/andybalholm/brotli` v1.2.0 → v1.2.1
- `golangci-lint` → v2.11.4
- `codecov/codecov-action` → v6

## [v4.0.0] - 2026-03-21

### 🚀 New Features

- **📸 Snapshot Store** — Introduced a dedicated `SnapshotStore` interface (`persistence/snapshot_store.go`) for persisting entity state snapshots independently from events. This decouples state recovery from event replay, significantly improving recovery performance for entities with long event histories. Configurable via `WithSnapshotStore()` engine option and `WithSnapshotInterval()` spawn option.

- **🔄 Event Adapters (Schema Evolution)** — Added the `eventadapter` package with an `EventAdapter` interface and a `Chain()` function for composing adapters. Event adapters transform persisted events from older schema versions into the current shape during replay and projection consumption, enabling seamless event schema evolution without rewriting stored data.

- **📊 OpenTelemetry Integration** — First-class observability via the new `Telemetry` struct and `WithTelemetry()` engine option. Includes:
  - Trace spans on command processing (`ego.command`) with `ego.persistence_id` and `ego.command_type` attributes
  - Metrics:
    - `ego.commands.total` (counter) — total number of commands processed
    - `ego.commands.duration` (histogram, ms) — command processing latency
    - `ego.events.persisted` (counter) — total number of events persisted
    - `ego.projection.events.processed` (counter) — total events processed by projections
    - `ego.entities.active` (up/down counter) — number of currently active entities
    - `ego.projections.active` (up/down counter) — number of currently active projections

- **💀 Dead Letter Handler** — Added the `DeadLetterHandler` interface (`projection/deadletter.go`) for receiving events that a projection failed to process after exhausting its recovery policy. Includes a `DiscardDeadLetterHandler` as the default no-op implementation. Configurable via `WithProjection()`.

- **🔁 Projection Rebuild** — New `Engine.RebuildProjection(ctx, name, from)` API method that stops a running projection, resets its offset to a given timestamp, and restarts it. Enables re-processing events from any point in time.

- **🧪 Testkit Scenarios** — Fluent Given/When/Then API for testing behaviors without starting an engine:
  - `EventSourcedScenario` — `Given(events...)`, `When(command)`, then assert with `ThenEvents()`, `ThenState()`, `ThenError()`, or `ThenNoEvents()`
  - `DurableStateScenario` — `Given(state, version)`, `When(command)`, then assert with `ThenState()`, `ThenVersion()`, or `ThenError()`

- **🔀 Migration Utility** — One-time `Migrator` (`migration/`) that reads legacy events (which embedded `resulting_state` at proto field 5) and extracts that state into the new `SnapshotStore`. Supports configurable page size and logging. Idempotent and safe to run multiple times.

- **📈 Projection Lag Monitoring** — Operators can now observe how far behind each projection is relative to the latest events in the store. Includes:
  - New metrics:
    - `ego.projection.lag_ms` (gauge, ms) — per-projection, per-shard lag
    - `ego.projection.latest_offset` (gauge, ms) — current projection offset timestamp per shard
    - `ego.projection.events_behind` (gauge) — approximate number of unprocessed events per shard
  - New `Engine.ProjectionLag(ctx, projectionName)` API returning per-shard lag as `map[uint64]time.Duration`

- **🗑️ Snapshot/Event Retention Policies** — Automatic cleanup of old events and snapshots after a snapshot has been successfully written, preventing unbounded storage growth. Configurable via `WithRetentionPolicy()` spawn option with:
  - `DeleteEventsOnSnapshot` — delete events up to the snapshot sequence number
  - `DeleteSnapshotsOnSnapshot` — delete older snapshots, keeping only the latest
  - `EventsRetentionCount` — number of events to retain before the snapshot point as a safety margin

- **🔐 Event Encryption / GDPR Support** — Transparent encryption of event and snapshot payloads at rest with crypto-shredding support for GDPR "right to erasure". Includes:
  - New `encryption` package with `Encryptor` and `KeyStore` interfaces
  - Default AES-256-GCM implementation (`encryption.AESEncryptor`)
  - New `encryption_key_id` and `is_encrypted` fields on `Event` and `Snapshot` protobuf messages
  - Events encrypted before persistence, decrypted during entity recovery and projection consumption
  - `Engine.EraseEntity(ctx, persistenceID, full)` API for GDPR erasure (physical deletion of events/snapshots)
  - `WithEncryptor()` engine option to enable encryption
  - In-memory `testkit.KeyStore` for testing

- **📝 Pluggable Logger Interface** — Introduced a minimal `Logger` interface (`logger.go`) that lets developers plug in any logging backend (zap, zerolog, slog, logrus, etc.). Methods follow the slog convention with structured key-value pairs. The engine now stores `Logger` directly and wraps it via `loggerAdapter` when passing to the underlying actor system. Includes:
  - `Logger` interface with `Debug`, `Info`, `Warn`, `Error` methods
  - Optional `LeveledLogger` interface for engine-side log gating
  - `DiscardLogger` — exported no-op logger for tests or silent operation
  - Default `slog`-based logger used when no logger is explicitly configured
  - `WithLogger()` engine option now accepts `Logger` instead of `log.Logger`

- **🔄 Saga/Process Manager** — First-class abstraction for long-running business processes that coordinate multiple entities with compensation logic for rollback on failures. Includes:
  - `SagaBehavior` interface with `HandleEvent`, `HandleResult`, `HandleError`, `ApplyEvent`, and `Compensate` methods
  - `SagaAction` type for declaring commands to send, events to persist, and completion/compensation signals
  - `SagaCommand` type for targeting commands to specific entities with configurable timeouts
  - Event-sourced saga actor that subscribes to the event stream, persists its own events, and recovers after restarts
  - `Engine.Saga(ctx, behavior, timeout)` API to start a saga
  - `Engine.SagaStatus(ctx, sagaID, timeout)` API to query saga state
  - Automatic compensation on timeout

### 💥 Breaking Changes

- **📦 Module Path** — Module path changed from `github.com/tochemey/ego/v3` to `github.com/tochemey/ego/v4`. All import paths must be updated.

- **🗃️ Event Proto Schema** — The `resulting_state` field (field 5) has been **removed** from the `Event` protobuf message and marked as reserved. Events are now "pure" — they no longer carry inline entity state. State is managed separately via the new Snapshot Store.

- **📐 Projection Handler Signature** — `projection.Handler.Handle()` signature changed: the `state *anypb.Any` parameter has been removed.
  - **Before:** `Handle(ctx, persistenceID, event, state, revision)`
  - **After:** `Handle(ctx, persistenceID, event, revision)`

- **🔧 Event-Sourced Recovery Rewrite** — Entity recovery no longer reads state from the latest event's `resulting_state`. Recovery now loads the latest snapshot (if available) and replays only subsequent events, applying event adapters in the chain.

- **📝 Logger Interface** — `WithLogger()` now accepts `ego.Logger` instead of `goakt/log.Logger`. Callers that previously passed a GoAkt logger (e.g. `log.DiscardLogger`) must switch to the new `ego.Logger` interface (e.g. `ego.DiscardLogger`). The engine's internal logging calls use `Logger.Debug/Info/Warn/Error` instead of `Debugf/Infof`.

- **🏗️ Internal Extension Constructors** — `extensions.NewProjectionExtension()` now requires an additional `DeadLetterHandler` parameter.

- **🏗️ EventSourcedActor Constructor** — `newEventSourcedActor()` no longer accepts arguments. Per-entity configuration (snapshot interval, retention policy) is now passed via the `extensions.EntityConfig` dependency, ensuring correct behavior during cluster relocation.

- **🌐 Cluster discovery API** — `WithCluster()` now accepts `ego.ClusterProvider` instead of GoAkt’s `discovery.Provider`. The engine wraps your implementation when wiring the actor system, so application code no longer depends on GoAkt’s discovery package for this option. Implement `ClusterProvider` (`ID`, `Start`, `DiscoverPeers`, `Stop`) or add a thin adapter around a GoAkt discovery implementation if you still use one.

### ⬆️ Dependencies

- **Go OpenTelemetry** — `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/metric`, and `go.opentelemetry.io/otel/trace` v1.42.0 promoted from indirect to direct dependencies
- **Publisher modules** (Kafka, NATS, Pulsar, WebSocket) — Import paths updated to v4; no functional changes

### 🧹 Improvements

- **Event proto** — Added `encryption_key_id` and `is_encrypted` fields to `Event` and `Snapshot` protobuf messages
- **Event-sourced actor** — Snapshot-based recovery with configurable intervals reduces event replay overhead
- **Event-sourced actor** — Retention policy cleanup runs after snapshot write, deleting old events and snapshots
- **Event-sourced actor** — Transparent encrypt/decrypt of event and snapshot payloads when an encryptor is configured
- **Projection runner** — Event adapters applied during projection consumption; dead letter forwarding on `RetryAndSkip` and `Skip` recovery policies
- **Durable state actor** — Added OpenTelemetry tracing and metrics on command processing
- **Test coverage** — Added `internal/extensions/extensions_test.go` and comprehensive tests for all new packages
- **Testkit** — Added in-memory `SnapshotStore` implementation (`testkit/snapshotstore.go`) for testing
- **Testkit** — Added in-memory `KeyStore` implementation (`testkit/keystore.go`) for encryption testing
- **Projection runner** — Decrypts encrypted events before handing them to the handler and event adapters
- **Projection runner** — Records per-shard lag, offset, and events-behind metrics when telemetry is enabled
- **Clustering** — `ClusterProvider` documentation aligned with engine lifecycle; peer discovery is configured only through ego’s `ClusterProvider` surface (no direct `discovery.Provider` on `WithCluster`)

### 📖 Migration Guide

To upgrade from v3 to v4:

1. **Update import paths** — Replace all `github.com/tochemey/ego/v3` imports with `github.com/tochemey/ego/v4`
2. **Update projection handlers** — Remove the `state *anypb.Any` parameter from your `Handle()` implementations
3. **Run the migration utility** — Use `migration.NewMigrator()` to extract inline state from legacy events into the new snapshot store
4. **Configure a snapshot store** — Pass a `SnapshotStore` implementation via `WithSnapshotStore()` for optimal recovery performance
5. **Update logger usage** — Replace `WithLogger(log.DiscardLogger)` or any `goakt/log.Logger` value with an `ego.Logger` implementation (e.g. `ego.DiscardLogger`). If you have a custom GoAkt logger, wrap it in the new `Logger` interface instead
6. **Regenerate protobuf** — If you depend on the `Event` message directly, regenerate from the updated `.proto` files
7. **Cluster mode** — Replace `WithCluster(goaktDiscoveryProvider, ...)` with `WithCluster(ego.ClusterProvider, ...)`. Map GoAkt’s `Initialize`/`Close` to `Start`/`Stop`, and `DiscoverPeers()` to `DiscoverPeers(ctx)`; `Register`/`Deregister` can be no-ops if your backend folds them into start/stop
