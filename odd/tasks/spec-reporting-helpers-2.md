# Report test failures through go-specs, part 2 (follow-up of #258)

## Problem

Unit tests report failures through go-specs (`ctx.Expect(...).To(matcher)`), not through raw `t.Fatalf` or
`t.Errorf`. #258 cleaned the files on the original list and named the ones it had not covered. This change
handles those: `compose/goakt` (app, runtime and fixtures tests), `publisher/websocket/conformance_test.go`,
and the three files that turned out to need no change.

## What changes

Only `*_test.go` files change.

- `compose/goakt/fixtures_test.go`, `app_test.go`: the helpers `connected`, `newFixture` and `mustNew` took a
  `*testing.T` and called `t.Fatalf`. They now take a `*specs.Context` and assert with
  `ctx.Expect(err).To(specs.BeNil())`. Their callers in `cluster_test.go` pass `ctx` instead of `ctx.T`.
- `compose/goakt/app_test.go` and `runtime_test.go`: eleven plain `Test` functions (about 70 raw reporting
  sites) now run under `specs.Describe`, with one `It` each. `TestStart_FailureAtEachStepReleasesEverything`
  keeps its per-step subtests, one `It` per step under an unnamed `Describe`, so its subtest names are unchanged.
  The single `ctx := context.Background()` of those tests is renamed `bg`, because `ctx` is now the spec context.
  Timeouts on channel receives became "receive or leave nil, then assert not nil".
- `publisher/websocket/conformance_test.go`: `requireAdapterOutcomes` and `requirePublishingOutcomes` took
  `*testing.T` and called `t.Errorf`. They now run a `specs.Describe` with one `It` that asserts the result
  count and each outcome. The harness calls (`adaptertest.Run`, `publishingtest.RunEvents`, `RunState`) stay
  outside it, so their subtest names do not move.

## What does not change, and why (kept call sites)

- `compose/goakt`, `mustNew` and `connected`: the teardown is registered with `ctx.T.Cleanup`, not
  `ctx.Cleanup`. `cluster_test.go` builds the two nodes inside a `BeforeAll` hook. `ctx.Cleanup` would run
  when that hook ends and stop the cluster before the cases. The Describe's `T` keeps the old lifetime. Teardown
  is category (a): no spec step is left to report through.
- `t.Logf` diagnostics (`step 2 failed with`, `D7 observed`, the outcome detail in websocket): not failures.
- `adaptertest.Run(t, ...)`, `publishingtest.RunEvents(t, ...)` and their `New(*testing.T)` and `Received`
  callbacks: harness APIs that take `*testing.T` by design.
- `internal/engine/protocol/expected_revision_test.go` (1 site): `FuzzPreconditionFromRevision`, a fuzz target
  whose `*testing.T` comes from `f.Fuzz`. Its `Test` function is already inside `specs.Describe`.
- `benchmark/benchmark_test.go` (23 sites): the file contains only `Benchmark*` functions and helpers taking
  `*testing.B`. `b.Fatalf` is category (a). There is no `Test*` to convert.
- `example/cluster/stores_postgres_test.go` (2 sites, `countOutcomes` and `waitForLockWaiters`): the whole file
  is gated by `EGO_EXAMPLE_POSTGRES_DSN` and skips without it. It needs a real database, so it is not a unit
  test (out of the unit phase). Left as is.
- Rejected: moving teardown to `ctx.Cleanup` (see above), and wrapping the harness calls inside the `It`
  (it would add path segments to existing PASS names).
- Production code, `engine/`, `internal/engine/{saga,eventsource,durablestate}` are untouched.
- Lost detail: assertion failures no longer carry the old hand-written message. go-specs reports the value
  and the line instead.

## Constraints

Every case stays and no `--- PASS` name is removed (compared before and after with `go test -v`). Added names are
the `Describe`/`It` segments of the converted plain tests and the one new `It` per websocket conformance test.
Top-level `Test` names are unchanged. No `-race`, no workbench, no external resource.
TDD runner: `go test` from the repo root for `compose/goakt`, from `publisher/websocket` for the nested module.

## Tasks

- [x] T1 `compose/goakt` helpers and plain tests to go-specs. Route: inline (one python pass plus hand-written
  bodies). RED: `ErrNotStartable` replaced by another error in `app.go` fails
  `TestStop_AfterStopIsNoOp` (`expected error mutated (stopped) ... to match ...: app cannot be started`) and
  `TestRuntime_NilAfterFailedStart`; reverted. GREEN: `go test -count=5`. Commit `da08da1`.
- [x] T2 websocket conformance outcomes. Route: inline. RED: marking `AT-1` as `NotExercised` in
  `wantAdapterOutcomes` fails `TestEventsPublisherAdapterConformance` through the `It`; reverted. Commit `a8cd1cb`.
- [x] T3 classify the rest and record this document. Route: inline. Commit: the one that adds this file.

## Follow-up

None. `example/cluster/stores_postgres_test.go` stays raw until that file moves to the integration lane.

## Progress

Coverage before and after (identical): compose/goakt 87.1%, publisher/websocket 87.9%. `go test -count=5`
passes on both. `golangci-lint` reports 0 issues on both, `gofmt -l` is empty, `go build ./...` and `go vet`
are clean.
