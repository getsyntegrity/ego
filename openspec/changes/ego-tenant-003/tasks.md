# Tasks — EventStore tenant isolation (EGO-TENANT-003)

Tracker `#92`, epic `#23`. All five slices this change was cut into (T1–T5)
are complete, each committed on `feat/ego-tenant-003-eventstore-isolation`.
The remaining isolation work — read-side/projection isolation, an
administrative bypass path, and tenant-qualified actor identity — does not
fit in this change; it is named below as an explicit follow-up chain of
later, separately-authorized SDD changes.

| Task | Summary | Commit |
| --- | --- | --- |
| T1 | Introduce `persistence.Scope` | `0154133` |
| T2 | Extend the tenant-aware SPI (`EventsStore`/`StateStore`/`SnapshotStore` take `Scope`) | `787550a` |
| T3 | Cross-tenant store conformance suite | `5b1d16f` |
| T4 | Engine/actor wiring | `99eccad` |
| T5 | Migration and compatibility documentation | this commit |

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

## T2 — Extend the tenant-aware SPI

- [x] 2.1 Changed every record-addressing method on `EventsStore`
      (`WriteEvents`, `DeleteEvents`, `ReplayEvents`, `GetLatestEvent`,
      `PersistenceIDs`), `StateStore` (`WriteState`, `GetLatestState`), and
      `SnapshotStore` (`WriteSnapshot`, `GetLatestSnapshot`,
      `DeleteSnapshots`) to take a `scope Scope` parameter immediately
      after `ctx`, mirroring `WritePrecondition`'s placement (design.md,
      "What this slice deliberately leaves open for T2-T5"). `Connect`,
      `Disconnect`, `Ping`, `GetShardEvents`, and `ShardOffsets` were left
      unchanged, deliberately: the first three are connection lifecycle,
      not record addressing; the last two are shard-level projection
      reads, whose tenant isolation is EGO-TENANT-004's scope.
      Req: `specs/persistence-tenant-isolation/spec.md` - "Effective
      Identity Is The Pair (Scope, persistence_id)".
- [x] 2.2 Re-keyed the in-repo `testkit` stores (`EventStore`,
      `DurableStore`, `SnapshotStore`; `testkit/eventstore.go`,
      `testkit/durablestore.go`, `testkit/snapshotstore.go`) structurally on
      `(Scope, persistenceID)` - never on `Scope.String()` or a
      concatenated string - with an invalid zero-value `Scope` rejected via
      `ErrInvalidScope` before any state is touched. The WRITE-004
      CAS/precondition compare now happens within a scope, so
      `ExpectGenesis()` succeeds for a second tenant on a `persistenceID`
      the first tenant already owns; `testkit/scope_test.go` (new, 238
      lines) pins this directly.
- [x] 2.3 Gave `persistence.ConflictError` a required `Scope` constructor
      parameter (`NewConflictError(scope, persistenceID, expected, ...)`,
      recovered via `(*ConflictError).Scope()`) and extended its canonical
      wire grammar and `ParseConflictError` with a `scope=` field ahead of
      `persistence_id=` (`persistence/conflict.go`,
      `persistence/conflict_test.go`).
- [x] 2.4 Hand-updated the three generated mocks
      (`mocks/persistence/events_store.go`, `snapshot_store.go`,
      `state_store.go`) and the ad-hoc test fakes
      (`preconditionSpyEventsStore`, `slowEventsStore`, `slowStateStore`) to
      the new signatures, and updated every production call site (actors,
      engine, `migration/migration.go`, saga) to pass `persistence.Unscoped()`
      for now, each marked `// TENANT-003 T4: carries the resolved tenant
      scope once entity actors bind one at spawn.` - so this slice is
      observably a no-op for existing non-tenant deployments.
      `example/cluster` is a separate Go module excluded from the main
      build/test; its `PostgresEventStore.WriteEvents` was already stale on
      the pre-WRITE-004 signature before this change and was left as is.
- [x] 2.5 `go build -mod=vendor ./...` and the full `go test -mod=vendor
      ./...` (excluding `example/cluster`) both clean after the call-site
      updates; 45 files touched across production code, tests, and mocks
      (see the commit's own `--stat`).

**Evidence**: commit `787550a`. Production:
`persistence/events_store.go`, `persistence/state_store.go`,
`persistence/snapshot_store.go`, `persistence/conflict.go`,
`testkit/eventstore.go`, `testkit/durablestore.go`,
`testkit/snapshotstore.go`, `migration/migration.go`, plus every actor and
engine call site listed in the commit. Tests: new `testkit/scope_test.go`,
`persistence/conflict_test.go` extended, and every existing test file that
called a store method updated to the new signature (see the commit's
`--stat` for the full 45-file list). The interface change alone is
sufficient evidence of the break: any pre-T2 implementation of
`EventsStore`/`StateStore`/`SnapshotStore` fails to compile against this
package, exactly as the doc comments' manual-verification recipe
describes.

## T3 — Cross-tenant conformance suite

- [x] 3.1 Added `persistence/conformance`, a store-agnostic,
      factory-driven suite (`RunEventsStoreConformance`,
      `RunStateStoreConformance`, `RunSnapshotStoreConformance`, each
      taking a `func(t *testing.T) <Store>` that must return a fresh, empty
      store per subtest) proving EGO-TENANT-003's isolation requirements
      directly against `EventsStore`, `StateStore`, and `SnapshotStore`
      implementations, independent of any actor or mailbox - the issue's
      own acceptance-criteria wording, "tests cross-tenant
      direct-store/conformance independientes del mailbox del actor".
      Req: `specs/persistence-tenant-isolation/spec.md`.
- [x] 3.2 The matrix (`persistence/conformance/events.go`,
      `state.go`, `snapshot.go`) covers, per store: read isolation
      (`ReadIsolation/OtherTenantGetsNothing`,
      `ReadIsolation/UnscopedAndTenantDoNotCrossRead`,
      `ReadIsolation/BothTenantsReadTheirOwnRecord`), write isolation
      including scoped delete (`WriteIsolation/OtherTenantWriteLeavesRecordUntouched`,
      `WriteIsolation/DeleteIsScoped`), WRITE-004 CAS semantics preserved
      per scope (`CAS/ExpectGenesisSucceedsForNewTenantOnEstablishedID`,
      `CAS/ExpectRevisionConflictCarriesItsScope`,
      `CAS/ConflictInOneScopeNotObservableInAnother`), `PersistenceIDs`
      enumeration isolation for `EventsStore`
      (`Enumeration/PersistenceIDsScopedToOwnTenant`), and the
      `Unscoped()`-vs-tenant-named-"unscoped" forging guard
      (`Unscoped/NeverCollidesWithTenantNamedUnscoped`) on all three.
- [x] 3.3 Connected/disconnected around every subtest
      (`persistence/conformance/check.go`'s `runConformance`), skipping a
      subtest (not failing it) when `Connect` reports the store
      unreachable, so an external adapter's CI with no live database is
      never misread as a passing isolation proof.
- [x] 3.4 Wired all three suites into the in-repo stores
      (`testkit/conformance_test.go`: `TestEventStoreConformance`,
      `TestDurableStoreConformance`, `TestSnapshotStoreConformance`),
      proving both that the suite is usable end to end and that T2's
      implementations are correct.
- [x] 3.5 Wrote the suite's own permanent self-check,
      `TestConformanceCatchesNonIsolatingStore`
      (`testkit/conformance_test.go`), which runs the same named checks
      (via `CaptureEventsStoreChecks`/`CaptureStateStoreChecks`/
      `CaptureSnapshotStoreChecks`, `persistence/conformance/check.go`)
      against wrappers that collapse every caller-supplied `Scope` to
      `Unscoped()` before delegating to an otherwise-correct store -
      exactly the shape of a naive tenant_metadata-only adapter. Verified
      RED first, directly: every one of the 10 event checks genuinely
      failed against the non-isolating wrapper before the capturing
      harness existed, then GREEN once the capture harness asserted on the
      expected failures instead of propagating them.

**Evidence**: commit `5b1d16f`. New package:
`persistence/conformance/{doc,check,events,state,snapshot,helpers}.go`
(1,134 lines added). Wiring and self-check: `testkit/conformance_test.go`
(194 lines added).

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

## T5 — Migration and compatibility documentation

- [x] 5.1 Added a Breaking Changes entry to `CHANGELOG.md`'s
      `[Unreleased]` section: the old/new signature for every changed
      method on `EventsStore`, `StateStore`, and `SnapshotStore`; why
      `Connect`/`Disconnect`/`Ping`/`GetShardEvents`/`ShardOffsets` did
      NOT change; that `ConflictError` and its wire grammar gained a
      `scope=` field; the structural-key upgrade recipe (real column,
      never `Scope.String()`); the zero-migration guarantee for a
      deployment that never activates tenancy, and what an operator must
      deliberately decide for a deployment that adopts tenancy on
      existing data; the `persistence/conformance` acceptance-test
      wiring; and the shared-actor-name known limitation.
- [x] 5.2 Added a "Tenant scoping" subsection to `readme.md`'s existing
      `## Persistence` section, pointing at `persistence/conformance` and
      `tenancy.WithSingleTenant`/`ego.WithTenantResolver`, and added it to
      the table of contents.
- [x] 5.3 Reconciled this task file with reality: T2 and T3 were
      committed (`787550a`, `5b1d16f`) but the "Follow-up chain" section
      still listed them as unchecked bullets. Replaced that section with
      completed T2/T3 task blocks carrying their real evidence, and this
      T5 block, plus the acceptance-criteria mapping below.
- [x] 5.4 Verified no production code or test logic changed:
      `go build -mod=vendor ./...` stays clean, and `git diff --stat`
      touches only `CHANGELOG.md`, `readme.md`, and this file.

**Evidence**: `CHANGELOG.md`, `readme.md`, this file
(`openspec/changes/ego-tenant-003/tasks.md`). Commit: this commit (see
`git log -1` on `feat/ego-tenant-003-eventstore-isolation` for its SHA).

## Follow-up chain (not part of this change; each a separate, later,
## explicitly-authorized SDD change)

- **Read-side/projection isolation (EGO-TENANT-004)**: `GetShardEvents`
  and `ShardOffsets` stayed deliberately unscoped in T2 (see
  `persistence/events_store.go`'s doc comment); projection consumers do
  not yet filter by tenant. This is the next slice of the isolation work
  this issue started.
- **Idempotency (#66)**: not addressed by this change.
- **Atomic multi-event append (#67)**: not addressed by this change.
- **Administrative bypass path (TENANT-008)**: `Engine.Entity` and
  `Engine.DurableStateEntity` refuse an administrative-scope spawn
  outright (`ErrAdministrativeScopeEntitySpawn`, T4.1); a deliberate,
  audited administrative bypass is out of scope here and left to
  TENANT-008.
- **Tenant-qualified actor identity**: the known limitation documented in
  `design.md` and the CHANGELOG entry above — a GoAkt actor's name is the
  bare `entityID`/`sagaID`, not `(tenant, entityID)`, so two tenants
  sharing an id contend for one actor (fail-closed, not a leak). Deriving
  the actor name from the tenant is its own design pass, since it likely
  touches cluster placement/rebalancing and any tooling that addresses an
  actor by bare entity id.

## Acceptance criteria (issue #92) — evidence mapping

Issue #92 lists eight acceptance criteria. Each is mapped below to the
concrete test, file, or spec requirement that satisfies it, or marked
partial with what remains.

1. **Tenant-aware store mutations and reads receive an explicit tenant
   scope and do not depend only on descriptive metadata.** MET. Every
   record-addressing method on `EventsStore`/`StateStore`/`SnapshotStore`
   takes a `scope Scope` parameter (`persistence/events_store.go`,
   `state_store.go`, `snapshot_store.go`); `tenant_metadata` is
   documented and enforced as non-authoritative — `persistence.Scope`'s
   doc comment's "tenant_metadata stays non-authoritative" section, and
   `resolveScope`'s cross-check via `tenancy.VerifyUnchanged`
   (`TestEventSourcedActorRecoverRejectsMismatchedSpawnBoundTenant`,
   `event_sourced_actor_tenant_persist_test.go`) proves a disagreeing
   `tenant_metadata` is rejected rather than trusted.

2. **Two tenants with the same `persistence_id` do not share an effective
   storage identity.** MET. The `testkit` stores key structurally on
   `(Scope, persistenceID)` (T2, `787550a`); proven directly by
   `ReadIsolation/BothTenantsReadTheirOwnRecord` in
   `persistence/conformance/{events,state,snapshot}.go`.

3. **A tenant cannot read state/events belonging to another tenant through
   a `persistence_id` collision.** MET.
   `ReadIsolation/OtherTenantGetsNothing` and
   `ReadIsolation/UnscopedAndTenantDoNotCrossRead`
   (`persistence/conformance/{events,state,snapshot}.go`) prove this at
   the store level; `TestEventSourcedActorPreStartFailsClosedWithoutTenantScope`
   and `TestEventSourcedActorSpawnBindsExactTenantScope`
   (`event_sourced_actor_scope_test.go`) prove it at the actor level.

4. **A tenant cannot modify another tenant's revision/state/event
   stream.** MET. `WriteIsolation/OtherTenantWriteLeavesRecordUntouched`
   and `WriteIsolation/DeleteIsScoped`
   (`persistence/conformance/events.go`, `snapshot.go`;
   `stateOtherTenantWriteLeavesRecordUntouched` in `state.go`) at the
   store level; `TestEngineEraseEntityCannotEraseAnotherTenantsRecord`
   (`engine_erase_entity_tenant_test.go`) at the engine level.

5. **WRITE-004's optimistic concurrency semantics are preserved within
   each tenant scope.** MET.
   `CAS/ExpectGenesisSucceedsForNewTenantOnEstablishedID`,
   `CAS/ExpectRevisionConflictCarriesItsScope`, and
   `CAS/ConflictInOneScopeNotObservableInAnother`
   (`persistence/conformance/events.go`, `state.go`) prove the persisted
   revision a precondition compares against is scoped; `ConflictError`
   carries its `Scope` (`persistence/conflict.go`).

6. **`single_tenant_mode` keeps working without tenant plumbing invented
   by the application.** MET, with a naming correction: there is no
   literal `single_tenant_mode` flag in the code. The implemented form is
   the built-in resolver `tenancy.WithSingleTenant(id)`
   (`tenancy/resolver.go`), registered once via `ego.WithTenantResolver`.
   `TestSendCommandSingleTenantZeroPlumbing` (`engine_test.go`) proves a
   command succeeds with a plain `context.Background()` and zero
   `tenancy.Attach`/`tenancy.Require` calls at the call site; T4's
   `resolveScope` binds the same `persistence.Scope` machinery for this
   resolver as for any multi-tenant resolver (`TestSendCommandResolverSwapIdenticalSequence`,
   same file) — there is no special-cased single-tenant code path to
   diverge from the isolation guarantees above.

7. **Cross-tenant direct-store/conformance tests exist, independent of
   the actor mailbox.** MET. `persistence/conformance` (T3, `5b1d16f`) is
   exactly this: a factory-driven suite that never constructs an actor or
   mailbox, wired into the in-repo stores by
   `testkit/conformance_test.go`. Its own permanent regression guard,
   `TestConformanceCatchesNonIsolatingStore`, proves the suite is not a
   tautology — it genuinely fails against a store that ignores `Scope`.

8. **Compatibility and migration of existing implementations are
   explicit.** PARTIALLY MET. The compatibility contract itself is fully
   explicit: the `CHANGELOG.md` `[Unreleased]` entry and the doc comments
   on `EventsStore`/`StateStore`/`SnapshotStore` state the old/new
   signatures, the structural-key upgrade recipe, and the zero-migration
   guarantee for a deployment that never activates tenancy (every call
   already carries `Unscoped()`, unchanged). What remains unmet: this
   change ships no migration *tool* for a deployment that adopts tenancy
   on already-existing data. Existing rows have no tenant of their own —
   they were written under `Unscoped()`, a real and distinct scope, not a
   wildcard — and this change is explicit that assigning them to a
   tenant is a deliberate, deployment-specific operator decision this
   repository does not automate. A reader adopting tenancy on a live,
   already-populated store needs to plan that migration themselves; nothing
   here does it for them.
