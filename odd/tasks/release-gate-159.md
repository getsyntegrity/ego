# Release gate for `v*` tags (#159, F4 PR-B)

## Problem

`release.yml` fires on every push of a `v*` tag and immediately creates a
GitHub Release, then bumps and tags the four `publisher/*` modules against
it. Nothing checks that the tagged commit ever had a green `build.yml` run.
A human can tag a broken or unreviewed commit and the whole publish chain
runs anyway.

## What changes

A new job `gate` runs first in `release.yml`, before `release-ego` (which
creates the GitHub Release) and before `release-publishers` (which bumps
and tags the publisher modules, `needs: release-ego` already). `gate`:

1. Dereferences the pushed tag to its commit (`git rev-parse "$TAG^{commit}"`
   — handles a lightweight tag pointing at an annotated tag object).
2. Confirms that commit is reachable from `origin/main` (`git merge-base
   --is-ancestor`); rejects the tag otherwise.
3. Runs a new Go CLI, `internal/cmd/releasegate`, which polls the GitHub
   REST API for `build.yml` runs on that exact commit SHA and fails unless
   the most recent completed run's conclusion is `success`. The wait is
   bounded (default 20 minutes) so an in-flight `build.yml` run gets a
   chance to finish, but the job does not hang forever.

`release-ego` becomes `needs: gate`; `release-publishers`'s existing
`needs: release-ego` is untouched, so the full chain is
`gate → release-ego → release-publishers`. No publisher step's logic or
order changes.

## Why

The maintainer wants a hard backstop: nothing publishes off a commit that
never had, or no longer has, a green `build.yml` run for that exact SHA.
"Green on a nearby commit" or "green a while ago on a different SHA" must
not count.

## Scope

- New package `internal/cmd/releasegate` (root module, standard library
  only): a pure decision function (`Decide`) plus a thin GitHub REST client
  and a bounded-wait CLI.
- `.github/workflows/release.yml`: new `gate` job; `release-ego` gains
  `needs: gate`.
- `docs/ci.md`: new "Release gate" section.
- This file and its Engram mirror (`odd/release-gate-159/tasks`).

**Out of scope / explicitly not touched:** `internal/cmd/releaseplan/**`,
`scripts/ci/release-modules.txt` (branch `feat/159-f4a-releaseplan`, PR
#169), the publisher steps' own logic, and the pre-existing
`git push origin HEAD:main` conflict with branch-protection documented
below.

## Constraints

- Standard library only in `internal/cmd/releasegate` (no new module
  dependency for one CLI).
- Every `${{ }}` expression in a `release.yml` `run:` body goes through
  `env:`, per this repo's existing convention (see `build.yml`,
  `pull_request.yml`).
- Strict TDD: RED (failing test) observed before GREEN for every non-trivial
  unit.
- `go test` runs without `-race`; no workbench.

## Known limitation to document, not fix

`release.yml:169-170` (existing `release-publishers` job) commits publisher
`go.mod`/`go.sum` bumps and runs `git push origin HEAD:main`. `main` is
protected with a required, strict "CI Gate" check and
`enforce_admins: true`, so this direct push of a commit that never ran
"CI Gate" is rejected by GitHub (`GH006: Protected branch update failed`).
This is pre-existing and out of scope for this change; documented in
`docs/ci.md` and flagged in the final report for a maintainer decision.

## TDD

Mode: **strict** (user's global `CLAUDE.md`: "Strict TDD Mode: enabled").
Runner: `GOROOT= GOWORK=off GOFLAGS=-mod=mod /home/pablog/sdk/go1.26.6/bin/go test -count=1 ./internal/cmd/releasegate/...`.
Route: delegated direct is available per the mandatory triggers, but this
session is already the dedicated worker for the whole task (see harness
note), so tasks run inline in this same session; each task is still one
bounded, independently-verified unit.

## Tasks

- [x] **T1 — Pure decision function.** `internal/cmd/releasegate/decision.go`:
  `Decide(sha string, onMain bool, runs []Run) Result` (Pass/Wait/Fail +
  reason). Tests: success (latest completed run for SHA is `success`),
  different SHA (no match for target SHA among other SHAs' runs), failure,
  cancellation, pending (latest run for SHA is `in_progress`/`queued` →
  `Wait`), no run for SHA (`Wait`), off-main (`Fail` regardless of runs),
  plus the "most recent completed run governs" rule (3 sub-cases). RED
  observed: compile failure, `Run`/`Decide`/`Fail`/`Wait`/`Pass` undefined.
  GREEN: `go test -count=1 -v ./internal/cmd/releasegate/...` — 12/12 PASS.
  `gofmt -l` clean. `go vet`/`go build` on the package itself only succeed
  once T3 adds `func main`; expected, standard `package main` multi-file
  layout (matches `internal/cmd/archcheck`'s loader.go/baseline.go split).
  Commit: (recorded after commit below).
- [x] **T2 — GitHub REST client.** `internal/cmd/releasegate/client.go`:
  `Client.ListBuildRuns(ctx, repo, sha)` against
  `/repos/{owner}/{repo}/actions/workflows/build.yml/runs?head_sha=...`,
  paginated, `httptest.Server`-testable via `Client.BaseURL`. Filters by
  `head_sha` only (not `branch`/`event`) — see client.go's doc comment and
  docs/ci.md for why. RED observed: compile failure, `Client`/`NewClient`/
  `DefaultBaseURL` undefined. GREEN: `go test -count=1 -v
  ./internal/cmd/releasegate/...` — 18/18 PASS (6 new client tests: single
  page, pagination across 2 pages/101 runs, 401 non-OK status, malformed
  JSON, no-token omits Authorization header, NewClient defaults). `gofmt
  -l` clean.
- [ ] **T3 — CLI with bounded wait.** `internal/cmd/releasegate/main.go`:
  flags `-repo -sha -on-main -timeout -interval`, `GITHUB_TOKEN` env,
  injectable clock/sleeper. Tests: pass, fail, wait-then-timeout (fake
  clock/sleeper, no real sleeping), `-timeout 0` single-check mode, missing
  required flag/env. Check: `go test -count=1 -v
  ./internal/cmd/releasegate/...` RED then GREEN; `go vet
  ./internal/cmd/releasegate/...`; `staticcheck ./internal/cmd/releasegate/...`.
- [ ] **T4 — `release.yml` gate job.** Add `gate` (permissions
  `contents: read`, `actions: read`; `fetch-depth: 0`; dereference tag;
  `git merge-base --is-ancestor`; run `releasegate`); `release-ego` gains
  `needs: gate`. No publisher step touched. Check: `actionlint
  .github/workflows/release.yml` (0 findings); `bash -n` on every changed
  `run:` block; no `${{` inside a `run:` body; `git diff
  origin/main...HEAD -- .github/workflows/release.yml` shows only the new
  job and the `needs` line.
- [ ] **T5 — Docs + real dry-run.** `docs/ci.md` "Release gate" section
  (what it checks, why exact SHA, bounded wait, how to test without
  publishing, the lightweight-tag dereference note, the GH006 limitation,
  a note that the file needs a rebase if PR #169 merges first). Run the
  real read-only dry test against `getsyntegrity/ego` for a green main SHA,
  a PR-branch SHA with no run, and record results. Check: dry-run output
  captured in this file; `gofmt -l` clean; final full verification pass
  (below).

## Delivery

One work-unit commit per task on this branch (`ci/159-release-gate`),
Conventional Commits, no Co-Authored-By/AI-attribution trailers. Push, PR,
and merge are the coordinator's/user's decision, not this session's.

## Verification (recorded per task as evidence accumulates)

- `go test -count=1 ./internal/cmd/releasegate/...`
- `go vet ./internal/cmd/releasegate/...`
- `staticcheck ./internal/cmd/releasegate/...`
- `actionlint .github/workflows/release.yml`
- real read-only dry runs against `getsyntegrity/ego`
- `git diff --stat origin/main...HEAD`
