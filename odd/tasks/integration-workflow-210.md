# Integration workflow and full-execution gate (#210)

Issue: https://github.com/getsyntegrity/ego/issues/210 (related: #38, #159).
Branch: `ci/integration-workflow`, from `origin/develop` at `920d10f`. Target: `develop` through a pull request.

## Problem

Ego has no integration lane. Tests that need a real resource either run inside the ordinary unit/root build or skip silently. The clearest case is `example/cluster/stores_postgres_test.go`: its 19 `TestPostgresEventStore_*` tests call `t.Skip` when `EGO_EXAMPLE_POSTGRES_DSN` is unset, and no CI job sets it, so they report "ok" in every run without ever executing. A skip looks like a pass, and nobody notices.

The epic plan is to move such tests behind a build tag later. Before moving anything, there must be a lane that runs them and a gate that proves they really ran. Otherwise a moved test becomes an orphan: it is excluded from the unit build and runs nowhere.

## What changes

1. **A manifest of expected suites**, `.github/integration-suites.txt`. Each line names one top-level test that the integration lane must execute: module directory, package directory inside the module, and test function name. It starts with the 19 Postgres tests above.
2. **A gate tool**, `.github/scripts/integrationgate` (same layout as the existing `.github/scripts/unitgate`). It reads the manifest and the `go test -json` output of the integration run, and fails when:
   - a listed test produced no result (it was never executed),
   - a listed test was skipped (an unexpected skip),
   - a listed test failed,
   - a listed test no longer exists in the source (stale entry),
   - a top-level test in a file built with `//go:build integration` is not listed (an orphan). There are none today; the check is in place before the tests move.
   It writes a Markdown summary for `$GITHUB_STEP_SUMMARY`.
3. **A reusable workflow**, `.github/workflows/integration.yml`. It runs on push to `main`, on `workflow_dispatch`, and as `workflow_call` from `ci.yml`. It starts a Postgres service, sets `EGO_EXAMPLE_POSTGRES_DSN`, runs `go test -json -tags integration -run '^(...)$'` per manifest package, uploads the JSON and summary as artifacts, has job timeouts, and ends with the gate.
4. **Wiring in `ci.yml`**. A new `integration` job calls the reusable workflow and is added to the `needs` of `ci-ok`. It runs on every pull request to `main` (so `main` cannot receive a change without a green integration gate), and on pull requests to `develop` only when integration code, adapters or the integration machinery changed. It does not run on the ordinary feature/hotfix build otherwise, and it does not run on push to `develop`. Branch protection does not change, because `ci-ok` stays the only required check.

## Why this shape

- **Per-test manifest instead of per-package.** A package-level entry would accept a package where half the tests skip. The issue asks to detect unexpected skips, so the unit of the manifest is the top-level test.
- **Through `ci-ok`, not a new required check.** `docs/ci.md` already states that a new check is added by listing it in `ci-ok`'s `needs`. A separate required check would need a branch-protection change and would block every PR that legitimately skips integration. The rejected alternative was requiring `integration-ok` on `main` protection directly.
- **Selective on PRs by paths.** The issue asks to keep integration out of the usual feature/hotfix build. A `dorny/paths-filter` output in the existing `plan` job is the mechanism `ci.yml` already uses for skipping tests, so no new selector is introduced.
- **`-tags integration` already now.** It is harmless while no file uses the tag, and it means moving a test behind the tag later needs no workflow change.

## Scope and constraints

- No test is moved, tagged or excluded from the unit build in this change (explicit user instruction).
- Every new test uses go-specs v0.3.3 (`specs.Describe`, go-specs matchers); no testify.
- No race detector, no workbench.
- No direct push to `develop`/`main`; the change reaches `develop` through a PR.

## Execution

- TDD: strict (source: user CLAUDE.md "Strict TDD Mode: enabled"). Runner: `go test ./.github/scripts/integrationgate`.
- RDD: off (global), so no native review. Ordinary checks only.
- Delivery strategy: `ask-on-risk`. The forecast is about 600 authored lines, mostly the gate tool and its tests, delivered as one PR to `develop` because the pieces only make sense together. If the review finds it too large, the chain can be split as T1 and then T2–T4.

## Tasks

- [x] **T1 Gate tool.** `.github/scripts/integrationgate`: manifest parser, `go test -json` evaluator (missing, skipped, failed), stale-entry and orphan source scan, Markdown summary, exit code. go-specs tests with fixtures cover every failure kind plus the green path. Check: `go test ./.github/scripts/integrationgate`, `go vet ./.github/scripts/integrationgate`. Route: delegated writer (2+ non-trivial files).
- [x] **T2 Manifest and reusable workflow.** `.github/integration-suites.txt` with the 19 Postgres tests; `.github/workflows/integration.yml` (push main, dispatch, workflow_call; Postgres service; JSON artifacts; summary; timeouts; gate). Check: the gate tool's stale scan passes locally against the manifest; `actionlint` if available. Route: delegated writer.
- [x] **T3 `ci.yml` wiring.** `plan` gains an `integration` paths output; the `integration` job calls the reusable workflow under the selection rule above; `ci-ok` needs it. Check: `actionlint`; the YAML parses. Route: delegated writer.
- [x] **T4 Docs.** `docs/ci.md` (job table, "Other workflows") and the testing docs explain the lane, the manifest format and how to add a suite. Check: structural readback. Route: delegated writer.
- [x] **T5 Gate demos.** On the draft PR: one green run where all 19 tests execute, and one red run from a temporary commit that unsets the Postgres DSN (every suite skips, the exact failure this lane exists to catch), then reverted. Record both run URLs here. Route: inline (parent).

## Progress and evidence

- T1 `9a3eaa0` (delegated writer). RED observed first: with stubs, `go test ./.github/scripts/integrationgate` failed 54 cases across 7 tests. GREEN after implementation. `go vet` and `gofmt -l` clean; `go run ./.github/scripts/unitgate -strict` ok; `go test ./.github/scripts/unitgate` ok. The tool has three modes: `-plan`, `-check-manifest`, and full evaluation (JSON files via `-json` or as arguments).
- T2 `eaecc0c`. `-check-manifest` ok (19 suites); `actionlint` clean. Real run against `postgres:17-alpine` in Docker on port 55433: gate ok, 19 of 19 passed. Same run with the DSN unset: every suite reported as skipped by the gate. DSN in the workflow: `postgres://postgres:pg@localhost:5432/postgres?sslmode=disable`.
- T3 `b320283`. `actionlint` clean on `ci.yml` and `integration.yml`; YAML parses. The integration paths filter is a separate `paths-filter` step, because the existing one uses `predicate-quantifier: every`. `unit-gate` also runs the gate's tests and `-check-manifest`.
- T4 `f50c069`. `docs/ci.md` (job table, "Integration tests", "Other workflows") and a link from `docs/testing/go-specs.md`. Structural readback only.

- T5 (inline), PR #278.
  - Green: run https://github.com/getsyntegrity/ego/actions/runs/36899940417. Every job succeeded, `integration / integration` included, and the gate printed `integration gate: ok (19 suites)`.
  - Red: temporary commit `831dacd`, which set `EGO_EXAMPLE_POSTGRES_DSN` to an empty string. Run https://github.com/getsyntegrity/ego/actions/runs/36900650299 had the gate print `integration gate: 19 problem(s)`, with all 19 reported as `skipped`. `integration / integration` and `ci-ok` both failed, so the merge was blocked. The commit was reverted by `54d0033`.

## Next step

Review of PR #278. Follow-up in the epic: move the real-resource tests behind `//go:build integration` and list each moved test in `.github/integration-suites.txt`. The orphan check fails any tagged test that is not listed.
