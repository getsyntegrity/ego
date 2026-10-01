# engine unit tests: fixed waits in the actor-system tests replaced by ctx.Eventually

Follows #241 (`odd/tasks/engine-rest-go-specs-v033.md`), now merged.

## Problem

After #241 and #240 (which migrated `option_test.go`, `logger_test.go` and `runtime_compat_test.go` to
go-specs), the `engine` package still waited with `pause.For` and `time.Sleep` in tests that start a real goakt
actor system. `docs/testing/go-specs.md` asks for `ctx.Eventually` instead of fixed waits. Those tests are out
of the unit lane, so they are not restructured; only the waits change.

This document first also covered migrating the option, logger and runtime-compat tests. #240 merged the same
work while this branch was open, so that part was dropped in favor of develop's version.

## What changes

Only `*_test.go` files in `engine/` change. No production code is touched.

- `engine/engine_test.go` (8 tests) and `engine/publisher_test.go` (5 tests): `pause.For` after
  `StartProjection` is `ctx.Eventually` on `IsProjectionRunning`, the single-node cluster wait is
  `ctx.Eventually` on `sys.InCluster()`, the saga wait polls `SagaStatus`, and the publisher tests poll the
  recorded count. The `time.Sleep(50ms)` drain loop in `TestEngineSubscribeReceivesEventsAndStates` is two
  `Eventually` calls over a drain function.
- Each of those tests is wrapped in one `Describe`/`It` only to reach `ctx.Eventually`; the first body line is
  `t := sc.T`, so the old `require` calls work unchanged.
- New `engine/specs_helpers_test.go`: `projectionRunning` and `waitTimeout`.

## What does not change, and why

- **`TestEnginePublisherIdleCPU`.** Its wall-clock window is the measurement.
- **`waitFor`/`waitForCond` in `publisher_test.go`.** They are polling helpers (20 ms interval). `waitForCond(60s)`
  in the high-partition test polls and then skips, which `Eventually` cannot do; the ordering test was fixed
  separately (#251).
- **The three architecture tests** (`go list` subprocess or tree walk): not unit tests.
- **Every other test that starts an actor system.** Out of the unit lane; no fixed wait to replace.
- **`require.Eventually` already in actor-system tests.** Same behavior as `ctx.Eventually`; left for the
  follow-up.

## Constraints

- Strict TDD, runner `go test ./engine`. For test-only work, RED is a deliberate production mutation, reverted.
- Every top-level `Test` name stays. No `-race`, no workbench, no force-push.

## Tasks

- [x] T1 `engine_test.go` fixed waits to `ctx.Eventually` (8 tests). Route: inline script plus manual review.
      RED: making `IsProjectionRunning` return false gave `Eventually: timed out after 10.000647566s (981
      attempts) ... last observed: false`. The two cluster tests got faster (12.07 s to 10.15 s) because the
      1 s waits are gone.
      Flake note: CI saw `actor not found` in `TestEngineRebuildProjectionSuccess` from the immediate
      `IsProjectionRunning` lookup right after `RebuildProjection`. The final lookup is now only the
      `Eventually` poll, which treats a lookup error as "not running yet". `-count=30` passes.
- [x] T2 `publisher_test.go` fixed waits to `ctx.Eventually` (5 tests). Route: inline. RED: dropping the
      `StatesTopic` publish in the durable-state actor gave `Eventually: timed out after 10.000345093s ...
      expected 0 to be greater than or equal to 1`.
- [x] T3 Reconcile with develop after #241 and #240 merged: take develop's option/logger/runtime-compat tests,
      re-apply T1 and T2. Verify and deliver (see Progress).

## Follow-up (not in this document)

- `engine-go-specs-actor-lane`: the tests that start a real actor system (entity, saga, tenant, projection,
  durable-state) need a production seam or a component lane. Includes moving the existing `require.Eventually`
  calls to `ctx.Eventually`, and the architecture tests that shell out or walk the tree.
- `enginetest-adapters-dedup`, as listed in `engine-rest-go-specs-v033.md`.

## Progress

- 2026-09-30: T1 and T2 done; T3 merged `origin/develop`, dropped the duplicate migration. Engram mirror:
  pending.
