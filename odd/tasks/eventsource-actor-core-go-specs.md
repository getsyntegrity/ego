# S3a: eventsource actor core tests on go-specs v0.3.3

This is spec 3a of `engine-actors-go-specs-chain.md`. It is a stacked PR on `refactor/engine-test-seams`
(S1, #244). It does not depend on S2 (#245).

## Problem

`internal/engine/eventsource/event_sourced_actor_test.go` has 5107 lines. Two of its Test functions,
`TestEventSourcedActor` (line 65, 26 `t.Run`) and `TestEventSourcedActorErrorPaths` (line 2289), are the
source of most of the package's cost:

- they start a real goakt actor system inline in almost every case;
- they mock with the generated testify `mocks/persistence.EventsStore`/`SnapshotStore`,
  `mocks/encryption.Encryptor` and `mocks/eventadapter.EventAdapter`;
- they wait with `pause.For`, mostly `pause.For(time.Second)` after `Start`/`Spawn`, and shorter pauses
  for flush timers.

The package takes 223.9 s.

## What changes

Only `event_sourced_actor_test.go` changes, and only inside those two Test functions. Any new shared helper
lives in a new `*_test.go` file.

- **About 11 `ErrorPaths` recovery-failure cases become unit tests.** These are the GetLatestEvent
  failure, replay failure, snapshot read/decrypt/unmarshal/type mismatch, event decrypt/unmarshal, the
  adapter chain, `UnmarshalNew` and `HandleEvent`. They build the `Actor` directly and call
  `recover(ctx)` with the S1 adapters (`enginetest.NewEventsStoreMock` and the others), with no goakt.
- **The remaining testify mocks in the two functions move to the S1 adapters.**
- **The `pause.For` waits become `ctx.Eventually`** on an observable condition: `pid.IsRunning()`, the
  persisted events or snapshot in the store, or a reply. A wait that is followed by a synchronizing `Ask`
  is simply removed.
- **The cases that need the actor system stay as component tests.** They are listed for S3b's
  reclassification.

## Constraints

- Every case, invariant and subtest name stays. The `--- PASS` count does not drop. A rename is allowed
  only when it is listed as old → new.
- No production change. No `-race`, no workbench.
- Strict TDD: in each task, a deliberate production mutation must be caught by the new tests, and then
  reverted.
- The other Test functions of the file and the other files of the package are out of scope. They belong
  to S3b.

## Tasks

- [x] T1 Move the `ErrorPaths` recovery-failure cases to unit tests on the S1 adapters. Route: delegated
      writer. Evidence: `30ff50a`. Nine cases now call `recover` on a directly built `Actor`. RED: making
      `recoverFromSnapshot` swallow the load error was caught.
- [x] T2 Move the remaining testify mocks in `ErrorPaths` to the S1 adapters. Route: the same writer.
      Evidence: `d7ce6f9`. The four mistyped-extension cases became one `specs.Table`. RED: letting the
      snapshot writer write plaintext after an encryption error was caught.
- [x] T3 Move the testify mocks in `TestEventSourcedActor` to the S1 adapters. Route: the same writer.
      Evidence: `7a1ac63`. Two more cases became unit tests. RED: both a ping runner mutation and a
      GetLatestEvent swallow were caught.
- [x] T4 Replace the `pause.For` waits in both functions with `ctx.Eventually` or a synchronizing `Ask`.
      Route: the same writer. Evidence: `03f64c8`, which adds the shared rig
      `event_sourced_actor_rig_test.go`.
      - RED: skipping `WriteEvents` and skipping `WriteSnapshot` were both caught, and the snapshot case
        fails within 5 s without hanging.
      - `-count=30` was stable.
- [x] T5 Deliver. Route: inline.
      - Names: 40 `--- PASS` before and after, identical.
      - Time: the two functions went from 215.6 s (`-count=3`) to 1.1 s, and the parent confirmed
        0.33 s on an uncached run. The full package went from 223.5 s to 152.2 s.
      - Coverage: 92.0%, unchanged.
      - vet, lint and gofmt are clean.
      - The native assessment was `medium` with RDD off, so the writer's self-verification stands.

## Findings from the rework

- The old `DeleteEvents`/`DeleteSnapshots` retention cases never proved that the asynchronous delete
  happened. The old testify expectations were also never checked with `AssertExpectations`. Both are now
  enforced, with exact counts where the count is deterministic.
- `with state recovery from event store` does not prove recovery from the store, before or after this
  change. It passes even when the events write is skipped, because `ReSpawn` keeps in-memory state.
  Proving it needs a second actor system on the same store, as the encryption recovery cases do. That fix
  is left for S3b or a follow-up.

## Component cases (they still start a real goakt actor system)

`TestEventSourcedActor` has 16:
- with state reply
- with error reply
- with unhandled command
- with state recovery from event store
- with no event to persist
- with unhandled event
- with snapshot store recovery
- with telemetry extension
- with encryption during command processing
- with snapshot persistence on interval
- with retention policy delete events on snapshot
- with event adapters during recovery
- with snapshot and encryption during recovery
- with encrypted event replay without snapshot store
- with retention policy delete snapshots on snapshot
- With events store ping failed

`TestEventSourcedActorErrorPaths` has 11:
- with missing behavior fails to start
- the four mistyped-extension cases
- with event encryption failure during command processing
- with snapshot encryption failure during command processing
- with DeleteEvents error in retention policy does not crash
- with DeleteSnapshots error in retention policy does not crash
- with unhandled non-command message does not crash
- with persistEvents write failure shuts down actor

## Left for S3b

There are 68 `pause.For` left:
- `TenancyGate`: 5
- `BatchTenantHomogeneity`: 3
- `ResetBatchDoesNotClearActorTenant`: 2
- `ZeroEventCrossTenant`: 3
- `ZeroEventSameTenant`: 3
- `GetStateDuringPersist`: 6
- `Batch`: 46

The `ego/mocks` imports and the testify `mock` imports remain only in `GetStateDuringPersist` and `Batch`.

## Progress

- 2026-09-30: spec created on `test/eventsource-actor-core-go-specs`, on top of S1.
