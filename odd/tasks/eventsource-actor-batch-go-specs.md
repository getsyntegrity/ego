# S3b: eventsource actor batch and tenancy tests on go-specs v0.3.3

Spec 3b of `engine-actors-go-specs-chain.md`, and the last one. It is a stacked PR on
`test/eventsource-actor-core-go-specs` (S3a, #246).

## Problem

After S3a, `internal/engine/eventsource` still waits with `pause.For` and still mocks with the generated
testify `mocks/*` in these places.

`event_sourced_actor_test.go` has 68 `pause.For` calls:

| Test function | `pause.For` |
|---|---|
| `TenancyGate` | 5 |
| `BatchTenantHomogeneity` | 3 |
| `ResetBatchDoesNotClearActorTenant` | 2 |
| `ZeroEventCrossTenant` | 3 |
| `ZeroEventSameTenant` | 3 |
| `GetStateDuringPersist` | 6 |
| `Batch` | 46 |

`GetStateDuringPersist` and `Batch` are the only remaining users of `ego/mocks` and of the testify `mock`
package.

`event_sourced_actor_tenant_persist_test.go` has 10 more `pause.For` calls, around 8 inline actor systems.

The full package takes 152.2 s.

## What changes

Only the `*_test.go` files in `internal/engine/eventsource` change. The S3a rig
(`event_sourced_actor_rig_test.go`) is reused.

- Each `pause.For` becomes a `ctx.Eventually` on an observable condition. Where the next `Ask` already
  synchronizes, the wait is dropped.
- Flush-timer waits poll for the flushed result. "No crash" waits become `Consistently` on `IsRunning`,
  bounded and short.
- The mocks in `GetStateDuringPersist` and `Batch` move to the S1 `enginetest` adapters. The package then
  no longer imports `ego/mocks` or the testify `mock` package.
- The `time.After` guards in `GetStateDuringPersist` stay. They are timeouts on a reply channel, not
  synchronization.
- The component cases of S2, S3a and S3b are reclassified in `docs/testing/unit-migration.md`, the lane
  inventory from #218. That commit goes to #218's branch, `test/202-test-lane-inventory`.

## Constraints

- Every case, invariant and subtest name is kept. The `--- PASS` count does not drop.
- No production change. No `-race`. No workbench.
- TDD is strict. RED is a deliberate production mutation per task, which must be caught and then reverted.
- Out of scope, and noted as a follow-up: making `with state recovery from event store` prove recovery
  from the store with a second system. This came from S3a's findings. It is out of scope because it
  changes what the case tests.

## Tasks

- [ ] T1 Replace the 16 `pause.For` in `TenancyGate`, `BatchTenantHomogeneity*` and
      `ResetBatchDoesNotClearActorTenant` with polls. Route: delegated writer. Check: green, and a
      cross-tenant batch mutation is caught.
- [ ] T2 In `GetStateDuringPersist`, move the mocks to the adapters and replace its 6 `pause.For`. Route:
      the same writer. Check: green, and a mutation that answers `GetState` with stale state is caught.
- [ ] T3 In `Batch`, move the mocks to the adapters and replace its 46 `pause.For`. Route: the same writer.
      Check: green with `-count=3`, and a mutation that skips the flush timer is caught. After this task
      there must be no `ego/mocks` and no testify `mock` import left in the package.
- [ ] T4 Replace the 10 `pause.For` in `event_sourced_actor_tenant_persist_test.go` with polls. Route: the
      same writer. Check: green, and a tenant persist mutation is caught. After this task there must be no
      `pause.For` left in the package.
- [ ] T5 Deliver. Route: inline (parent).
      - Write the component-lane reclassification on #218's branch.
      - Verify: full package time and coverage, vet, lint.
      - Run the native assessment, plus an independent verifier if the result is `high`.
      - Push and open the stacked PR.

## Progress

- 2026-09-30: spec created on `test/eventsource-actor-batch-go-specs`, on top of S3a.
