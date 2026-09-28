# Feature: impact-aware CI with a planning job, root in the matrix, and a stable gate (#159, spec 1)

Branch: `feat/159-impact-ci` · Base: `origin/main` `57c4b11` · Epic: #10 · Issue: #159 (CI track, owner #38)

## Problem

Every pull request pays for global work before anything is selected. The single `build` job in
`.github/workflows/pull_request.yml` runs `go mod tidy && go mod vendor`, `archcheck` and lint for the
root module, and only then runs `internal/cmd/ciselect` to decide what to test. The nested modules
(`publisher/*`, `benchmark`, `example/cluster`, `test/compat`) run in a `modules` matrix that `needs:
build`, so the root job is a universal prerequisite. The root module is never a matrix entry, no job
runs `govulncheck`, and there is no single status check that branch protection can require: when the
matrix is empty, the `modules` job is *skipped*, not green.

## What changes

1. **Stable gate (C4).** A `ci-gate` job in `pull_request.yml` and `build.yml` that always runs,
   depends on every other job, and fails if any selected job failed or was cancelled. It passes with
   zero, one or many matrix entries, so branch protection can require exactly one name.
2. **Planning job (C1).** A cheap `plan` job computes the diff and base, runs `ciselect`, publishes
   `plan.json`/`modules.json`/`summary.md`, and nothing else. If the selector fails it falls back to
   `-all` with the reason recorded; it never selects silently less. Heavy jobs consume its outputs.
3. **Root in the matrix (C2).** When the root module is affected, `.` appears in the matrix and runs in
   the same per-module job shape (from its dir, `GOWORK=off`, its own `go.mod`), keeping its root-only
   checks (vendor, archcheck, Buf) and its package-level test selection. `govulncheck` is added to the
   per-module verification.
4. **Selector hardening (C1/C3).** A test for the package-graph load-error path, and the explicit C3
   cases that are missing (runtime change with no publisher consumer, global file, summary content).
5. **Baseline and standalone plan (A1/C6).** Record the A1 baseline (module graph, closures, job
   timings with run IDs) and document in `docs/ci.md` the new job graph, the gate name, and how to run
   the plan outside GitHub Actions.

## Scope and constraints

- Touches `.github/workflows/{pull_request,build}.yml`, `scripts/ci/*`, `internal/cmd/ciselect/**`,
  `docs/ci.md`, `CHANGELOG.md`, this document. Not touched: `release.yml`, production Go code, module
  layout.
- `main`, release and `workflow_dispatch` keep the full gate (`ciselect -all`).
- TDD: strict (user global configuration); runner `go test`. Workflow YAML has no unit runner: its
  check is `actionlint` when available plus a YAML parse check. Never `-race` locally, never the
  workbench.
- Route: delegated direct (one writer; 2+ non-trivial files per task). Mapping delegated (4+ files).
- RDD: off (global) — ordinary checks only.
- Delivery: one work-unit commit per task on this branch; push and PRs are the user's decision.

## Tasks

- [x] T1 Stable `ci-gate` aggregate job in both workflows + `docs/ci.md` (C4).
- [x] T2 `plan` job split from heavy work; `build` and `modules` consume its outputs (C1).
- [x] T3 Root module as a matrix entry when affected; `govulncheck` in per-module verification (C2).
- [ ] T4 Selector tests: load-error path and missing C3 scenarios (C1/C3).
- [ ] T5 A1 baseline evidence and standalone-plan docs in `docs/ci.md` (A1/C6).

## Follow-up chain (not in this spec)

- **Spec 2 (#159 C5):** re-measure with the A1 scenarios on real PR runs after spec 1 merges; retire
  the old flow only with no false negatives and a measured gain.
- **Blocked on human decisions or other work:** A2 (D1 module path and F4 release — a decision), A3
  (#102 S2/S3), A4 (#147/#148; #148 implementation is not yet authorized), A5 (`testkit`, after A3),
  A6 (#124, needs a major version).

## Progress and evidence

- Mapping done by a delegated read-only mapper (workflows, `ciselect`, helpers).
- **T1 done** (commit below): added a `ci-gate` job to `pull_request.yml` and
  `build.yml`, `needs: [build, modules]`, `if: always()`. It fails when
  `build` did not succeed, or when `modules` finished as anything other than
  `success`/`skipped` (a skipped `modules` is only ever caused by `build`
  selecting zero modules, once `build` itself succeeded, so it is not a
  failure). Documented in `docs/ci.md` under "The `ci-gate` job: one
  required status check"; the required check name for branch protection is
  `CI Gate`. Verification: YAML parse OK for both workflows (`actionlint`
  unavailable — `go run .../actionlint@latest` hit a local Go toolchain
  version mismatch, see below); `go vet`/`go test` for `internal/cmd/ciselect`
  untouched by this task and green.
  - Environment note (not part of this change): this worktree's ambient
    `GOROOT` env var points at a Go 1.26.6 SDK while `go` itself resolves to
    a downloaded 1.27.1 toolchain, which breaks `go build`/`vet`/`test`
    ("compile: version ... does not match go tool version ..."). Running
    with `GOROOT` unset works around it; this is pre-existing and outside
    this feature's scope.
- **T2 done** (commit below): split both workflows into `plan` (checkout,
  setup-go, diff/base, `ciselect` with the existing `-all` fallback,
  uploads the whole ciselect out-dir as the `ciselect-plan` artifact,
  writes `summary.md` to `$GITHUB_STEP_SUMMARY`, outputs `mode`/`modules`)
  and `build` (`needs: plan`, `if: needs.plan.outputs.mode != 'none'`:
  vendoring/tidy, archcheck, lint, downloads `ciselect-plan`, runs
  `scripts/ci/go-test.sh` and the coverage summary). `modules` now
  `needs: plan`, not `build`. `ci-gate` now `needs: [plan, build, modules]`
  and fails on `plan` not succeeding, or `build`/`modules` finishing as
  anything but `success`/`skipped`.
  - **Judgement call**: `build` downloads `plan`'s own `ciselect-plan`
    artifact instead of re-running `ciselect` itself. Rejected alternative:
    let `build` re-invoke `ciselect` with the same `-changed`/`-base`
    inputs. Rejected because a second independent run could in principle
    disagree with `plan`'s (a flaky `go list`, a different fallback path),
    which would let `build` test something other than what `plan` and
    `modules` already agreed on — the decision must be made exactly once.
  - `build.yml`'s `plan` always uses `-all` (mode is always `full`, never
    `none`), so its `build` job effectively always runs, per the scope
    constraint that `main`/`workflow_dispatch` keep the full gate.
  - `docs/ci.md` updated: "What each workflow runs" now describes the
    plan/build split and the artifact hand-off; "The `modules` matrix job"
    and "The `ci-gate` job" now reference `plan` instead of `build`.
  - Verification: YAML parse OK for both workflows; every inline `run:`
    block parses with `bash -n` (script-extracted via a small Python/yaml
    check, since `actionlint` remains unavailable in this environment —
    see the T1 note); `bash -n` OK for `scripts/ci/go-test.sh`,
    `verify-module.sh`, `verify-published.sh` (unchanged by this task);
    `go build ./internal/cmd/ciselect/...` green (with `GOROOT` unset, see
    the T1 environment note).
- **T3 done** (commit below): the root module (`.`) now appears in
  `modules.json` (root first) whenever its `Plan` entry is `Selected`, and
  the `modules` matrix job (both workflows) runs it in the same per-module
  job shape, with root-only steps (download the plan, vendor/tidy,
  archcheck, the root's `golangci-lint-action` lint, `go-test.sh`,
  coverage) gated on `matrix.module == '.'` and `Verify module`
  (`verify-module.sh`) gated on `matrix.module != '.'`. `govulncheck` is
  installed once per matrix job and run: directly against the root
  (`govulncheck ./...` after its coverage step) and inside
  `verify-module.sh` for every nested module (skipped locally with one
  line when the binary is not installed, since CI always installs it).
  - **TDD (RED → GREEN)**: added
    `TestWriteOutputs_ModulesJSONIncludesSelectedRoot` and
    `TestWriteOutputs_ModulesJSONOmitsUnselectedRoot` to
    `internal/cmd/ciselect/modules_test.go`. RED: the first failed with
    `modules.json = "[]\n", want "[\".\",\"moda\"]\n"` before the fix (the
    old `modulesJSON(result.Modules)` only ever saw nested modules).
    GREEN after changing `writeOutputs` to call
    `modulesJSON(selectedModuleDirs(result.Plan))`, where
    `selectedModuleDirs` is a new helper returning every `Selected` plan
    entry's directory, root included. Updated the pre-existing
    `TestRun_AllSurvivesBrokenNestedGoMod` expectation from
    `["moda","modb","modc"]` to `[".","moda","modb","modc"]`, since `-all`
    now correctly also selects the root (this is a corrected assertion of
    intended new behavior, not a preserved regression).
  - **Judgement call**: `modules.json` folds `.` in directly (source: the
    already-computed `Selected` field of `result.Plan`'s root entry, which
    means exactly "the root lane is not `none`") rather than adding a
    second `root_selected` output. Rejected alternative: a separate output
    threaded through `plan`'s `GITHUB_OUTPUT` plus a second `if:` — this
    would duplicate a decision `Plan` already records, with two places to
    check it instead of one, for no behavioral difference. Recorded in
    `docs/ci.md`, "Root module in the matrix (ego-arch-006 spec 1, C2)".
  - `Result.Modules` (nested-only, drives the human-facing "Nested modules"
    summary section) is intentionally untouched — it predates this change
    and is not the workflow matrix's source.
  - No Buf step exists anywhere in these workflows today, so "Buf if
    present" from the task description had nothing to move; noted so a
    future Buf step knows to gate on `matrix.module == '.'` too.
  - Verification: `GOWORK=off go test ./internal/cmd/ciselect/...` green,
    `GOWORK=off go vet ./internal/cmd/ciselect/...` clean, YAML parse OK
    for both workflows, every inline `run:` block parses with `bash -n`,
    `bash -n` OK for the modified `verify-module.sh`, `GOWORK=off go build
    ./...` (repo root) green — all with `GOROOT` unset (T1 environment
    note). `golangci-lint run` was attempted but fails repo-wide on
    pre-existing vendor/go.mod drift unrelated to this change
    ("inconsistent vendoring... not marked as explicit in
    vendor/modules.txt"); not part of the required verification list for
    this task, and out of scope to fix here (would mean running `go mod
    vendor` and touching vendor/ broadly). `verify-module.sh`'s new
    govulncheck step was reviewed but not executed end-to-end locally
    (`govulncheck` is not installed in this environment, which exercises
    the intended local skip path; a full nested-module run also needs
    network access this environment does not exercise for this task).
