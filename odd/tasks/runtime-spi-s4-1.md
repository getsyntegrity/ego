# Feature: remove the last archcheck baseline entry via `internal/logging` (#147, slice S4-1)

Branch: `refactor/147-s4-1-logging` · Base: `origin/main` `f2b5130` · Epic: #10 · Parent issue: #11 · Issue: #147 ·
Design: `openspec/changes/ego-arch-001/design.md` (§2 diagram, §3 Application rules, §4 source-to-destination map)

## Problem

`internal/cmd/archcheck/baseline.go` carried exactly one baseline entry: `migration -> ego`
(`application-no-runtime`). Its justification text said `migration` "replays through ego's runtime
types", but that was stale — the only production use of package `ego` from `migration` was
`ego.ResolveLogger` (`migration/migration.go:149`, `migration/tenant_adoption.go:516`, defined at
`logger.go:84`), which resolves a nil-safe default kit-logger `Logger`. That single use does not
depend on the runtime SPI RUNTIME-001/002 (the rest of issue #147) at all, so the maintainer approved
retiring it independently and first (issue #147, decision 4, 2026-09-27): move `DefaultLogger`,
the typed-nil detection and `ResolveLogger` into a new runtime-free internal package,
`internal/logging`, and have `migration` import that instead of `ego`.

## What changes in this slice

- New package `internal/logging` (`internal/logging/logging.go`) holds `DefaultLogger`,
  `isNilLogger` and `ResolveLogger`. It imports only `kit-logger` and the standard library
  (`reflect`) — no GoAkt, no `internal/extensions`, no root package.
- `logger.go`'s `ego.DefaultLogger` and `ego.ResolveLogger` keep their exact signature and behavior
  and now delegate to `internal/logging`, so `ego.DefaultLogger()` still returns the same
  `kitlog.L()` instance every caller compared against with `assert.Same` before. `loggerAdapter`
  (which imports `goakt/v4/log`) stays in package `ego`, untouched.
- `migration/migration.go`, `migration/option.go` and `migration/tenant_adoption.go` import
  `internal/logging` instead of package `ego` in production code, and their doc comments that named
  `ego.DefaultLogger()` now describe the same behavior without requiring the reader to know migration
  imports `ego` (it no longer does). Migration's tests are unaffected and may still import `ego`.
- `internal/cmd/archcheck/baseline.go`'s `repoBaseline` is now an empty slice; the removed entry's
  justification text (which was already stale per #147's problem statement) goes with it.
- `internal/cmd/archcheck/rules/evaluate_test.go` gains two tests proving `Evaluate` and
  `ValidateBaseline` accept an empty baseline (this was previously only implied by tests that happened
  to pass `nil`, never asserted for an explicit empty slice, which is what `repoBaseline` now is).
- `migration/closure_test.go` (new) shells out to `go list -deps .` for the `migration` package and
  fails if the root package `ego` or any `github.com/tochemey/goakt/v4/...` package reappears in its
  production closure — the RED/GREEN guard for this slice, mirroring the publishers'
  `TestUnitTestClosureExcludesRuntimeAndRoot` (#122).
- `openspec/changes/ego-arch-001/design.md` §2 (diagram + notes), §3 (Application rules) and §4
  (source-to-destination map) updated for the removed `migration -> ego` edge and the new
  `internal/logging` package.
- `CHANGELOG.md`: one entry under "Improvements" (Unreleased) — no public API change.

## Why this shape

**Delegate, don't duplicate.** `ego.DefaultLogger`/`ego.ResolveLogger` could have kept their own
copy of the typed-nil check instead of delegating, but that would leave two implementations to keep
in sync and risks the identity test (`assert.Same(t, DefaultLogger(), ResolveLogger(nil))`) drifting
if one copy changed and the other did not. Delegation keeps exactly one implementation.

**`internal/logging`, not a new top-level package.** The issue text names `internal/logging`
explicitly (decision 4) as a package with no runtime dependency that both `ego` and `migration` can
import without either depending on the other for this. It is not a contract (nothing outside `ego`
and `migration` needs it yet), so `internal/` is the right visibility, matching `internal/queue` and
`internal/syncmap`.

**Known, out-of-scope staleness left alone.** `option.go:517`'s comment ("mirrors isNilLogger
(logger.go:72)") now points at a line that no longer defines `isNilLogger` — the function moved
entirely to `internal/logging`. `option.go` is explicitly outside this slice's file ownership (root
`option.go`, not `migration/option.go`), so the comment is left as-is; a maintainer or a later slice
can update the cross-reference.

## Resolved TDD mode

Strict TDD enabled (user global configuration, `~/.claude/CLAUDE.md`). Runner: `go test`
(`unset GOROOT`; Go 1.27.1 resolved via the module's toolchain directive; golangci-lint needed the
fallback `GOROOT=/home/pablog/sdk/go1.26.6 GOTOOLCHAIN=local` with that GOROOT's `bin/` prepended to
PATH — the toolchain-selected `go` in `$GOPATH/pkg/mod/golang.org/toolchain@.../bin` otherwise wins
`PATH` lookup and mismatches golangci-lint's own go1.26.6 build, breaking the assembler).

## Tasks

- [x] T1 `internal/logging` package: `DefaultLogger`, `isNilLogger`, `ResolveLogger`, importing only
  kit-logger and `reflect` (route: inline; single mechanical, already-understood package).
- [x] T2 `logger.go` delegates; identity and behavior tests pass unchanged (route: inline).
- [x] T3 `migration/migration.go`, `migration/option.go`, `migration/tenant_adoption.go`: import
  switch + comment updates; RED closure test before the switch, GREEN after (route: inline).
- [x] T4 `internal/cmd/archcheck/baseline.go`: empty `repoBaseline`; two new empty-baseline tests in
  `rules/evaluate_test.go` (route: inline).
- [x] T5 `openspec/changes/ego-arch-001/design.md` §2–§4, `CHANGELOG.md`, this task document
  (route: inline).

All five tasks landed together in one work-unit commit range on this branch (the slice is small and
every task depends on the last); see Progress and evidence below for the commit(s).

## Acceptance criteria and checks

- `repoBaseline` is empty; `archcheck` on the real repository reports
  `0 baselined, 0 violation(s), 0 stale entries`.
- `go list -deps github.com/pablogore/ego/v4/migration` (production) contains neither the root
  package `github.com/pablogore/ego/v4` nor any `github.com/tochemey/goakt/v4` package.
- `ego.ResolveLogger`/`ego.DefaultLogger` keep signature, behavior and identity; existing tests in
  `logger_test.go`, `option_test.go` and `migration/migration_test.go` pass unchanged.
- `apidiff` of package `ego` and package `migration` against `origin/main`: no changes at all (not
  even additions).
- Root suite, nested modules (`verify-module.sh`), golangci-lint (`--new-from-rev=origin/main`) and
  `ciselect -base origin/main` all pass/behave as expected.

## Progress and evidence

- **RED (migration closure).** Before the import switch, `go test ./migration/... -run
  TestProductionClosureExcludesRootAndGoAkt -v` failed: 32 lines, one per `github.com/tochemey/goakt/v4/...`
  package plus one for the root `github.com/pablogore/ego/v4`, all reachable through `ego.ResolveLogger`'s
  import of package `ego`.
- **GREEN (migration closure).** After the switch, the same test passes; `go list -deps
  github.com/pablogore/ego/v4/migration | rg "pablogore/ego/v4$|tochemey/goakt"` returns nothing.
- **`go list -deps` before:** included `github.com/pablogore/ego/v4` (root) and ~40
  `github.com/tochemey/goakt/v4/...` packages (`extension`, `actor`, `remote`, `crdt`, `discovery`,
  `supervisor`, `tls`, and their `internal/...` subpackages, etc.).
  **After:** neither appears.
- **archcheck before (with the stale entry still present after the migration fix, to prove the entry
  really was removable):** `archcheck: 8 modules checked, 46 packages checked, 194 edges checked,
  0 baselined, 0 violation(s), 1 stale entries` (exit 1, "no longer matches a violation; delete it").
  **After removing the entry:** `archcheck: 8 modules checked, 46 packages checked, 194 edges
  checked, 0 baselined, 0 violation(s), 0 stale entries` (exit 0).
- **New archcheck tests.** `TestValidateBaseline_EmptyBaselineIsValid` and
  `TestEvaluate_EmptyBaselineOnCleanGraphReportsZero` pass; `go test
  ./internal/cmd/archcheck/...` passes in full.
- **Identity.** `go test . -run
  'TestResolveLogger|TestDefaultLogger|TestKitLoggerIsTheOnlyLoggingBackend' -v`: all pass, including
  `TestDefaultLoggerIsKitLoggerGlobal` (`assert.Same(t, kitlog.L(), DefaultLogger())`).
- **apidiff.** Snapshotted package `ego` and package `migration` from a detached `origin/main`
  worktree (`f2b5130`), then diffed against this branch's working tree: both produce empty output —
  no additions, no incompatible changes, for either package.
- **golangci-lint.** `go mod tidy && go mod vendor` (matches `pull_request.yml`/`build.yml`'s lint
  prerequisite), then `GOFLAGS=-mod=vendor golangci-lint run --new-from-rev=origin/main ./...` with
  `GOROOT=/home/pablog/sdk/go1.26.6` prepended to `PATH`: `0 issues`.
- **Root suite.** `go build ./...`, `go vet ./...` clean. Full `go test ./...`: all packages `ok`
  (root package 381.6s, everything else sub-second; no `-race`).
- **Nested modules.** `scripts/ci/verify-module.sh` passes (download, `go mod tidy -diff`, build,
  vet, lint, test where applicable) for all 7: `benchmark`, `example/cluster`, `publisher/kafka`,
  `publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `test/compat`. `publisher/kafka` and
  `example/cluster` needed the golangci-lint `GOROOT=/home/pablog/sdk/go1.26.6 GOTOOLCHAIN=local`
  fallback (with that `GOROOT`'s `bin/` first on `PATH`) to avoid a `crypto/internal/randutil`
  typecheck failure against the go1.27.1 standard library — the same environment caveat as the root
  lint run; `benchmark` did not hit it (no crypto-touching import), so it passed even without the
  fallback.
- **`ciselect -base origin/main`.** Mode `full` ("logger.go changed (shared root package)"); all 28
  root packages and all 7 nested modules selected (`benchmark ← .`, `example/cluster ← .`, all four
  publishers `← .`, `test/compat ← .`) — expected, since `logger.go` (root package `ego`) is on the
  changed-file list and every nested module still imports the root package directly or transitively.

## Next step

Open the PR against `main`; the writer does not merge. Native review (RDD) was not run by the
writer.
