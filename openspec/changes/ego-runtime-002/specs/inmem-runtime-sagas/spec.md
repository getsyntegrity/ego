# Spec 3 of 5 — Sagas and `SagaStatus` (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 3 of 5.** Previous: [Spec 2 — durable state, publishers, tenancy, erasure](../inmem-runtime-state/spec.md). Next: [Spec 5 — neutrality proof](../runtime-neutrality/spec.md) (spec 4 runs in parallel) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D5, §D7; #153 as fixed by #163 |

## Purpose

This spec makes the in-memory runtime run sagas the way `saga_actor.go` does, with one deliberate difference: a saga receives each entity's events directly, in the order they were persisted, instead of through the unordered event stream (design §D5). That is what lets saga tests assert without sleeps. `SagaStatus` reports the lifecycle status that #153 fixed.

## Requirements

### Requirement: spawn and recovery

`SpawnSaga` MUST apply the lifecycle, events-store, family and tenant checks of an entity spawn, read only `WithTenant` among spawn options, and recover the saga's own events through `ApplyEvent` before returning.

### Requirement: event delivery and reaction

Every event an entity persists MUST be enqueued to every live saga's mailbox, in persist order, before the entity's command reply is returned. In its mailbox the saga MUST run `HandleEvent`, persist the action's events to the events store only (not the stream), apply them, then send each command through the runtime's internal dispatch with the saga's tenant and derived metadata (default timeout 5 s), and call `HandleResult` or `HandleError`.

#### Scenario: ordered reaction without sleeps

- GIVEN a saga that sends `CreditAccount` to `b` for every `AccountCredited` of `a`
- WHEN three commands credit `a`
- THEN `SagaStatus` (queried after the third `SendCommand` returns, under a context deadline) sees the saga state after all three, and `b`'s journal holds three credits in the same order

### Requirement: completion, compensation, timeout

`Complete` MUST set `SagaCompleted`. `Compensate` and the timeout path MUST run `behavior.Compensate` and every compensation command in one mailbox turn and set `SagaCompleted` when all succeed, `SagaFailed` otherwise, as `saga_actor.go:806-829`. `SagaCompensating` is therefore never returned by `SagaStatus`.

#### Scenario: failed compensation

- GIVEN a saga whose second compensation command fails
- WHEN compensation runs
- THEN `SagaStatus` reports `SagaFailed`

### Requirement: `SagaStatus`

`SagaStatus` MUST be answered from inside the saga's mailbox, return `ErrUndefinedEntityID` for an empty ID and an error for an unknown one, and fill `SagaInfo.ID`, `Status` and `State`.

## Tasks (4)

1. **Spawn and recovery** (RED first). *Check:* a saga with persisted events recovers its state; missing events store and undeclared saga family fail before spawning.
2. **Delivery and reaction.** *Check:* the ordered-reaction scenario; a saga command to an unknown entity reaches `HandleError`; no deadlock when the target entity's events wake the same saga.
3. **Completion, compensation, timeout.** *Check:* completed, compensation succeeded, compensation failed, timeout-triggered compensation (short timeout, `SagaStatus` awaited under a context deadline); the timer is stopped by `Stop` (goroutine count condition-waited).
4. **`SagaStatus`** and a tenant-aware saga (saga and entities under one tenant). *Check:* status table test; the tenant case writes under `NewTenantScope`.

## Checks

- `go test ./internal/inmemruntime/`
- closure test and `go run ./internal/cmd/archcheck`
- apidiff: no report for any public package

## File ownership

`internal/inmemruntime/**`: new saga files only; the entity event path gains one call that enqueues to sagas (coordinate with spec 2 if both are open).

## Dependencies

Spec 1 merged; spec 2 merged for task 4's tenant case.

## Next in the chain

[Spec 5](../runtime-neutrality/spec.md), after [spec 4](../compose-inmem/spec.md).
