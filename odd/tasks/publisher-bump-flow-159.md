# Publisher bump-and-tag flow (issue #159, F4)

## Problem

`main` is now protected (CI Gate required, strict, `enforce_admins`), and
`can_approve_pull_request_reviews` stays disabled so no workflow may open a
PR. The current `release-publishers` job in `.github/workflows/release.yml`
still does `git push origin HEAD:main` (line ~238) and pushes publisher tags
directly (line ~277) after tagging `ego`. That direct push now fails against
branch protection (`docs/main-branch-policy.md`, "Known conflict with
release.yml" paragraph) and, even if it didn't, tagging happens before a
human has looked at the bump — publishers must only be tagged from a commit
that already passed the full `build.yml` suite on `main`.

## What changes

1. `release.yml`'s publisher job stops after **preparing and pushing a
   branch** `release/publishers-<tag>` (no push to `main`, no publisher
   tags/releases here).
2. A new `workflow_dispatch`-only workflow, `release-publishers.yml`, is the
   explicit continuation: given the SHA of the *merged* bump PR and the ego
   version, it validates everything is safe, then (unless `dry_run`) creates
   and pushes the publisher tags and their GitHub releases.
3. `internal/cmd/releaseplan` gains the testable decision logic the
   continuation needs (required-version check, publishers-only tag
   computation, tag-conflict check) instead of ad hoc bash.
4. `internal/cmd/releasegate` gets the PR #171 follow-up: fail fast on
   permanent HTTP errors (401/404/403-non-rate-limited), keep retrying
   5xx/429/network/403-rate-limited.
5. Docs (`docs/ci.md`, `docs/main-branch-policy.md`) describe the resolved
   flow instead of the open conflict.

## Why

Binding maintainer decisions (from the request): nothing may push to `main`;
no workflow creates a PR; publishers are tagged only from a commit that has
already passed `build.yml` on `main`. A human must open the PR that merges
the bump branch; the continuation workflow is a deliberate, auditable,
dispatched-by-hand step, never an automatic re-run of `release.yml` (whose
checkout is the root tag, not the bump commit).

## Scope

In scope: `.github/workflows/release.yml` (publisher job only — `gate` and
`release-ego` untouched), new `.github/workflows/release-publishers.yml`,
`internal/cmd/releaseplan` (new flags/functions), `internal/cmd/releasegate`
(error classification), `docs/ci.md`, `docs/main-branch-policy.md`.

Out of scope: `gate`/`release-ego` jobs, `scripts/ci/verify-published.sh`
(reused unchanged), any real tag/push/PR (this branch is never pushed by me).

## Design decision: extend `releaseplan`, not a new tool

`internal/cmd/releaseplan` already has `discoverGraph`/`readGoMod` (full
`go.mod` `require` list per module, including the root's required version)
and `nextTag` (tag math per module, already tested). It is `package main` in
one directory, so new flags/files land in the same package with no import
boundary to cross. **Rejected alternative:** a standalone
`internal/cmd/releasetags` tool — would either duplicate `discoverGraph`,
`readGoMod`, and `nextTag`, or require lifting them into a shared internal
library first (extra churn for no behavioral gain), and would duplicate CLI
flag/plan-output plumbing that `releaseplan` already has. Extending
`releaseplan` is the smaller, faster, equally testable change.

The SHA-on-main check (item b) and the final tag push/GitHub-release creation
(item h) stay as workflow bash/`gh`/`git`, reusing the exact
`git merge-base --is-ancestor` pattern the `gate` job already uses, and the
exact `gh release create` block the old publisher job already used — both
are "already tested" in the sense of being unchanged, proven code, not new
untested logic. The new Go code covers the pure decisions: version format
validation, go.mod-required-version check, publishers-only tag computation,
and tag-existence conflict check (against a combined local+origin tag list
the workflow gathers with `git tag -l` + `git ls-remote --tags origin`).

## TDD

Strict TDD (per user's global `Strict TDD Mode: enabled`). Runner:
`GOROOT= GOWORK=off GOFLAGS=-mod=mod /home/pablog/sdk/go1.26.6/bin/go test
-count=1 ./internal/cmd/releasegate/... ./internal/cmd/releaseplan/...`.
Every Go change: observed RED (new/changed test fails first), then GREEN.
Workflow YAML has no test runner; verification is `actionlint` (0 findings),
YAML parse, `bash -n` on every `run:` body, and a scan confirming no raw
`${{ }}` inside a `run:` body (everything through `env:`).

Route: delegated direct for every task below (each touches 2+ non-trivial
files, or new Go files + tests). No SDD artifacts.

## Delivery

Every task ends in exactly one Conventional Commit on this branch
(`ci/159-publisher-bump-flow`), no push, no PR, no tags — those remain the
coordinator's decision. Delivery strategy: `single-pr` (small enough
forecast, ~<400 changed lines is not expected to hold for the whole feature,
but pushing/PR is out of scope for this session regardless).

## Tasks

- [x] **T1 — Split `release.yml`'s publisher job into a bump-branch
  preparer.** Remove `git push origin HEAD:main`, publisher tag
  creation/push, and the publisher GitHub-release step from
  `release-publishers` (job renamed, e.g. `prepare-publisher-bump`). Keep the
  per-publisher `go get`/`go mod tidy`/`verify-published.sh` steps and order
  unchanged. New behavior: commit the bumps on a new branch
  `release/publishers-<tag>` created from `origin/main`, push only that
  branch (fail clearly if it already exists on origin), write to
  `$GITHUB_STEP_SUMMARY` the compare link and the exact
  `gh pr create --base main --head release/publishers-<tag> ...` command plus
  a short pointer to the `release-publishers.yml` continuation. Job
  permissions: `contents: write` only on the step(s) that push the branch;
  everything else read.
  Check: `actionlint` 0 findings on `release.yml`; `bash -n` on every
  changed `run:` body; no raw `${{ }}` inside any `run:` body; `rg` shows no
  `push origin HEAD:main` and no publisher `git tag`/`gh release create` left
  in this file.

- [ ] **T2 — releasegate error classification (PR #171 follow-up).**
  `internal/cmd/releasegate/client.go`: classify `ListBuildRuns`'s non-200
  response into permanent (401, 404, 403-not-rate-limited) vs retryable
  (5xx, 429, network errors, 403 with `X-RateLimit-Remaining: 0` or a
  `Retry-After`/secondary-rate-limit message). `main.go`'s `waitForGate`:
  fail fast (return immediately, no more retries) on a permanent error.
  Add a test for `-timeout 0` combined with a fetch error (must `Fail`).
  Update `docs/ci.md`'s "Release gate" section's retry paragraph to name the
  exact retried vs fail-fast codes.
  Check: strict TDD RED→GREEN evidence for the new/changed tests; `go test
  -count=1 ./internal/cmd/releasegate/...` green; `go vet`, `staticcheck`,
  `gofmt -l` clean on changed files.

- [ ] **T3 — `releaseplan` continuation decision logic.** Add flags/functions
  for: (a) SHA (40 lowercase hex) and `ego_version` (`v\d+\.\d+\.\d+`) format
  validation; (d) per released publisher, does its `go.mod` `require` the
  root module at exactly the given version (using existing `readGoMod`,
  never hard-coding publisher names — read them from the release list); (e)
  publishers-only next-tag computation at a given SHA/bump using the existing
  `nextTag`, with the root excluded from the release list; (f) tag-existence
  conflict check against a combined tag list file (local + origin), failing
  and naming every tag that already exists. Follow the existing table-driven,
  `testdata/`-fixture test style.
  Check: strict TDD RED→GREEN evidence; `go test -count=1
  ./internal/cmd/releaseplan/...` green; `go vet`, `staticcheck`, `gofmt -l`
  clean; a manual dry run of the new flags against this repo's real
  `origin/main` state is expected to FAIL the (d) check (publishers require
  ego v4.4.3 today) with a clear message — capture that output — and a
  `testdata`-fixture run is expected to PASS.

- [ ] **T4 — New `release-publishers.yml` workflow.** `workflow_dispatch`
  only, inputs `sha` (required), `ego_version` (required), `bump`
  (patch|minor|major, default patch), `dry_run` (boolean, default true).
  Steps a–h per the request, using T2's `releasegate` (step c) and T3's
  `releaseplan` flags (steps a/d/e/f), bash `git merge-base --is-ancestor`
  for step b (and its step-g re-check), `git ls-remote --tags` + local
  `git tag -l` feeding T3's conflict check for step f (and its step-g
  re-check), and on `dry_run=false` `git push --atomic origin <tags…>` plus
  the exact `gh release create` block moved verbatim from the old
  `release-publishers` job for step h. `dry_run=true` prints the plan and
  exits 0 without creating anything. Permissions: `contents: write`,
  `actions: read`; every `${{ }}` reaches `run:` bodies only via `env:`.
  Check: `actionlint` 0 findings; YAML parse; `bash -n` on every `run:` body;
  no raw `${{ }}` inside any `run:` body (show the scan); `rg` confirms no
  `push origin HEAD:main` and no `gh pr create` outside an echoed/quoted
  string.

- [ ] **T5 — Docs.** `docs/ci.md`: rewrite the "Version policy" description
  of the flow (two-stage: prepare-branch job stops before tagging,
  `release-publishers.yml` tags) and replace the closing "known,
  pre-existing limitation" paragraph (the `GH006` direct-push conflict) with
  a description of the resolved flow — human opens the PR, no
  approval-free CI because a human opened it, how to dry-run the
  continuation without publishing, the continuation's inputs. Leave T2's
  retry-classification paragraph as T2 already wrote it.
  `docs/main-branch-policy.md`: replace the "Known conflict with
  release.yml" paragraph with the resolved flow (no more direct push;
  bump branch + human PR + explicit dispatched continuation).
  Check: docs read cleanly (plain prose, real paths/commands per the user's
  document-readability rules); no leftover reference to `git push origin
  HEAD:main` as current behavior.

## Progress

- Branch `ci/159-publisher-bump-flow` created from `origin/main` at
  `4458402` (confirmed via `git merge-base --is-ancestor`, exact match to
  the required commit).
- Exploration complete (release.yml job graph, releasegate retry logic,
  releaseplan reusable pieces, docs sections, publisher go.mod state) —
  findings folded into Scope/Design/Tasks above.
- TDD mode: strict, source = user's global CLAUDE.md rule
  (`Strict TDD Mode: enabled`), runner =
  `GOROOT= GOWORK=off GOFLAGS=-mod=mod /home/pablog/sdk/go1.26.6/bin/go test`.
- **T1 done.** `release-publishers` renamed to `prepare-publisher-bump`;
  `gate`/`release-ego` untouched. Removed: `git push origin HEAD:main`, the
  `bump-type` step (only ever fed tag/release creation, which moved out),
  publisher tag creation/push, and the "Create publisher GitHub releases"
  step (its `gh release create` block is preserved verbatim for T4 — see the
  session report). New steps: "Create publisher bump branch from
  origin/main" (fetches `origin/main`, `git ls-remote --exit-code --heads
  origin "$BRANCH"` conflict check with `::error::` on collision, `git
  checkout -B "$BRANCH" origin/main`), "Commit and push publisher bump
  branch" (commits staged `go.mod`/`go.sum` only if there is a diff, pushes
  only that branch), "Write step summary" (compare link + exact `gh pr
  create` command + pointer to the future `release-publishers.yml`
  dispatch). Every `${{ }}` used in a `run:` body now goes through `env:`
  (fixed 3 pre-existing direct uses in this job; `gate`'s existing `env:`
  usage was already correct and untouched).
  Job permissions narrowed to `contents: write` only (was inheriting
  workflow-level `contents: write` implicitly before). Checkout token
  switched from `secrets.RELEASE_PAT` to `secrets.GITHUB_TOKEN` — this job
  no longer pushes to protected `main`, only to a fresh unprotected
  `release/publishers-<tag>` branch, which the job's own `contents: write`
  permission already covers.
  Verification: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))"` →
  parsed clean. `bash -n` on all 7 `run:` blocks in `prepare-publisher-bump`
  (extracted via a small Python/yaml script) → all OK. `rg -n '\$\{\{'
  .github/workflows/release.yml` → every match is in an `env:`/`with:`
  mapping position (lines 67-70 `gate`'s pre-existing `env:`; 116 checkout
  `token:`; the rest `env:` in the new/changed steps) — none inside a
  `run:` body. `rg -n 'push origin HEAD:main' .github/workflows/release.yml`
  → no matches (exit 1). `actionlint` on the whole file → 0 findings.
  Commit: see repo `git log -1` on this branch (single Conventional Commit,
  no AI-attribution trailer, only `.github/workflows/release.yml` and this
  file staged).
- Next: T2.
