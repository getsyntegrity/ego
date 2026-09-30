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

- [ ] T1 Move the `ErrorPaths` recovery-failure cases to unit tests on the S1 adapters. Route: delegated
      writer. Check: green, and a mutation that swallows an error in `recover` is caught.
- [ ] T2 Move the remaining testify mocks in `ErrorPaths` to the S1 adapters. Route: the same writer.
      Check: green, and an encryption-failure mutation is caught.
- [ ] T3 Move the testify mocks in `TestEventSourcedActor` to the S1 adapters. Route: the same writer.
      Check: green, and a snapshot/encryption mutation is caught.
- [ ] T4 Replace the `pause.For` waits in both functions with `ctx.Eventually` or a synchronizing `Ask`.
      Route: the same writer. Check: `-count=3` is green, the package time drops, and a persist mutation
      fails within the poll timeout without hanging.
- [ ] T5 Record the component cases, verify, run the assessment (plus an independent verifier if the
      result is `high`), push, and open the stacked PR. Route: inline.

## Component cases (filled in by T5)

_pending_

## Progress

- 2026-09-30: spec created on `test/eventsource-actor-core-go-specs`, on top of S1.
