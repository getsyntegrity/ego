# Tasks: Runtime tenant wiring and zero-plumbing single-tenant mode (EGO-TENANT-006)

Tracker `getsyntegrity/ego#55` · spec `specs/tenancy-runtime/spec.md` · `design.md` (7 decisions closed).

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~850–950 (5 modified files, 2 new marker files, tests across engine/actor layers) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 config/marker/validation → PR2 T4-A trust boundary → PR3 T4-B invariant + demo + regression |
| Delivery strategy | ask-on-risk (no cached value received this session) |
| Chain strategy | stacked-to-main (ratified by repository owner; PR1 `getsyntegrity/ego#55`, mirrors EGO-TENANT-001 #49/#50/#51) |

Decision needed before apply: No — resolved (stacked-to-main)
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

Rationale: five production files across two subsystems (Config/Engine vs. two actor types), each requiring RED-before-GREEN pairs for two independent gates (T4-A, T4-B) plus a dedicated leak regression — mirrors EGO-TENANT-001's 3-PR precedent in this same repo.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Config/Option + marker extension + `NewEngine` validation (Phases 1–2) | PR 1 | `go test . -run 'TenantResolver\|Ambiguous\|Extension' -v` | N/A — pure config/constructor, no actor runtime | Revert `option.go`, `internal/extensions/extensions.go`, `engine.go` `NewEngine`/`validateActorSystemExtensions` additions |
| 2 | T4-A trust boundary: `SendCommand` + 3 actor gates + saga fail-closed proof (Phase 3) | PR 2 | `go test . -run 'SendCommand\|Require\|FailClosed\|Saga' -v` | Existing `engine_test.go` in-process goakt harness (real actors) | Revert `SendCommand` resolve block and the 3 `tenancy.Require` gate call sites |
| 3 | T4-B invariant, `resetBatch` leak fix, single-tenant demo, backward-compat regression (Phases 4–6) | PR 3 | `go test . -run 'VerifyUnchanged\|ResetBatch\|SingleTenant\|Legacy' -v` | `go test -mod=vendor -p 1 -timeout 0 -race ./...` (real exec, mirrors EGO-TENANT-001 task 4.1) | Revert `batchTenant` field, T4-B gate call sites, doc comment |

## Phase 1: Config/Option Foundation (T1, T3, DP1, DP2)

- [x] 1.1 RED `option_test.go`: nil / typed-nil `WithTenantResolver` register nothing, no error (mirrors `isNilLogger`, logger.go:72).
- [x] 1.2 RED `option_test.go`: non-nil registers as effective; nil-after-non-nil does not reset it.
- [x] 1.3 GREEN `option.go`: `Config.tenantResolver`, `tenantResolverCount`; `WithTenantResolver`; `isNilResolver`.
- [x] 1.4 GREEN `option.go` `GoaktOptions()` (:105): register tenancy marker extension only when `tenantResolver != nil`.
- [x] 1.5 GREEN `internal/extensions/extensions.go`: `TenancyExtensionID` const + marker type, no resolver field.
- [x] 1.6 RED `internal/extensions/extensions_test.go`: marker `ID()` returns `TenancyExtensionID`.

## Phase 2: `NewEngine` Validation (DP2, D2)

- [x] 2.1 RED `engine_test.go`: `NewEngine` fails on 2 distinct resolvers.
- [x] 2.2 RED `engine_test.go`: same error when one resolver value is registered twice (count, not identity).
- [x] 2.3 RED `engine_test.go`: exactly one resolver succeeds; zero resolvers succeeds (legacy, no error).
- [x] 2.4 GREEN `engine.go`: `ErrAmbiguousTenantResolver` var beside `ErrActorSystemRequired` (:70); `Engine.tenantResolver`; `NewEngine` (:161) rejects `count > 1`; defensive `count > 0 && tenantResolver == nil` guard.
- [x] 2.5 GREEN `engine.go` `validateActorSystemExtensions` (:217): add `{TenancyExtensionID, cfg.tenantResolver != nil}`.

## Phase 3: T4-A Trust Boundary (AC2, AC3, T2, T4-A, T5)

- [ ] 3.1 RED `engine_test.go`: `SendCommand` resolves once, attaches, handler observes via `tenancy.From` (resolver call-count spy).
- [ ] 3.2 RED `engine_test.go`: resolver error rejects the command before the actor system; zero writes.
- [ ] 3.3 GREEN `engine.go` `SendCommand` (:724): resolve + `tenancy.Attach` before `ref.noSender.SendSync` (:757), guarded by `tenantResolver != nil`.
- [ ] 3.4 GREEN `event_sourced_actor.go`: `tenantAware` field, populated from the marker at actor start.
- [ ] 3.5 RED `event_sourced_actor_test.go`: non-batched — missing `TenantContext` ⇒ `HandleCommand` never invoked, zero writes.
- [ ] 3.6 GREEN `event_sourced_actor.go` `processCommandAndReply` (:505): `tenancy.Require` gate before `HandleCommand` (:527).
- [ ] 3.7 RED `event_sourced_actor_test.go`: batched — same fail-closed; `flushBatch`'s `context.Background()` never reached.
- [ ] 3.8 GREEN `event_sourced_actor.go` `processAndBatch` (:752): same gate before `HandleCommand` (:772).
- [ ] 3.9 GREEN `durable_state_actor.go`: `tenantAware` field, populated at actor start.
- [ ] 3.10 RED `durable_state_actor_test.go`: missing `TenantContext` ⇒ `HandleCommand` never invoked, zero writes.
- [ ] 3.11 GREEN `durable_state_actor.go` `processCommand` (:172): same gate before `HandleCommand` (:194).
- [ ] 3.12 RED `saga_actor_test.go` (reads `saga_actor.go` (read-only)): saga-dispatched command in tenant-aware mode fails closed at the actor gate — documents the known #54 limitation, not ignored.

## Phase 4: T4-B Defensive Invariant (T4-B, D4)

- [ ] 4.1 RED `event_sourced_actor_test.go`: non-batched write path confirms tenant without re-invoking `Resolve` (call-count == 1 across accept+persist).
- [ ] 4.2 GREEN `event_sourced_actor.go` `processCommandAndReply`: gate after `buildEnvelopes` (:538), before `persistEvents` (:544).
- [ ] 4.3 RED `event_sourced_actor_test.go`: mixed-tenant batch rejected at append; `Resolve` never re-called.
- [ ] 4.4 GREEN `event_sourced_actor.go`: `batchTenant` field; record on first buffered entry, `tenancy.VerifyUnchanged` on later ones, before `batchBuffer` append (:814).
- [ ] 4.5 RED `event_sourced_actor_test.go`: explicit leak case — two sequential flush cycles; second flush's tenant wrongly compared to first flush's stale `batchTenant` — must fail before the fix.
- [ ] 4.6 GREEN `event_sourced_actor.go` `resetBatch` (:1031): clear `batchTenant`; 4.5 passes.
- [ ] 4.7 RED `durable_state_actor_test.go`: write path confirms tenant without re-invoking `Resolve`.
- [ ] 4.8 GREEN `durable_state_actor.go`: gate before `persistStateAndPublish` (:212).
- [ ] 4.9 GREEN `durable_state_actor.go` `PostStop` (:142): comment documenting the explicit T4-B exclusion (lifecycle flush, already passed T4-A); no gate added.

## Phase 5: Single-Tenant Demo & Backward Compat (AC4, AC5, T6, D6, D7)

- [ ] 5.1 Test (new or `engine_test.go`): `WithSingleTenant(id)` as sole resolver — commands succeed with zero manual `tenancy.Attach`/`Require` in application code.
- [ ] 5.2 Test: swap `WithSingleTenant` for a multi-tenant resolver — both traverse the identical resolve-attach-gate sequence.
- [ ] 5.3 Regression: run `engine_test.go`, `event_sourced_actor_test.go`, `durable_state_actor_test.go` (read-only, unmodified) with no resolver configured — zero behavior change.
- [ ] 5.4 GREEN `option.go`: doc comment on `WithTenantResolver` — nil semantics, ordering-independent security boundary, saga limitation until #54.

## Phase 6: Verification

- [ ] 6.1 Run `go test -mod=vendor -p 1 -timeout 0 -race ./...`; record result per EGO-TENANT-001 task-4.1 evidence convention.
- [ ] 6.2 Run `go vet ./...`; confirm `saga_actor.go`, `tenancy/` (read-only) byte-identical.
