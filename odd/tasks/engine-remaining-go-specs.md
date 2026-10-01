# engine tests: no testify and no fixed waits in engine_test.go and publisher_test.go

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

- `engine/engine_test.go` (7 tests) and `engine/publisher_test.go` (5 tests): `pause.For` after
  `StartProjection` is `ctx.Eventually` on `IsProjectionRunning`, the single-node cluster wait is
  `ctx.Eventually` on `sys.InCluster()`, the saga wait polls `SagaStatus`, and the publisher tests poll the
  recorded count. The `time.Sleep(50ms)` drain loop in `TestEngineSubscribeReceivesEventsAndStates` is two
  `Eventually` calls over a drain function.
- Each of those tests is wrapped in one `Describe`/`It` only to reach `ctx.Eventually`; the first body line is
  `t := sc.T`, so the old `require` calls work unchanged.
- New standing rule: nothing keeps testify in the files this PR touches. Every `require`/`assert` call in
  `engine_test.go` and `publisher_test.go` is now a go-specs matcher (`BeNil`, `MatchError`, `BeTrue`,
  `Equal`, `Contain`, `HaveLen` and so on), and the 34 plain tests there are wrapped in `Describe`/`It`
  (subtests became `s.It`; the sentinel-error loop became `s.Describe` with one `It` per sentinel). The
  describe names are derived from the test names and the `It` of a test without subtests is "holds"; a
  follow-up can give them real wording. `EqualValues` became `Equal` with the real type (`uint64(1)`),
  because `specs.Equal` is type-strict.
- Goroutines use `sc.Go`; shared per-`Describe` setup moved into `BeforeEach` (store connect, engine start
  once). The two helpers that other files call with a `*testing.T` (`newTestCluster`, `processCPUTime`) use
  `t.Fatalf` instead of testify. `assert.AnError` is now `errAnyFailure` in `specs_helpers_test.go`.
- New `engine/specs_helpers_test.go`: `projectionRunning`, `waitTimeout` and `errAnyFailure`.

## What does not change, and why

- **`TestEngineRebuildProjectionSuccess`.** It flakes about once in 300 runs on develop itself (CI saw `actor
  not found`; locally the projection is not running after the rebuild). Polling does not help: with
  `ctx.Eventually` the projection stayed down for the whole 10 s, so the actor really is not restarted.
  This looks like a race in `RebuildProjection` (Kill followed by an immediate Spawn of the same name),
  which needs a production fix. Its original pauses are left as they are so this PR does not make the flake
  worse. Follow-up: `engine-rebuild-projection-race`.
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
- Every top-level `Test` name stays (201, identical to develop); every old subtest name is still the last
  segment of a new name. `--- PASS` is 546 after. No `-race`, no workbench, no force-push.

## Tasks

- [x] T1 `engine_test.go` fixed waits to `ctx.Eventually` (7 tests). Route: inline script plus manual review.
      RED: making `IsProjectionRunning` return false gave `Eventually: timed out after 10.000647566s (981
      attempts) ... last observed: false`. The two cluster tests got faster (12.07 s to 10.15 s) because the
      1 s waits are gone.
- [x] T2 `publisher_test.go` fixed waits to `ctx.Eventually` (5 tests). Route: inline. RED: dropping the
      `StatesTopic` publish in the durable-state actor gave `Eventually: timed out after 10.000345093s ...
      expected 0 to be greater than or equal to 1`.
- [x] T3 Reconcile with develop after #241 and #240 merged: take develop's option/logger/runtime-compat tests,
      re-apply T1 and T2. Verify and deliver (see Progress).

## Follow-up (not in this document)

- `engine-no-testify`: the 28 other `engine/*_test.go` files still import testify: `behavior_dependency`,
  `behavior_kind`, `behavior_value_type`, `command_architecture`, `command_runtime_wiring`,
  `durable_state_actor_expected_revision`, `durable_state_actor_integration`, `e2e_legacy_compat`,
  `engine_entity_family`, `engine_erase_entity_tenant`, `engine_fixed_tenant_resolver`,
  `engine_lifecycle_fixes`, `engine_neutral_cluster`, `engine_spawn`, `engine_tenant_administrative_scope`,
  `engine_tenant_cluster`, `engine_tenant_respawn`, `engine_tenant_spawn`,
  `event_sourced_actor_batch_precondition_matrix`, `event_sourced_actor_expected_revision`,
  `event_sourced_actor_integration`, `helper_test` (`newTestEngine`), `logger_architecture`,
  `projection_actor`, `saga_status`, `telemetry_contract`, `tenancy_architecture` and
  `tenant_write_path_e2e` (all `_test.go` in `engine/`). The conversion script used here is mechanical; the
  work is reviewing the results and giving the describe names real wording.
- `engine-go-specs-actor-lane`: the tests that start a real actor system (entity, saga, tenant, projection,
  durable-state) need a production seam or a component lane. Includes moving the existing `require.Eventually`
  calls to `ctx.Eventually`, and the architecture tests that shell out or walk the tree.
- `engine-rebuild-projection-race`: fix the `RebuildProjection` restart race, then replace that test's pauses.
- `enginetest-adapters-dedup`, as listed in `engine-rest-go-specs-v033.md`.

## Progress

- 2026-09-30: T1 and T2 done; T3 merged `origin/develop`, dropped the duplicate migration. Engram mirror:
  pending.
