# eventsource events writer, janitor and scope tests on go-specs v0.3.3 (follow-up of #246)

## Problem

PR #246 moves the eventsource actor tests to go-specs. Five test files in
`internal/engine/eventsource` were left out of it and still use testify and the generated
`mocks/persistence` package, and they wait with `pause.For(time.Second)`, `require.Eventually`
or a fixed 500 ms window:

- `event_sourced_actor_scope_test.go` (tenant scope bound at spawn)
- `events_janitor_actor_test.go` (event and snapshot retention)
- `events_write_sequence_test.go` (write, publish, reply order)
- `events_writer_actor_test.go` (the events writer actor)
- `events_writer_contract_test.go` (the writer's contract with the store and the stream)

The standing rule is that no touched file keeps testify, a generated mock, or a fixed wait.

## What changes

Only `*_test.go` files in `internal/engine/eventsource`, plus one new typed adapter in
`internal/engine/enginetest`.

- Every test sits inside `specs.Describe` and `It`. Top-level `Test` names and the case names
  (the last subtest segment) stay the same.
- Stores are `enginetest.EventsStoreMock` and `enginetest.SnapshotStoreMock` on a
  `mock.Controller`. Where a hand-written recorder only counted calls
  (`recordingStore`, `recordingStream`), the case now declares its expectations and the controller
  checks them, including the order of the write and the two publications.
- `enginetest.EventStreamMock` is new: the eventstream port had no typed adapter yet. It is a full
  `eventstream.Stream` adapter on a controller, in `internal/engine/enginetest/event_stream_mock.go`.
- Fixed waits are gone. The actor system is stopped through `ctx.Cleanup`, registered after the
  controller so the system stops before the controller verifies. Success is awaited with
  `ctx.Eventually`; "nothing was published after a failed write" uses a short `ctx.Consistently`.
- The janitor cases are one `specs.Table` of eight rows. Each case ends by sending a second
  "sentinel" request. The janitor handles its mailbox in order, so once the sentinel's delete is
  seen, the case's own request is fully handled, and a delete that should have been skipped would
  already have shown up as an unexpected call. This replaces the 1 s and 3 s sleeps.

## What does not change, and why

- **Production code.** No production file is touched.
- **The files PR #246 owns** (`event_sourced_actor_test.go`, `..._tenant_persist_test.go`,
  `..._rig_test.go`, `retry_test.go`, `clock_test.go`, `enginetest/mocks_test.go`) and the
  `snapshots_*` files, which another writer owns.
- **The real actor system.** These tests keep starting a goakt actor system, as before.
- **`sequenceEventsStore`, `sequenceEventsStream`, `writeSequence`.** They wrap a real in-memory
  store and stream and record the order of observable steps. That order is what the test checks, so a
  controller expectation would say less. They are fakes with behavior, not recorders of an external call.
- **Real time inside the janitor retries.** `eventsJanitorActor` calls `defaultBackoff()` directly,
  so a failing delete still waits the real backoff (about 0.7 s for four attempts). The clock seam from
  #244 only reaches `retryWithBackoff` callers that pass a backoff. The two failure rows therefore wait
  with `ctx.Eventually` (up to 10 s) instead of sleeping. Making the clock injectable is a production
  change, so it is the follow-up below.

## Constraints

- Strict TDD, runner `go test ./internal/engine/eventsource/`. For test-only work RED is a deliberate
  production mutation that the migrated cases must catch, then reverted.
- Unit tests reach no database or network. No `-race`. No workbench.
- Rejected alternative: injecting the clock into the janitor in this PR. It would change production code
  in a PR that is meant to be test-only.

## Tasks

- [x] T1 Add `enginetest.EventStreamMock`. Route: inline (one mechanical file). Evidence: commit
      `test(enginetest): add a typed EventStream mock on mock.Controller`, exercised by T3.
- [x] T2 `events_writer_actor_test.go` plus the shared helpers (system start with cleanup, type
      matcher, envelope builder). Route: inline. Evidence: RED mutation "publish also on a failed
      write" failed `returns_error_in_response_when_store_write_fails`.
- [x] T3 `events_writer_contract_test.go` and `events_write_sequence_test.go`. Route: inline. Evidence:
      mutation "publish before the write" failed the order expectation (`order violation`) and all three
      sequence cases; "writer drops the request scope" failed `the_tenant_scope_of_the_request_reaches_the_store_unchanged`.
- [x] T4 `events_janitor_actor_test.go` as a table with sentinels. Route: inline. Evidence: mutations
      `deleteUpTo > 0` to `>= 0` and `counter > interval` to `>=` each failed their skip row with
      `unexpected call ... 0`.
- [x] T5 `event_sourced_actor_scope_test.go`. Route: inline. Evidence: mutations "recovery read uses
      Unscoped", "missing scope no longer fails closed" and "legacy mode binds a tenant scope" each failed
      their case.

Route note: the five files total about 1,350 lines, but the work was one coherent migration with one
writer, so no further delegation was used.

## Follow-up spec (not in this document)

`eventsource-janitor-clock`: give `eventsJanitorActor` an injectable `backoff`, unexported, so the retry
rows run on a manual clock and the real-time wait disappears.

## Progress

- Verification results (coverage before and after, `-count=5`, lint, CI) are in the PR description.
