# compose/goakt tests on go-specs v0.3.3 (#233)

## Problem

PR #233 (branch `test/205-migrate-compose-goakt`) moved the pure unit tests of `compose/goakt` to go-specs
v0.3.1. They still have these problems:

- The migrated `New`/`Start`/`Stop` tests check publisher closes and store pings through hand-written
  counters (`eventPublisher.closed`, `pingCountingEventsStore.pings`) and read them with
  `ctx.Expect(x.Load()).ToEqual(int32(n))`. That is a recording fake standing in for a dependency.
- `errText` exists only to dodge a nil error in a text expectation.
- `ve.Rule` / `ve.Field` are checked with two separate `ToEqual` lines, in six places.
- `TestNew_ReportsEveryProblem` builds its cases with a loop and `TestNew_G2_ActorSystemName` is a raw
  `t.Run` loop with `t.Fatalf`.

The conventions (`docs/testing/go-specs.md`) require go-specs `mock.Controller` for replaced dependencies, and
recommend `specs.Table` and the specific matchers.

## What changes

Only `compose/goakt/app_test.go` changes.

- The migrated tests get a small `mockedFixture`. Its publishers and its events store `Ping` are forwarded to a
  `mock.Controller`, and each case declares the number of `Close`/`Ping` calls with `Times(n)` or `Never()`.
- A `validationError(rule, field)` matcher built with `specs.Project` replaces the repeated
  `MatchErrorAs` + `Rule` + `Field` lines. `errText` goes away.
- `TestNew_ReportsEveryProblem` and `TestNew_G2_ActorSystemName` use `specs.Table`.

## What does not change, and why

- **Production code.**
- **`fixtures_test.go` and the recording fakes in it.** The tests that start the actor system need real
  channels and counters that the fakes carry (a publisher that records what it receives and when it closed).
  They are not replaced dependencies of the migrated tests.
- **The tests that start a real GoAkt actor system** (`TestApp_ValidSpecRunsAnEngine`,
  `TestStart_FailureAtEachStepReleasesEverything`, `TestStop_*`, `TestStart_Publisher*`,
  `TestStart_ActorSystemStepFailsForReal`, `TestEngine_UndeclaredFamilyReturnsTypedError` and the three
  `runtime_test.go` tests that Start). The conventions list an actor system as an external resource, so these are
  component tests, not unit tests. The #233 PR left them as they were and so does this rework. They are the
  follow-up below.
- **`TestRuntime_NilBeforeStart`'s `rt == nil` with `BeTrue`.** go-specs `BeNil` also matches a typed nil inside an
  interface, and that is exactly the bug the test exists to catch.

## Constraints

- Every case and invariant stays. Subtest names stay, except the `TestNew_G2_ActorSystemName` row for the empty
  name (`specs.Table` refuses an empty row name, so it is called `empty name`). The case count per `Test` does not
  drop.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. The runner is `go test ./compose/goakt`. RED is shown per task by a deliberate production
  mutation the rewritten cases must catch, then reverted.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge commit `ec1f651`; no conflicts; the
      module already pins go-specs v0.3.3; `go build ./...` is clean.
- [x] T2 `app_test.go`: `mockedFixture` (publishers and events-store `Ping` through `mock.Controller`), the
      `validationError` matcher, `errText` removed, `specs.Table` for `TestNew_ReportsEveryProblem` and
      `TestNew_G2_ActorSystemName`. Route: inline (one test file, already understood). Evidence: `2787ee8`.
      Three RED mutations were caught:
      1. `releasePublishers` skipped the state publishers. The probe-failure, cancelled-context and
         never-started cases failed with `mock: unmet expectation states-pub.Close(any value) ... want 1, got 0`.
      2. `probeStores` skipped every `Ping`. The probe-failure case failed with
         `mock: unexpected call EventsStore.Ping(...)` and `expected an error assignable to **compose.StartError,
         got a nil error`.
      3. V7 reported as `V7x`, and G2 reported as `G3` for an empty name. They failed with
         `All: #2: "Rule: expected V7x to equal V7"` and, on the row `empty name`, `expected false to equal true`.
      The recording fakes in `fixtures_test.go` stay for the component tests listed above. A mock expectation's
      "declared at" line points to the `expectClose` helper, not to the case; this is accepted for the shorter
      cases.
- [x] T3 Verify and deliver. Route: inline. Evidence: `go build ./...`, `go vet`, `golangci-lint` (0 issues)
      and `gofmt -l` are clean; `go test -count=5 ./compose/goakt` passes. 55 `--- PASS` before and after; the
      names are identical except the ten `TestNew_G2_ActorSystemName` rows, which gain the `Describe` segment
      (the first row moves from `#00` to `empty name`). Coverage of `compose/goakt` is 87.1% before and after.

## Follow-up spec (not in this document)

`compose-goakt-component-lane`: move the tests that start a real actor system out of the unit lane, or give
the composition root an actor-system seam so they can run on a fake. Both need either a production change or a
decision about the component lane, so neither belongs here.

## Progress

- 2026-09-30: T1-T3 done; branch pushed.
