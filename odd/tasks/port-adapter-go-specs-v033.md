# port/adapter tests on go-specs v0.3.3 (#229)

## Problem

PR #229 (branch `test/205-migrate-port-adapter`) moved the unit tests of `port/adapter` and
`port/adapter/adaptertest` to go-specs v0.3.1. The tests still have these problems:

- `adapter_test.go` checks lifecycle calls through a hand-written recording fake. `full` counts `started`
  and `pinged`, and the tests assert `f.started == 1`.
- The harness tests (`adaptertest_test.go`) assert through `requireOutcome` and `requireOnlyFailure`,
  which call `t.Fatalf` and bypass the spec. They also loop over results by hand.
- Many near-identical single-`It` tests (`Capture_*FailsAT1`, `*FailsAT3`) could be table rows.
- `len(...) ToEqual`, `== nil` with `BeTrue`, and `strings.Contains` are used where a v0.3.3 matcher says more.

The #219 conventions now require go-specs `mock.Controller` for replaced dependencies. They also recommend
`specs.Table` and the specific matchers.

## What changes

Only the `*_test.go` files under `port/adapter/` and `port/adapter/adaptertest/` change.

- `full` becomes a `mock.Controller` adapter for `Describer`/`Starter`/`Pinger`. `Start`/`Ping` are verified
  through `Expect(...).Times(1)`, not through counters. `undeclared` and `pingOnly` stay: they are type-shape
  fixtures the accessors are tested against.
- In `adaptertest_test.go`, the fakes stay (`owned`, `starter`, `store`, `lazyBlock`, `failedCloser`). They
  are the inputs of a conformance test: deliberately broken adapters with real behavior, not recorders
  standing in for an external dependency. A counter kept on a fake only to record calls moves to a
  `mock.Spy` or `mock.Controller`.
- `requireOutcome`/`requireOnlyFailure` become matchers: `HaveKey`/`HavePair`, `ExactlyNElements` and
  `EveryElement`/`Project`. They no longer call `t.Fatalf`.
- Tables: the accessor loops, the declares/serves loops, `OnlyErrUnreachableSkips`, the `Capture_*Fails*`
  family (as far as it stays readable), `TargetCapabilitiesAreCheckedBothWays`, `InvalidTargetFails` and
  the implied-ready loops.
- `publisher/websocket/go.sum` carries v0.3.3 sums. The v0.3.1 lines in the PR were wrong after #242.

## What does not change, and why

- **The `adaptertest` harness API and production code.** `Run`/`Capture`/`Target` take `*testing.T` and
  are used by `publisher/websocket/conformance_test.go` and `testkit/conformance_test.go`. The package is
  standard-library-only by design (`adaptertest/architecture_test.go:43`), so it cannot import go-specs.
- **Architecture tests** (`assertion_sites_test.go`'s repository scans, `adapter_architecture_test.go`,
  `adaptertest/architecture_test.go`). They read source from disk or run `go list`, so they are not unit
  tests. They stay out of phase.
- **AT-4's real-time wait** (`stallDeadline` 200 ms and `grace` 1 s in `adaptertest.go:152-154`) makes
  `TestCapture_CloseIgnoringTheDeadlineFailsAT4` wait about 1.2 s. Removing it needs a clock inside the
  harness, which is a production change. That work is the follow-up below.

## Constraints

- Every case and invariant stays. Subtest names stay. The case count per `Test` does not drop (the PR
  reports 62 cases).
- No force-push: `develop` is merged into the branch. No `-race` (it has not been requested for this PR).
  No workbench.
- TDD is strict. The test runner is `go test ./port/adapter/...`. RED is shown per task by a deliberate
  mutation that the rewritten cases must catch, and the mutation is then reverted.
- Delivery: push to `test/205-migrate-port-adapter` and update the #229 description.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3) and fix the websocket sums. Route: inline. Evidence: the merge
      commit, `go mod tidy` in `publisher/websocket`, and a clean build.
- [x] T2 `adapter_test.go`: `full` becomes `mock.Controller`, with better matchers and tables. Route:
      delegated writer. Evidence: `e3ba2bc`. RED mutation: `StarterOf` returned nil, and the test failed
      with `unmet expectation Start(any value) ... want 1, got 0`.
- [x] T3 `adaptertest_test.go` and `implied_internal_test.go`: matcher-based helpers, tables and the
      spy/controller for pure counters. Route: the same writer. Evidence: `1c6c981`. No `t.Fatalf` is left.
      Two RED mutations were caught: AT-3 skipping its second Close, and AT-1 accepting an empty name. The
      single-case `Capture_*Fails*` functions stay as they are, because a one-row table adds nothing. The
      `owned.closed`/`calls` fields drive behavior, so they stay on the fake.
- [x] T4 `assertion_sites_test.go` negative control: use `ContainTheSameElementsAs` instead of sort + Equal.
      Route: the same writer. Evidence: `1d03a98`. The RED mutation dropped one detection rule, and the
      failure lists the missing sites.
- [x] T5 Verify and deliver: vet, lint, coverage (`port/adapter` 95.0%, `adaptertest` 94.0%), `-count=5`,
      the native assessment plus an independent verifier if `high`, then push and update the PR. Route: inline.
      Evidence: 111 `--- PASS` before and after, with the names identical. Coverage 95.0% / 94.0% is
      unchanged. `-count=5`, vet, lint and gofmt are clean, and so are `testkit`, `port` and
      `publisher/websocket`. The assessment returned `medium` with RDD off, so the writer's self-verification
      with mutations stands and no separate verifier was run. The parent spot check re-ran the package with
      `-cover`.

## Follow-up spec (not in this document)

`adaptertest-clock`: give the conformance harness an internal clock seam, without changing its public API,
so the AT-4 stall check can run without real time.

## Progress

- 2026-09-30: T1 done. Engram mirror: pending (Engram reports duplicate active sessions).
