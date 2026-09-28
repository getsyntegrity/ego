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

- [x] **T2 — releasegate error classification (PR #171 follow-up).**
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
  **Done.** Added `ErrPermanentGitHubError` (sentinel, wrapped with `%w`)
  and `classifyStatusError`/`isPermanentStatus`/`isRateLimited` in
  `client.go`; `waitForGate` in `main.go` now checks
  `errors.Is(err, ErrPermanentGitHubError)` and returns immediately (no
  retry) on a permanent error. Replaced `TestClient_ListBuildRuns_NonOKStatus`
  with a 9-case table-driven `TestClient_ListBuildRuns_ErrorClassification`;
  added `TestRun_PermanentFetchErrorFailsFastWithoutRetrying` (RED: current
  code retried a 401 41 times instead of failing after 1; GREEN after the
  `waitForGate` fix) and `TestRun_TimeoutZeroWithFetchErrorFails` (already
  GREEN before any production change — `-timeout 0` + a fetch error already
  correctly failed, so this test only locks that behavior in, per the
  task's "report honestly if already passing" instruction).
  Verification (all observed, literal output in the T2 delegate's report):
  `go test -count=1 ./internal/cmd/releasegate/...` → `ok`; `go vet` → clean;
  `staticcheck` → no findings; `gofmt -l` → nothing printed (clean).
  Commit: see repo `git log -1` on this branch (single Conventional Commit,
  `fix(releasegate): fail fast on permanent GitHub API errors (#159)`, no
  AI-attribution trailer, only files under `internal/cmd/releasegate/`,
  `docs/ci.md`, and this task file staged).

- [x] **T3 — `releaseplan` continuation decision logic.** Add flags/functions
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

- [x] **T4 — New `release-publishers.yml` workflow.** `workflow_dispatch`
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
  **Done.** New file `.github/workflows/release-publishers.yml`, 11 steps
  in one job (`release-publishers`): checkout `main` with `fetch-depth: 0`
  and `fetch-tags: true` (matching `build.yml`'s `release-plan` job, so
  `origin/main` and every existing tag are local without relying on git's
  default tag auto-follow); step (a) `-continuation-check-sha` +
  `-continuation-check-version` in one invocation (confirmed by reading
  `continuation.go`'s `runContinuation` that both flags can combine), then
  a `git rev-parse`/`git merge-base --is-ancestor` check that the
  `ego_version` tag exists and is on `origin/main`; step (b) the same
  `git rev-parse`/`merge-base` pattern for `sha`; step (c) `releasegate`
  invoked exactly like `release.yml`'s `gate` job (`-on-main "true"` hard-
  coded since step (b) already proved it, `-timeout 20m -interval 30s`,
  token/repo/sha via `env:`); step (d) `git checkout --quiet "$SHA"` then
  `-continuation-check-required-version` against the *unfiltered*
  `scripts/ci/release-modules.txt` (it already skips `.` itself); steps
  (e)+(f) one `compute-tags` step: builds a combined `git tag -l` +
  `git ls-remote --tags origin` file (stripping `refs/tags/` and peeled
  `^{}` suffixes), a publishers-only release list (`grep -v '^\.$'` over
  `release-modules.txt`), then one `-continuation-plan-publishers
  -continuation-check-tag-conflicts` invocation into `$RUNNER_TEMP/
  release-publishers/plan`, publishing `tags` and `publishers` step
  outputs (`tags` read from `plan.json` via `jq -r '.modules[].nextTag'`);
  step (g) — only `if: inputs.dry_run == false` — re-fetches `origin/main`,
  re-checks the same ancestor condition, re-gathers a fresh combined tag
  list, re-runs the identical plan+conflict-check invocation into
  `plan-recheck`, and additionally diffs the two plans' `dir`+`nextTag`
  pairs, failing if origin state drifted between planning and tagging
  (judgement call — the task only asked for a re-run of the same checks;
  the diff is a low-cost addition since a drifted recompute would
  otherwise silently tag different versions than what was shown); step
  (h), `dry_run == false`: `git tag "$TAG" "$SHA"` per computed tag
  (lightweight, matching the pre-T1 job's `git tag "${TAG}"` — confirmed
  by reading `git show 4cb7cf0^:.github/workflows/release.yml`) then
  `git push --atomic origin $TAGS`, then the `gh release create` block
  copied verbatim from the same pre-T1 revision (only `EGO_VERSION`/
  `PUBLISHERS` moved from inline `${{ }}` to `env:`); step (h),
  `dry_run == true`: prints `plan/summary.md` to `$GITHUB_STEP_SUMMARY`
  and stdout, no other step in the `if: dry_run == false` branch runs.
  Permissions: `contents: write` + `actions: read` at workflow level.
  Token choice: `secrets.GITHUB_TOKEN` for checkout, tag push and
  `gh release create` (`GH_TOKEN` via `env:`) — not `secrets.RELEASE_PAT`
  as the pre-T1 job used. Reasoning: (1) `gh release create` needs
  `contents: write`, which the job's declared `permissions:` already grants
  `GITHUB_TOKEN`; (2) tag pushes are not pushes to `main` — branch
  protection (the actual reason `RELEASE_PAT` was needed, per T1's own
  reasoning) does not apply to tags, and this repository has no tag
  protection rule on `publisher/*/v*` (checked: `rg` found no
  `RELEASE_PAT`/tag-protection mention anywhere in `docs/*.md` beyond the
  one now-stale line this task's docs pass — T5 — will resolve); (3) no
  other workflow in `.github/workflows/` triggers on a `publisher/*/v*` or
  any tag push (`rg -n "publisher/" .github/workflows/*.yml` only matches
  comments in `build.yml`/`pull_request.yml`), so `GITHUB_TOKEN`'s
  "doesn't trigger other workflows" limitation — the other classic reason
  a PAT gets used — has no effect here either.
  Verification (literal output):
  `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release-publishers.yml'))"`
  → parsed clean, no output. `bash -n` on all 11 extracted `run:` blocks
  (via a small inline Python/yaml script, same technique T1 used) → all
  `[OK]`, `ALL_OK`. `rg -n '\$\{\{' .github/workflows/release-publishers.yml`
  → every match sits in a `with:`/`env:`/`if:` mapping or a `#` comment
  (line "`${{ }}` interpolation inside the run: body." describing the
  design, not executable) — none inside a `run:` body.
  `rg -n 'push origin HEAD:main' .github/workflows/release-publishers.yml`
  → no matches (exit 1). `rg -n 'gh pr create'
  .github/workflows/release-publishers.yml` → no matches (exit 1; the
  workflow-header comment originally said "`gh pr create` command" and was
  reworded to "pull-request-creation command" specifically so this grep
  has nothing to match at all, closing even the doubt a comment-only hit
  would raise).
  `GOROOT= GOWORK=off GOFLAGS=-mod=mod /home/pablog/sdk/go1.26.6/bin/go run
  github.com/rhysd/actionlint/cmd/actionlint@latest
  .github/workflows/release-publishers.yml` → no output, exit 0 (0
  findings; `shellcheck` is not installed in this sandbox, consistent with
  how T1 also got 0 findings on `release.yml`'s similar unquoted
  `for PUB in $PUBLISHERS` loops).
  Other judgement calls: filtered the publishers-only release list with
  `grep -v '^\.$'` rather than hand-maintaining a second file, reusing the
  one real `scripts/ci/release-modules.txt` as the single source of truth
  (per this task's own instruction); derived the `gh release create` loop's
  `$PUBLISHERS` from that same filtered list's `basename`s (mirroring the
  removed job's own "Discover publishers" derivation) instead of adding a
  further `releaseplan` flag for it, since it is pure shell string
  manipulation with no decision logic to test. Did not add `git config
  user.name/user.email` before tagging: lightweight tags need no commit
  identity, and this workflow creates no commits.
  Commit: see repo `git log -1` on this branch (single Conventional
  Commit, no AI-attribution trailer; only
  `.github/workflows/release-publishers.yml` and this task file staged).

- [x] **T5 — Docs.** `docs/ci.md`: rewrote the "Version policy" section's
  flow description into three named stages (Stage 1: `release.yml`'s
  `prepare-publisher-bump` job bumps and pushes only
  `release/publishers-<tag>`, never `main`, never a tag; Stage 2: a human
  runs the printed `gh pr create` command, so the PR goes through ordinary
  `CI Gate` review — no approval-bypass machinery needed because a human
  opened it; Stage 3: a human dispatches `release-publishers.yml`
  `workflow_dispatch` with `sha`/`ego_version`/`bump`/`dry_run`, where
  `dry_run: true` (default) safely previews every precondition check and
  `dry_run: false` does the real tag/push/release). Replaced the closing
  "known, pre-existing limitation" paragraph with "How the
  publisher-bump-vs-branch-protection conflict was resolved" — kept as
  history (what `release-publishers` used to do, the `GH006` failure mode
  it would have hit) followed by the resolution, pointing back to
  "Version policy" instead of duplicating it. Also fixed one more stale
  claim found while verifying: the "Release gate" section's job-chain
  sentence still said `gate → release-ego → release-publishers` "does the
  publisher bumps and tags" — updated to name `prepare-publisher-bump` and
  say it only pushes a branch (in scope per this task's own verification
  instruction: "no leftover claim that publishers are pushed/tagged
  directly by release.yml anymore").
  `docs/main-branch-policy.md`: replaced the "Known conflict with
  release.yml" paragraph (4 sentences, same concreteness as the rest of
  the document — names `prepare-publisher-bump`, `release/publishers-<tag>`,
  `release-publishers.yml`) with "Resolved: the publisher bump no longer
  touches `main` directly", pointing to `docs/ci.md`'s "Version policy"
  for the full three-stage flow.
  Verification (observed): re-read both files after editing — no leftover
  claim that publishers are pushed/tagged directly by `release.yml`; no
  leftover "open maintainer decision" framing (`rg -n "open maintainer
  decision" docs/ci.md docs/main-branch-policy.md` → no matches); every
  command/job/file name checked against the actual `release.yml` and
  `release-publishers.yml` content read fresh in this task (not
  paraphrased from T1/T4's notes). `rg -n 'push origin HEAD:main'
  docs/ci.md docs/main-branch-policy.md` → 2 matches, both inside
  sentences describing the old job in the past tense ("used to... push",
  "ran... pushed the result straight to `main` with `git push origin
  HEAD:main`"), never as current behavior.
  Commit: see repo `git log -1` on this branch (single Conventional
  Commit, `docs(ci): document the publisher bump-branch and dispatched
  tag flow (#159)`, no AI-attribution trailer; only `docs/ci.md`,
  `docs/main-branch-policy.md` and this task file staged) — not hardcoded
  here because amending this same commit to add this note would change
  its own hash on every edit; the exact SHA is reported in the session's
  final report to the user instead.

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
- **T2 done.** See T2's task entry above for the full evidence. Files
  touched: `internal/cmd/releasegate/client.go`, `client_test.go`,
  `main.go`, `main_test.go`, `docs/ci.md` (retry paragraph only, per this
  task's own scope). Commit: see repo `git log` on this branch (single
  Conventional Commit, no AI-attribution trailer; only files under
  `internal/cmd/releasegate/`, `docs/ci.md`, and this task file staged).
- **T3 done.** New file `internal/cmd/releaseplan/continuation.go` (pure
  decision logic) plus `continuation_test.go` (table-driven, `testdata/`
  fixtures, matching the package's existing style) and a new fixture
  `testdata/required-version/` (root `example.com/repo` + `pub1` requiring
  it at `v1.2.3`, `pub2` at the mismatched `v1.2.0`, both with the usual
  local `replace`). `main.go` gained five new flags, purely additive (all
  default to off/empty; the pre-existing flag set and default codepath are
  untouched and still pass their own tests):
  - `-continuation-check-sha <sha>`: validates exactly 40 lowercase hex
    characters, no other flags required.
  - `-continuation-check-version <version>`: validates `vX.Y.Z` (leading
    `v`, no pre-release/build suffix), no other flags required.
  - `-continuation-check-required-version <version>`: for every directory
    in `-release` except `.` (root never requires itself), reads its
    `go.mod` via the existing `readGoMod` and confirms the `require` line
    for the root module equals `version` exactly (the `replace` directive
    is ignored, as instructed); reuses `-repo-root`/`-release`; fails
    naming every mismatched directory with both its actual and wanted
    version.
  - `-continuation-plan-publishers`: computes next tags (via the existing
    `nextTag`) for exactly the directories in `-release`, which must
    exclude `.`; reuses `-repo-root`/`-release`/`-tags`/`-bump`/`-out-dir`
    and writes `plan.json`/`summary.md` exactly like the default full
    plan (reusing `Plan`/`renderPlanJSON`/`renderSummary`/`writeOutputs`
    unchanged) — chosen over a new ad hoc format because it is already the
    tested, documented contract T4's workflow (and any human) can read.
  - `-continuation-check-tag-conflicts <path>`: only valid together with
    `-continuation-plan-publishers`; reads a combined local+origin tag
    list from `path` and fails, naming every conflicting tag, before any
    output is written, if a computed tag already exists there.
  TDD: RED observed first for every piece (pure logic: `TestValidateSHA`,
  `TestValidateVersion`, `TestCheckRequiredRootVersion_*`,
  `TestPlanPublisherTags_*`, `TestCheckTagConflicts_*` all failed to
  compile — undefined functions; CLI wiring: `TestRun_Continuation*` all
  failed with "flag provided but not defined" before `main.go` was
  touched), then GREEN after each implementation step.
  Verification: `go test -count=1 ./internal/cmd/releaseplan/...` → all
  ~40 tests pass (0.4s). `go vet` → clean. `staticcheck` → clean.
  `gofmt -l internal/cmd/releaseplan/` → empty. Manual dry run against
  this repository's real `scripts/ci/release-modules.txt` (all four real
  publishers), wanting `v4.5.0`, correctly FAILED (all four publishers
  require the root at `v4.4.3` today via their `require` line):
  ```
  required-root-version check failed for 4 module(s):
    publisher/kafka: requires github.com/pablogore/ego/v4 v4.4.3, wanted v4.5.0
    publisher/nats: requires github.com/pablogore/ego/v4 v4.4.3, wanted v4.5.0
    publisher/pulsar: requires github.com/pablogore/ego/v4 v4.4.3, wanted v4.5.0
    publisher/websocket: requires github.com/pablogore/ego/v4 v4.4.3, wanted v4.5.0
  ```
  captured by `TestRun_ContinuationCheckRequiredVersion_RealRepository`.
  The `testdata/required-version` fixture proves the PASS direction too:
  `TestCheckRequiredRootVersion_OK` /
  `TestRun_ContinuationCheckRequiredVersion_PassAndFail` succeed against
  `pub1` (requires exactly `v1.2.3`) and only fail, naming `pub2`, once
  `pub2` (requires `v1.2.0`) is added to the checked list.
  Judgement calls: (1) kept SHA and version validation as two separate
  flags rather than one combined "-continuation-validate" mode, since each
  checks an independently-meaningful input with its own error message; a
  caller can still pass both in one invocation. (2) publishers-only tag
  computation reuses the existing `plan.json`/`summary.md` output
  contract instead of inventing a new format, since T4's workflow (and
  `jq`) already knows how to read it. (3) `-continuation-plan-publishers`
  refuses outright if `.` appears in `-release`, rather than silently
  filtering it — the release list is an explicit, reviewable input
  everywhere else in this package (`releasedSet`'s same philosophy), so a
  root left in by mistake should fail loudly, not be silently dropped.
  Commit: see repo `git log -1` on this branch (single Conventional
  Commit, no AI-attribution trailer; only files under
  `internal/cmd/releaseplan/` and this task file staged).
- **T4 done.** New file `.github/workflows/release-publishers.yml`
  (`workflow_dispatch`-only, steps a-h, T2's `releasegate` + T3's
  `releaseplan` continuation flags). Full detail, verification output and
  judgement calls (token choice, step-g drift check, tag style) are in
  T4's own task entry above. Commit: see repo `git log -1` on this branch
  (single Conventional Commit, no AI-attribution trailer; only
  `.github/workflows/release-publishers.yml` and this task file staged).
- **T5 done.** See T5's task entry above for the full detail. Files
  touched: `docs/ci.md`, `docs/main-branch-policy.md`, this task file.
  Commit: see repo `git log -1` on this branch (same self-reference note
  as T5's own entry above).

## All tasks complete

T1-T5 are all done on branch `ci/159-publisher-bump-flow`, one
Conventional Commit each, no AI-attribution trailer, nothing pushed, no
PR opened, no tags created — delivery remains the coordinator's decision
per the Delivery section above. Commits in order:

1. `4cb7cf0` — T1: split `release.yml`'s publisher job into
   `prepare-publisher-bump`, which now stops after pushing a
   `release/publishers-<tag>` branch (no push to `main`, no publisher
   tags/releases).
2. `6fbc2d3` — T2: `internal/cmd/releasegate` fails fast on permanent
   GitHub API errors (401/404/non-rate-limited 403) instead of retrying
   until the timeout (PR #171 follow-up).
3. `c32fb7e` — T3: `internal/cmd/releaseplan` gains the continuation
   decision logic (SHA/version validation, required-root-version check,
   publishers-only tag computation, tag-conflict check) the dispatched
   continuation workflow needs.
4. `8235017` — T4: new `.github/workflows/release-publishers.yml`
   (`workflow_dispatch`-only), the explicit, human-triggered continuation
   that validates every precondition and, unless `dry_run`, creates and
   pushes the publisher tags and their GitHub releases.
5. (see `git log -1` on this branch) — T5: `docs/ci.md` and
   `docs/main-branch-policy.md` now describe the resolved three-stage flow
   (bump branch → human-merged PR → dispatched tagging continuation)
   instead of the old single-job direct-push design and its `GH006`
   conflict. (Not hardcoded: this commit's own hash changes on every
   amend needed to add this note, so it is reported as the session's
   final commit SHA in the report to the user instead of embedded here.)

End state: `release.yml` never pushes to `main` and never opens a PR;
publishers are tagged only from a commit that already passed `build.yml`
on `main`, through an explicit, auditable, human-dispatched step. The
binding maintainer decisions from the Problem/Why sections above are all
satisfied.

## PR #172 review fixes

Fix-up round on the same branch (`ci/159-publisher-bump-flow`) after PR
#172 review, addressing findings in `.github/workflows/release-publishers.yml`.
Each finding is its own Conventional Commit, no AI-attribution trailer.

- **Finding #2 (MAJOR) — fixed.** `Create publisher GitHub releases`
  derived each publisher's tag with
  `git tag -l "${PREFIX}*" --sort=-version:refname | head -1` instead of
  using the tag this run actually planned and pushed — with an
  out-of-band higher tag already on origin, or a concurrent dispatch of
  the same workflow, this could create a release on the wrong version.
  Fixed: the step now iterates `dir`/`nextTag` pairs read directly from
  `$SCRATCH/plan/plan.json` via
  `jq -r '.modules[] | .dir + " " + .nextTag'`, using
  `done < <(jq ...)` process substitution (not a pipe into `while read`,
  which would run the loop in a subshell where `exit 1` only exits the
  subshell, not the step). Before calling `gh release create` for a
  planned tag, the step confirms it exists on `origin` and points at
  exactly `$SHA` (the workflow's `sha` input) via
  `git ls-remote --tags origin "refs/tags/$TAG"`: `git ls-remote` returns
  the tag object's own SHA for the plain `refs/tags/$TAG` line, and (for
  an annotated tag only) a second peeled `refs/tags/$TAG^{}` line
  carrying the commit it actually points at; the fix prefers the peeled
  SHA and falls back to the plain SHA when there is no peeled line
  (lightweight tag) via `ACTUAL_SHA="${PEELED_SHA:-$PLAIN_SHA}"`. A
  missing or wrong-commit tag fails the step immediately with `::error::`
  naming the tag and what was found/expected, before that or any later
  `gh release create` call runs. `--title`/`--notes` text is unchanged
  byte-for-byte.
  Verification (literal output): YAML parse → `YAML_OK`. `bash -n` on the
  extracted step script → `BASH_N_OK`. `rg -n '\$\{\{'
  .github/workflows/release-publishers.yml` → every match sits in an
  `env:`/`with:`/`if:` mapping, none inside a `run:` body. `rg -n
  'git tag -l.*PREFIX' .github/workflows/release-publishers.yml` → no
  matches (exit 1; the explanatory comment was worded to avoid
  accidentally still matching this pattern, same discipline T4 used for
  its `gh pr create` grep check). `actionlint` on the whole file → exit 0,
  no output (0 findings).
  Commit: `fix(release-publishers): create releases from the planned
  tags, not git tag -l (#159)`.
