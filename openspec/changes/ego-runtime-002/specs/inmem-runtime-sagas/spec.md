# Spec 3 of 6 — Sagas, tenant rules and `SagaStatus` (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 3 of 6.** Previous: [Spec 2 — durable state, publishers, tenancy, erasure](../inmem-runtime-state/spec.md). Next: [Spec 6 — neutrality proof](../runtime-neutrality/spec.md) (specs 4 and 5 can run in parallel with this one) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D5, §D7; #153 as fixed by #163; maintainer decision on Q8 (2026-09-27) |

## Purpose

This spec makes the in-memory runtime run sagas the way `saga_actor.go` does, including its tenant rules. One difference is deliberate: a saga receives each entity's events directly, in persist order, instead of through the unordered event stream (design §D5). That order is **not part of the contract** (maintainer decision on Q8), and no test in this spec may depend on it. `SagaStatus` reports the lifecycle status that #153 fixed. The spec 1 method table rows for `SpawnSaga` and `SagaStatus` change from the unsupported placeholder to the behavior below.

## Requirements

### Requirement: spawn and recovery

`SpawnSaga` MUST check started, family, events store, then tenancy, in that order. It MUST read only `WithTenant` among the spawn options, and recover the saga's own events through `ApplyEvent` before returning.

### Requirement: event delivery and reaction

- **Delivery.** Every event an entity persists MUST be enqueued to every live saga's mailbox before the entity's command reply is returned.
- **Reaction.** In its mailbox the saga MUST run `HandleEvent`, persist the action's events to the events store only (not the stream), and apply them. It then sends each command through the runtime's internal dispatch, with the saga's tenant and derived metadata and a default timeout of 5 s, and calls `HandleResult` or `HandleError`.
- **Not running.** A saga whose status is not `SagaRunning` MUST ignore every further event, with no `HandleEvent` and no write (`saga_actor.go:482-484`).

#### Scenario: reaction without sleeps

- GIVEN a saga that sends `CreditAccount` to `b` for every `AccountCredited` of `a`
- WHEN three commands credit `a`
- THEN `SagaStatus`, queried after the third `SendCommand` returns, sees the saga state after all three, and `b`'s journal holds three credits. The assertion is about the final state and the count only; it must not depend on the order in which the saga received the events

#### Scenario: completed saga ignores events

- GIVEN a saga that has completed
- WHEN an entity persists an event the saga would react to
- THEN the saga's journal and state are unchanged

### Requirement: tenant rules (design §D5)

- **SG4**: a live event with invalid tenant metadata MUST be dropped. An event whose tenant differs from the saga's bound tenant MUST be dropped before `HandleEvent`, so the behavior never sees a foreign payload.
- **SG5**: every replayed saga event MUST carry the spawn-bound tenant. A mismatch MUST fail the spawn and register nothing.
- **SG-DUR1**: replay MUST recognize an `emptypb.Empty` tenant-binding marker and skip it without calling `ApplyEvent`. The runtime MUST NOT write new markers.

### Requirement: completion, compensation, timeout

`Complete` MUST set `SagaCompleted`. `Compensate` and the timeout path MUST run `behavior.Compensate` and every compensation command in one mailbox turn, then set `SagaCompleted` when all succeed and `SagaFailed` otherwise, as `saga_actor.go:806-829` does. `SagaCompensating` is therefore never returned by `SagaStatus`. The timeout timer comes from the runtime clock (design §D11).

#### Scenario: failed compensation

- GIVEN a saga whose second compensation command fails
- WHEN compensation runs
- THEN `SagaStatus` reports `SagaFailed`

### Requirement: `SagaStatus` and `EntityExists`

`SagaStatus` MUST be answered from inside the saga's mailbox. It MUST return `ErrUndefinedEntityID` for an empty ID and an error for an unknown one, and fill `SagaInfo.ID`, `Status` and `State`. `EntityExists` MUST report true for a live saga's ID, as GoAkt's `ActorExists` does.

## Tasks (5)

1. **Spawn and recovery** (RED first). *Check:* a saga with persisted events recovers its state; a missing events store and an undeclared saga family fail before spawning, family first; options other than `WithTenant` have no effect.
2. **Delivery and reaction.** *Check:* the reaction and completed-saga scenarios; a saga command to an unknown entity reaches `HandleError`; no deadlock when the target entity's events wake the same saga; `EntityExists` on the saga ID.
3. **Tenant rules**, one check per rule. *Check:*
   - SG4: a foreign-tenant event never reaches `HandleEvent` (a counting behavior), and invalid tenant metadata is dropped;
   - SG5: a journal with one foreign-tenant saga event fails the spawn;
   - SG-DUR1: a journal holding a marker recovers without `ApplyEvent` being called for it, and no marker is written.
4. **Completion, compensation, timeout.** *Check:* completed; compensation succeeded; compensation failed; timeout-triggered compensation, driven by advancing the manual clock (this task may use a stub clock until spec 4 lands, then switches to it); the timer is stopped by `Stop`, with the goroutine count checked by `awaitCondition`.
5. **`SagaStatus`**. *Check:* a status table test (running, completed, failed, unknown ID, empty ID); a tenant-aware saga writes under `NewTenantScope`.

## Checks

- `go test ./internal/inmemruntime/`
- closure test and `go run ./internal/cmd/archcheck`
- review check: no assertion in this spec's tests depends on the order in which a saga received events (maintainer decision on Q8)
- apidiff: no report for any public package

## File ownership

`internal/inmemruntime/**`: new saga files only. The entity event path gains one call that enqueues to sagas; coordinate with spec 2 if both are open.

## Dependencies

Spec 1 merged; spec 2 merged for the tenant tasks.

## Next in the chain

[Spec 6](../runtime-neutrality/spec.md), after [spec 5](../compose-inmem/spec.md).
