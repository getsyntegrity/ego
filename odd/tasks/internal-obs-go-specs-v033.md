# internal observability tests on go-specs v0.3.3 (#235)

## Problem

PR #235 (branch `test/205-migrate-internal-obs`) moved the unit tests of `internal/instrumentation`,
`internal/logging` and `projectionrunner/option_test.go` to go-specs v0.3.1. They still use the older style:

- `instrumentation_test.go` checks collections with `len(x) ToEqual`, `== nil` plus `BeTrue`, and a manual
  sort before `Equal` (`TestSendCommandSpan`, `TestInstallPropagator`). `TestRecordingMethods` collects
  offenders in a side slice, and `findSpan` calls `t.Fatalf`, which bypasses the spec.
- `TestShardRecordsTheGaugesWithProjectionAttributes` loops over cases by hand instead of using a table.
- `logging_test.go` registers teardown with `ctx.T.Cleanup`, not `ctx.Cleanup`.
- `option_test.go` repeats six near-identical option cases and checks nil and length with `BeTrue`.

The conventions (`docs/testing/go-specs.md`) now recommend `specs.Table`, the specific matchers and
`ctx.Cleanup`.

## What changes

Only `*_test.go` files in the three packages change.

- `instrumentation_test.go`: `BeNil` / `Not(BeNil())`, `HaveLen`, `BeEmpty`, `BeGreaterThanOrEqual`,
  `ContainTheSameElementsAs` for the propagator fields, `EveryElement` + `Project` for "no attributes",
  `HaveElementsInOrder` + `Project` for recorded values, `specs.Table` for the shard gauges, `ctx.Cleanup`.
  `findSpan` (with `t.Fatalf`) becomes `spansNamed` plus a `HaveLen(1)` expectation.
- `logging_test.go`: teardown moves to `ctx.Cleanup`.
- `option_test.go`: the six option cases become one `specs.Table`; nil and length checks use matchers.

## What does not change, and why

- **Production code.** Nothing outside `*_test.go` changes.
- **`fakeMeter` and its instrument fakes.** They are not call recorders standing in for an external
  service. They implement the OpenTelemetry `Meter` with real behavior (a catalog and per-name
  measurements) and the tests assert on that state. A `mock.Controller` would only restate the same data
  as expectations, so they stay.
- **`TestInstrumentationStaysRuntimeNeutral`, `TestLoggingStaysRuntimeNeutral`.** They run `go list -deps`
  as a subprocess and scan the dependency graph. That makes them architecture tests, out of phase for the
  unit lane, so their testify assertions stay.
- **Logger identity checks in `logging_test.go`** (`kitlog.L() == DefaultLogger()`). Pointer identity is
  the point (the logger must be looked up per call, never cached); `ToEqual` would compare deeply and be
  weaker. `BeTrue` on `==` stays.
- **`TestSendCommandSpan` ordering.** The sort by status is removed. The in-memory syncer exports spans in
  the order they end, which the test controls, so the order is deterministic.
- **`runner_test.go`** is untouched (its own PR; it is timing dependent).

## Constraints

- Every case and invariant stays. Subtest names stay. The count per `Test` does not drop.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. The runner is `go test ./internal/instrumentation/... ./internal/logging/... ./internal/projectionrunner/...`.
  RED is shown per task by a deliberate production mutation that the rewritten cases catch, then reverted.
- Planning heuristic: about 400 authored changed lines per task; every task here is far below it.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3). Route: inline. Evidence: merge commit `71b913e`; clean
      build; `go.mod` already on v0.3.3.
- [x] T2 `instrumentation_test.go`: matchers, a table for the shard gauges, `ctx.Cleanup`, no `t.Fatalf`.
      Route: delegated writer. Evidence: `57b6cd1`. RED mutations caught: `EntityStopped` adding `+1`
      (`HaveElementsInOrder: 1 of 2 positions failed ... value: expected 1 to equal -1`) and the shard
      attribute key renamed to `shard_id` (`Set.data[1].Key: expected "shard", actual "shard_id"`).
- [x] T3 `logging_test.go`: teardown through `ctx.Cleanup`. Route: same writer. Evidence: `8f4bf02`.
      RED mutation: `ResolveLogger` checking `logger == nil` instead of `isNilLogger` failed
      `typed_nil_falls_back_to_the_default` (`expected true, got false`).
- [x] T4 `option_test.go`: option cases as a table; `BeNil`, `HaveLen`. Route: same writer. Evidence:
      `e4d3336`. RED mutations caught: `maxBufferSize = bufferSize + 1` (`expected 6 to equal 5`) and
      `WithEventAdapters(nil)` storing an empty slice (`expected nil, got []`).
- [x] T5 Verify and deliver. Route: inline. Evidence: 90 `--- PASS` before and after, names identical.
      Coverage: `internal/instrumentation` 96.6% -> 96.6%, `internal/logging` 100% -> 100%,
      `internal/projectionrunner` 92.6% -> 93.0% (the package also holds the untouched, timing dependent
      `runner_test.go`). `go build ./...`, `go vet`, `golangci-lint` (0 issues), `gofmt -l` and
      `go test -count=5` are clean.

## Follow-up spec (not in this document)

None needed for these files. `runner_test.go` (real-time waits in the runner tests) belongs to its own
migration PR.

## Progress

- 2026-09-30: T1-T5 done. Engram mirror: not written by the writer; the parent saves it.
