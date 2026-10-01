# engine tests, group g4, on go-specs v0.3.3 (#252)

## Problem

Nine test files in `engine/` still used testify (`require`, `assert`) after #252: `telemetry_contract_test.go`,
`engine_lifecycle_fixes_test.go`, `engine_entity_family_test.go`, `projection_actor_test.go`,
`saga_status_test.go`, `engine_neutral_cluster_test.go`, `e2e_legacy_compat_test.go`,
`logger_architecture_test.go` and `engine_spawn_test.go`. The `engine/` package is split across four writers;
this document is group g4. Besides testify, these files used hand-written counting fakes for publishers, plain
`t.Run` loops, `require.Eventually` and a channel with a fixed timeout.

## What changes

Only the nine files above change, plus one new file, `engine/specs_helpers_g4_test.go`, with go-specs versions
of helpers that today live in testify files owned by other groups (`dispatchWithMetadataG4`) and three small
store helpers that connect a `testkit` store and disconnect it with `ctx.Cleanup`.

- Every `Test` keeps its top-level name and now holds one `specs.Describe`.
- The publisher fakes in `engine_lifecycle_fixes_test.go` (`failingCloseEventPublisher`, `countingStatePublisher`,
  `countingEventPublisher`) become `mock.Controller` adapters from `engine/specs_mocks_test.go`. "Closed exactly
  once" is a `Times(1)` expectation, and "never closed" is the absence of a `Close` expectation, which the
  controller reports as an unexpected call. No new adapter was needed, so no `specs_mocks_g4_test.go` exists.
- `require.Eventually` becomes `ctx.Eventually`. The saga "has not finished" case no longer waits on a channel
  with a 10 s `select`: it polls an `atomic.Bool`.
- Near-identical cases became `specs.Table` rows: the entity-family spawn matrix (4 tests, one row per entry
  point), the legacy-compat actor types, the saga wire statuses, the domain-only cluster spawns, the
  events/states publisher kinds and the no-events-store spawns.
- `logger_architecture_test.go` collects violations and checks `BeEmpty()`, so a failure lists every offending
  path and construct instead of stopping at the first.
- `telemetry_contract_test.go` compares the instrument creation counts as one map, so a failure names the
  instrument.

## What does not change, and why

- **No lane move.** The tests that start a real goakt actor system or cluster still do: `engine_lifecycle_fixes`
  and `projection_actor` stay in `.github/unit-test-gate-resources.txt`, and the others reach the system through
  `newTestEngine`/`newTestCluster` in `helper_test.go` and `engine_test.go`, which this group does not edit.
- **`flakyEventsStore` and the recording meter** stay. They are decorators with behavior, not recorders of calls.
- **`.github/unit-test-gate-pending.txt`** is not edited, as agreed: the coordinator removes the stale lines at
  the end.
- **No production code.**

## Constraints

- Case count: the top-level `Test` names are identical before and after (546 `--- PASS` lines before, 571
  after, because tables and the extra `Describe` segment add subtests). Subtest names move one level down.
- The cluster test now starts one two-node cluster per case instead of sharing one, because a go-specs case
  owns its cleanup. The test takes about 6 s instead of 1.5 s.
- Strict TDD, runner `go test ./engine/`. No `-race`, no workbench. Release note: NONE.

## Tasks

- [x] T1 Spawn, legacy compat, entity family and the g4 helpers. Route: inline (the files were read in full).
      Evidence: `63fad46`. RED: `entities.go` not-started guard returned nil (spawn), `PreconditionFromRevision`
      genesis branch disabled (legacy compat), family guard always true (entity family).
- [x] T2 Lifecycle fixes and saga status. Route: inline. Evidence: `1c00b27`. RED: `Engine.Stop` returned on the
      first publisher error, duplicate check ignoring registered IDs, no-events-store check removed, the saga
      completed status set to running, the wire COMPENSATING mapping changed.
- [x] T3 Neutral cluster and projection actor. Route: inline. Evidence: `b699191`. RED: placement cause changed to
      `ErrBehaviorNotPointer`, projection runner error directive changed from stop to resume.
- [x] T4 Telemetry contract and logger architecture. Route: inline. Evidence: `9d03087`. RED: one instrument
      description changed, a banned `fmt.Println(` added to `engine/engine.go`; the stream-close mutation is
      caught by `TestWithEventStream_UsesTheGivenStream`.
- [x] T5 Verify and deliver. Route: inline. Evidence: below.

Every RED mutation was applied to production code, run, and reverted (`git status` shows test files only).

## Verification

- `go build ./...`, `go vet ./engine/`, `gofmt -l engine` and `golangci-lint run ./engine/` are clean.
- `go test ./engine/ -count=5` on the 16 top-level tests of the group passes.
- Full package: `ok ... 64.5s coverage: 94.8% of statements`, the same 94.8% as before.
- Unit-test gate: no violation in the nine files. It reports one failure on `develop` in a file outside this
  group (`internal/engine/eventsource/event_sourced_actor_rig_test.go`).

## Follow-up

Folding the test-local adapters in `engine/specs_mocks_test.go` and `internal/engine/enginetest` into one set
(see `engine-rest-go-specs-v033`).
