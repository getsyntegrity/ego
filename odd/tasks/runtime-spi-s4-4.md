# Feature: `compose/goakt.App.Runtime()` and the end-to-end consumer (#147, slice S4-4)

Branch: `feat/147-s4-4-app-runtime` · Base: `origin/main` (`33ac9fe`) · Epic: #10 · Issue: #147 ·
Design: `openspec/changes/ego-runtime-001/design.md` (§D6, §D8, §D9, §5 test table, §9 row S4-4)

## Problem

After S4-3 the contract package `port/runtime` has interfaces and `*ego.Engine` implements them, but
nothing hands a consumer a `runtimeport.Runtime`: the composition root `compose/goakt.App` only
exposes `Engine() *ego.Engine`, so code that wants to stay runtime-neutral still has to name the
GoAkt adapter. And nothing yet shows a consumer package whose whole build closure is free of `ego`
and GoAkt driving a real application.

## What changes in this slice

`compose/goakt/app.go` gains `App.Runtime() runtimeport.Runtime` (design §D6). It returns an
untyped nil until `Start` succeeds and for good after a failed `Start`; after `Stop` it returns the
stopped engine, which refuses work with `ErrEngineNotStarted`, exactly as `Engine()` documents.
Returning `a.published.Load()` directly would wrap a nil `*ego.Engine` in a non-nil interface, so the
method checks for nil first. `Engine()` stays unchanged. The package doc example switches to
`app.Runtime().SpawnEventSourced`.

`internal/runtimeconsumer` (new, root module, never released) holds an account behavior written
against `port/behavior` and the `test/data/testpb` messages, and `Run(ctx, runtimeport.Entities)`,
which spawns it, sends `CreateAccount` then `CreditAccount`, and returns the final `*testpb.Account`.
Its `closure_test.go` runs `go list -deps` on the package and fails if the root package or any GoAkt
package appears. `compose/goakt/runtime_e2e_test.go` starts a real `App` and drives it through
`runtimeconsumer.Run(ctx, app.Runtime())`.

`CHANGELOG.md` extends the S4 entry; ego-arch-001 §3–§5 record that S4 is done, that the runtime
SPI lives in `port/runtime`, and what remains for #124.

## Why this shape

The consumer is a separate package, not a test file in `compose/goakt`, because a test file shares
`compose/goakt`'s closure, which contains GoAkt, and so proves nothing (§D8). It lives under
`internal/` because it is evidence, not API; it needs no archcheck layer because its closure test is
stricter than a direct-edge rule. Accessor tests go in a new file, `compose/goakt/runtime_test.go`
(package `goakt`, to reach the failed-`Start` hook), instead of `app_test.go` as §9 names: same
package and same checks, and it keeps the hot test file untouched (dispatcher preference).

## Constraints

- TDD: strict (user global configuration, `~/.claude/CLAUDE.md` "Strict TDD Mode: enabled"). Runner:
  `go test` (root module). RED for a new symbol is the build failure.
- apidiff: `compose/goakt` additions only (`App.Runtime`); `ego` no change.
- archcheck: 0 violations, 0 baselined, no new baseline entry.
- No `-race`, no workbench. No `Deprecated:` markers. No capability declarations.
- Out of scope: `engine.go`, `port/runtime`, `option.go`, `spawn_config.go`, publishers,
  `internal/cmd/*`, `.github/`.
- Route: direct inline (one writer; the design names every file). Delivery: `single-pr`
  (S4-4 row, ≤4 tasks).

## Tasks

- [x] T1 RED accessor tests (nil before `Start` as an untyped nil, nil after a failed `Start`, the
  same engine as `Engine()` after `Start`, the stopped engine after `Stop`). Check: build failure.
  Route: inline.
- [x] T2 `App.Runtime()` and the doc example. Check: `go test ./compose/goakt/`. Route: inline.
- [x] T3 `internal/runtimeconsumer` with its closure test (negative control), and the end-to-end
  test. Check: `go test ./internal/runtimeconsumer/ ./compose/goakt/`. Route: inline.
- [x] T4 `CHANGELOG.md` and ego-arch-001 §3/§4/§5; evidence (apidiff, archcheck, lint, ciselect,
  root suite). Route: inline.

## Progress

- **RED 1** (T1, build): `go vet ./compose/goakt/` → `runtime_test.go:41:35: app.Runtime undefined
  (type *App has no field or method Runtime)`.
- **RED 2** (the nil check): a naive `return a.published.Load()` failed
  `TestRuntime_NilBeforeStart` (`Runtime() before Start = (*ego.Engine)(nil), want an untyped nil
  interface`) and `TestRuntime_NilAfterFailedStart`.
- **GREEN** (T2, commit `98474ce`): with the nil check, `TestRuntime_NilBeforeStart`,
  `TestRuntime_IsTheEngineAfterStartAndAfterStop`, `TestRuntime_NilAfterFailedStart` pass.
- **RED 3** (T3): `go vet ./compose/goakt/` → `internal/runtimeconsumer: no non-test Go files`; the
  closure test with no production code failed (`must contain ".../port/runtime"`, `".../port/behavior"`).
- **GREEN** (T3, commit `a616ae5`): `TestProductionClosureExcludesRootAndGoAkt` and
  `TestRuntime_ConsumerDrivesTheAppEndToEnd` pass. Production closure of `internal/runtimeconsumer`,
  ego-owned part: `tenancy`, `command`, `port/behavior`, `internal/queue`, `internal/syncmap`,
  `eventstream`, `port/runtime`, `test/data/testpb`; no root package, no GoAkt.
- **Closure negative control**: a throwaway `internal/runtimeconsumer/zz_mutation.go` with
  `import _ "github.com/pablogore/ego/v4"` made the closure test fail with 46 errors (first:
  `must not reach the GoAkt runtime; got "github.com/tochemey/goakt/v4/extension"`); file removed,
  test green again.
- **T4** (commit `f5f97d0`): `CHANGELOG.md` S4 entry extended; ego-arch-001 §3 (contracts list,
  adapter implements the SPI), §4 (`port/runtime` and `internal/runtimeconsumer` rows, adapter rows
  point at #124), §5 (S4 done, what remains for #124).
- **archcheck**: `8 modules checked, 51 packages checked, 209 edges checked, 0 baselined, 0
  violation(s), 0 stale entries`. `internal/runtimeconsumer` has no layer (design §D8).
- **apidiff** vs `33ac9fe`: `compose/goakt` compatible only, `(*App).Runtime: added`; `ego` empty.
- **golangci-lint** `--new-from-rev=origin/main ./...` (go1.26.6 SDK, after `go mod vendor`;
  `vendor/` removed after): 0 issues.
- **ciselect** `-base origin/main`: mode `affected`, `compose/goakt` and `internal/runtimeconsumer`;
  no nested module selected. Both packages pass, including #146's
  `TestApp_TwoNodeClusterPlacesAndStopsCleanly`.
- **Full root suite** (`go test -count=1 ./...`, no `-race`): 29 ok, exit 0.
- **Nested-consumer check** (ego-arch-001 §5, build + vet): publishers ×4, `benchmark`,
  `example/cluster`, `test/compat`, `mocks/ego` all OK.
- Review tier / RDD: not run by this writer (the parent owns review routing).

- **Review nits** (independent review of `05e18ac`, approve with nits): N1 the closure test now
  also fails on any first-party package outside an explicit allowlist (`port/runtime`,
  `port/behavior`, `command`, `tenancy`, `eventstream`, `internal/queue`, `internal/syncmap`,
  `test/data/testpb`, the set `go list -deps` reports); negative control: a temporary
  `import _ ".../testkit"` failed it on `egopb`, `encryption`, `offsetstore`, `persistence`,
  `port/adapter`, `testkit`; reverted. N2 the e2e test's cleanup `Stop` carries a comment that `Stop`
  is idempotent and its error is deliberately ignored.

## Post-merge check

The PR lane ran ciselect `affected`, not `full`. After merge, watch `main`'s full-lane `build.yml`
run for the merge commit; if it fails, reopen #147 (the PR says `Closes #147`).

## Next step

Open the pull request; CI must be green (the last #147 criterion).
