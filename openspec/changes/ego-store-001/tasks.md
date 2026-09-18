# Tasks — Persistence store contract, canonical form (EGO-STORE-001)

Tracker `#70`, epic `#13`. Ratification/spec work only — no production Go code in this change. `sdd-apply` for this change writes/adjusts `spec.md`, `design.md`, and (T3) at most one narrowly-scoped conformance test if the existing evidence turns out not to cover the store layer directly.

## T1 — Formalize canonical persistence contracts

- [ ] 1.1 `spec.md` R1: `EventsStore`, `StateStore`, `SnapshotStore` documented as canonical, runtime-neutral, adapter-neutral persistence ports, with file:line citations to the current interfaces.
- [ ] 1.2 `spec.md` R2: `WritePrecondition`/`*ConflictError` documented as shared correctness primitives, cited by reference to WRITE-004 (#65) design.md, not restated as new requirements.

## T2 — Formalize existing read/lifecycle capabilities

- [ ] 2.1 `spec.md` R3: forward-read contract (`ReplayEvents`/`GetLatestEvent`/`PersistenceIDs`/`GetShardEvents`/`ShardOffsets`) ratified with citations.
- [ ] 2.2 `spec.md` R3 scenario: explicit non-goal statement that no backward-read method exists or is required, citing the absence of any consumer found during exploration.
- [ ] 2.3 `spec.md` R4: lifecycle (`Connect`/`Disconnect`/`Ping`) ratified as already part of both store interfaces — no new capability interface introduced.

## T3 — Conformance evidence for single-mutation atomicity

- [ ] 3.1 `spec.md` R5: atomicity boundary stated precisely per design.md D4 (single-call/single-`persistence_id` only; explicitly not multi-event batch atomicity, which is WRITE-006's).
- [ ] 3.2 Verify `TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit` and `TestDurableStateConcurrentGenesisWritersYieldExactlyOneCommit` still pass under `-race` on this branch's HEAD and still exercise two independent `Engine` instances against one shared `testkit` store (re-run, don't just cite from memory).
- [ ] 3.3 If 3.2 finds the existing evidence proves the guarantee only through an actor, not directly against `testkit.EventsStore`/`testkit.StateStore`, add one narrowly-scoped test directly against the store (still test-only, no production code) and cite it instead/in addition. Otherwise, cite the existing tests as-is and skip this sub-task.

## T4 — Document opaque persistence identity / tenant boundary

- [ ] 4.1 `spec.md` R6: `persistence_id` documented as an opaque, caller-assigned `string` with no structural tenant awareness, citing `event_sourced_actor.go`/`durable_state_actor.go` call sites that pass it through unchanged.
- [ ] 4.2 `spec.md` R7: `tenant_metadata` documented as transported (proto field, `tenancy.MarshalMetadata`/`UnmarshalMetadata`) but never consulted by any `WritePrecondition`/CAS path — cite `tenancy.VerifyUnchanged` as the only (after-the-fact, non-preventive) existing safeguard.
- [ ] 4.3 `spec.md` R8: explicit statement that resolving this gap (composite key, namespace, or other mechanism) is TENANT-003's design decision, not pre-decided here.

## T5 — Validate no scope leakage into WRITE-005/006/TENANT-003

- [ ] 5.1 Re-read `spec.md` end-to-end and confirm no requirement redefines `WritePrecondition`/`ConflictError`/CAS ownership.
- [ ] 5.2 Confirm no requirement mentions `operation_id` persistence, idempotency, deduplication, or retry identity (WRITE-005 territory).
- [ ] 5.3 Confirm no requirement claims or implies multi-event/batch all-or-nothing atomicity on a real adapter (WRITE-006 territory).
- [ ] 5.4 Confirm no requirement invents a tenant-scoped ID scheme, namespace, or prefixing convention (TENANT-003 territory).
- [ ] 5.5 Confirm no requirement proposes merging `EventsStore`+`StateStore` or any other mega-interface.
- [ ] 5.6 Confirm `spec.md` line count is within the 300-line hard ceiling (target ≤250).
- [ ] 5.7 Confirm every requirement traces to existing code, an existing test, or a new test named in T3.3 — no requirement without a consumer or evidence.

## T6 — Verify and reconcile #70

- [ ] 6.1 Confirm issue #70's reconciled body (updated 2026-09-17) still matches this proposal's MUST/MUST NOT — no further edit needed unless a real discrepancy is found during T5.
- [ ] 6.2 Note in the PR description that this proposal formalizes #70 without closing it; closure/archival is a separate, later, explicitly-authorized step (mirrors the WRITE-004 pattern: `sdd-apply` → `sdd-verify` → explicit close).
