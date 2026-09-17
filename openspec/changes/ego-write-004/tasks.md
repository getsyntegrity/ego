# Tasks — Expected revision and optimistic concurrency (EGO-WRITE-004)

Executable work only. Rationale lives in `design.md` (cited by decision ID);
requirements/scenarios live in `specs/*/spec.md`. No Go code is written by
this phase — `sdd-apply` implements these tasks later.

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | ~250 (PR1) / ~450 (PR2, breaking SPI + testkit CAS + mock regen + 5 call sites) / ~200 (PR3) / ~150 (PR4) / ~200 (PR5) |
| 400-line budget risk | High (PR2 only) |
| Chained PRs recommended | Yes |
| Suggested split | PR1 → PR2 → PR3 → PR4 → PR5 (feature-branch-chain) |
| Delivery strategy | ask-on-risk |
| Chain strategy | feature-branch-chain |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | PR | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 1 | `command-envelope` delta: metadata, carrier, failure code | PR1 (base = tracker branch) | `go test ./command/...` | N/A — pure unit/contract tests, no runtime actor | Revert `command/` files; additive only |
| 2 | `persistence-concurrency`: SPI break, precondition/conflict types, testkit CAS | PR2 (base = PR1 branch) | `go test -race ./persistence/... ./testkit/...` | `go test -race -run TestConcurrent ./testkit/...` (real goroutines vs store) | Revert `persistence/`, `testkit/`, mocks, 5 call sites |
| 3 | `event-sourced-concurrency` integration | PR3 (base = PR2 branch) | `go test -race -run EventSourced ./...` | Integration test with real `EventSourcedActor` + testkit `EventsStore` | Revert `event_sourced_actor.go`, `events_writer_actor.go` |
| 4 | `durable-state-concurrency` integration | PR4 (base = PR3 branch) | `go test -race -run DurableState ./...` | Integration test with real `DurableStateActor` + testkit `StateStore` | Revert `durable_state_actor.go` |
| 5 | E2E/compat/docs, full AC reconciliation | PR5 (base = PR4 branch) | `go test -race ./...` | Full-suite integration run, both actor types | Revert new e2e test files + migration doc section; no prior-PR code touched |

## PR1 — `command-envelope` delta (capability: `command-envelope`)

- [ ] 1.1 Add `expectedRevision uint64` / `hasExpectedRevision bool` fields to `command/metadata.go`; per D5. Req: Expected Revision Metadata Field (AC1).
- [ ] 1.2 Add `WithExpectedRevision(revision uint64) MetadataOption` to `command/metadata.go`. Req: Expected Revision Metadata Field (AC1).
- [ ] 1.3 Add `(m Metadata) ExpectedRevision() (uint64, bool)` accessor to `command/metadata.go`. Req: Expected Revision Metadata Field (AC1).
- [ ] 1.4 Confirm `Metadata.Derive` does NOT copy `expectedRevision`/`hasExpectedRevision` (per D5, WRITE-003 D7 fail-explicit rule) in `command/metadata.go`. Req: Legacy Caller Compatibility (AC8).
- [ ] 1.5 Add carrier key literal `ego.cmd.expected_revision` to `command/carrier.go`; add `"expected_revision"` to `reservedCustomKeys` per D5. Req: Expected Revision Carrier Propagation (AC1).
- [ ] 1.6 `MarshalMetadata` emits the key only when present (`strconv.FormatUint`); `UnmarshalMetadata` rejects a malformed value with `ErrInvalidMetadata` and never defaults to `0` when absent, in `command/carrier.go`. Req: Expected Revision Carrier Propagation (AC1); Legacy Caller Compatibility (AC8).
- [ ] 1.7 Add `const CodeConcurrencyConflict = "concurrency_conflict"` to `command/result.go` per D6. Req: Concurrency Conflict Failure Code (AC5).
- [ ] 1.8 Test: presence absent by default / present at `N` incl. `N=0`, `command/metadata_test.go`. Req: Expected Revision Metadata Field (AC1). T5 (contract half).
- [ ] 1.9 Test: genesis (`0`) vs absence are distinct states, never conflated by accessor/serialization, `command/metadata_test.go`. Req: Expected Revision Semantics (AC2).
- [ ] 1.10 Test: `N>0` round-trips unrounded/unclamped through the accessor, `command/metadata_test.go`. Req: Expected Revision Semantics (AC2).
- [ ] 1.11 Test: `Carrier` round-trip present-at-`N` and absent-stays-absent (no key emitted), `command/carrier_test.go`. Req: Expected Revision Carrier Propagation (AC1); Legacy Caller Compatibility (AC8).
- [ ] 1.12 Test: `Envelope` construction path preserves absent-never-becomes-`0`, `command/envelope_test.go`. Req: Legacy Caller Compatibility (AC8). T5.
- [ ] 1.13 Test: pre-existing caller unaffected — command built without `WithExpectedRevision` is byte-identical to today across `Metadata`/`Envelope`, `command/envelope_test.go`. Req: Legacy Caller Compatibility (AC8). T5.
- [ ] 1.14 Test: `Failure.Code()=="concurrency_conflict"` checkable without string inspection via `NewRejected`+`WithFailureCode`, `command/result_test.go`. Req: Concurrency Conflict Failure Code (AC5).
- [ ] 1.15 Test: `Outcome` type still declares exactly six kinds after this delta, `command/result_test.go`. Req: Concurrency Conflict Failure Code (AC5) — "No seventh outcome" scenario.

## PR2 — `persistence-concurrency` (capability: `persistence-concurrency`)

- [x] 2.1 Create `persistence/precondition.go`: `WritePrecondition` struct (unexported fields, zero value invalid) per D1; constructors `Unconditional()`, `ExpectGenesis()`, `ExpectRevision(revision uint64)`; accessors `IsUnconditional()`, `IsGenesis()`, `Revision() (uint64, bool)`, `Valid() bool`. Req: Explicit Write Precondition Type (AC3, AC4).
- [x] 2.2 Create `persistence/conflict.go`: `ConflictError` type, `NewConflictError`, `WithActualRevision` option, `PersistenceID()`/`Expected()`/`ActualRevision()`/`Is()`/`Error()`, sentinels `ErrConcurrencyConflict`/`ErrInvalidPrecondition`/`ErrPreconditionScope`, and `ParseConflictError` (exact inverse of `Error()`, canonical grammar per D7). Req: Atomic Conditional Write — EventsStore/StateStore (AC3, AC4).
- [x] 2.3 Modify `persistence/events_store.go`: `WriteEvents(ctx, events, precondition WritePrecondition) error`; document atomic no-interleave contract; enforce single-`PersistenceId`-per-batch scope (`ErrPreconditionScope`, incl. empty slice) per D2. Req: Atomic Conditional Write — EventsStore (AC3).
- [x] 2.4 Modify `persistence/state_store.go`: `WriteState(ctx, state, precondition WritePrecondition) error`; same atomicity contract. Req: Atomic Conditional Write — StateStore (AC4).
- [x] 2.5 Restructure `testkit/eventstore.go`: re-key `db` to `persistenceID → *eventLog{revision, events}` (immutable per-cell), implement conditional write via `CompareAndSwap`/`LoadOrStore` per D11; mark `EventKey` `// Deprecated:`, unused; fix `DeleteEvents` to truncate `events` while preserving `revision` (retention trap, D11). Req: Real Compare-and-Swap in testkit Stores (AC9).
- [x] 2.6 Restructure `testkit/durablestore.go` with the same CAS/`LoadOrStore` primitives over its `pid → *egopb.DurableState` map. Req: Real Compare-and-Swap in testkit Stores (AC10).
- [x] 2.7 Update production call sites to the new signatures: `events_writer_actor.go:97`, `saga_actor.go:574`, `saga_actor.go:611` (both pass `persistence.Unconditional()` per D12 — mechanical, no behavior change), `durable_state_actor.go:534`, `durable_state_actor.go:573`. Req: Atomic Conditional Write — EventsStore/StateStore (AC3, AC4); D12.
- [x] 2.8 Regenerate `mocks/persistence/events_store.go` and `mocks/persistence/state_store.go` via mockery for the new signatures. Req: Atomic Conditional Write — EventsStore/StateStore (AC3, AC4).
- [x] 2.9 Update the 72 test/benchmark call sites across 12 files (per design.md M-1 inventory) to compile against the new signatures. Req: Atomic Conditional Write — EventsStore/StateStore (AC3, AC4).
- [x] 2.10 Test: precondition zero value is invalid, rejected with `ErrInvalidPrecondition`; genesis and exact-revision(0) are distinguishable, never same sentinel; unconditional reachable only via its constructor, `persistence/precondition_test.go`. Req: Explicit Write Precondition Type (AC3, AC4).
- [x] 2.11 Test: `ConflictError.Error()`/`ParseConflictError` round-trip (property test) per canonical grammar, `persistence/conflict_test.go`. Req: Atomic Conditional Write — EventsStore/StateStore (AC3, AC4); D7.
- [x] 2.12 Test: `WriteEvents` with matching exact-revision(N) commits and advances the persisted revision, `testkit/eventstore_test.go`. Req: Atomic Conditional Write — EventsStore. T1.
- [x] 2.13 Test: `WriteEvents` with stale exact-revision(N-1) commits nothing and returns a conflict identifiable via `errors.As`, `testkit/eventstore_test.go`. Req: Atomic Conditional Write — EventsStore — "Stale exact-revision precondition is rejected atomically". T2.
- [x] 2.14 Test: `WriteState` with matching exact-revision(N) commits and becomes the persisted `VersionNumber`, `testkit/durablestore_test.go`. Req: Atomic Conditional Write — StateStore. T1.
- [x] 2.15 Test: `WriteState` with stale exact-revision(N-1) commits nothing and returns a conflict identifiable by type, `testkit/durablestore_test.go`. Req: Atomic Conditional Write — StateStore. T2.
- [x] 2.16 Test: single-writer genesis precondition against a nonexistent persistence ID succeeds (`WriteEvents` and `WriteState`), `testkit/eventstore_test.go`, `testkit/durablestore_test.go`. Req: Real Compare-and-Swap in testkit Stores. T3.
- [x] 2.17 Test: genesis precondition against an already-committed persistence ID is rejected with a conflict (`WriteEvents` and `WriteState`), `testkit/eventstore_test.go`, `testkit/durablestore_test.go`. Req: Real Compare-and-Swap in testkit Stores. T4.
- [x] 2.18 RED test: two goroutines racing `WriteEvents` with no shared lock — assert the store's own CAS decides the winner, not caller ordering, `-race`, `testkit/eventstore_test.go`. Req: Real Compare-and-Swap in testkit Stores (AC9).
- [x] 2.19 GREEN: verify 2.18 passes against the 2.5 implementation.
- [x] 2.20 RED/GREEN test (architectural gate): two independent goroutine writers, same `ExpectedRevision=N`, concurrent `WriteEvents` against one persistenceID → exactly 1 success + 1 conflict, `-race`, `testkit/eventstore_test.go`. Req: Two Independent EventStore Writers, Same Expected Revision (AC9). T8, T11 (EventStore half — writers call `WriteEvents` directly, no actor/mailbox in the path, so this is also the proof the guarantee is persistence-owned).
- [x] 2.21 RED/GREEN test (architectural gate): same as 2.20 for `WriteState`, `testkit/durablestore_test.go`. Req: Two Independent StateStore Writers, Same Expected Revision (AC10). T9, T11 (StateStore half — same rationale as 2.20).
- [x] 2.22 RED/GREEN test (architectural gate): two independent goroutine writers, both `ExpectGenesis()`, concurrent against a nonexistent persistenceID (`WriteEvents` and `WriteState`) → exactly 1 commit + 1 conflict, `-race`, `testkit/eventstore_test.go`, `testkit/durablestore_test.go`. Req: Concurrent Genesis Writers (AC11). T10.
- [x] 2.23 Test: `checkPreconditions` passing does not by itself prevent a `StateStore`-level conflict when a competing writer already advanced `VersionNumber` — construct that exact race, `durable_state_actor_test.go` or `testkit/durablestore_test.go`. Req: `checkPreconditions` Keeps Its Distinct, Narrower Responsibility.
- [x] 2.24 Document the Revision Model Mapping table (`ExpectedRevision`/`CurrentRevision`/`StorageRevision` for `EventsStore` and `StateStore`) verbatim per design.md D4, in code comments on `persistence/precondition.go` and/or package doc. Req: Revision Model Mapping for Current Adapters (AC3, AC4).
- [x] 2.25 Confirm `design.md` (read-only)'s M-1..M-4 Migration/Rollout table (already drafted) matches the landed PR2 signatures; reference that table from the PR2 description per M-4. Req: Breaking-Change Declaration and Migration Path (AC12).
- [x] 2.26 Test: an implementation written against the pre-WRITE-004 signature fails to compile against the new interface (compile-time check, e.g. a `_ EventsStore = (*oldImpl)(nil)` assertion left deliberately failing in a scratch/CI-only file, or documented as a manual verification step). Req: Breaking-Change Declaration and Migration Path (AC12) — "External implementer fails at compile time" scenario.

## PR3 — `event-sourced-concurrency` (consumes PR1+PR2 contracts)

- [ ] 3.1 In `event_sourced_actor.go`, extract `ExpectedRevision` from command metadata after `dispatchToBehavior` returns; resolve absent → `Unconditional()`, per D4/D9 step 1. Req: ExpectedRevision Extracted for the Persist Request Only (AC6).
- [ ] 3.2 Confirm `HandleCommand`/`HandleEnvelope` argument lists are untouched (no precondition parameter added). Req: ExpectedRevision Extracted for the Persist Request Only (AC6).
- [ ] 3.3 Add the precondition field to the direct-path persist request built by `persistAsync`. Req: Persist Request Carries the Precondition (AC6).
- [ ] 3.4 Implement `flushBatch` precondition resolution per D9 steps 1–4: `batchBase` anchor, admission gate (`E == batchCounter`), resolution to `Unconditional()`/`ExpectGenesis()`/`ExpectRevision(batchBase)`, forced flush on inadmissible command. Req: Persist Request Carries the Precondition (AC6); flushBatch merge policy per D9.
- [ ] 3.5 In `events_writer_actor.go`'s `handlePersistEvents`, pass the request's precondition into the `EventsStore.WriteEvents` call instead of `Unconditional()` by default. Req: eventsWriterActor Delegates to the Conditional Write (AC6).
- [ ] 3.6 In `event_sourced_actor.go`, gate `applyConfirmedState`/`eventsCounter`/`batchCounter` advancement strictly on the store-confirmed write result — no advancement on conflict. Req: Store Result Is the Sole Commit-Success Authority (AC6).
- [ ] 3.7 Create `reply_classification.go`: ordered classifier registry per D8 (`errActorContextCanceled`→`NewCanceled`, `errActorDeadlineExceeded`→`NewTimedOut`, `persistence.ErrConcurrencyConflict`→`NewRejected`+`concurrency_conflict`, default→`NewFailed`); `strings.HasPrefix` matching, first match wins; conflict entry runs `ParseConflictError` and attaches via `WithFailureCause`. Update `engine.go:1365-1370`'s `resultFromReply` to delegate to it — moved forward from PR5 task 5.1, since PR3 is the classifier's first real consumer and cannot demonstrate its own AC without it; PR4 task 4.6 reuses it, PR5 reconciles/tests it further, neither recreates it. Wire `EventSourcedActor`'s error path so a `*persistence.ConflictError` reaches `sendErrorReply` unwrapped and is classified as `OutcomeRejected`/`concurrency_conflict`. Req: Conflict Surfaces as OutcomeRejected/concurrency_conflict (AC6).
- [ ] 3.8 Implement D10 post-conflict lifecycle for `EventSourcedActor`: if `ConflictError.ActualRevision()` is known and equals `eventsCounter`, reply and stay alive; otherwise reply then set `directShutdown`/`shutdownOnDrain` for supervisor-driven `recover()`. Req: Store Result Is the Sole Commit-Success Authority (AC6); D10.
- [ ] 3.9 Test: handler's received arguments contain no `ExpectedRevision`, `event_sourced_actor_test.go`. Req: ExpectedRevision Extracted for the Persist Request Only.
- [ ] 3.10 Test: legacy command with no `ExpectedRevision` extracts as `Unconditional()`, `event_sourced_actor_test.go`. Req: ExpectedRevision Extracted for the Persist Request Only.
- [ ] 3.11 Test: direct-path request's precondition equals `N`, `event_sourced_actor_test.go`. Req: Persist Request Carries the Precondition.
- [ ] 3.12 Test: batched flush's persist request carries an explicit, non-substituted precondition per D9's admission/resolution rules, `event_sourced_actor_test.go`. Req: Persist Request Carries the Precondition.
- [ ] 3.13 Test: `eventsWriterActor` issues the store call with precondition `N`, verified on the call itself, `events_writer_actor_test.go`. Req: eventsWriterActor Delegates to the Conditional Write.
- [ ] 3.14 Test: on conflict, `currentState`/`eventsCounter` (and `batchState`/`batchCounter`) stay at pre-write values, `event_sourced_actor_test.go`. Req: Store Result Is the Sole Commit-Success Authority.
- [ ] 3.15 Test: on success, confirmed state/counter come from the store-confirmed write, not a pre-reply local increment, `event_sourced_actor_test.go`. Req: Store Result Is the Sole Commit-Success Authority.
- [ ] 3.16 Test: caller receives `OutcomeRejected`/`Failure.Code=="concurrency_conflict"` on conflict, and a distinct code on an unrelated store I/O error, `event_sourced_actor_test.go`. Req: Conflict Surfaces as OutcomeRejected/concurrency_conflict.
- [ ] 3.17 Integration test (real actor + real testkit `EventsStore`, not a call-recording mock): exact-revision command commits and advances the store; stale-revision command is rejected and the store is unchanged; two concurrent `ExpectedRevision=0` commands yield exactly one committed genesis and one conflict, `event_sourced_actor_integration_test.go`. Req: End-to-End Propagation Proof (AC6, T6).

## PR4 — `durable-state-concurrency` (consumes PR1+PR2 contracts, independent of PR3)

- [ ] 4.1 In `durable_state_actor.go`, extract `ExpectedRevision` from command metadata after `dispatchToBehavior` returns; do not mutate `priorVersion`/`priorState`. Req: ExpectedRevision Is Read as a Write Precondition, Not Domain Input (AC7).
- [ ] 4.2 Confirm `HandleCommand`/`HandleEnvelope` still receive the same `(ctx, cmd, priorVersion, priorState)` shape. Req: ExpectedRevision Is Read as a Write Precondition, Not Domain Input (AC7).
- [ ] 4.3 Translate the extracted value into `WritePrecondition` (absent→`Unconditional()`, `0`→`ExpectGenesis()`, `N>0`→`ExpectRevision(N)`) and pass it into `commitState`'s `StateStore.WriteState` call at `durable_state_actor.go:534`/`:573`; remove any read-compare-write against `entity.currentVersion` used as a commit-decision substitute. Req: Conditional Write Delegates the Compare-and-Commit to Persistence, Including Genesis (AC7).
- [ ] 4.4 On conflict, skip the commit step's in-memory mutation (`currentState`, `currentVersion`, `actorTenant`, cached marshal) and skip publish. Req: Conflict Surfaces as OutcomeRejected/concurrency_conflict (AC7).
- [ ] 4.5 Confirm `checkPreconditions` (`durable_state_actor.go:461`) is unmodified and its failure path stays distinct from the new `ConflictError` path — different `Failure.Code()`. Req: `checkPreconditions`/`priorVersion` Stays a Separate, Unmodified Concern.
- [ ] 4.6 Map a `*persistence.ConflictError` from `commitState` to `OutcomeRejected`/`concurrency_conflict` by reusing the classifier registry created in PR3 task 3.7 (`reply_classification.go`) — do not recreate it. Req: Conflict Surfaces as OutcomeRejected/concurrency_conflict (AC7).
- [ ] 4.7 Implement D10 post-conflict handling for `DurableStateActor` (no shutdown path): re-run `recoverFromStore` before the next command when actual revision is unknown or differs from `currentVersion`. Req: Conflict Surfaces as OutcomeRejected/concurrency_conflict (AC7); D10.
- [ ] 4.8 Test: handler receives the unchanged 4-arg shape regardless of `ExpectedRevision`, `durable_state_actor_test.go`. Req: ExpectedRevision Is Read as a Write Precondition, Not Domain Input.
- [ ] 4.9 Test: `N` reaches the conditional write step and never appears as a handler argument, `durable_state_actor_test.go`. Req: ExpectedRevision Is Read as a Write Precondition, Not Domain Input.
- [ ] 4.10 Test: exact-match precondition commits, new state at N+1 durably committed, `durable_state_actor_test.go`. Req: Conditional Write Delegates the Compare-and-Commit to Persistence, Including Genesis.
- [ ] 4.11 Test: stale precondition rejected at the store even though the actor's own `currentVersion` still reads N (simulate another writer advancing storage the local actor never observed), `durable_state_actor_test.go`. Req: Conditional Write Delegates the Compare-and-Commit to Persistence, Including Genesis.
- [ ] 4.12 Test: two independent `DurableStateActor` instances, both `ExpectedRevision=0` against one persistence ID with no prior record → exactly one commit, one `concurrency_conflict`, `-race`, `durable_state_actor_test.go`. Req: Conditional Write Delegates the Compare-and-Commit to Persistence, Including Genesis (genesis scenario).
- [ ] 4.13 Test: handler returns a version adjacent to `currentVersion` (passes `checkPreconditions`) yet `ExpectedRevision` mismatches → command still fails with `concurrency_conflict`, `durable_state_actor_test.go`. Req: `checkPreconditions`/`priorVersion` Stays a Separate, Unmodified Concern.
- [ ] 4.14 Test: a non-adjacent-version `checkPreconditions` failure never reports `Failure.Code()=="concurrency_conflict"`, `durable_state_actor_test.go`. Req: `checkPreconditions`/`priorVersion` Stays a Separate, Unmodified Concern.
- [ ] 4.15 Test: on conflict, actor's in-memory state/version unchanged, no state published, `durable_state_actor_test.go`. Req: Conflict Surfaces as OutcomeRejected/concurrency_conflict.
- [ ] 4.16 Test: caller inspects `command.Result` on conflict → `OutcomeRejected` + `Failure.Code()==("concurrency_conflict", true)`, `durable_state_actor_test.go`. Req: Conflict Surfaces as OutcomeRejected/concurrency_conflict.
- [ ] 4.17 Test: actor `currentVersion` behind real `StorageRevision` (another process committed) → conditional write evaluated against `StorageRevision`, rejects, `durable_state_actor_test.go`. Req: Revision Model Mapping for DurableStateActor/StateStore.
- [ ] 4.18 Integration test (real actor + real testkit `StateStore`): two commands identical except `ExpectedRevision` (one matches, one doesn't) against the same real store → matching commits and advances revision, non-matching rejected with `concurrency_conflict` and storage unchanged, `durable_state_actor_integration_test.go`. Req: End-to-End Propagation Proof (T7).

## PR5 — E2E, compatibility, docs, AC reconciliation (distributes across PR1/PR3/PR4, not a capability)

- SUPERSEDED — 5.1 no longer creates `reply_classification.go`: the D8 classifier registry was moved forward and created in PR3 task 3.7 (its first real consumer), then reused as-is by PR4 task 4.6. PR5 does not recreate it. Governance note: tasks.md's original PR ordering had 3.7 depending backward on a not-yet-created PR5 task; this was caught as a genuine cross-PR scope inconsistency during PR3 and corrected by moving creation to PR3. See 5.2–5.4 below for the registry's remaining PR5-owned tests and 5.6 for full AC reconciliation.
- [ ] 5.2 Test: registry invariant — no sentinel's error text is a prefix of another's, `reply_classification_test.go`. Req: D8 registry invariant (supports AC6/AC7 conflict classification).
- [ ] 5.3 Test: a `*ConflictError` reaching `sendErrorReply` unwrapped classifies as `concurrency_conflict`; a prefix-wrapped one degrades to `OutcomeFailed` (documents the D7 invariant, doesn't weaken it), `reply_classification_test.go`. Req: D7 unwrapped-error invariant.
- [ ] 5.4 Test: `errors.As(result.Err(), &ce)` recovers expected/actual revisions through `command/errors.go:93`'s `Unwrap`, `reply_classification_test.go`. Req: D8 reconciliation.
- [ ] 5.5 E2E test: legacy command (no `ExpectedRevision`) sent through the real `EventSourcedActor`+`EventsStore` and real `DurableStateActor`+`StateStore` paths behaves unconditionally end-to-end — no `concurrency_conflict` ever produced, `e2e_legacy_compat_test.go`. Req: Legacy Caller Compatibility (AC8). T5 (e2e half).
- [ ] 5.6 Reconcile AC1–AC12 against #65: confirm each AC's owning requirement(s) per the proposal.md AC-placement table are implemented and tested (do not edit issue #65 — that is a separate, later step per proposal.md scope). Req: AC1–AC12 full reconciliation.
- [ ] 5.7 Verify `design.md` (read-only)'s M-1..M-4 Migration/Rollout table is accurate to the landed PR2 diff (method signatures, call-site count, mock regen) and referenced from the PR2 release notes per M-4. Req: Breaking-Change Declaration and Migration Path (AC12).
- [ ] 5.8 Run full suite `go test -race ./...` across all four capabilities together to confirm no cross-capability regression before closing #65. Req: cross-cutting, all ACs.
