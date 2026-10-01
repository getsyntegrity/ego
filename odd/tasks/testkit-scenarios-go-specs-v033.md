# testkit concurrency, conformance, scenario and scope tests on go-specs v0.3.3 (#225)

## Problem

PR #225 (branch `test/205-migrate-testkit-scenarios`) moved four test files in `testkit/` to go-specs
v0.3.1: `concurrency_test.go`, `conformance_test.go`, `scope_test.go` and `scenario_test.go`. They still
have the pre-v0.3.3 habits that the conventions (#203) and the reworked precedent (#229) remove:

- Helpers that call `t.Fatalf` (`markedEvent`, `eventMarker`, `markedState`, `stateMarker`,
  `countOutcomes`) and payload setup that calls `t.Fatalf` in `scope_test.go`. Their failures bypass the spec.
- Hand-written collection checks: an "offenders" loop in `TestStoresAdapterConformance`, a failed-names loop
  in `assertSuiteDetectedNonIsolation`, and counters `successes`/`conflicts` instead of one matcher.
- Weak assertions: `len(x) ToEqual`, `len(x) > 0` with `BeTrue`, `Contain` on `err.Error()`.
- `for ... s.It(...)` loops that are plain `specs.Table` cases, one of them over a map, so its order was random.

## What changes

Only `testkit/*_test.go` files change.

- `concurrency_test.go`: the payload helpers take a `*specs.Context` and fail through it. The race result is
  checked with one matcher (`ContainTheSameElementsAs` over "committed"/"conflict"), so a third kind of
  error shows its message. The winner's marker is checked with one expression, not an if/else.
- `conformance_test.go`: the adapter-lifecycle check compares result lines with
  `ContainTheSameElementsAs`; the two loops become `specs.Table`; the non-isolation guard uses
  `Not(BeEmpty())` and `AnyElement(Project(...))`.
- `scope_test.go`: payloads are built inside each case, through the spec, and `len(...)` checks become
  `BeEmpty`/`HaveLen`.
- `scenario_test.go`: the arrangement-failure loop becomes a `specs.Table`, and the error message check
  uses `Project`.

## What does not change, and why

- **Production code.** Nothing outside `*_test.go` is touched.
- **No `mock.Controller`.** These tests use the real in-memory stores. `recordingTB` is a recorder of
  assertion failures that is itself the thing the scenario assertions are run against, and the
  non-isolating store wrappers are deliberately broken inputs with real behavior. None of them stands in
  for an external dependency.
- **No `Eventually`/`Consistently`.** There are no sleeps or fixed waits. `raceTwoWriters` and
  `recordFailure` join their goroutines, and those goroutines never touch a spec context, so they stay.
- **Race tests are not merged into a table.** The five T8/T9/T10 tests have distinct `Test` names and
  different store types; merging them would rename subtests.
- **`conformance.Run*Conformance` and `adaptertest.Run`** take a `*testing.T`, so they still receive
  `ctx.T`.

## Constraints

- Every case and subtest name stays: 210 `--- PASS` before and after, names identical.
- No force-push (`develop` is merged). No `-race`. No workbench. TDD strict: runner `go test ./testkit/`,
  RED is a deliberate production mutation per task, reverted afterward.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3 already in `go.mod`; no nested module touched). Route:
      inline. Evidence: merge commit `75c1ca9`, clean build.
- [x] T2 `concurrency_test.go`. Route: inline (one file). Evidence: `137da74`. RED: the genesis loser in
      `EventStore.writeConditional` returned a plain error; failure was
      `expected [unexpected error: boom committed] to contain the same elements as [committed conflict]`.
- [x] T3 `conformance_test.go`. Route: inline. Evidence: `846f8d3`. RED: a wrong port on the EventStore
      descriptor failed the AT-1 row; a wrong descriptor name failed
      `Descriptor.Name: expected "testkit-memory", actual "x"`.
- [x] T4 `scope_test.go` and `scenario_test.go`. Route: inline. Evidence: `3175bb7`, `c06f928`. RED: removing
      the scope guard from `DeleteEvents` and `PersistenceIDs` failed `expected an error matching
      persistence: scope is not valid ..., got a nil error`; changing the arrangement error text and
      dropping `requireArranged` failed with `expected given events broke: unhandled event to contain
      given events could not be applied` and `expected true, got false`.
- [x] T5 Verify and deliver. Route: inline. Evidence: 210 `--- PASS` before and after, names identical.
      Coverage of `testkit` is 94.3% before and after (it varies 93.8-95.0% between runs on both sides,
      because racing writers take different branches). `go build ./...`, vet, golangci-lint (0 issues),
      gofmt and `go test -count=5 ./testkit` are clean.

## Follow-up

None needed for this spec.

## Progress

- 2026-09-30: T1-T5 done. Engram mirror: pending.
