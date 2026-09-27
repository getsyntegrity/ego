# Feature: `port/runtime` interfaces, `Engine` assertion and test double (#147, slice S4-3)

Branch: `feat/147-s4-3-runtime-interfaces` · Base: `origin/main` · Epic: #10 · Issue: #147 ·
Design: `openspec/changes/ego-runtime-001/design.md` (§D1, §D4, §D5, §D7, §D9, §9 row S4-3)

## Problem

S4-2 moved the neutral types, options and errors into the contract package `port/runtime`, but the
package still has no interfaces: consumer code cannot name "a runtime" without naming `*ego.Engine`,
the GoAkt adapter. Nothing yet proves that the contract can be implemented without GoAkt.

## What changes in this slice

`port/runtime/runtime.go` declares one small interface per capability: `Entities`
(`SpawnEventSourced`, `SpawnDurableState`, `EntityExists`, `SendCommand`, `Dispatch`, `EraseEntity`),
`Sagas` (`SpawnSaga`, `SagaStatus`), `Projections` (`StartProjection`, `StopProjection`,
`IsProjectionRunning`, `RebuildProjection`, `ProjectionLag`), `Events` (`Subscribe`), and the
composite `Runtime`. Each signature is the `*ego.Engine` method's, type for type. The comments say
what every runtime guarantees: `ErrEngineNotStarted` when not started, and an `ErrUnsupported` error,
before any side effect, for an operation the runtime lacks. `SagaStatus` records the #153 gap (the
GoAkt adapter always reports `SagaRunning`). Runtime-specific sentences (cluster singleton, GoAkt
serialization, `WithBehaviorKinds`) stay on the `Engine` methods, which do not change.

`engine_runtime.go` (new, package `ego`) holds only `var _ runtimeport.Runtime = (*Engine)(nil)`, so
`engine.go` is not touched. `port/runtime/double_test.go` holds a map-backed double that implements
`Runtime` with no actor system, and `runtime_architecture_test.go` gains a `go list -deps -test`
check that keeps GoAkt and package `ego` out of the test build that contains the double.

Carry-over doc nits from the S4-2 review, in `port/runtime/spawn.go`: `WithAdapterSetting` and
`isComparableKey` now say the key must be of a comparable *type* (the `context.WithValue` rule), and
the garbled `StopDirective` bullet of `SupervisorDirective`'s comment is rewritten.

No capability declarations (ego-arch-004 F-E, #106's later composition work) and no `Deprecated:`
markers are added.

## Why this shape

Small interfaces plus a composite, and a string entity ID, are maintainer decisions (design §2). The
assertion lives in its own file because `engine.go` is shared with other open work (design §D5, §9).
The double lives in a `_test.go` file of `port/runtime`, as design §D7 says: it is evidence, not API,
and a test file of package `ego` or `compose/goakt` could not prove GoAkt-freedom because their
closures contain GoAkt. Alternative rejected: an `internal/` package for the double; the design keeps
that for S4-4's consumer (`internal/runtimeconsumer`), which needs a production closure to check.

## Constraints

- TDD: strict (user global configuration, `~/.claude/CLAUDE.md` "Strict TDD Mode: enabled"). Runner:
  `go test` (root module). RED for new symbols is the build failure.
- apidiff: `ego` no change; `port/runtime` additions only.
- archcheck: 0 violations, 0 baselined, no new baseline entry.
- No `-race`, no workbench, no release tag between S4-2 and S4-3.
- Out of scope: `engine.go`, `compose/` (S4-4), `option.go`, `spawn_config.go`, `saga.go`,
  `supervisor.go`, publishers, `.github/`.
- Route: direct inline (one writer; the design names every file; four files of real content).

## Tasks

- [x] T1 RED: `double_test.go`, the `-test` closure test and `engine_runtime.go`'s assertion, before
  the interfaces exist. Check: build failure observed. Route: inline.
- [x] T2 The five interfaces with their contract documentation (§D1, §D4) and the two spawn.go doc
  nits. Check: `go test ./port/runtime/`. Route: inline.
- [x] T3 `engine_runtime.go`. Check: `go vet .`, full root suite. Route: inline.
- [x] T4 Double tests and the `-test` closure; `CHANGELOG.md` line; evidence (apidiff, archcheck,
  lint, ciselect, nested modules). Route: inline.

## Progress

- **RED** (T1). With `double_test.go`, the closure test and `engine_runtime.go` present and no
  `runtime.go`: `go vet ./port/runtime/` → `double_test.go:46:15: undefined: runtime.Runtime`;
  `go build .` → `engine_runtime.go:32:19: undefined: runtimeport.Runtime`.
- **GREEN** (T2–T4). `go test -count=1 ./port/runtime/` ok: `TestDoubleSpawnResolvesDocumentedDefaults`,
  `TestDoubleSpawnAppliesOptionsInOrderAndSkipsNil`, `TestDoubleSendCommandRunsTheBehavior`,
  `TestDoubleUnsupportedOperations` (12 subtests, one per unsupported operation, each through its
  capability interface: matches `ErrUnsupported` and `errors.ErrUnsupported`, names runtime and
  operation, entity map and hosted entity unchanged), `TestRuntimeDependsOnlyOnContracts`,
  `TestRuntimeTestClosureExcludesGoAktAndRoot`.
- **Closure negative control**: a temporary `port/runtime/zz_mutation_test.go` importing package `ego`
  made `TestRuntimeTestClosureExcludesGoAktAndRoot` fail (`must not depend on GoAkt package
  "github.com/tochemey/goakt/v4/extension"`); file removed.
- Base after rebase: `origin/main` `8eb01d1` (#158 merged while this slice was in progress; no
  conflicts). Work-unit commit: `feat(runtime): add per-capability runtime interfaces (#147, S4-3)`.
- **apidiff** vs `8eb01d1`: `ego` no changes (empty report); `port/runtime` compatible only:
  `Entities`, `Events`, `Projections`, `Runtime`, `Sagas` added.
- **Consumer program** (S4-2's, copied to `/tmp/s43-consumer`, which binds the `Engine` methods to
  function variables of their base types): build and vet clean against base and head, 28 lines of
  output, byte-identical (sha256 `471c8153…9622` both sides, the same as S4-2's).
- **archcheck**: `8 modules checked, 50 packages checked, 205 edges checked, 0 baselined, 0
  violation(s), 0 stale entries`. No baseline entry added.
- **golangci-lint** `--new-from-rev=origin/main ./...` (go1.26.6 SDK, after `go mod vendor`; `vendor/`
  removed after): 0 issues.
- **ciselect** `-base 8eb01d1`: mode `full` (`engine_runtime.go` changed, shared root package); every
  nested module selected.
- **Full root suite** (`go test -count=1 ./...`, no `-race`): 28 ok, the rest without test files,
  exit 0 (includes `compose/goakt`'s two-node test of #146).
- **Nested modules**: `scripts/ci/verify-module.sh` OK for `benchmark`, `example/cluster`,
  `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `test/compat`
  (go1.26.6 SDK, `GO_TEST_RACE` unset).
## Next step

Open the PR. S4-4 (`App.Runtime()`, `internal/runtimeconsumer`, end-to-end test) follows after merge.
