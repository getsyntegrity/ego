# Feature: migrate `example/eventssourced` to `compose/goakt` (#105, slice IMPL-5)

Branch: `docs/105-impl-5-example` · Base: `origin/main` `f2b5130` · Epic: #10 · Issue: #105 ·
Design: `openspec/changes/ego-arch-003/design.md` (§2.2, §5.1, §6 row IMPL-5)

## Problem

`example/eventssourced/main.go` still builds its deployment by hand — `ego.NewConfig`,
`goakt.NewActorSystem`, `ego.NewEngine`, `engine.Start` — the exact pattern design.md §2.2 names as the
source of two confirmed leaks: `NewEngine` failing after `sys.Start` already succeeded leaks the actor
system under the example's `os.Exit(1)`, and `_ = engine.Start(ctx)` discards the one error `Start` can
return. IMPL-4 built `compose/goakt`, the composition root that fixes both classes of defect; nothing in
the tree used it yet.

## What changes in this slice

`example/eventssourced/main.go` is rewritten to build and run through `egoakt.New`/`App.Start`/
`App.Stop` (design §5.1's target walkthrough), restructured as `main` calling a `run(ctx, logger) error`
so every defer (`app.Stop`, `eventStore.Disconnect`) executes before the process can exit — no `os.Exit`
call now precedes cleanup, because `os.Exit` only ever runs in `main`, after `run` has already returned
and its defers have already run. No other example, and no production package, changes.

## Why this shape

- `run(ctx, logger) error` instead of inline `os.Exit` at each failure: Go defers run when a function
  returns, not when the process exits, so an `os.Exit` inside the old linear `main` skipped whatever
  defers already existed. Moving every early-return path into `run` means `main`'s only `os.Exit` call
  happens after `run` returns, by which point `defer app.Stop(ctx)` and `defer eventStore.Disconnect(ctx)`
  have already executed.
- `defer eventStore.Disconnect(ctx)` is registered before `defer app.Stop(ctx)`, so it runs *after*
  `app.Stop` at return (Go's LIFO defer order) — matching design §5.1's walkthrough comment ("runs after
  app.Stop") and the ownership rule in §D5 (stores are the consumer's, disconnected only after the
  composition root has released everything it owns).
- The doc reference: `readme.md` links to `./example/eventssourced` in two places (the behavior-contract
  section and the Examples list). Both are plain directory links, not embedded code, and both examples
  still resolve to a working directory that builds and runs exactly as advertised, so no text edit was
  needed there. `readme.md`'s own Quick Start section demonstrates the still-supported manual composition
  path (`NewConfig`/`GoaktOptions`/`NewEngine`) with projections and a different actor-system name — design
  §D1 keeps that path supported for v4, so it was left untouched rather than migrated.

## Constraints

- Only `example/eventssourced/**`, its doc reference (none needed textually, see above), `CHANGELOG.md`
  and this task document. No production code, no `compose/`, no `engine.go`/`option.go`, no other example.
- No `os.Exit` before cleanup; the two leaks in design §2.2/§5.1 must no longer reproduce.
- `unset GOROOT`; golangci-lint fallback `GOROOT=/home/pablog/sdk/go1.26.6 GOTOOLCHAIN=local` with `PATH`
  pointed at that SDK's `bin` (golangci-lint is built with go1.26.6; without a matching `PATH` its internal
  `go build` calls mismatch GOROOT and the toolchain binary and fail typechecking `sync/atomic`).
- No `-race`, no workbench.

## Tasks

- [x] **T1** Reproduce both leaks on `main`@`f2b5130` before changing anything: a throwaway harness (own
  `go.mod`, `replace` back to this worktree, discarded after use, never committed) built the actor system
  exactly as the old `main.go` does, then called `NewEngine` with a config declaring an offset store the
  actor system was never given as a GoAkt extension — the same "stale/edited `cfg` between actor-system
  and engine construction" mistake the old code has no guard against. `NewEngine` failed with
  `ErrMissingRequiredExtensions`, and `sys.Running()` was still `true`: exactly the leak the old code's
  `os.Exit(1)` branch (`main.go:69-73` pre-migration) would hit while leaving `sys` running forever. The
  second leak — `_ = engine.Start(ctx)` discarding the only error `Start` can return
  (pre-migration `main.go:75`) — is evidenced by reading `engine.Start`'s only failure branch
  (`engine.go:424-427`, unreachable after a successful `NewEngine`) together with the literal discarded
  assignment in the old source; forcing that branch to fail would require breaking `NewEngine`'s own
  invariant, so this leak is evidenced by source reading, not by execution, per the task brief's allowance.
- [x] **T2** Rewrite `main.go` to build and run through `egoakt.New`/`App.Start`/`App.Stop`, restructured
  around `run(ctx, logger) error` so cleanup always executes before `os.Exit`.
- [x] **T3** Build, vet, gofmt, run end to end with a real `SIGINT`, and confirm clean exit code 0 with
  the correct account balances logged and a "shutdown successfully" record; confirm neither leak
  reproduces (no leaked actor system on a construction failure path exists any more — `App.Start` already
  rolls back partial failures per design §D6 — and every error `run` can receive, including `App.Start`'s,
  is now checked and returned, not discarded).
- [x] **T4** archcheck, golangci-lint (`--new-from-rev=origin/main`), ciselect + the suite it selects,
  CHANGELOG, this document, PR.

## Verification evidence

- **Before (leak repro, throwaway, not committed):** actor system reported `sys.Running() = true`
  immediately after `NewEngine` failed with `ErrMissingRequiredExtensions`; the repro printed
  `LEAK 1 REPRODUCED` and confirmed the started system was never stopped by the failure path, matching
  `main.go:69-73` pre-migration exactly. Full transcript in the PR description.
- **After (migrated example, real run):** `go build ./example/eventssourced/...` succeeds; `gofmt -l`
  reports no files; `go vet ./example/eventssourced/...` reports nothing. Built binary run in the
  background, sent a real `SIGINT` ~1.5s in: logs `current balance on opening balance=500`,
  `current balance after a credit of 250 balance=750`, then on the signal `shutdown process begins` …
  `shutdown successfully`; process exit code `0`. No `os.Exit` call executes before `app.Stop`/
  `eventStore.Disconnect` — `main`'s only `os.Exit` runs after `run` returns.
- `archcheck`: `8 modules checked, 45 packages checked, 193 edges checked, 1 baselined, 0 violation(s), 0
  stale entries` (one edge more than IMPL-4's post-state, from `example/eventssourced` now importing
  `compose`/`compose/goakt`; `composition-leaf` allows this — examples are consumers). No new baseline
  entry.
- `golangci-lint run --new-from-rev=origin/main ./...` (go1.26.6, `PATH` pointed at that SDK,
  `GOFLAGS=-mod=vendor` after a clean `go mod tidy && go mod vendor`, matching CI): `0 issues.`
- `ciselect -changed <diff> -base origin/main`: falls back to `full` mode (27 of 27 packages; a leaf
  `main` package with no reverse dependents left an otherwise-empty selection). `GO_TEST_RACE=0
  scripts/ci/go-test.sh`: see PR description for the full pass/fail readout.
- `go mod tidy`: no diff (`go.mod`/`go.sum` unchanged).

## Next step

None: IMPL-5 is the last row `#105`'s GoAkt half needs. IMPL-6 (`compose/inmem`) stays blocked on `#123`,
the in-memory runtime and the runtime-neutral engine API (design §5.2, §9); `#105` stays open until then.
