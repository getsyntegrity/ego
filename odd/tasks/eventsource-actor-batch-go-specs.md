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

- [x] T1 Replace the 16 `pause.For` in `TenancyGate`, `BatchTenantHomogeneity*` and
      `ResetBatchDoesNotClearActorTenant` with polls. Route: delegated writer. Evidence: `a5a0b26`. RED:
      when the `actorTenant` gate was disabled, three functions failed.
- [x] T2 In `GetStateDuringPersist`, move the mocks to the adapters and replace its 6 `pause.For`. Route:
      the same writer. Evidence: `9e1180d`. The stash waits now poll until `StashSize() == 2`. RED: when
      the stash was skipped, the poll timed out on the last observed value.
- [x] T3 In `Batch`, move the mocks to the adapters and replace its 46 `pause.For`. Route: the same writer.
      Evidence: `e751ab6`. `Batch` went from about 40 s to 0.7 s, and `-count=10` passed. RED: both
      skipping `startFlushTimer` and changing the threshold from `>=` to `>` were caught.
- [x] T4 Replace the 10 `pause.For` in `event_sourced_actor_tenant_persist_test.go` with polls. Route: the
      same writer. Evidence: `8361a67`. RED: when `marshalEvent` dropped `TenantMetadata`, the restart
      test failed.
- [x] T5 Deliver. Route: inline.
      - Names: 62 `--- PASS` became 70. No old name was lost; eight single-case functions each gained one
        `It`.
      - Package: 152.2 s became 81.1 s, and coverage is 92.0%. One run showed 91.8%, which was timing
        noise; the parent re-ran it uncached and got 92.0%.
      - vet, lint and gofmt are clean. The native assessment returned `medium` with RDD off.
      - The scoped files (`event_sourced_actor_test.go` and `..._tenant_persist_test.go`) have zero
        `pause.For`, zero `ego/mocks` and zero testify `mock`.

## Correction to this spec

The "Problem" section said that `GetStateDuringPersist` and `Batch` were the last users of `ego/mocks` in
the package. That is true only within `event_sourced_actor_test.go`. Four other files in the package are
not part of #238's nine files, and they still have 44 `pause.For` and the generated mocks:

| File | `pause.For` |
|---|---|
| `events_janitor_actor_test.go` | 17 |
| `snapshots_writer_actor_test.go` | 15 |
| `event_sourced_actor_scope_test.go` | 8 |
| `events_writer_actor_test.go` | 4 |

The package-wide zero criterion in T3 and T4 was therefore wrong. Those files are covered by the S4
follow-up.

## Reclassification

No change to `docs/testing/unit-migration.md` (#218) is needed. Every Test function that still starts an
actor system is already listed there as "out of phase: starts an actor system":

- the 9 durablestate functions;
- the 23 eventsource functions.

The inventory is per Test function and is recorded at `0de4249`, so editing it here would put it out of
step with that snapshot.

The caveat is that `TestEventSourcedActor`, `TestEventSourcedActorErrorPaths` and
`TestDurableStateBehavior` now hold both unit cases and component cases. Splitting each of them into a
unit function and a component function would rename subtests. That split is left to a follow-up.

## Weak cases found (reported, not changed)

- The two "batch persist failure" cases check only the error reply. Their names promise a shutdown and
  telemetry, but they assert neither, and telemetry is a noop.
- Three tenant-persist rejection tests would pass even if the write were skipped, because the
  spawn-bound tenant seeds `actorTenant`. Only `TenantIdentitySurvivesRestart` catches a metadata
  regression.
- `BatchTenantHomogeneity` and `ZeroEventSameTenant` still wait out real 1 s flush windows. Their
  `Consistently` over 500 ms is also a real-time bound.

## Follow-ups

- **S4 `eventsource-writers-go-specs`:** the janitor, the snapshot writer, the scope and the events writer
  tests. That is 44 `pause.For` plus the persistence and encryption generated mocks.
- **Prove `with state recovery from event store`:** run a second system on the same store. This came out
  of S3a.
- **Strengthen the weak cases above.**
- **Split the mixed unit/component Test functions,** once renaming them is acceptable.

## Progress

- 2026-09-30: spec created on `test/eventsource-actor-batch-go-specs`, on top of S3a.
