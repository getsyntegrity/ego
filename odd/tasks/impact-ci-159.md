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
- [x] T3 Root module as a matrix entry when affected; `govulncheck` in per-module verification, gated by a reviewed, expiring exception list (C2).
- [x] T4 Selector tests: load-error path and missing C3 scenarios (C1/C3).
- [x] T5 A1 baseline evidence and standalone-plan docs in `docs/ci.md` (A1/C6). Evidence:
  `docs/ci/baseline-159-a1.md` (measured by a delegated read-only agent at `57c4b11`, local paths and
  machine notes removed) and the "Running the plan outside GitHub Actions" section of `docs/ci.md`.
  Check: structural readback (docs only).

## Follow-up chain (not in this spec)

- **Spec 2 (#159 C5):** re-measure with the A1 scenarios on real PR runs after spec 1 merges; retire
  the old flow only with no false negatives and a measured gain.
- **Finding from A1 for spec 2:** 4 of 5 sampled PRs ran in full mode because any root-dir `.go` file is
  a full-fallback path in `selector/classify.go`. Refining that rule is possible only while the root
  package exists; #124 removes the cause.
- **Blocked on human decisions or other work:** A2 (D1 module path and F4 release — a decision), A3
  (#102 S2/S3), A4 (#147/#148; #148 implementation is not yet authorized), A5 (`testkit`, after A3),
  A6 (#124, needs a major version).

## Progress and evidence

- **Draft PR #167, first real run `36371025289`:** `plan` ok (25s), matrix of 8 entries incl. `.`
  (root 7m20s), `CI Gate` read `plan: success`, `modules: failure` and failed, as designed. The failure
  is `publisher/pulsar`'s new `govulncheck` step: GO-2026-5046/5047/5048 in `github.com/hamba/avro/v2`
  v2.31.0 (transitive), reachable, "Fixed in: N/A". Pre-existing; surfaced by C2. Pending the user's
  decision on how `govulncheck` treats findings with no available fix.

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
- **T4 done** (commit below): re-checked the existing selector test suite
  first — the C3 scenarios the mapper flagged as possibly missing
  ("runtime change with no publisher consumer", "a global file", "summary
  lists module/cause") already exist:
  `TestSelect_Modules_RootLeafChangeNotImportedByAnyModuleSelectsNoneAndKeepsRootFastLane`,
  `TestModuleGraph_GlobalChange`/`TestSelect_Modules_CIPathChangeSelectsEveryModule`,
  and `TestSelect_Modules_SatelliteChangeIsReportedAsModuleLane`/
  `TestModuleGraph_SummaryHasWhyTable`, respectively — so nothing was added
  for those (per instructions: "only add cases genuinely missing"). The one
  real gap was the package-graph load-error path (`main.go:119-127`,
  `if len(loadErrs) > 0 { return ... }`), which had no test at all.
  - Added `TestRun_ChangedFailsOnPackageLoadError` and
    `TestRun_AllFailsOnPackageLoadError` to
    `internal/cmd/ciselect/modules_test.go`, with a new
    `writeUnparsablePackage` fixture (a `.go` file `go list -e -json`
    reports as a package-level `Error`, not a `DepsErrors` entry — verified
    manually against real `go list -e -json` output before writing the
    fixture). Both tests **passed immediately** against the existing code
    (the guard already existed; this is a coverage-only addition, not a
    bugfix), so I verified they are not vacuous: temporarily disabled the
    check (`if false && len(loadErrs) > 0`) and re-ran — both failed as
    expected (`-changed` failed for a different reason, proving a second,
    independent guard also exists in `discoverModuleImports`; `-all`
    **succeeded silently**, proving the check is load-bearing for `-all`
    specifically), then restored the original file (`git diff` empty
    afterward) and confirmed GREEN again.
  - Verification: `GOWORK=off go test ./internal/cmd/ciselect/...` green
    (both new tests plus the full existing suite), `GOWORK=off go vet
    ./internal/cmd/ciselect/...` clean (`GOROOT` unset, per the T1
    environment note).
- **T3 reopened, then re-closed** (commit below): draft PR #167's first real
  run (`36371025289`) turned the "Draft PR #167" note above from a
  hypothesis into a real finding — `publisher/pulsar` failed a bare
  `govulncheck ./...` on three pre-existing, unfixable vulnerabilities
  (GO-2026-5046/5047/5048, `github.com/hamba/avro/v2`, pulled in indirectly
  through `github.com/apache/pulsar-client-go`) — so T3's `govulncheck`
  addition was incomplete: it had no way to accept a specific, reviewed
  finding, only "fail every PR touching pulsar forever" or "drop the check
  and lose future coverage too". The user reviewed the three findings and
  approved a bounded, reviewed, expiring exception policy (four conditions:
  every entry names module + ID + owner + reason + exposed surface +
  removal criterion + a review date that expires it; the gate processes
  `govulncheck`'s structured `-format json` output rather than trusting its
  exit code, since that exit code is always 0; any unlisted ID, or the same
  ID under a different dependency module, still blocks; only these three
  IDs for `publisher/pulsar`, nothing broader). This correction reopened T3
  to add that gate.
  - **New package `internal/cmd/vulngate`** (root module, standard library
    only): decodes a `govulncheck -format json` report
    (`{"config":...}`/`{"progress":...}`/`{"osv":...}`/`{"finding":...}`
    stream) with `json.Decoder`, and matches each blocking finding against
    `scripts/ci/govulncheck-allow.json`. **TDD (RED → GREEN)**: wrote
    `internal/cmd/vulngate/main_test.go` first (26 table-driven/unit tests)
    against a package containing only a license header and an empty
    `main()`; RED confirmed by compile failure (`undefined: parseReport`,
    `undefined: loadAllowList`, ...; `go test` reported "too many errors"
    and `FAIL ... [build failed]`). Implemented `parseReport`,
    `loadAllowList`, `evaluate` and `formatSummary` in `main.go`; all 26
    tests then passed (GREEN), confirmed by
    `GOWORK=off go test -v ./internal/cmd/vulngate/...`.
  - **What counts as "called"**: `golang.org/x/vuln/internal/govulncheck`'s
    JSON types are an internal package and not importable, and the
    `-format json` schema is not otherwise documented, so this was verified
    empirically: `GOWORK=off govulncheck -format json ./...` was run for
    real against `publisher/pulsar` (after reinstalling `govulncheck@v1.8.0`
    with `GOROOT` unset — the previously-installed binary was built with
    go1.26.6 and could not parse a go1.27-syntax file in
    `k8s.io/apimachinery`, a transitive dependency, an unrelated instance of
    this worktree's T1-documented ambient-`GOROOT` problem). Each OSV ID
    appears as up to three findings of increasing detail — module-only
    (`trace: [{module, version}]`), package-only (adds `package`), and one
    finding per call chain once actually called (adds `function` on every
    frame, frame 0 being the vulnerable symbol and the last frame the
    scanned module's own code, e.g. `pulsar.go:161:25:
    pulsar.NewDurableStatePublisher calls fmt.Errorf, which eventually
    calls avro.Freeze`) — exactly matching the rule "blocks when trace[0]
    has a non-empty `function`", and matching `govulncheck`'s own text mode,
    which put exactly the three call-level IDs under "=== Symbol Results
    ===" / "Your code is affected by 3 vulnerabilities". `publisher/nats`
    real output confirmed the negative case: one real `golang.org/x/crypto`
    finding with no `function` (required, never called), correctly excluded
    from blocking and confirmed by `govulncheck`'s own text mode ("0
    vulnerabilities... your code doesn't appear to call these").
  - **`scripts/ci/govulncheck-allow.json`** (new): the three approved
    entries for `publisher/pulsar` (`vulnerable_module`
    `github.com/hamba/avro/v2`, `owner` `@pablogore`, `review_by`
    `2026-12-28`), each with the reason (no fixed version exists; only the
    fork `github.com/iskorotkov/avro/v2` is fixed, from 2.33.0), the
    exposure (package-level OSV record with no listed affected symbols;
    ego's pulsar publisher only produces protobuf payloads and never uses
    Pulsar's Avro schema support), and the removal criterion (bump
    `hamba/avro/v2` once fixed, or once `pulsar-client-go` drops it; the
    gate fails the entry as stale on its own once that happens).
  - **Wiring**: `scripts/ci/verify-module.sh` now runs
    `govulncheck -format json ./... > "$report"` then
    `go -C "$repo_root" run ./internal/cmd/vulngate -module "$module_dir"
    -report "$report" -allow "$repo_root/scripts/ci/govulncheck-allow.json"`
    (unchanged local-skip behavior when `govulncheck` is not installed).
    Both workflows' "govulncheck (root)" step does the same from the repo
    root directly. A new "Compute the govulncheck report path" step
    sanitizes `matrix.module` (GitHub Actions expressions have no
    string-replace function) into a stable report path and artifact name,
    and a new "Upload the govulncheck report" step, `if: always()`, uploads
    it so a failed scan or a failed gate still leaves the report as
    evidence. `Install govulncheck` is now pinned to `@v1.8.0` in both
    workflows instead of `@latest`.
  - **Corrected (user review, before push)**: the first version of this
    correction keyed staleness on (scanned module, ID) only, ignoring
    `vulnerable_module` — an entry whose ID resurfaced through a different
    dependency module was reported *blocked* only, deliberately not also
    stale, reasoning that requiring `vulnerable_module` too would
    "double-report one problem". The user reviewed this and identified it
    as wrong: an exception must identify the triple (Ego module, ID,
    vulnerable module), not just (Ego module, ID); reporting only "blocked"
    silently drops the fact that the old entry (naming the *other*
    vulnerable module) is now pointless and must be removed — that is a
    second, independently actionable fact, not a duplicate of the first.
    Fixed: `evaluate` now keys both blocking findings and allow-list
    entries by the full pair `findingID{ID, Module}` (`Module` being the
    vulnerable dependency), so the same ID under two different vulnerable
    modules is two distinct keys. When the same ID's vulnerable module
    changes, the gate now reports **both**: the new pair blocks (no entry
    names it) and the old entry goes stale (its own pair has no finding
    left); the blocked item's `Reason` names the stale entry's
    `vulnerable_module` explicitly so the connection between the two is not
    left implicit. **TDD**: rewrote
    `TestEvaluate_SameIDInAnotherModuleBlocks` (renamed
    `TestEvaluate_SameIDDifferentVulnerableModuleBlocksAndMarksOldEntryStale`)
    to assert `len(res.Stale) == 1` (previously asserted `== 0`); RED
    against the pre-fix code (compile failure first, since `blocking`'s type
    changed from `map[string]string` to `map[findingID]bool` across every
    test using it — 9 call sites updated), then GREEN after the `evaluate`/
    `parseReport` rewrite; all 26 tests (25 previous + this rewritten one)
    pass. Re-verified against the real `publisher/pulsar` report: unaffected
    (still 3 excepted, 0 blocked/stale/expired, since the real
    `vulnerable_module` values match exactly), plus a new real-data check —
    an allow file with `GO-2026-5046`'s `vulnerable_module` deliberately
    changed to a wrong value — correctly produced 1 blocked
    (`GO-2026-5046` "found in `github.com/hamba/avro/v2`") **and** 1 stale
    (`GO-2026-5046`, the wrong-module entry) together, with the blocked
    item's reason naming the stale entry's module. `docs/ci.md`'s
    "Matching" paragraph, which already (correctly) described the triple,
    is corrected where it had drifted from the actual (buggy) staleness
    behavior ("whose `id` no longer appears" → the full pair).
  - **Judgement call**: `review_by` is inclusive of its own day (`today >
    review_by` is expired, `today == review_by` is not) — the date names
    the last day the exception is still assumed valid, not the first day it
    lapses. Covered by
    `TestEvaluate_ReviewDateOnTheDayItselfStillPasses`.
  - Verification: `GOROOT= GOWORK=off go test -count=1
    ./internal/cmd/vulngate/... ./internal/cmd/ciselect/...` green (26
    vulngate tests plus the untouched ciselect suite); `GOROOT= GOWORK=off
    go vet ./internal/cmd/vulngate/...` clean; `GOROOT= GOWORK=off go build
    ./...` green. `govulncheck@v1.8.0` (matching the pin) was installed
    locally and run for real against `publisher/pulsar` and
    `publisher/nats` (network available in this environment); against the
    real `publisher/pulsar` report, `vulngate` passed with all three
    exceptions (0 blocked/stale/expired), removing one entry made it fail
    as blocked (1 blocked), and `-today 2026-12-29` made it fail as expired
    (3 expired, `review_by 2026-12-28`); against the real `publisher/nats`
    report (module-only finding, no allow entries for that module), it
    passed with nothing flagged — proving no stale false-positive. The
    exact `verify-module.sh` wiring (`govulncheck -format json` then
    `go -C <repo_root> run ./internal/cmd/vulngate ...`, cwd inside the
    nested module) was reproduced by hand for `publisher/pulsar` and
    passed. `go -C <repo_root> run ./internal/cmd/vulngate` was also
    confirmed to work with `GOPROXY=off` (vulngate is stdlib-only, so it
    never needs network or a `vendor/` directory, which matters for a
    nested-module job that never vendors the root module) and separately
    under `GOFLAGS=-mod=vendor` for the root case's own logic (module `.`).
    Both workflows parse as YAML (`python3 -c "import yaml; ...`) and pass
    `go run github.com/rhysd/actionlint/cmd/actionlint@latest` with zero
    findings — unlike the T1/T2 environment note, `actionlint` was
    reachable via `go run` this time. Every inline `run:` block of both
    workflows parses with `bash -n` (26 steps checked). `bash -n` OK for the
    modified `verify-module.sh`.
  - **Partial/blocked**: `golangci-lint run` against the new package was
    attempted and hit the same pre-existing repo-wide vendor/go.mod drift
    T3's original evidence already documented ("inconsistent vendoring...
    not marked as explicit in vendor/modules.txt") — not part of the
    required verification list, and out of scope to fix here (unrelated to
    this change; would mean running `go mod vendor` and touching `vendor/`
    broadly). The same drift also makes `GOFLAGS=-mod=vendor go run
    ./internal/cmd/vulngate -module . ...` fail locally in this worktree
    before `vulngate` itself ever runs (Go's own vendor-consistency check
    rejects it); in the real CI job this is a non-issue because the
    preceding "Vendoring and Tidy" step (`go mod tidy && go mod vendor`)
    regenerates a consistent `vendor/` first. `docs/ci.md` gained a new
    "The govulncheck exception gate (vulngate)" section (what blocks, the
    allow-file fields, matching rules, how to add or retire an entry, the
    exact wiring).
- **T3 follow-up `d440ffa`:** `internal/cmd/vulngate/` added to both global path lists (root lane full-fallback and module graph), so a change to the gate is verified against every module. RED: `TestModuleGraph_GlobalChange/internal/cmd/vulngate/main.go` and `TestSelect_Modules_CIPathChangeSelectsEveryModule` failed; GREEN after the change. `go test -count=1 ./internal/cmd/ciselect/... ./internal/cmd/vulngate/...` ok.
