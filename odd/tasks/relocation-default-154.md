# Feature: fix WithRelocation's docs to match its actual default (#154)

Branch: `docs/154-relocation-default` · Base: `origin/main` `33ac9fe` · Epic: #10 · Issue: #154 ·
Related: #147 (S4, `runtime.WithRelocation`), design `openspec/changes/ego-runtime-001/design.md`
FU-2, RUNTIME-003 of #11 (placement/supervision/passivation semantics).

## Problem

`WithRelocation`'s documentation and the actual spawn-configuration default contradicted each other:
the doc said "In cluster mode, entities are relocatable by default to ensure system resilience and
high availability", but `newSpawnConfig` (`spawn_config.go`) and `runtime.ResolveSpawnOptions`
(`port/runtime/spawn.go`) always resolve relocation to `false` unless `WithRelocation(true)` is
passed, and `engine.go`'s `buildSpawnOptionsFromConfig` calls `goakt.WithRelocationDisabled()`
whenever `!config.toRelocate`. A caller trusting the doc believes an entity is redeployed on a
healthy node automatically when its host node dies; it is not.

## Decision

Maintainer chose **option (a)** from the issue: keep today's behavior in v4 (relocation disabled
unless `WithRelocation(true)` is passed) and fix the documentation. No behavior change. Whether the
default should change is left to RUNTIME-003 or a later major version (#124).

## What changes

Doc-only plus one new test:

- `spawn_config.go`: `ego.WithRelocation`'s comment now states the real default in its own words
  (previously it deferred entirely to `runtimeport`'s doc).
- `port/runtime/spawn.go`: `runtime.WithRelocation`'s comment states the same default explicitly and
  drops the framing that made #154 read as still-open ("RUNTIME-003 owns ... including whether its
  default should change (#154)"); it now says #154 settled this for v4 and points any future default
  change to RUNTIME-003 or a later major version.
- New `relocation_default_test.go` (package `ego`): `TestRelocationDisabledByDefault` pins the
  default at both the `ego` layer (`newSpawnConfig()`) and the `port/runtime` SPI layer
  (`runtimeport.ResolveSpawnOptions()`), and pins that `WithRelocation(true)` flips both to enabled.
  Fails if either default is ever flipped without updating the docs.
- `CHANGELOG.md`: one Bug Fixes entry under `[Unreleased]`, referencing #154.

Note while exploring: `port/runtime/spawn_test.go` already had `TestResolveSpawnOptionsDefaults`
(added with #147/S4-2, commit `956f84b`), which already asserted `Relocation()` is `false` by
default — so the acceptance criterion "a test that pins the default" was already partly met at the
SPI layer before this change. This change adds the `ego`-layer pin (`newSpawnConfig`, the exact
layer `engine.go` reads and the issue's own reproduction) and tightens the doc wording that still
implied #154 was an open default-change question.

## Constraints

- Doc-only change to `spawn_config.go` and `port/runtime/spawn.go`; no change to `engine.go`,
  `compose/goakt/app.go`, `internal/runtimeconsumer`, saga files, or the ego-arch-001 S4-4 work in
  flight.
- No `-race`, no workbench. `unset GOROOT` for `go test`/`go vet`; lint with
  `GOROOT=/home/pablog/sdk/go1.26.6 GOTOOLCHAIN=local` and that SDK's `bin` prepended to `PATH`
  (needed so `asm` matches `GOROOT`), `--modules-download-mode=mod`,
  `--new-from-rev=origin/main`.
- TDD (user configuration): enabled. The default-pinning test passes on `origin/main` already (no
  behavior change), so RED is shown by mutation instead: temporarily flip the resolved default to
  `true` in a throwaway copy of `port/runtime/spawn.go`, observe both the new test and the
  pre-existing `TestResolveSpawnOptionsDefaults` fail, then revert (never committed).

## Tasks

Route: direct inline (2 doc-only files + 1 new test file, already understood; no research needed).

- [x] **T1** Read issue #154, `spawn_config.go`, `port/runtime/spawn.go`, `engine.go`'s
  `buildSpawnOptionsFromConfig`; confirm the doc/code contradiction and that #147/S4-2 had already
  fixed `runtime.WithRelocation`'s doc and added a defaults-pinning test at the SPI layer.
- [x] **T2** Rewrite both `WithRelocation` doc comments to state the real default explicitly
  (decision (a)); add `relocation_default_test.go` pinning the default at the `ego` layer too.
- [x] **T3** Mutation evidence (RED via flipped default, reverted), then GREEN; `apidiff` (both
  packages, no change), `archcheck` (0/0), `go vet`, `gofmt` on touched files, `golangci-lint
  --new-from-rev=origin/main` (0 issues); root tests matching `Relocat`.
- [x] **T4** `CHANGELOG.md` entry referencing #154; this task document.

## Verification evidence

**Before/after doc text.**

`spawn_config.go` (`ego.WithRelocation`), before:
```go
// WithRelocation controls whether an entity should be relocated to another
// node in the cluster when its hosting node shuts down unexpectedly. It
// returns [runtimeport.WithRelocation], whose documentation states the
// contract: relocation is disabled unless WithRelocation(true) is passed.
```
after:
```go
// WithRelocation controls whether an entity should be relocated to another
// node in the cluster when its hosting node shuts down unexpectedly.
//
// In cluster mode, entities are NOT relocated by default: WithRelocation(false)
// is the default, and relocation stays disabled unless WithRelocation(true) is
// passed. WithRelocation(true) makes the entity eligible for relocation to a
// healthy node when its host node goes down. It returns
// [runtimeport.WithRelocation], whose documentation states the same contract.
```

`port/runtime/spawn.go` (`runtime.WithRelocation`), before:
```go
// WithRelocation controls whether an entity should be relocated to another node in the cluster
// when its hosting node shuts down unexpectedly.
//
// Relocation is disabled unless WithRelocation(true) is passed. When it is
// enabled, the entity is redeployed on a healthy node if the original node
// becomes unavailable, which suits entities that can resume without
// node-specific context. RUNTIME-003 owns the contract of this setting,
// including whether its default should change (#154).
```
after:
```go
// WithRelocation controls whether an entity should be relocated to another node in the cluster
// when its hosting node shuts down unexpectedly.
//
// In cluster mode, entities are NOT relocated by default: WithRelocation(false)
// is the default, and relocation is disabled unless WithRelocation(true) is
// passed (#154 settled this for v4: keep today's behavior, fix the docs). When
// it is enabled, the entity is eligible for relocation to a healthy node if
// its host node goes down, which suits entities that can resume without
// node-specific context. Whether the default itself should change is left to
// RUNTIME-003 or a later major version.
```

**Mutation evidence (RED).** Temporarily added `relocation: true` to the `spawnConfig{...}` literal
inside `runtime.ResolveSpawnOptions` (`port/runtime/spawn.go`), never committed, reverted right
after:
```
--- FAIL: TestRelocationDisabledByDefault (0.00s)
    --- FAIL: TestRelocationDisabledByDefault/newSpawnConfig_with_no_options_disables_relocation
        Error: Should be false
        Messages: relocation must stay disabled by default; WithRelocation(true) is required to opt in
    --- FAIL: TestRelocationDisabledByDefault/runtimeport.ResolveSpawnOptions_with_no_options_disables_relocation
        Error: Should be false
        Messages: relocation must stay disabled by default; WithRelocation(true) is required to opt in
--- FAIL: TestResolveSpawnOptionsDefaults (port/runtime/spawn_test.go:38)
        Error: Should be false
        Messages: relocation is disabled unless WithRelocation(true) is passed
```
Reverted (`diff` against the pre-mutation copy empty), then GREEN:
```
--- PASS: TestRelocationDisabledByDefault (0.00s)
    --- PASS: .../newSpawnConfig_with_no_options_disables_relocation
    --- PASS: .../newSpawnConfig_with_WithRelocation(true)_enables_relocation
    --- PASS: .../runtimeport.ResolveSpawnOptions_with_no_options_disables_relocation
    --- PASS: .../runtimeport.ResolveSpawnOptions_with_WithRelocation(true)_enables_relocation
ok  	github.com/pablogore/ego/v4	0.011s
--- PASS: TestResolveSpawnOptionsDefaults
ok  	github.com/pablogore/ego/v4/port/runtime	0.004s
```

**Checks.**

| Check | Result |
|---|---|
| `apidiff` package `ego`, `origin/main` `33ac9fe` vs branch | No differences reported |
| `apidiff` package `port/runtime`, `origin/main` `33ac9fe` vs branch | No differences reported |
| `go run ./internal/cmd/archcheck` | `0 violation(s), 0 stale entries` |
| `go vet ./...` | clean |
| `gofmt -l` on touched files (`spawn_config.go`, `port/runtime/spawn.go`, `relocation_default_test.go`) | empty (clean); repo-wide `gofmt -l .` lists 5 pre-existing files unrelated to this change |
| `golangci-lint run --modules-download-mode=mod --new-from-rev=origin/main ./...` | `0 issues` |
| `go test . ./port/runtime/... -run 'Relocat\|TestResolveSpawnOptionsDefaults'` | PASS |

## Next step

PR against `main`, label `bug`, `Closes #154`. Review and merge are the maintainer's decision.
