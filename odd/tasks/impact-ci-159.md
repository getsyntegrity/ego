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
- [ ] T2 `plan` job split from heavy work; `build` and `modules` consume its outputs (C1).
- [ ] T3 Root module as a matrix entry when affected; `govulncheck` in per-module verification (C2).
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
