# Proposal — Tenant-aware write path and aggregate identity (EGO-TENANT-002)

| Field | Value |
|---|---|
| Change | `ego-tenant-002` |
| Date | 2026-09-16 |
| Phase | `sdd-propose` — proposed |
| Tracker | [`#54`](https://github.com/getsyntegrity/ego/issues/54), epic [`#23`](https://github.com/getsyntegrity/ego/issues/23) |
| Depends on | [`#45`](https://github.com/getsyntegrity/ego/issues/45) (`tenancy-core`, shipped), [`#59`](https://github.com/getsyntegrity/ego/issues/59) (`command-envelope`, shipped) |
| Sibling | [`#55`](https://github.com/getsyntegrity/ego/issues/55) (`tenancy-runtime`, shipped) — deferred this gap here, not an ancestor |
| Evidence base | Engram `sdd/explore/ego-tenant-002`, reconciled against merged code |
| **Status** | **PR1 of this change is already merged** — [`#73`](https://github.com/getsyntegrity/ego/pull/73), branch `feat/54-tenant-002-pr1-proto-es-actor`, commits `2a4a465`/`f7b99a6`/`39c55fc`, merge `ddf9337` on `main`. Covers proto (all three messages) + `event_sourced_actor.go`. `durable_state_actor.go` and `saga_actor.go` remain untouched. |

This proposal was written by an executor with no shell against a stale local checkout (4 commits behind `origin/main`) and, on first pass, mistook the real, already-merged PR1 work for a stale/fabricated Engram cycle. Corrected after fast-forwarding local `main` to `ddf9337` and independently verifying `#73` on GitHub plus reading the merged diff directly: proto field tags (`Event`=10, `Snapshot`=7, `DurableState`=7), `actorTenant` actor-lifetime binding, fail-closed `seedActorTenant`/`applyPersistedEvent` checks, and the two read-path/replay tenant-isolation fixes in `39c55fc` (`getStateAndReply`, `applyPersistedEvent`) all match TA1–TA8 below with no contradiction. #54 and #75 re-checked live and remain consistent with this proposal's scope.

## Problem

`tenancy-core` ratified "Resolve-Once, Propagate-After Discipline" and "Tenant Plus Aggregate Effective Identity" with no consumer. #55 wired tenant through ctx up to command acceptance, then documented its own gap: "a tenant-aware app with sagas fails closed until #54 lands." Tenant dies at every durable/async boundary — `saga_actor.go` resets to `context.Background()` at 5 sites (242, 262, 279, 335, 376), `event_sourced_actor.go`'s `batchTenant` is cleared at `resetBatch` (1184) so it never survives `recover()`, and `durable_state_actor.go` has no tenant-switch check at all.

**Success**: tenant is unforgeable data on the write path, enforced through real dispatch including a saga hop and an actor restart.

**PR1 (`#73`) already closed the event-sourced half of this gap**: `batchTenant` widened to actor-lifetime `actorTenant`, seeded fail-closed at `recover()`/`recoverFromSnapshot()`, enforced in both the batched and non-batched command paths, and — per human review before merge — also in the read path (`getStateAndReply`) and per-event replay (`applyPersistedEvent`), which the original design/tasks breakdown had missed. The remaining gap is exactly `durable_state_actor.go` (no tenant-switch check at all) and `saga_actor.go` (5 `context.Background()` reset sites).

## Decisions (closed here)

| # | Decision | Rationale |
|---|---|---|
| TA1 | Tenant travels as **data on the persisted record**, never via `context.Context` | Events reach the saga via in-process `Tell(context.Background(), ...)`; the originating ctx is structurally gone (explore) |
| TA2 | Additive tenant metadata on **three** messages: `egopb.Event`, `egopb.Snapshot`, `egopb.DurableState` | Refines explore's two-message read: with `DeleteEventsOnSnapshot`, `recoverFromSnapshot` is the only survivor, so a tenant-less `Snapshot` makes AC3 unprovable |
| TA3 | **Fail closed** on missing or invalid tenant metadata in tenant-aware mode: no backfill, no grandfathering, no implicit default tenant, no pre-existing-record exemption | Owner-ratified. Ego has no historical data and no legacy users, so the cheap-but-permanent compatibility hole buys nothing |
| TA4 | Reuse `tenancy.MarshalMetadata`/`UnmarshalMetadata` under the existing `ego.tenant.*` keys; no second tenant serialization, no metadata map of our own, no resolution mechanism outside `tenancy.TenantResolver` | `command/carrier.go:94,165` already delegates exactly this way; reinventing it on the persisted record is the AC-level duplication failure |
| TA5 | Scope is **write-side only**: commands, persisted events, snapshots, durable states. Query envelopes are excluded | #54's AC1 was revised; queries moved to [`#75`](https://github.com/getsyntegrity/ego/issues/75) (EGO-QUERY-001) because ego has no `Query` type or dispatch path. #75 is **not** a dependency — #54 neither blocks on it nor consumes it |
| TA6 | Per-aggregate tenant binding must be **durable across actor restart**: widen `batchTenant` from batch scope to actor lifetime, seeded at `recover()`; add the equivalent check to `durable_state_actor.go`, which has none | Today's check is homogeneity within one in-flight batch, which a restart erases; `tenancy.VerifyUnchanged` (`event_sourced_actor.go:887`) is reused, not replaced |
| TA7 | `tenancy-core`'s two requirements are **consumed and proven**, not re-decided; `command-envelope`'s field semantics are untouched | #54 owns the proof against real code, not the contract text |
| TA8 | Architecture conformance applies only to a newly introduced pure-value package, if any | The three actor files import goakt by design — #55 precedent |

## Scope

**In**: additive tenant metadata on `egopb.Event`/`Snapshot`/`DurableState` + regeneration; write at persist time and read back at `recover()`/`replayEvents`/`recoverFromSnapshot`, fail-closed per TA3; `TenantContext` reconstruction at the five `saga_actor.go` reset sites; actor-lifetime tenant binding and cross-tenant command rejection in **both** actor kinds; one end-to-end test over the real `Engine.SendCommand` → actor → saga path.

**Out**:

| Deferred | Owner |
|---|---|
| Query envelope, `Query` type, query dispatch | #75 (EGO-QUERY-001) |
| Redefining operation/correlation/causation/principal/custom/deadline semantics | #59 (shipped) |
| Resolver configuration, auto-invoke, single-tenant mode | #55 (shipped) |
| EventStore namespacing, read-side/projection and topic isolation | TENANT-003/004/005 |
| Transport-level propagation | TRANSPORT-003 |
| Cross-tenant conformance suite | TENANT-007 |
| `EraseEntity` / `Migrator.Run` tenancy semantics | TENANT-008 |

## Capabilities

- **New**: `tenancy-write-path` — durable tenant metadata on persisted events/snapshots/state, fail-closed decode, reconstruction at async boundaries, tenant + aggregate effective identity in both actor kinds.
- **Modified**: None. `tenancy-core` and `command-envelope` are consumed unchanged; `tenancy-runtime`'s T4-A/T4-B gates keep their current semantics and the new enforcement is additive.

## Approach

One mechanism satisfies both open ACs: tenant metadata persisted on the record, decoded at every boundary that reads it back. The saga reconstructs `TenantContext` from the event it receives; the actor re-establishes its binding from the snapshot or replayed events after restart.

Field shape, tag numbers, key set, decode helpers and the rejection error model are **already fixed by PR1** (`#73`): `map<string,string> tenant_metadata` on `Event`(10)/`Snapshot`(7)/`DurableState`(7), `tenancy.MarshalMetadata`/`UnmarshalMetadata`, `tenancy.VerifyUnchanged` → `ErrDenied`, absent/malformed → `ErrInvalid`. `sdd-design` for the remaining work **reuses this exactly** for `durable_state_actor.go` and **deferred to `design.md`** only: where each new gate sits relative to `durable_state_actor.go`'s existing `verifyTenantForPersist`-style checks, and how `saga_actor.go` threads a per-event reconstructed `context.Context` through its five reset sites without binding the saga actor itself to one tenant (it legitimately observes events from many).

## Affected areas

| Area | Impact | Change |
|---|---|---|
| `protos/ego/ego.proto`, `egopb/` | **Shipped (#73)** | Additive tenant metadata on `Event`(10), `Snapshot`(7), `DurableState`(7) + regen — all three messages, so no further proto change is expected for PR2/PR3 |
| `event_sourced_actor.go` | **Shipped (#73)** | Persist/recover tenant; actor-lifetime `actorTenant` replacing batch-scoped `batchTenant`; enforcement in batched, non-batched, read (`getStateAndReply`) and replay (`applyPersistedEvent`) paths |
| `durable_state_actor.go` | Pending (PR2) | Net-new tenant-switch enforcement + persist/recover, same carrier fields already on the wire |
| `saga_actor.go` | Pending (PR3) | Reconstruct `TenantContext` at the 5 ctx reset sites |
| e2e test | Pending (PR3) | Real-dispatch saga-hop and restart tenant integrity |
| `tenancy/`, `command/`, `engine.go`, `option.go` | Unchanged | Consumed as-is (TA4, TA7) |

**Public consumer surfaces**: `egopb.Event`, `egopb.Snapshot` and `egopb.DurableState` gain a field — any third-party `persistence.EventsStore`/`SnapshotStore`/`StateStore` implementation sees a wider message. Byte-identical after this change: `Engine.SendCommand`, `Command`/`Event`/`State`, `SagaCommand`, the three behavior interfaces, `Config`/`Option`, the `command` package.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Wire-format change is the first in this chain; a wrong field shape is expensive to undo once persisted | **Resolved** | Shipped in PR1 (`#73`) — `Event`(10)/`Snapshot`(7)/`DurableState`(7), additive on free tags, already reviewed and merged. No wire-format decision remains open for PR2/PR3, which consume the same fields. |
| Fail-closed (TA3) makes any tenant-aware deployment reject records written by a pre-change build | High | Accepted and intended — no historical data, no legacy users; proven for the event-sourced actor in PR1 (`seedActorTenant`, `ErrInvalid`/`ErrDenied`); still to prove for the durable-state actor in PR2 |
| Regenerated `egopb/*.pb.go` plus vendoring inflates the diff | **Resolved for PR1** | Generated files excluded from the authored review budget in `#73`; same pattern applies if PR2/PR3 need regen (they shouldn't — no new proto fields expected) |
| 400-line budget across proto + 3 actors + e2e | Med (was High) | PR1's authored diff already landed within its slice's own budget; PR2 (durable-state only) and PR3 (saga + e2e) are the remaining, smaller slices — `sdd-tasks` re-forecasts each |
| Design/tasks breakdowns that enumerate specific code paths can silently miss sibling paths carrying the same invariant | Med | Learned the hard way on PR1: the original Phase-3/4 breakdown covered only the command-write paths and missed `getStateAndReply` (read) and `applyPersistedEvent` (per-event replay), both fixed in `39c55fc` before merge. `sdd-design`/`sdd-tasks` for PR2/PR3 must explicitly enumerate every case in `DurableStateActor.processCommand`'s dispatch and every step in `SagaActor`'s reset sites, not just the ones named in prose. |

## Delivery plan

Stacked chain — incremental, not from-scratch. One more slice than the #55/#59 precedent because of the proto boundary, but that slice is already done.

1. **PR1** — **DONE, merged as [`#73`](https://github.com/getsyntegrity/ego/pull/73).** Proto (3 messages) + regen; event-sourced persist/recover wiring, fail-closed decode, plus the two read-path/replay tenant-isolation fixes found in review (`39c55fc`). Foundation everything else consumes.
2. **PR2** — durable-state persist/recover + tenant-switch enforcement on the same carrier fields already on the wire. No proto change expected.
3. **PR3** — saga reconstruction at the five `context.Background()` sites + the real end-to-end dispatch test.
4. **PR4** — only if PR3's authored diff (saga wiring + e2e) exceeds the budget on its own; **default is to keep it inside PR3**, reversing PR1's proposal-time assumption now that PR1's own diff showed slices run smaller than forecast.

## Human gate

Required per `spec-governance` §10 — architecture, public API, and **persisted data format**. TA3 (fail-closed, no backfill) is already owner-ratified. **The proto field shape and tag allocation gate is already satisfied** — PR1 shipped it (`#73`) with human review before merge, covering all three messages this change needs. No outstanding proto/wire-format decision remains for PR2 or PR3, since both consume fields already on the wire. If `sdd-design` finds either slice needs an additional field, that reopens this gate; absent that, PR2/PR3 need no pre-apply human gate on the wire format itself — only the standard PR review.

## Rollback

Revert the commits, `make proto`, `go mod vendor`. The field is additive on free tags, so a reverted build ignores tenant metadata already written and no data migration is undone. Under TA3 there is no grandfathering path to unwind, and no caller signature or reply payload changes at any point.

## Success criteria

- [ ] Saga boundary reconstructs `TenantContext` from the carried metadata, proven by a test that forces the `context.Background()` reset. (PR3, pending)
- [x] Event-sourced: a command against an aggregate already bound to another tenant is rejected, batched and non-batched, read and write paths — proven in PR1 (`#73`). — [ ] Durable-state: same, pending PR2.
- [x] Event-sourced: tenant binding survives actor restart, proven via snapshot recovery and via event replay — proven in PR1. — [ ] Durable-state: pending PR2.
- [x] Event-sourced: missing or invalid tenant metadata on an event or snapshot fails closed in tenant-aware mode, zero writes, no default-tenant fallback — proven in PR1 (TA3 negative-path evidence). — [ ] Durable-state: pending PR2.
- [ ] No new envelope, no new metadata map, no tenant serialization outside `tenancy.MarshalMetadata`/`UnmarshalMetadata`; `command/` and `tenancy/` diffs are empty. (Holds for PR1; re-check for PR2/PR3.)
- [ ] One end-to-end test proves tenant integrity through `Engine.SendCommand` → actor → saga hop; the existing `TestSendCommandTenantResolution` explicitly does not satisfy this. (PR3, pending)
- [x] Legacy (non-tenant-aware) engines observe no behavior change — held for PR1's full-suite regression; re-verify for PR2/PR3.
- [x] Field shape, tag numbers, decode helpers and error model are fixed — shipped in PR1 (`map<string,string> tenant_metadata`, `Event`=10/`Snapshot`=7/`DurableState`=7, `tenancy.MarshalMetadata`/`UnmarshalMetadata`, `ErrInvalid`/`ErrDenied`). `sdd-design` for PR2/PR3 reuses this, it does not redecide it.
