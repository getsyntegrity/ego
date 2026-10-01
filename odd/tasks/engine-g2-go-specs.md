# engine group G2: expected-revision and batch-precondition tests on go-specs v0.3.3 (#252)

## Problem

Issue #252 moves every `engine/` test off testify. This group, G2, owns five files that still import
`require` and `assert`:

- `engine/durable_state_actor_expected_revision_test.go`
- `engine/durable_state_actor_integration_test.go`
- `engine/event_sourced_actor_expected_revision_test.go`
- `engine/event_sourced_actor_integration_test.go`
- `engine/event_sourced_actor_batch_precondition_matrix_test.go`

They hold 28 top-level tests. Each one starts a real goakt actor system through `newTestEngine`, asserts with
testify, and the race and batch tests drive goroutines and `select`/`time.After` by hand.

## What changes

Only those five files and one new helper file, `engine/specs_helpers_g2_test.go`.

Every `Test` keeps its name and now holds a `specs.Describe` with one `It` per flow, or one `specs.Table`
row per case. The U/C admission matrix is a `specs.Table`, and its row names (`U_U`, `C_C_C_C`, ...) are
the same as the old `t.Run` names. These tests keep starting a real actor system, as the spec asks, but they
follow the go-specs conventions:

- `startEngineG2` replaces `newTestEngine` and stops the engine and the actor system with `ctx.Cleanup`.
  The stores come from `connectedEventsStoreG2` and `connectedDurableStoreG2`.
- Assertions are `ctx.Expect(...).To(...)` and `specs.ExpectT`. The repeated "rejected with
  concurrency_conflict" block is `expectConcurrencyConflictG2`, and the `errors.As` step is
  `conflictErrorG2`.
- The two genesis races run each dispatch in `ctx.Go`. The old code called `require` from a bare
  goroutine, which cannot fail a test safely. Their outcome check is one
  `ContainTheSameElementsAs(success, rejected)` instead of two counters.
- The batch harness waited on a channel with `select` and `time.After`. It is now `receiveG2`, which
  polls the channel with `ctx.Eventually` (10 s ceiling, 1 ms interval). It is split into `startBatchSteps`
  and `collectBatchResults` so the external-writer case no longer repeats the dispatch code by hand.
- `preconditionSpyEventsStore` gained `recorded()`, which copies the recorded preconditions under its lock.
  The old tests read the slice without it.

## What does not change, and why

- **Production code.** Nothing outside `engine/*_test.go` changes.
- **`dispatchWithMetadata(t *testing.T, ...)`** stays, now without testify (`t.Fatalf`). `e2e_legacy_compat_test.go`
  belongs to another group and still calls it. It goes away with that file. The decision to keep it was
  made over renaming it, which would have broken the other group's file.
- **`helper_test.go`.** It is shared and still uses testify. The final cleanup removes it.
- **The real actor system.** Replacing it with a mock is not possible: goakt's `ReceiveContext` has
  unexported fields, so an actor's `Receive` cannot be driven without a system.
- **No mocks.** These tests use the in-memory `testkit` stores, which the conventions allow because they
  check behavior on top of the store, not the calls made to it. The recording spy wraps a real store.

## Constraints

- Top-level test names and cases are identical. The case count per `Test` does not drop.
- No `-race` and no workbench. TDD is strict. The runner is `go test ./engine/`.
- Coverage of `engine` must not drop below 94.8%.
- Release note: NONE.

## Tasks

- [x] T1 Helpers plus the two durable state files. Route: inline (writer boundary: 3 files, one
      mechanical pattern). Evidence: `5569595`. RED mutation: `durablestate` ignored the declared
      revision (`PreconditionFromRevision(revision, hasRevision && false)`); 7 of the 9 durable tests
      failed, among them `TestDurableStateExpectedRevisionEndToEndPropagation`.
- [x] T2 The two event sourced files. Route: inline. Evidence: `cbf21b2`. RED mutation: the same
      change in `eventsource`; 7 tests failed, among them
      `TestEventSourcedIntegrationStaleRevisionRejectedStoreUnchanged` and
      `TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit`.
- [x] T3 The batch precondition matrix. Route: inline. Evidence: `e8f5858`. RED mutation:
      `if !entity.batchHasPrecondition || true` in `eventsource`; all 8 conditional matrix rows
      and the 4 other batch tests failed.
- [x] T4 Verify and deliver: vet, lint, gofmt, `-count=5`, full package with coverage, push, PR, CI.
      Evidence: see Progress.

## Follow-up

None for this group. The final cleanup for #252 deletes `helper_test.go` and `dispatchWithMetadata` once
every group has moved.

## Progress

- Before: 37 `--- PASS` lines in the filtered run (28 top-level plus the 9 matrix rows); `engine` coverage
  94.8%, 59.8 s.
- After: 64 `--- PASS` lines (28 top-level, 27 `It` cases, 9 matrix rows; go-specs adds the `Describe`
  segment to every name); top-level names identical; `engine` coverage 94.8%, 60.0 s.
- `go build ./...`, `go vet ./engine/`, `golangci-lint run ./engine/...` and `gofmt -l engine` are clean.
  `go test -count=5` over the 28 tests passes in 0.6 s.
