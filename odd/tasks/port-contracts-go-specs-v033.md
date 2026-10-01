# port contract tests on go-specs v0.3.3 (#230)

## Problem

PR #230 (branch `test/205-migrate-port-contracts`) moved the unit tests of `port/behavior`,
`port/publishing/publishingtest` and `port/runtime` to go-specs v0.3.1.

Those tests already meet the no-external-resource rule. They use no testify, no `mocks/*`, no waits, and no
network or files. What they still lack is the v0.3.3 style:

- **Helpers that fail outside the spec.** `requireOutcome`, `requireOnlyFailure` and `byCheck` in
  `publishingtest_internal_test.go`, and `spawn()` in `double_test.go`, call `t.Fatalf`, so their failures
  bypass the spec.
- **Hand-written collection checks.** Manual "offenders" loops collect a slice and expect it to be nil.
- **Weak assertions.** `len(...) ToEqual`, `== ""` with `BeTrue`, and `proto.Equal(...)` with `BeTrue`
  appear where a specific matcher says more.
- **Loops instead of tables.** Several `for ... s.It(tc.name, ...)` loops are textbook `specs.Table` cases.

## What changes

Only the `*_test.go` files in the three packages change.

- **Helpers become matchers.** The helpers are replaced by matcher-based assertions: `HaveKey`/`HavePair`,
  `Project`, `EveryElement`/`NoElement`, `Contain` (which also matches strings) and `HaveLen`/`BeEmpty`.
  `spawn()` asserts through the spec.
- **Loops become tables.** These move to `specs.Table`, keeping the existing case names:
  - `TestDoubleUnsupportedOperations`, 12 rows
  - `TestSentinelMessagesAreKept`
  - `TestSagaStatusString`, with names built as `name + " reads " + want`
  - `TestWithAdapterSettingPanicsAtBuildTime`
  - `TestCapture_OnlyUnreachableSkips`
- **The panics helper.** If go-specs v0.3.3 has a panic matcher, it replaces the `panics()` helper;
  otherwise the helper stays and reports through the spec.
- **Websocket sums.** `publisher/websocket/go.sum` carries v0.3.3 sums.

## What does not change, and why

- **Fakes that are not dependencies.** The `double` runtime in `port/runtime/double_test.go` is the
  implementation under test. It proves that the `runtime.Runtime` contract is implementable without goakt,
  and nothing outside that file uses it. The `publishingtest` fakes (`fakeEvents`, `fakeState`) are
  deliberately broken conformance inputs. The `behavior_test.go` types are compile-time conformance stubs.
  None of these stand in for a dependency, so none become `mock.Controller`.
- **The public `publishingtest` API and all production code.** `RunEvents`/`RunState` take `*testing.T`
  and are called by `publisher/websocket/conformance_test.go`. The package is dependency-restricted by its
  architecture test.
- **Architecture tests** (`go list`) stay out of phase.
- **Single-`It` cases are not split.** Splitting a single-`It` case with several assertions into table
  rows would rename its subtest. This applies to `TestEnumValuesAreUnchanged` and the behavior
  conformance `It`.

## Constraints

- Every case, invariant and subtest name stays. The PR lists 44 cases. The case count per Test function
  does not drop.
- No force-push. `develop` is merged into the branch.
- No `-race` (not requested for this PR) and no workbench.
- TDD is strict. The runner is `go test ./port/behavior/... ./port/publishing/... ./port/runtime/...`. RED
  is a deliberate production mutation per task, caught and then reverted.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3) and refresh the websocket sums. Route: inline.
- [x] T2 `publishingtest`: replace the helpers with matchers, fold the offenders loops into quantified
      matchers, and turn `OnlyUnreachableSkips` into a table. Route: delegated writer. Evidence: `5ca981a`.
- [x] T3 `port/runtime` (`double`, `errors`, `saga`, `spawn`): add the tables, better matchers and a
      spec-reporting `spawn()`/`panics()`. Route: a writer. The first writer was stopped by the user partway
      through T3, and a fresh writer resumed from its uncommitted `double_test.go` edits. Evidence:
      `60e6634`. RED: changing the `ErrUndefinedEntityID` message in `errors.go` failed the sentinel row.
      go-specs v0.3.3 has no panic matcher, so a local `panicWhenCalled()` matcher replaces `panics()`.
- [x] T4 `port/behavior`: `BeEmpty`/`HaveLen`. Route: the same writer. Evidence: `404c39a`. RED is a fixture
      mutation, because no production code is reachable from this test.
- [x] T5 Verify and deliver. Route: inline. Evidence:
      - **Case names.** The parent compared the sorted `--- PASS` names in a temporary worktree against both
        `dff2086` (before the rework) and `5ca981a` (before T3): 102 and 102, identical.
      - **Coverage.** 87.0% for `publishingtest` and 100% for `port/runtime`, both unchanged.
      - **Tests and lint.** `-count=5`, vet, lint and gofmt are clean. `go test ./port/...` and
        `publisher/websocket` pass.
      - **Assessment.** The assessment returned `medium`, and RDD is off. The writer's self-verification
        with mutations stands, so no separate verifier was run.

## Progress

- 2026-09-30: T1 done. Engram mirror: pending.
