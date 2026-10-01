# engine tenant tests (group g3) on go-specs v0.3.3 (#252)

## Problem

`engine/` still imports testify in 28 test files. This spec is group g3, eight tenancy test files that
together hold 16 top-level tests. They use `require`/`assert`, `t.Run` subtests, `require.Eventually`
polling and, in two places, a shared testify-based engine builder (`newTestEngine` in
`engine/helper_test.go`).

## What changes

Only these test files change, plus one new helper file:

- `engine/tenant_write_path_e2e_test.go`, `engine/engine_tenant_spawn_test.go`,
  `engine/engine_tenant_respawn_test.go`, `engine/engine_tenant_cluster_test.go`,
  `engine/engine_tenant_administrative_scope_test.go`, `engine/engine_erase_entity_tenant_test.go`,
  `engine/engine_fixed_tenant_resolver_test.go`, `engine/tenancy_architecture_test.go`.
- New `engine/specs_helpers_g3_test.go` holds `newSpecsEngineG3` (a go-specs version of `newTestEngine`
  that registers its stops with `ctx.Cleanup`) and `newConnectedEventsStoreG3`. The `G3` suffix avoids
  clashes with the other three writer groups.

Every top-level `Test` keeps its name and holds one `specs.Describe`. Assertions use go-specs matchers
(`MatchError`, `MatchErrorAs`, `BeZero`, `BeGreaterThan`, `BeEmpty`). `require.Eventually` becomes
`ctx.Eventually`. The existing `mock.Controller` adapters (`eventsStoreMock`, `stateStoreMock`) are reused
unchanged. Tests that start a real goakt actor system or a two-node cluster still do so.

## What does not change, and why

- **Production code and `helper_test.go`.** `helper_test.go` is shared with other groups and is removed in
  a final cleanup, so g3 does not edit it.
- **The shared architecture helpers keep a `*testing.T` signature.** `architectureModuleRoot` and
  `tenancyArchitectureGoList` are also called from `command_architecture_test.go` and
  `logger_architecture_test.go` (other groups). They now report through `t.Fatalf` instead of testify, so
  those callers compile unchanged.
- **Subtests become `It` blocks under the `Describe`.** Case names are kept; the `Describe` segment is
  added to the path, as described in `docs/testing/go-specs.md`. `TestEngineSagaStatusTenantIsolation`
  now builds its engine per case (a `BeforeEach`) instead of sharing one across both subtests; the cost is
  one extra short actor system, the gain is that each case stands alone.
- **No `specs.Table`.** No group of cases here differs only by input and expected result.

## Constraints

- Strict TDD. Runner: `go test ./engine/`. Because this is a test-only change, RED is shown by deliberate
  production mutations (reverted afterwards).
- No `-race`, no workbench, no testify in any touched file.

## Tasks

- [x] T1 Spawn, respawn and fixed-resolver tests plus the shared helper file. Route: inline (the files
  are tightly coupled to the helper). Evidence: `go test -count=5` green; RED by mutating
  `spawnTenantScope` to return `nil, nil` (spawn, fixed-resolver and administrative cases failed) and
  `classifyTenantBinding` to ignore `Matches` (respawn, concurrent and cluster cases failed). Commit:
  `2c8c979`.
- [x] T2 Write-path e2e, erase and administrative-scope tests. Route: inline. Evidence: RED by making
  `Engine.EraseEntity` use `persistence.Unscoped()` (erase failed), by not attaching the resolved tenant in
  `Engine.SagaStatus` (both saga status cases failed), and by calling `Resolve` at spawn (resolves-once and
  e2e failed). Commit: `9aedc19`.
- [x] T3 Cluster and tenancy architecture tests. Route: inline. Evidence: RED by making
  `Engine.Dispatch` accept `TenantBindingQuery` (dispatch case failed), and by adding a file under
  `tenancy/` that imports a third-party package (architecture failed); the cluster test is covered by the
  `classifyTenantBinding` mutation above. Commit: `b5ca5a6`.
- [x] T4 This document and the pull request. Route: inline.

## Follow-up

The final `engine/` cleanup removes `helper_test.go` and folds `newSpecsEngineG3` and the other groups'
suffixed helpers into one go-specs helper file.

## Progress

All eight files are migrated. Before and after, the same 16 top-level tests pass. Full `go test ./engine/`
passes with coverage 94.8% before and after. `go build ./...`, `go vet ./engine/`, `gofmt -l` and
`golangci-lint run ./engine/...` are clean. The 16 tests pass with `-count=5`.
