# Spec 2 of 6 — Durable state, publishers, tenancy and erasure (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 2 of 6.** Previous: [Spec 1 — runtime core](../inmem-runtime-core/spec.md). Next: [Spec 3 — sagas](../inmem-runtime-sagas/spec.md) and [Spec 4 — passivation](../inmem-runtime-passivation/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D4, §D6, §D7, §D8 (the `Stop` order); maintainer decision on `EraseEntity` (Q7, #166) |

## Purpose

After spec 1 the runtime handles event-sourced entities. This spec adds the rest of what a `compose.Spec` can configure for entities: durable-state entities, encryption and event adapters, event and state publishers, tenant-aware mode, and GDPR erasure. It also gives `Runtime.Stop` the order `compose/inmem` will rely on. The spec 1 method table rows for `SpawnDurableState` and `EraseEntity` change from the unsupported placeholder to the behavior below.

## Requirements

### Requirement: durable-state entities

`SpawnDurableState` MUST check started, family, state store, then tenancy, in that order, and recover from `GetLatestState`. A command's new state MUST have the current state's protobuf type, and its new version MUST differ from the prior version by exactly one (`durable_state_actor.go:575-590`). Otherwise the result is `OutcomeFailed` with nothing written. A valid command MUST write once, update memory, then publish the `egopb.DurableState` to `topic.states`.

#### Scenario: version rule

- GIVEN a durable-state entity at version 3
- WHEN the handler returns version 5
- THEN the result is `OutcomeFailed` and `GetLatestState` still returns version 3

#### Scenario: type rule

- GIVEN a durable-state entity whose state is `testpb.Account`
- WHEN the handler returns a different message type at the next version
- THEN the result is `OutcomeFailed` and nothing is written

### Requirement: encryption and event adapters

With `Config.Encryptor` set, event payloads MUST be stored encrypted and decrypted on recovery. Durable state MUST NOT be encrypted (design §2.2). Event adapters MUST run on recovered events before `HandleEvent`.

#### Scenario: round trip

- GIVEN an encryptor and an entity with two persisted events
- WHEN the runtime is stopped, a new one is built on the same stores and the entity is spawned again
- THEN the recovered state equals the state before, and the stored payloads are not the plaintext

### Requirement: publishers and `Stop`

- **Attaching.** `AddEventPublishers` and `AddStatePublishers` MUST give each publisher its own subscriber and goroutine, reject a duplicate ID, and deliver only messages published after attachment.
- **Stopping.** `Stop`, bounded by its context, MUST do the following in this order (design §D8):
  1. refuse new calls;
  2. close every publisher and the stream, attempting every close and joining the errors;
  3. for each entity and saga, wait until the mailbox turn in progress has finished; only then does a durable-state entity write its state once more, unconditionally;
  4. answer items still queued behind the current turn with `ErrEngineNotStarted`. This is provisional; the drain policy belongs to #24 (LIFE-004).
- **Timeout.** When the context expires during step 3, `Stop` MUST return an error naming the entities still busy, and MUST skip their final write.

#### Scenario: no leak

- GIVEN a runtime with two publishers and entities that have processed commands
- WHEN `Stop` returns
- THEN both publishers are closed, and the goroutine count, including the mailbox drain goroutines and the publisher goroutines, returns to its value before `Start` (checked with `awaitCondition`, no sleep)

#### Scenario: a turn in progress

- GIVEN a durable-state command whose handler is blocked on a test channel
- WHEN `Stop` is called and the channel is then released
- THEN the command completes, its write happens before the final unconditional write, and `Stop` returns without error

### Requirement: tenancy

With `Config.TenantResolver` set:

- spawns MUST bind a tenant: `WithTenant`, else `tenancy.FixedTenantOf`, else `ErrSpawnTenantUndetermined`;
- re-spawning under another tenant MUST fail with `ErrSpawnTenantMismatch`;
- `Dispatch` MUST resolve the caller's tenant and refuse a mismatch;
- persistence MUST use `persistence.NewTenantScope`;
- the resolver's `Resolve` MUST NOT be called at spawn.

#### Scenario: cross-tenant command

- GIVEN entity `a` bound to tenant `t1`
- WHEN a caller resolved to `t2` dispatches to `a`
- THEN the command is refused and nothing is written

### Requirement: erasure

`EraseEntity` MUST follow the outcome of [#166](https://github.com/getsyntegrity/ego/issues/166) (maintainer decision, 2026-09-27; design §D6, Q7):

- in tenant-aware mode it resolves the caller's tenant, and fails closed with `tenancy.ErrDenied` for a caller without a tenant, **even when `full == false`**;
- it deletes the entity's encryption key through the additive extension #166 defines, and that key belongs exclusively to the affected entity and tenant;
- with `full`, it also deletes events and snapshots up to the latest sequence number.

The key granularity (entity plus tenant, where keys are selected by persistence ID alone today: `encryption/aes_encryptor.go:48`, `testkit/keystore.go:39`) is #166's to settle. This spec implements what #166 decides and does not decide it itself.

## Tasks (5)

1. **Durable state**: spawn, recovery, commands, `topic.states`. *Check:* version-rule, type-rule, recovery and publish tests.
2. **Encryption and event adapters.** *Check:* the round-trip scenario with `testkit`'s key store, and an adapter test.
3. **Publishers and `Stop`.** *Check:* the duplicate ID is rejected; a publisher attached after a publish sees only later messages; the no-leak and turn-in-progress scenarios; a `Stop` whose context expires names the busy entity; a publisher whose `Close` fails does not stop the others from closing.
4. **Tenancy.** *Check:* one test per error of the requirement, plus a counting resolver that proves `Resolve` is not called at spawn.
5. **Erasure**, per #166. *Check:* full and non-full, tenant-scoped and legacy; the fail-closed case with `full == false`; the entity's key is deleted, after which its encrypted events can no longer be decrypted; the key of the same persistence ID under another tenant is not deleted.

## Checks

- `go test ./internal/inmemruntime/`
- `go run ./internal/cmd/archcheck`; spec 1's closure test still passes
- apidiff: no report for any public package

## File ownership

`internal/inmemruntime/**`: new files for durable state, publishers, tenancy and erasure, plus the `Stop` function. Spec 3 works on separate saga files.

## Dependencies

- Spec 1 merged.
- **[#166](https://github.com/getsyntegrity/ego/issues/166) is a blocking dependency.** This spec does not merge before #166 has settled the crypto-shredding extension and the key granularity (maintainer requirement, 2026-09-27).

## Next in the chain

[Spec 3](../inmem-runtime-sagas/spec.md) and [spec 4](../inmem-runtime-passivation/spec.md). [Spec 5](../compose-inmem/spec.md) also waits on spec 4.
