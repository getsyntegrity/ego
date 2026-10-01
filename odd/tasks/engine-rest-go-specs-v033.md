# engine unit tests: last testify mocks on go-specs v0.3.3 (#241)

## Problem

PR #241 (branch `test/205-migrate-engine-rest`) moved most of the `engine` package unit tests to go-specs. Two
files still used the generated testify mocks (`mocks/ego`, `mocks/persistence`, `mocks/offsetstore`) with
`mock.Anything` and `mock.AnythingOfType`:

- `engine/engine_test.go`: the EraseEntity, ProjectionLag and RebuildProjection error tests, and the eight event/state
  publisher tests.
- `engine/engine_tenant_spawn_test.go`: two "refuses to spawn and touches no store" cases that proved the absence
  of store calls with an expectation-less generated mock and `AssertExpectations`.

The conventions in `docs/testing/go-specs.md` require go-specs `mock.Controller` for replaced dependencies. A
testify mock only verifies when the test remembers `AssertExpectations`, and its failures bypass the spec.

## What changes

Only `*_test.go` files in `engine/` change.

- New `engine/specs_mocks_test.go` holds typed, test-local `mock.Controller` adapters for `EventsStore`,
  `SnapshotStore`, `StateStore`, `OffsetStore`, `EventPublisher` and `StatePublisher`. Each adapter forwards every
  method of its port, so a call the case did not declare is reported as an unexpected call.
- The erase, lag and rebuild tests declare their calls with exact arguments (`"pid-3"`, `uint64(7)`, the projection
  name) instead of `AnythingOfType("string")`. The lag test also states that the offset store is not read after
  `ShardOffsets` failed (no expectation declared).
- The publisher tests became `specs.Describe` cases. `Publish` is declared `AtLeast(1)` or `AtLeast(2)` and the
  tests poll the call count with `ctx.Eventually` instead of a buffered channel and `time.After`. `Close` on the
  Stop-error tests is `Times(1)`.
- `lags == nil` with `BeTrue` is now `ctx.Expect(lags).To(specs.BeNil())`; `require.ErrorIs` is `MatchError`.
- The two tenant-spawn cases use an expectation-less controller. The explicit `AssertExpectations` calls are gone
  because the controller verifies when the case ends.

## What does not change, and why

- **Production code.** Nothing outside `*_test.go` was touched.
- **The actor-system based tests.** These tests start a real goakt actor system in process. They use no database or
  network, and moving them off the actor system is a seam change in production code. Out of scope.
- **`TestEnginePublisherIdleCPU`.** It measures real CPU over a real 2 s window; the wall-clock wait is the
  measurement, not a synchronization. It only swapped its two mocks for controller adapters and stays a plain test.
- **`TestEventPublisherReceivesEventsFromEntity` and its ordering assertion** (`engine/publisher_test.go`). See the
  decision below.
- **Rejected alternative:** wrapping every touched plain test in `specs.Describe` just to use `Eventually`. Only the
  tests that waited on a publisher, or that use a controller bound to a spec case, were wrapped.

## Constraints

- Every case and invariant stays. Every old subtest name is still present (identical set). The eight plain tests
  that became `Describe` cases gained one subtest each, so `--- PASS` goes from 468 to 476.
- No force-push (`develop` is merged into the branch). No `-race`. No workbench.
- Strict TDD, runner `go test ./engine`. RED is shown by a deliberate production mutation, then reverted.
- Delivery: push to `test/205-migrate-engine-rest` and append a section to the #241 description.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3 already pinned; #229 came in). Route: inline. Evidence: merge
      commit `ca21594`, clean `go build ./...`.
- [x] T2 `engine_test.go`: controller adapters and matchers replace the generated mocks. Route: delegated writer.
      Evidence: `191d1d7`. RED mutations caught: `DeleteSnapshots(..., seq+1)` in `engine/entities.go` gave
      `mock: unexpected call DeleteSnapshots(context.Background, unscoped, "pid-3", 8)`; skipping the event publisher
      `Close` in `Engine.Stop` gave `mock: unmet expectation Close(any value) ... want 1, got 0`.
- [x] T3 `engine_tenant_spawn_test.go`: expectation-less controllers. Route: same writer. Evidence: `76ecd9b`. RED
      mutation: a store `Ping` inside `spawnTenantScope` gave `mock: unexpected call Ping(context.Background): no
      expectations declared for this method`.
- [x] T4 Decide the flaky `TestEventPublisherReceivesEventsFromEntity`. Route: inline investigation, no code change.
      Decision below.
- [x] T5 Verify and deliver. Evidence: `--- PASS` 468 before, 476 after, old names all present; coverage `engine`
      94.8% before and after; `go vet`, `golangci-lint run ./engine/...` (0 issues) and `gofmt -l` clean;
      `go test -count=5 ./engine` passes.

## Decision: publisher ordering test (T4)

CI failed twice on this test with the two events seen as sequence 2 then 1. It reproduces locally
(`go test -count=300 -run '^TestEventPublisherReceivesEventsFromEntity$' ./engine` fails roughly once in 300 to 400
runs).

Is per-entity order guaranteed? By the Ego code, yes: the entity stashes the next command until the previous write
completes, and `eventsWriterActor.handlePersistEvents` publishes to the stream before it replies. So
`Publish` calls reach the stream in sequence order.

I added temporary prints (reverted) at the writer and at `Engine.sendEvent`. In a failing run the order was
`WRITER-PUB 1, WRITER-PUB 2, SEND-EVT 2, SEND-EVT 1`. The writer published in order and the consumer received them
swapped, so the reorder happens between `eventstream.Publish` and the subscriber's `Iterator()`. That path is
`github.com/tochemey/goakt/v4@v4.5.4/eventstream` over `internal/queue`, a lock-free queue that recycles nodes through
a `sync.Pool` (a plausible ABA source; not proven here).

Because order is guaranteed by design, the assertion is NOT relaxed: that would mask a real ordering defect. The
test is left unchanged and the defect is reported as a production/dependency bug (event stream delivery reorders
two ordered publishes). No production change was made.

## Follow-ups (not in this document)

- `engine-ordering-bug`: fix or work around the goakt event stream reordering (production or dependency change),
  then keep the strict assertion.
- `enginetest-adapters-dedup`: `refactor/engine-test-seams` has typed mock adapters in `internal/engine/enginetest`.
  When that branch lands, replace `engine/specs_mocks_test.go` with them.
- Remaining plain-`t` tests in `engine/` (`require`/`assert`, `pause.For` waits) are the next migration spec.

## Progress

- 2026-09-30: T1 to T5 done. Engram mirror: pending.
