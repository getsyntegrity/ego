# Feature: `port/runtime` neutral types, options, `SpawnSettings` and sentinels (#147, slice S4-2)

Branch: `feat/147-s4-2-runtime-types` · Base: `origin/main` `e2d89ec` · Epic: #10 · Issue: #147 ·
Design: `openspec/changes/ego-runtime-001/design.md` (§D2, §D3, §D4, §D9, §9 row S4-2)

## Problem

Consumer code can only run Ego through `*ego.Engine`, because every type the runtime operations need
lives in package `ego`, and `ego.SpawnOption` is sealed: its only method takes the unexported
`ego.spawnConfig`, so no other runtime can read what a caller asked for at spawn. The runtime SPI
(`port/runtime`, #147) needs those types in a contract package before its interfaces (S4-3) can be
written.

## What changes in this slice

A new contract package, `port/runtime` (package clause `runtime`), receives the neutral types, each
with its doc comment: `EntitiesPlacement` and its four constants, `SupervisorDirective` and its two
constants, `SagaStatus` with its four constants and `String`, `SagaInfo`, `SpawnOption`, and ten
neutral sentinel errors (same names, same messages). It also gets what is new: `SpawnSettings` (a
read-only value with getters), `ResolveSpawnOptions`, `WithAdapterSetting`, the five neutral option
constructors, and `ErrUnsupported`/`UnsupportedError` (design §4 lists them under S4-2).

Package `ego` keeps every old name as an alias (`type SagaInfo = runtimeport.SagaInfo`,
`var ErrEngineNotStarted = runtimeport.ErrEngineNotStarted`, ...), so values and error identity are
the same. The five neutral `ego.With*` options become one-line wrappers. The four write-side options
(`WithSnapshotInterval`, `WithRetentionPolicy`, `WithBatchThreshold`, `WithBatchFlushWindow`) stay in
`ego` and travel through `runtimeport.WithAdapterSetting` under unexported key types;
`newSpawnConfig` reads them back, so `engine.go`'s spawn code does not change.

No `Deprecated:` marker is added (maintainer decision Q1, design §10). The per-capability interfaces,
the `Engine` assertion and the test double are S4-3; `App.Runtime()` and the consumer package are S4-4.

## Why this shape

Aliases, not new types: a new `runtime.SagaInfo` distinct from `ego.SagaInfo` would force a change to
`Engine`'s method signatures, a v4 break (design §12). Getters, not exported fields: a struct literal
would be a second way to state settings whose zero value does not match the defaults (design §D3).
Write-side options through adapter settings, not in the contract: whether they belong there is #12's
decision (maintainer decision 3, Q2).

## Constraints

- TDD: strict (user global configuration, `~/.claude/CLAUDE.md` "Strict TDD Mode: enabled"). Runner:
  `go test` (root module). RED for new symbols is the build failure.
- apidiff `ego` vs `origin/main`: exactly the 31 documented alias false positives (design §D3); anything
  else stops the slice.
- archcheck: 0 violations, no new baseline entry.
- No `-race`, no workbench. Out of scope: `compose/`, `logger.go`, `option.go`, `internal/cmd/*`,
  `.github/`, publishers.
- Route: direct inline (one writer; the design names every file).

## Tasks

- [x] T1 RED: `port/runtime` option/settings/adapter-setting/nil-option/`ErrUnsupported` tests and
  `runtime_compat_test.go` (`errors.Is` both ways for the ten sentinels, alias identity, option round
  trip through `ResolveSpawnOptions`). Check: build failure observed. Route: inline.
- [x] T2 `port/runtime` types, options, `SpawnSettings`, sentinels, doc and architecture test. Check:
  `go test ./port/runtime/`. Route: inline. Commit: see Progress.
- [x] T3 aliases and wrappers in `ego`; write-side options via adapter settings; `spawn_config_test.go`
  rewritten through `newSpawnConfig`; `CHANGELOG.md`. Check: targeted root tests then full root suite.
  Route: inline.
- [x] T4 evidence: apidiff (`ego`, `port/runtime`), consumer program on base and head, nested modules,
  archcheck, lint. Route: inline.

## Progress

Base after rebase: `origin/main` `8e6fd7c` (#152, S4-1, merged while this slice was in progress; the
rebase had no conflicts).

- **RED** (T1). With only `port/runtime/doc.go` present: `go vet ./port/runtime/` →
  `spawn_test.go:82:31: undefined: runtime.SpawnOption`; `go vet .` →
  `runtime_compat_test.go:47:60: undefined: runtimeport.ErrEngineNotStarted`.
- **GREEN** (T2, T3). `go test ./port/runtime/` ok; targeted root run
  `-run 'TestRuntime|TestEgoSpawnOptions|TestNewSpawnConfig|TestSpawnOption|Batch|Snapshot|Retention|Passivat|Relocat|Supervis|SagaStatus|Tenant'`
  ok (110.9s); full root suite: see below.
- **apidiff `ego`** vs `origin/main` `8e6fd7c`: exactly 31 incompatible-change lines, nothing else, no
  compatible-change section: 16 "changed from X to X" (the seven `Engine` methods `DurableStateEntity`,
  `Entity`, `Saga`, `SagaStatus`, `SpawnDurableState`, `SpawnEventSourced`, `SpawnSaga`, and the nine
  `With*` spawn options) and 15 "changed from X to github.com/pablogore/ego/v4/port/runtime.X" (the five
  types and ten constants). No report for the sentinels. **`port/runtime`**: new package (no base).
- **Consumer program** (`/tmp/s42-consumer`, fresh): builds all nine options through function values of
  their old types, embeds `ego.SpawnOption`, takes `ego.SpawnOption.Apply`, binds the seven `Engine`
  methods to function variables of their base types, switches over every moved constant, calls
  `SagaStatus.String`, checks `errors.Is` on the ten sentinels. Build and vet clean against base and head;
  28 lines of output, byte-identical (sha256 `471c8153…9622` both sides).
- **archcheck**: `8 modules checked, 47 packages checked, 198 edges checked, 0 baselined, 0 violation(s),
  0 stale entries` (base: 46 packages, 195 edges, 0 baselined). No baseline entry added.
- **golangci-lint** `--new-from-rev=origin/main ./...`: 0 issues (run with the go1.26.6 SDK after
  `go mod vendor`, as `.golangci.yml` sets `modules-download-mode: vendor`; `vendor/` removed after).
- **ciselect** `-changed … -base origin/main`: mode `full` (shared root package files changed).
- **Full root suite** (the 29 selected packages, `go test -count=1`, no `-race`): 25 ok, 4 without
  test files, exit 0.
- **Nested modules**: `scripts/ci/verify-module.sh` OK for `benchmark`, `example/cluster`,
  `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `test/compat`
  (go1.26.6 SDK, `GO_TEST_RACE` unset).
- Doc note: the `port/runtime` sentinels carry neutral doc comments (runtime-specific detail, such as
  `TenantBindingQuery` or `NewConfig`, stays on the `ego` names, whose comments are unchanged).

## Next step

Open the PR; S4-3 (interfaces, `engine_runtime.go` assertion, test double) follows after merge. No
release tag between S4-2 and S4-3 (design §9).
