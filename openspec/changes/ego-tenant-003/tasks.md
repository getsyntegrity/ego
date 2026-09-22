# Tasks — EventStore tenant isolation (EGO-TENANT-003)

Tracker `#92`, epic `#23`. This change carries exactly one atomic task,
capped by this repository's hard 4-5-task spec-sizing rule at "as many
tasks as this slice needs, no more" — this slice needs one. The remaining
isolation work does not fit in this change; it is named below as an
explicit follow-up chain of later, separately-authorized SDD changes.

## T1 — Introduce `persistence.Scope`

- [x] 1.1 Write `persistence/scope_test.go` first (strict TDD is in force
      for this repository). Covers: zero value invalid; `Unscoped()` and a
      tenant scope both valid; `NewTenantScope` rejects an empty
      `tenancy.TenantID` with `ErrInvalidScope`; `Unscoped()` is never
      `Equal()` to a tenant scope in either direction; two tenant scopes
      compare equal iff their `tenancy.TenantID` is equal; `IsUnscoped()`
      is true only for `Unscoped()`; `TenantID()` round-trips for a tenant
      scope and is the zero value when unscoped; `String()` distinguishes
      the two kinds; a tenant literally named `"unscoped"` still does not
      equal `Unscoped()` (the forging guard — see design.md D3).
      Req: `specs/persistence-tenant-isolation/spec.md` — "Explicit Tenant
      Scope At The Boundary", "Effective Identity Is The Pair (Scope,
      persistence_id)", "Unscoped Backward Compatibility And
      Non-Collision".
- [x] 1.2 Ran the test suite before `scope.go` existed and observed it
      fail to compile (RED): `go test -mod=vendor ./persistence/...` →
      `undefined: persistence.Scope`, `undefined: persistence.Unscoped`,
      `undefined: persistence.NewTenantScope`,
      `undefined: persistence.ErrInvalidScope` (10 errors, capped at "too
      many errors" by the compiler).
- [x] 1.3 Implement `persistence/scope.go`: `Scope` value type (unexported
      `kind`/`tenant` fields, invalid zero value), `Unscoped()`,
      `NewTenantScope(tenancy.TenantID) (Scope, error)`, `ErrInvalidScope`
      sentinel, `Valid()`, `IsUnscoped()`, `TenantID()`, `Equal()`,
      `String()` — mirroring `persistence/precondition.go`'s conventions
      exactly (unexported fields, named constructors, `Valid()`,
      `String()`), per design.md D1–D3.
      Req: same as 1.1, plus "tenant_metadata Stays Non-Authoritative"
      (this task does not touch `tenant_metadata` handling at all, which
      is itself the evidence for that requirement at this slice).
- [x] 1.4 Ran the test suite again and observed it pass (GREEN):
      `go test -mod=vendor ./persistence/... -run TestScope -v` → 10/10
      `PASS`, then the full package `go test -mod=vendor ./persistence/...`
      → `ok`.
- [x] 1.5 Confirmed `tenancy` gained no dependency:
      `go test -mod=vendor -run TestTenancyArchitecture .` → `PASS`
      (import is one-directional, `persistence` → `tenancy`, per
      design.md D5).
- [x] 1.6 `go build -mod=vendor ./...` and
      `go vet -mod=vendor ./persistence/...` both clean.
- [x] 1.7 Wrote this proposal, `design.md`,
      `specs/persistence-tenant-isolation/spec.md`, and this task file.

**Evidence**: `persistence/scope.go`, `persistence/scope_test.go`. All
four verification commands captured in the apply/commit record for this
change (`go build -mod=vendor ./...`, `go test -mod=vendor
./persistence/...`, `go test -mod=vendor -run TestTenancyArchitecture .`,
`go vet -mod=vendor ./persistence/...`) — all clean. No production
interface signature was changed; `Scope` exists as an unused-by-any-store
type at the end of this slice, exactly as scoped.

## T4 — Engine/actor wiring

- [x] 4.1 Resolved the caller's real `tenancy.TenantContext` at spawn time
      in `Engine.Entity`/`Engine.DurableStateEntity`/`Engine.Saga`
      (`engine.go`'s `resolveSpawnTenantScope`), and injected it as a new
      per-spawn dependency, `internal/extensions.EntityTenantScope`, rather
      than resolving inside the actor itself. A resolved
      `tenancy.ScopeAdministrative` context is refused outright with the
      new `ErrAdministrativeScopeEntitySpawn` sentinel (administrative
      entity/saga spawn is out of scope for TENANT-003; see TENANT-008);
      legacy mode (no resolver configured) injects nothing and is
      byte-identical to before.
- [x] 4.2 Each of `EventSourcedActor`, `DurableStateActor`, and `SagaActor`
      gained a `scope persistence.Scope` field, bound once in `PreStart`
      via a new `resolveScope` method — called before `loadOptionalExtensions`/
      `setConfig` and before any store read, including recovery. Legacy
      mode binds `persistence.Unscoped()`; tenant-aware mode reads the
      injected `EntityTenantScope` dependency and fails closed with the new
      `ErrEntityTenantScopeMissing` sentinel when it is absent or invalid —
      no store read or write ever happens in that case.
- [x] 4.3 Threaded `scope` onto the request structs the parent actor sends
      its child persistence actors, since those are separate actors with no
      access to the parent's own dependencies: `persistEventsRequest.scope`
      (`events_writer_actor.go`), `persistSnapshotRequest.scope`
      (`snapshots_writer_actor.go`), `applyRetentionRequest.scope`
      (`events_janitor_actor.go`). Every store call in all three child
      actors now passes the caller-scoped value instead of a hardcoded
      `persistence.Unscoped()`.
- [x] 4.4 Turned recovered `tenant_metadata` from a first-seed into a
      cross-check (D6 strengthening): since `resolveScope` pre-seeds
      `actorTenant`/`boundTenant` from the spawn-bound tenant before
      recovery runs, a recovered record whose `tenant_metadata` disagrees
      with the spawn-bound tenant now fails closed via
      `tenancy.VerifyUnchanged` instead of silently being adopted as the
      actor's identity.
- [x] 4.5 `Engine.EraseEntity` now resolves the caller's tenant and scopes
      its erasure to it, instead of unconditionally calling the stores
      with `persistence.Unscoped()` regardless of who called it — the
      exact cross-tenant erasure hole this ticket exists to close. A
      resolver error, or a resolved context carrying no tenant identity
      (administrative scope, or invalid), fails the erasure closed; legacy
      mode is unchanged.
- [x] 4.6 Wrote the tests the implementation above did not yet have (strict
      TDD: for each, the corresponding production line was temporarily
      reverted, the test observed to fail (RED) for that specific reason,
      then the line was restored and the test observed to pass (GREEN)):
      - `TestEventSourcedActorSpawnBindsExactTenantScope`
        (`event_sourced_actor_scope_test.go`) — a tenant-scoped entity's
        recovery read and command write both carry its exact tenant
        `Scope`, proven with a `*mocks.EventsStore` whose expectations name
        that scope explicitly (not `mock.Anything`). RED: hardcoding
        `persistence.Unscoped()` in `events_writer_actor.go`'s `WriteEvents`
        call made the mock reject the unexpected-scope call (command timed
        out). GREEN after restoring.
      - `TestEventSourcedActorPreStartFailsClosedWithoutTenantScope`
        (same file) — tenancy active, no `EntityTenantScope` dependency
        injected: `PreStart` fails with `ErrEntityTenantScopeMissing`
        before `Ping`/`GetLatestEvent`/`WriteEvents` are ever called
        (`store.AssertNotCalled`). RED: removing `resolveScope`'s
        fail-closed default (falling back to `Unscoped()` instead of
        returning the sentinel) caused the mock to panic on an
        unexpected `Ping` call. GREEN after restoring.
      - `TestEventSourcedActorLegacyModeAlwaysUsesUnscopedStore` (same
        file) — no resolver configured: every store call still carries
        `persistence.Unscoped()`, unchanged for existing non-tenant users.
        RED: changing the legacy branch to bind an invalid
        `persistence.Scope{}` instead of `Unscoped()` made the mock panic
        on the mismatched-scope call. GREEN after restoring.
      - `TestEventSourcedActorRecoverRejectsMismatchedSpawnBoundTenant`
        (`event_sourced_actor_tenant_persist_test.go`) — a record stored
        under the actor's own spawn-bound scope but whose `tenant_metadata`
        names a different tenant fails recovery with
        `tenancy.ErrDenied`. RED: temporarily skipping the `actorTenant`
        pre-seed in `resolveScope` made the test's expected error vanish
        (recover silently adopted the foreign tenant). GREEN after
        restoring.
      - `TestEngineEntitySpawnRejectsAdministrativeScope`
        (`engine_tenant_spawn_test.go`) — an administrative-scope resolver
        blocks both `Engine.Entity` and `Engine.DurableStateEntity` with
        `ErrAdministrativeScopeEntitySpawn`, and no actor is ever spawned
        (`EntityExists` stays `false`). RED: making the administrative
        branch of `resolveSpawnTenantScope` return `(nil, nil)` (i.e. fall
        through as legacy) surfaced a different failure
        (`ErrEntityTenantScopeMissing` from the actor's own fail-closed
        guard) instead of the expected sentinel — still a clear failure.
        GREEN after restoring.
      - `TestEngineEraseEntityCannotEraseAnotherTenantsRecord`
        (`engine_erase_entity_tenant_test.go`) — two records under the
        same `persistence_id` but different tenant scopes; erasing as
        tenant A removes only tenant A's record and leaves tenant B's
        untouched. RED: disabling `EraseEntity`'s tenant-resolution branch
        (falling back to unconditional `Unscoped()`) made tenant A's own
        erasure silently no-op, since `Unscoped()` no longer matches any
        tenant-scoped record — failing the first assertion. GREEN after
        restoring.
- [x] 4.7 Documented the residual limitation this slice deliberately leaves
      open: a GoAkt actor's name is still the bare `entityID`/`sagaID`, not
      tenant-qualified, so two tenants sharing the same id still contend
      for one actor. Added a "Known limitation" section to `design.md`, a
      matching non-goal/requirement pair to
      `specs/persistence-tenant-isolation/spec.md`, and confirmed (via
      `rg -n 'Known limitation|not tenant-qualified' *.go`) that the
      production doc comments on `EventSourcedActor.scope`,
      `DurableStateActor.scope`, and `SagaActor.scope` already carried this
      note.

**Evidence**: `engine.go`, `event_sourced_actor.go`, `durable_state_actor.go`,
`saga_actor.go`, `events_writer_actor.go`, `snapshots_writer_actor.go`,
`events_janitor_actor.go`, `migration/migration.go`,
`internal/extensions/extensions.go` (production); new test files
`event_sourced_actor_scope_test.go`, `engine_tenant_spawn_test.go`,
`engine_erase_entity_tenant_test.go`, plus a new test added to
`event_sourced_actor_tenant_persist_test.go`; existing tests across
`engine_test.go`, `option_test.go`, `durable_state_actor_test.go`,
`durable_state_actor_tenant_persist_test.go`, `event_sourced_actor_test.go`,
`event_sourced_actor_tenant_persist_test.go`, `saga_actor_tenant_test.go`,
`events_janitor_actor_test.go`, `events_writer_actor_test.go`,
`snapshots_writer_actor_test.go`, and `tenant_write_path_e2e_test.go`
adapted for the new spawn-time resolve and the `scope` field. All 6 new
tests observed RED (for the reason named above) and GREEN. Full
verification command results are recorded in this change's commit.

## Follow-up chain (not part of this change; named here per this
## repository's 4-5-task spec cap, each a separate, later SDD change)

- **T2 — Extend the tenant-aware SPI**: change
  `EventsStore.WriteEvents`/`ReplayEvents`/`GetLatestEvent`/etc.,
  `StateStore.WriteState`/`GetLatestState`, and `SnapshotStore`'s methods
  to take a `persistence.Scope` parameter (placement and exact signature
  shape is T2's own design decision — see design.md, "What this slice
  deliberately leaves open"); update the `testkit` stores' keying to the
  pair `(Scope, persistence_id)`; regenerate/update any mocks; update
  every production call site (actors, migration tooling, projection
  runner) to pass `Unscoped()` until T4 wires a real tenant through. This
  is expected to be a breaking-signature change of similar shape and size
  to `ego-write-004`'s PR2 (SPI break + testkit CAS + mock regen + call
  sites) and should be planned as its own chained-PR sequence.
- **T3 — Cross-tenant conformance suite**: a test suite proving read and
  write isolation directly against the `testkit` stores (independent of
  any actor mailbox, mirroring `ego-store-001`'s T3.3 finding that
  direct-against-store evidence is stronger than actor-mediated evidence)
  — two tenants presenting the same `persistence_id` must not observe or
  overwrite each other's records, under `-race`.
- **T5 — Migration and compatibility documentation**: write the
  external-adapter migration guidance for this breaking change, in the
  style of the WRITE-004 breaking-change block already present at the top
  of `persistence/events_store.go` ("Breaking change: WriteEvents gained a
  required precondition parameter").
