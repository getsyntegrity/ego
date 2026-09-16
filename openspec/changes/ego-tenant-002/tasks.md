# Tasks: Tenant-aware write path, remaining slices (EGO-TENANT-002)

Tracker [#54](https://github.com/getsyntegrity/ego/issues/54), epic #23 · `specs/tenancy-write-path/spec.md` (read-only) · `design.md` (read-only) · `proposal.md` (read-only). PR1 (`#73`) and **PR2 (`#77`) are already merged** — proto, `event_sourced_actor.go`, and `durable_state_actor.go` are consumed unchanged, not edited by any task below. **PR2's task list below is a historical record of already-completed work; do not re-run it.** Only PR3 remains open.

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | PR2 ~250-350 (1 file + 1 new test file); PR3 ~450-600 (1 file + 2 new test files) |
| 400-line budget risk | PR2: Low-Med; PR3: Medium |
| Chained PRs recommended | Yes |
| Suggested split | PR2 (durable-state) → PR3 (saga + e2e) |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

Rationale: `proposal.md`'s delivery plan is already a stacked chain (PR1 merged); `design.md`'s PR Slicing table confirms disjoint file sets, PR3 targeting PR2's merged base, and explicitly names PR4 as contingency only — no PR4 task group is created here.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 1 | Durable-state tenant persist/recover/identity/read-gate | PR 2 | `go test -mod=vendor -race -run 'TestDurableState.*Tenant' .` | N/A — actor unit tests via `mocks.StateStore` | Revert `durable_state_actor.go` diff + delete `durable_state_actor_tenant_persist_test.go` |
| 2 | Saga tenant binding + real-dispatch e2e | PR 3 | `go test -mod=vendor -race -run 'TestSaga.*Tenant|TestTenantWritePathE2E' .` | `require.Eventually` over `testkit.NewEventsStore()` (real dispatch) | Revert `saga_actor.go` diff + delete `saga_actor_tenant_test.go`, `tenant_write_path_e2e_test.go` |

---

## PR2 — Durable-State Tenant Enforcement — **DONE, merged as [`#77`](https://github.com/getsyntegrity/ego/pull/77)**

Targeted PR1's merged base (`main` @ `ddf9337`). No proto change. New test file: `durable_state_actor_tenant_persist_test.go`. All tasks below are checked off as a historical record; the implementation went through its own review round with two refinements beyond what's listed here — a `commitState` single-commit-point consolidation and a `currentVersion > 0` discriminant for DS3's unseeded-`PostStop` skip (semantically equivalent to `actorTenant == noTenantContext`, more robust). See `design.md`'s PR2 header note.

### Phase 1: Foundation — seed at `recoverFromStore` (DS2)

- [x] 1.1 RED `durable_state_actor_tenant_persist_test.go` — `recoverFromStore` seeds `actorTenant` from a record with valid tenant metadata
- [x] 1.2 RED `durable_state_actor_tenant_persist_test.go` — genesis (`durableState == nil || proto.Equal(...)`) leaves the actor unseeded (`noTenantContext`), no `ErrInvalid`
- [x] 1.3 RED `durable_state_actor_tenant_persist_test.go` — absent tenant metadata on a non-genesis record (tenant-aware mode) fails `PreStart` with `ErrInvalid`
- [x] 1.4 RED `durable_state_actor_tenant_persist_test.go` — malformed/undecodable tenant metadata fails `PreStart` with `ErrInvalid`
- [x] 1.5 GREEN `durable_state_actor.go` — add `actorTenant tenancy.TenantContext` field; seed in `recoverFromStore` after the genesis branch (~line 178), before `resultingState.UnmarshalTo` (~line 185), per DS2

### Phase 2: T4-A gate extension (DS1)

- [x] 2.1 RED `durable_state_actor_tenant_persist_test.go` — command from a different tenant than the established `actorTenant` rejected with `ErrDenied` before `HandleCommand` runs and before `entity.currentState` mutates (behavior spy, zero invocations)
- [x] 2.2 RED `durable_state_actor_tenant_persist_test.go` — missing ctx tenant in tenant-aware mode rejected with `ErrMissing`
- [x] 2.3 RED `durable_state_actor_tenant_persist_test.go` — matching-tenant command proceeds unchanged (positive path)
- [x] 2.4 GREEN `durable_state_actor.go` — extend the existing T4-A gate in `processCommand` (~lines 222-227): capture `tc`, `tenancy.Require`, `VerifyUnchanged` against `actorTenant`, `establishActorTenant` on first command (DS1). `verifyTenantForPersist` (T4-B, ~line 254) stays unchanged, per DS1's rejected-alternatives table

### Phase 3: Persist writes `actorTenant` + unseeded `PostStop` skip (DS3, 4 mandatory tests)

- [x] 3.1 RED `durable_state_actor_tenant_persist_test.go` — `persistStateAndPublish` via the `processCommand` path writes exact `ego.tenant.*` keys via `MarshalMetadata(actorTenant)`; legacy mode writes none
- [x] 3.2 RED `durable_state_actor_tenant_persist_test.go` — (DS3 mandatory test a) tenant-aware + never seeded: `PostStop` does not call `persistStateAndPublish` (store call-count assertion, zero calls)
- [x] 3.3 RED `durable_state_actor_tenant_persist_test.go` — (DS3 mandatory test b) tenant-aware + seeded: `PostStop` writes with `actorTenant`'s metadata (store capture, assert `tenant_metadata` matches)
- [x] 3.4 RED `durable_state_actor_tenant_persist_test.go` — (DS3 mandatory test c) legacy mode: `PostStop` keeps today's unconditional flush unchanged (regression assertion against pre-PR2 behavior)
- [x] 3.5 RED `durable_state_actor_tenant_persist_test.go` — (DS3 mandatory test d) recovery with invalid/malformed tenant metadata fails at `PreStart` via DS2; `PostStop` is never reached (assert `PreStart` returns wrapped `ErrInvalid`, actor never becomes ready)
- [x] 3.6 GREEN `durable_state_actor.go` — `persistStateAndPublish` writes `entity.actorTenant` metadata at both call sites (`processCommand:259`, `PostStop:166`); `PostStop` skips the call when `actorTenant == noTenantContext` in tenant-aware mode; legacy mode keeps the unconditional flush (DS3)

### Phase 4: `GetStateCommand` read gate (DS4)

- [x] 4.1 RED `durable_state_actor_tenant_persist_test.go` — `GetStateCommand` from a foreign tenant rejected with `ErrDenied`
- [x] 4.2 RED `durable_state_actor_tenant_persist_test.go` — `GetStateCommand` with missing ctx tenant (tenant-aware mode) rejected with `ErrMissing`
- [x] 4.3 RED `durable_state_actor_tenant_persist_test.go` — `GetStateCommand` from the matching tenant succeeds (positive path)
- [x] 4.4 GREEN `durable_state_actor.go` — add `tenancy.Require` + `VerifyUnchanged` gate before `sendStateReply` in the `*egopb.GetStateCommand` case (~line 141), mirroring `event_sourced_actor.go:613-628` (read-only reference) per DS4

### Phase 5: Regression + verification (PR2)

- [x] 5.1 Run the existing suite in legacy mode; confirm no behavior change (`TestEngineDurableState` unmodified)
- [x] 5.2 `go mod vendor && go test -mod=vendor -p 1 -timeout 0 -race ./...` green
- [x] 5.3 `go build ./...` and `go vet ./...` clean; confirm `command/` (read-only), `tenancy/` (read-only), `event_sourced_actor.go` (read-only), `protos/` (read-only), `egopb/` (read-only) diffs are empty against `main`

---

## PR3 — Saga Tenant Binding + End-to-End Integrity

Depends on PR2 only for the durable-state e2e target (targets PR2's merged base per `design.md`'s PR Slicing table); the saga-only logic itself has no PR2 dependency. No proto change. New test files: `saga_actor_tenant_test.go`, `tenant_write_path_e2e_test.go`.

### Phase 1: Foundation — `eventContext` helper (SG2)

- [ ] 1.1 RED `saga_actor_tenant_test.go` — `eventContext` reconstructs `TenantContext` (tenant + administrative scope) from valid metadata (table test)
- [ ] 1.2 RED `saga_actor_tenant_test.go` — `eventContext` returns wrapped `ErrInvalid` on absent/malformed tenant metadata in tenant-aware mode; parent ctx unchanged
- [ ] 1.3 RED `saga_actor_tenant_test.go` — `eventContext` legacy-mode passthrough returns `(parent, nil)`
- [ ] 1.4 GREEN `saga_actor.go` — add `tenantAware bool` and `boundTenant tenancy.TenantContext` fields (`noTenantContext` until seeded); implement `eventContext(parent context.Context, event *egopb.Event) (context.Context, error)` (SG2)

### Phase 2: Bind-on-first-event + `VerifyUnchanged` (SG4)

- [ ] 2.1 RED `saga_actor_tenant_test.go` — a freshly started saga instance binds to the first valid tenant it processes (behavior spy asserting `boundTenant` set after event 1)
- [ ] 2.2 RED `saga_actor_tenant_test.go` — a second event from a different tenant, once bound, rejected with `ErrDenied` before `HandleEvent` runs; no mutation, no dispatch, no persisted event (two-tenant fixture, zero-invocation assertion)
- [ ] 2.3 RED `saga_actor_tenant_test.go` — tenant-less/malformed event at a reset site: `HandleEvent` never invoked, no dispatch, `status` unchanged, saga still consumes the next valid event
- [ ] 2.4 RED `saga_actor_tenant_test.go` — `compensate` dispatches under `boundTenant` (timeout path uses `boundTenant` directly)
- [ ] 2.5 RED `saga_actor_tenant_test.go` — unbound tenant-aware timeout (no event ever processed) fails closed: `status = SagaFailed`, zero dispatches
- [ ] 2.6 GREEN `saga_actor.go` — bind-or-verify in `handleStreamEvent`: on `eventContext` decode success, bind `boundTenant` if unseeded else `tenancy.VerifyUnchanged(tc, boundTenant)`; reject + log (saga ID, persistence ID, sequence number only) + return on `ErrInvalid`/`ErrDenied`; `compensate` reads `boundTenant` directly and fails closed if unbound (SG4)

### Phase 3: Thread ctx through reset sites; saga events carry metadata (SG1, SG3)

- [ ] 3.1 RED `saga_actor_tenant_test.go` — `persistAndApplyEvents` writes `tenant_metadata` on saga-emitted events in tenant-aware mode; legacy mode writes none (SG3)
- [ ] 3.2 RED `saga_actor_tenant_test.go` — `sendCommand` dispatches on a ctx where `tenancy.Require` succeeds with the bound tenant (entity-side probe)
- [ ] 3.3 GREEN `saga_actor.go` — thread the per-event ctx from `eventContext` through `handleStreamEvent` (~line 262) → `processAction` (~line 279) → `persistAndApplyEvents` and `sendCommand` (~line 335); each gains a `context.Context` parameter. Site 242 (`consumeEvents` → `Tell(context.Background(), ...)`) stays unchanged by design (SG1). `persistAndApplyEvents` sets `TenantMetadata: tenancy.MarshalMetadata(tc)` guarded by `tenantAware` (SG3)

### Phase 4: Replay validation against first-replayed-event `boundTenant` (SG5)

- [ ] 4.1 RED `saga_actor_tenant_test.go` — `recover()` establishes `boundTenant` from the first successfully-decoded replayed event
- [ ] 4.2 RED `saga_actor_tenant_test.go` — a later replayed event from a different tenant fails recovery closed at `PreStart` (`require.ErrorIs(..., tenancy.ErrDenied)`), mirroring `applyPersistedEvent`'s replay-path gate (read-only reference, PR1)
- [ ] 4.3 RED `saga_actor_tenant_test.go` — a replayed event with absent/undecodable tenant metadata fails `PreStart` with `ErrInvalid` (TA3, no backfill)
- [ ] 4.4 GREEN `saga_actor.go` — wire `recover()`'s per-event replay (~lines 184-194) through `eventContext` + the Phase 2 bind-or-verify logic (SG5)

### Phase 5: Real-dispatch end-to-end integrity

- [ ] 5.1 RED `tenant_write_path_e2e_test.go` — `Engine.SendCommand` (stub resolver → one tenant) → entity A persists → saga consumes → `SagaCommand` → entity B's `HandleCommand` observes the same tenant via `tenancy.From`; use `require.Eventually` + `testkit.NewEventsStore()`. Must NOT be `TestSendCommandTenantResolution` (engine_test.go:232, read-only) — that test stops short of the saga hop, per spec.md's "Real-Dispatch End-to-End Tenant Integrity" requirement
- [ ] 5.2 GREEN — no new production mechanism expected; this test proves Phases 1-4's wiring end-to-end. If it fails, fix the minimal gap it surfaces in `saga_actor.go`, not a new mechanism

### Phase 6: Regression + verification (PR3)

- [ ] 6.1 Run the existing suite in legacy mode; confirm no behavior change (`saga_test.go` (read-only), `publisher_test.go` (read-only) unmodified)
- [ ] 6.2 `go mod vendor && go test -mod=vendor -p 1 -timeout 0 -race ./...` green
- [ ] 6.3 `go build ./...` and `go vet ./...` clean; confirm `command/` (read-only), `tenancy/` (read-only), `protos/` (read-only), `egopb/` (read-only), `durable_state_actor.go` (read-only) diffs are empty against PR2's merged base

### Optional / Stretch — NOT authorized PR3 scope

- [ ] OPT.1 `replyWithState` (`*egopb.GetStateCommand` on `SagaActor`) has no read gate. `design.md`'s Open Questions records this as a PR3 follow-up *recommendation* only — not authorized scope. If the human explicitly authorizes it, add the same `tenancy.Require` + `VerifyUnchanged` shape as DS4, gated against `boundTenant`, in `saga_actor.go`. Do not implement without explicit sign-off; do not fold into Phase 1-6 above.
