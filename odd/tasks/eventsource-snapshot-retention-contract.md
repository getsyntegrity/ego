# Feature: snapshot and retention contract tests for the eventsource package

Branch: `test/eventsource-snapshot-retention-contract` · Base: `origin/main` `9440b11`
Scope: tests only. No production file changes.

## Problem

PR #194 added contract and sequence tests for the snapshot path, then was closed as superseded by
#195 (the split of `engine` into `internal/...` packages). Comparing #194's tests with what main
has after #195 shows that main lost several invariants nobody else pins: the order of store calls
around a snapshot, the tenant scope on snapshot writes and retention deletes, the retry count of a
failing snapshot write, and what happens on encryption failure. Without them a refactor could pass
`Unscoped()` to the snapshot store, or delete events before the snapshot exists, and stay green.

## What changes

Characterization tests that pin what main does today. Each test passes on main as written.

- `internal/engine/eventsource/snapshots_sequence_test.go` (entity level, real `Actor`):
  1. events write, then snapshot write, then `DeleteEvents(2)` and `DeleteSnapshots(1)` after two
     commands with `SnapshotInterval: 1` and a delete-everything retention policy;
  2. a failed events write replies an error and never writes a snapshot nor deletes;
  3. with tenant `acme`, every events write, snapshot write and retention delete carries
     `persistence.NewTenantScope("acme")`, never `Unscoped()`.
- `internal/engine/eventsource/snapshots_writer_contract_test.go` (writer plus janitor, no entity):
  a. a non-`Unscoped` scope reaches `WriteSnapshot`, `DeleteEvents` and `DeleteSnapshots` unchanged;
  b. the snapshot write precedes both deletes, and the previous snapshot is `eventsCounter -
     snapshotInterval`;
  c. a failing `WriteSnapshot` is attempted `defaultMaxRetries+1` times, then no delete happens;
  d. an encryption failure writes nothing and forwards no retention;
  e. a nil retention request writes the snapshot and triggers no deletes.
- `internal/extensions/lookup_test.go`: both wrong-type cases now also assert the message contains
  `was registered with unexpected type`.

Both new files share one ordered call log (`storeCalls`) that wraps the events and snapshot stores.

## Deliberately not ported

- `snapshots.Tell`, `snapshots.Retention` and the package-location assertions from #194: those
  types no longer exist on main. The actors moved into `eventsource` and the request structs are
  private, so asserting their placement would pin an accident of the old layout.

## Semantic differences from #194

None. Main produces the same sequence numbers #194 expected (`DeleteEvents(2)`,
`DeleteSnapshots(1)` at the entity level; `DeleteEvents(4)`, `DeleteSnapshots(2)` at the writer
level).

## TDD note

Strict TDD is on, but these are characterization tests of existing behavior, so there is no RED
before implementation. The evidence that they can fail is mutation: each invariant was broken in
production code, the test was seen failing, and the change was reverted.

| Mutation | Result |
|---|---|
| entity passes `persistence.Unscoped()` in `snapshotAndRetain` | tenant test fails: WriteSnapshot, DeleteEvents, DeleteSnapshots "must receive the tenant scope" |
| writer calls `WriteSnapshot` with `Unscoped()` | entity tenant test and writer scope test fail |
| writer forwards retention before `WriteSnapshot` | writer order test and failed-write test fail |
| writer retries with 0 retries | retry-count test fails: "Condition never satisfied" |

The order test uses a 150 ms delay inside the snapshot store's `WriteSnapshot` (before it records
the call), so a retention forwarded too early is observed first. On main the order is guaranteed by
the actor code, so the delay does not make the test flaky.

## Verification

Recorded in the commit report: `go build ./...`, `go vet ./internal/...`, `gofmt -l internal`,
`go test -count=1 ./internal/engine/eventsource/ ./internal/extensions/`, five repeated runs of the
new tests, `go run ./internal/cmd/archcheck`, `golangci-lint run ./internal/...`, and an empty
production diff against `origin/main`.

## Next step

Open a PR from this branch against `main`. Nothing is pushed yet.
