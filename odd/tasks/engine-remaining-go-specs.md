# engine unit tests: the plain-`t` tests that are really unit tests, on go-specs v0.3.3

Depends on #241 (`test/205-migrate-engine-rest`, see `odd/tasks/engine-rest-go-specs-v033.md`).

## Problem

PR #241 moved the testify mocks of the `engine` package to go-specs, but it left a long tail of plain `t`
tests that use testify `require` and `assert`, and several tests that wait with `pause.For` or `time.Sleep`.
`docs/testing/go-specs.md` asks for `specs.Describe`/`It`, specific matchers, `specs.Table` where it reads
well, and `ctx.Eventually` instead of fixed waits. It also says a test that starts a real resource (an actor
system, a subprocess) is not a unit test.

Most of the remaining `engine` tests start a real goakt actor system, so they are out of the unit lane. This
spec migrates the ones that are real unit tests, and only replaces the fixed waits in the ones that are not.

## What changes

Only `*_test.go` files in `engine/` change. No production code is touched.

- `engine/option_test.go`: the 23 `TestOption*` tests and `TestClusterKindsExposesEgoActors` are
  `specs.Describe` cases. `assert.Same` became `beTheSamePointer`, `assert.Nil`/`Zero`/`Len` became
  `BeNil`/`BeZero`/`HaveLen`, and the kind-name list uses `HaveElementsInOrder`.
- `engine/logger_test.go`: `TestResolveLogger`, `TestDefaultLoggerIsKitLoggerGlobal` and
  `TestDiscardLoggerDisablesEveryLevel`. The six levels of the last one are now a `specs.Table`, so a failure
  names the level (the old testify message did that, a go-specs expectation takes no message).
- `engine/runtime_compat_test.go`: all five tests. The ten sentinels, the moved types, the moved constants and
  the four adapter settings are `specs.Table` rows named by what they check.
- `engine/engine_test.go` and `engine/publisher_test.go`: fixed waits are gone. `pause.For` after
  `StartProjection` is `ctx.Eventually` on `IsProjectionRunning`, the single-node cluster wait is
  `ctx.Eventually` on `sys.InCluster()`, the saga wait polls `SagaStatus`, and the publisher tests poll the
  recorded count. The `time.Sleep(50ms)` drain loop in `TestEngineSubscribeReceivesEventsAndStates` is two
  `Eventually` calls over a drain function.
- New `engine/specs_helpers_test.go`: `beTheSamePointer` (go-specs has no identity matcher; `Equal` is deep),
  `panicValue` (go-specs has no not-panics matcher), `projectionRunning` and `waitTimeout`.

The tests that start an actor system keep their bodies. Each is wrapped in one `Describe`/`It` only so it can
reach `ctx.Eventually`; the first line of the body is `t := sc.T`, so the old `require` calls work unchanged.

## What does not change, and why

- **`TestEventPublisherReceivesEventsFromEntity`** and the `waitFor` helper it uses. A separate PR fixes the
  ordering bug (see the decision in `engine-rest-go-specs-v033.md`).
- **`TestCommandArchitecture`, `TestTenancyArchitecture`, `TestKitLoggerIsTheOnlyLoggingBackend`.** They run
  `go list` as a subprocess or walk the module tree outside `TempDir`. By the conventions page that is not a
  unit test, so it is not restructured.
- **`TestGoaktOptionsCarryTheResolvedLogger` and the `TestConfigGoaktOptions*` tests.** They build or start a
  real actor system and have no fixed wait.
- **Every other test that starts an actor system** (entity, saga, tenant, projection, durable-state tests).
  Out of the unit lane; they have no fixed wait to replace.
- **`TestEnginePublisherIdleCPU`.** Its wall-clock window is the measurement.
- **`waitForCond(60s)` in the high-partition test.** It polls and then skips the test; `Eventually` cannot
  skip.
- **`require.Eventually` already in actor-system tests.** Same behavior as `ctx.Eventually`; left for the
  follow-up.
- Rejected alternative: a `Describe("")` so subtest names stay identical. The conventions page asks for a real
  description, and the PR lists the name mapping instead.

## Constraints

- Strict TDD, runner `go test ./engine`. For test-only work, RED is a deliberate production mutation, reverted.
- Every top-level `Test` name stays (201 before, 201 after, none missing, none new). Every old subtest name
  is still there as the last segment of a new name (0 missing). `--- PASS` goes from 476 to 545: one subtest
  per wrapped plain test, plus the table rows that replaced loops.
- No `-race`, no workbench, no force-push. `develop` is merged only if #241 is merged first.
- The 400-line planning heuristic is exceeded (about 1150 added, 900 removed, mostly re-indentation of
  wrapped bodies). Splitting it further would not shrink the diff, so it stays as one PR.

## Tasks

- [x] T1 `option_test.go` to go-specs. Route: inline (one file, mechanical). Commit `e670e4e`. RED (mutations
      of production code): removing `reflect.Func` from `isNilResolver` gave `expected 1 to be the zero value
      of int`; swapping two kinds in `ClusterKinds` gave `HaveElementsInOrder: 2 of 4 positions failed`.
- [x] T2 `logger_test.go` and `runtime_compat_test.go` to go-specs, plus `specs_helpers_test.go`. Route: inline.
      Commit `4c93df2`. RED: making `ErrNotACommand` a different error value gave `expected not a command to
      satisfy "the identical error value as ErrNotACommand in port/runtime"`; adding 1 to `batchThreshold`
      failed the round-trip with `spawnConfig.batchThreshold: expected 8, actual 9`, and the nil-option case
      with `expected 3 to equal 2`.
- [x] T3 `engine_test.go` fixed waits to `ctx.Eventually` (8 tests). Route: inline script plus manual review.
      Commit `a941d77`. RED: making `IsProjectionRunning` return false gave `Eventually: timed out after
      10.000647566s (981 attempts) ... last observed: false`. The two cluster tests got faster (12.07 s to
      10.15 s) because the 1 s waits are gone.
- [x] T4 `publisher_test.go` fixed waits to `ctx.Eventually` (5 tests). Route: inline. Commit `79d3ca0`. RED:
      dropping the `StatesTopic` publish in the durable-state actor gave `Eventually: timed out after
      10.000345093s ... expected 0 to be greater than or equal to 1`.
- [x] T5 Verify and deliver. Evidence: `--- PASS` 476 before, 545 after, names checked as above; coverage
      `engine` 94.8% before and after; `go build ./...`, `go vet ./engine`, `gofmt -l engine` and
      `golangci-lint run ./engine/...` (0 issues) clean; `go test -count=5 ./engine` passes.

## Follow-up (not in this document)

- `engine-go-specs-actor-lane`: the tests that start a real actor system (entity, saga, tenant, projection,
  durable-state) need a production seam to leave the actor system, or a component lane. Includes moving the
  existing `require.Eventually` calls to `ctx.Eventually`, and the three architecture tests that shell out or
  walk the tree.
- `engine-ordering-bug` and `enginetest-adapters-dedup`, as listed in `engine-rest-go-specs-v033.md`.

## Progress

- 2026-09-30: T1 to T5 done. Engram mirror: pending.
