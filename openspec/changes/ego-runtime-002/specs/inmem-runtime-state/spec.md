# Spec 2 of 5 — Durable state, publishers, tenancy and erasure (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 2 of 5.** Previous: [Spec 1 — runtime core](../inmem-runtime-core/spec.md). Next: [Spec 3 — sagas](../inmem-runtime-sagas/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D4, §D6, §D7, §D8 (the `Stop` order); open question Q7 |

## Purpose

After spec 1 the runtime handles event-sourced entities. This spec adds the rest of what a `compose.Spec` can configure for entities: durable-state entities, encryption and event adapters, event and state publishers, tenant-aware mode, and GDPR erasure. It also gives `Runtime.Stop` the order `compose/inmem` will rely on.

## Requirements

### Requirement: durable-state entities

`SpawnDurableState` MUST recover from `GetLatestState`. A command MUST produce exactly the prior version plus one, else `OutcomeFailed` with nothing written; a valid command MUST write once, update memory, then publish the `egopb.DurableState` to `topic.states`.

#### Scenario: version rule

- GIVEN a durable-state entity at version 3
- WHEN the handler returns version 5
- THEN the result is `OutcomeFailed` and `GetLatestState` still returns version 3

### Requirement: encryption and event adapters

With `Config.Encryptor` set, event payloads MUST be stored encrypted and decrypted on recovery; durable state MUST NOT be encrypted (design §2.2). Event adapters MUST run on recovered events before `HandleEvent`.

#### Scenario: round trip

- GIVEN an encryptor and an entity with two persisted events
- WHEN the runtime is stopped, a new one is built on the same stores and the entity is spawned again
- THEN the recovered state equals the state before, and the stored payloads are not the plaintext

### Requirement: publishers

`AddEventPublishers` and `AddStatePublishers` MUST give each publisher its own subscriber and goroutine, reject a duplicate ID, and deliver only messages published after attachment. `Stop` MUST, in this order, refuse new calls, close every publisher and the stream (attempting every close and joining errors), then stop the entities, durable-state entities writing their state once more unconditionally (design §D8).

#### Scenario: no leak

- GIVEN a runtime with two publishers and a saga-free workload
- WHEN `Stop` returns
- THEN both publishers are closed and the goroutine count returns to its value before `Start` (condition-waited, no sleep)

### Requirement: tenancy

With `Config.TenantResolver` set, spawns MUST bind a tenant (`WithTenant`, else `tenancy.FixedTenantOf`, else `ErrSpawnTenantUndetermined`), re-spawning under another tenant MUST fail with `ErrSpawnTenantMismatch`, `Dispatch` MUST resolve the caller's tenant and refuse a mismatch, and persistence MUST use `persistence.NewTenantScope`. The resolver's `Resolve` MUST NOT be called at spawn.

#### Scenario: cross-tenant command

- GIVEN entity `a` bound to tenant `t1`
- WHEN a caller resolved to `t2` dispatches to `a`
- THEN the command is refused and nothing is written

### Requirement: erasure

`EraseEntity` MUST behave as the GoAkt code does (design §2.5, §2.6, Q7): tenant-scoped in tenant-aware mode (failing closed with `tenancy.ErrDenied` for a caller without a tenant), and with `full` deleting events and snapshots up to the latest sequence number.

## Tasks (5)

1. **Durable state** spawn, recovery, commands, `topic.states`. *Check:* version-rule, recovery and publish tests.
2. **Encryption and event adapters.** *Check:* the round-trip scenario with `testkit`'s key store; an adapter test.
3. **Publishers and `Stop` order.** *Check:* duplicate ID rejected; attach-after-publish sees only later messages; the no-leak scenario; a publisher whose `Close` fails does not stop the others from closing.
4. **Tenancy.** *Check:* one test per error of the requirement, plus a counting resolver that proves `Resolve` is not called at spawn.
5. **Erasure.** *Check:* full and non-full, tenant-scoped and legacy, and the fail-closed case.

## Checks

- `go test ./internal/inmemruntime/`
- `go run ./internal/cmd/archcheck`; the closure test of spec 1 still passes
- apidiff: no report for any public package

## File ownership

`internal/inmemruntime/**`: new files for durable state, publishers, tenancy and erasure, plus the `Stop` function. Spec 3 works on separate saga files.

## Dependencies

Spec 1 merged.

## Next in the chain

[Spec 3](../inmem-runtime-sagas/spec.md); [spec 4](../compose-inmem/spec.md) may start in parallel with spec 3.
