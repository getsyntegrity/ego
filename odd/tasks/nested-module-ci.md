# Nested module CI (#111)

## Problem

The repository has six nested Go modules: `publisher/{kafka,nats,pulsar,websocket}`,
`benchmark` and `example/cluster`. None of them is inside the root
`go list ./...` graph, and no CI job builds them. When a pull request touches
only `publisher/kafka`, the test selector (`internal/cmd/ciselect`) classifies
every file as `ClassSatellite`, returns `ModeNone`, and the check goes green
without compiling Kafka (`selector/select.go:129-146`). The release workflow
also runs `go get ego@VERSION` while the local `replace ../../` stays in place,
so the published root version is never actually built against.

## What changes

1. `ciselect` learns about modules. A changed file under a nested module
   selects that module. A root change selects every nested module that
   directly imports a package in the root affected set; nested imports are
   read with `go/parser` (no network, no `go list` inside the module). `-all`,
   a changed `go.mod`/`go.sum` at the root, or a changed CI path
   (`.github/`, `scripts/ci/`, `internal/cmd/ciselect/`, `.golangci.yml`)
   selects all modules. The selection is written to `modules.json` and to
   the job summary with one reason per module.
2. `scripts/ci/verify-module.sh <dir>` builds, vets, lints (root
   `.golangci.yml`) and tests one module, with the race detector in CI.
3. `pull_request.yml` and `build.yml` fan out one matrix job per selected
   module, fed by `modules.json`. The module list is never written by hand.
4. `release.yml` discovers publishers from `publisher/*/go.mod` and, after
   `go get`, builds each publisher with its `replace` dropped against the
   proxy. A missing root tag fails with a clear message.
5. `docs/ci.md` and `CHANGELOG.md` describe the module lane, the toolchain
   requirements and the version policy.

Rejected alternative: a single job that loops over all modules. With no
branch protection on `main`, every check counts, and per-module jobs make
failures and selection reasons visible without reading logs.

Rejected alternative: selecting all modules on every root change. Simpler, but
it drops the root fast lane from #108 for leaf changes. The import-intersection
rule keeps leaf changes cheap and still catches real reverse consumers.

## Constraints

- TDD: strict (source: user global CLAUDE.md). Runner: `go test ./internal/cmd/ciselect/...`.
- No `-race` in local runs; CI keeps its existing race policy.
- Root fast lane and `ModeNone` for docs-only changes are preserved.
- RDD: off (global). Delivery follows ordinary repository policy.

## Tasks

- [x] T1 `selector`: module selection (satellite change, reverse consumer via
      direct imports, full on `-all`/go.mod/CI path), `Result.Modules` with
      reasons, summary section. Check: `go test ./internal/cmd/ciselect/...`.
      Route: delegated writer (writer trigger: selector + tests + main).
- [x] T2 `ciselect` main: parse nested imports, write `modules.json`.
      Check: run ciselect on a kafka-only change list → kafka selected;
      docs-only → `[]`.
- [x] T3 `scripts/ci/verify-module.sh` + matrix jobs in `pull_request.yml` and
      `build.yml`. Check: script passes locally for all six modules;
      `actionlint` if available.
- [x] T4 `release.yml`: discovered publishers + published-version build with
      `replace` dropped. Check: `scripts/ci/verify-published.sh` fails
      clearly for an unpublished version (v4.4.3) and passes for a published one.
- [x] T5 Docs: `docs/ci.md`, `CHANGELOG.md`, toolchain notes. Check: readback.

## Acceptance (from #111)

- A Kafka-only change runs Kafka build, vet and lint; the check cannot pass without them.
- All six modules are discovered and verified in the full gate; no static list.
- Root-only leaf changes keep the fast lane.
- A failing nested build/test/vet/lint fails the PR check.
- Selection and executed checks are visible in job summaries.
- Release verification fails clearly if the root version is not published.
- Tests cover satellite-only changes and module discovery.

## Delivery

Forecast about 600 authored lines. Single PR with one commit per task
(assumption, see PR description); it can be split at T3/T4 if a reviewer asks.

Actual, `git diff --stat` from before T1 to the end of T5: 972 insertions,
22 deletions across 15 files (one commit per task, plus one
`docs(odd)` progress commit per task on the feature branch). This is
above the ~600-line forecast, mostly `docs/ci.md` (172 lines, replacing
and substantially extending the old "Multi-module path (#104)" section)
and the two new test files (127 + 129 lines) the strict-TDD rule
requires alongside the selector and main.go changes. Per this
repository's advisory-only line heuristic, this is reported rather than
split or trimmed: no task boundary was artificial, each commit is an
independent, reviewable work unit, and no test, doc or comment was cut to
fit the number. No PR was opened as part of this work (out of scope for
this session); pushing and opening the PR remain the user's decision.

## Progress

### T1+T2 (commit c22056d)

- TDD: RED observed first — `internal/cmd/ciselect/selector/modules_test.go`
  and `internal/cmd/ciselect/modules_test.go` were written against
  `Options.Modules`, `Module`, `ModuleSelection`, `Result.Modules` and
  `discoverModuleImports`, none of which existed yet:
  `GOROOT= go test ./internal/cmd/ciselect/...` failed to compile
  (`opts.Modules undefined`, `undefined: Module`, `undefined: ModuleSelection`,
  `undefined: discoverModuleImports`).
- Implemented `selector.Select` as `selectRoot` (unchanged root logic) plus
  `selectModules` (new, independent of root Mode): changed-file-under-dir,
  root-affected-set intersection, and a closed full-gate set
  (`-all`, root `go.mod`/`go.sum`, `.github/`, `scripts/ci/`,
  `internal/cmd/ciselect/`, `.golangci.yml`). Updated
  `TestSelect_SatelliteOnlyIsNone` per the task's own instruction — it now
  asserts the root lane still reports `ModeNone` for a `benchmark`-only
  change, but the `benchmark` module is now selected (the gap it used to
  encode).
- `GOROOT= go test ./internal/cmd/ciselect/... -v`: all 28 tests pass (19
  selector, 1 main-level discovery test, 8 new module-selection tests).
- Local toolchain note: this environment's `GOTOOLCHAIN=auto` resolves
  `go` to the `go1.27.1` toolchain module while the shell exports
  `GOROOT=/home/pablog/sdk/go1.26.6`, which breaks any command that
  compiles stdlib (`go vet`, `golangci-lint`) with
  `compile: version "go1.26.6" does not match go tool version "go1.27.1"`
  or, for `golangci-lint` (built with go1.26.6), a stdlib typecheck error
  in `internal/poll` — reproduced identically on the untouched
  `internal/cmd/archcheck` package, confirming it is pre-existing and
  environmental, not caused by this change. Worked around by running with
  the matching `go1.26.6` SDK explicitly
  (`PATH=/home/pablog/sdk/go1.26.6/bin:$PATH GOROOT=/home/pablog/sdk/go1.26.6
  GOTOOLCHAIN=local`), which satisfies the root `go.mod`'s `go 1.26.0`
  directive: `go vet ./internal/cmd/...` clean, `golangci-lint run
  ./internal/cmd/...` → `0 issues`.
- Manual check: `go run ./internal/cmd/ciselect -changed <kafka-only>
  -out-dir <dir>` → `modules.json` = `["publisher/kafka"]`, mode `none`;
  same for a docs-only change → `modules.json` = `[]`, mode `none`.

### T3 (commit 190a779)

- `scripts/ci/verify-module.sh <module-dir>`: `go mod download`, `go build`
  (scratch `-o` dir only when the module has a `main` package, otherwise a
  bare `go build ./...`, to avoid dropping a stray binary into the module's
  own tree), `go vet`, `golangci-lint run --modules-download-mode=mod
  --config <repo-root>/.golangci.yml` (root config, overridden off
  `vendor` mode since nested modules have no `vendor/`), and `go test`
  only when a `*_test.go` file exists, `-race` gated by `GO_TEST_RACE`
  (default off). Writes a short block to `$GITHUB_STEP_SUMMARY` when set.
- Ran it locally (`GO_TEST_RACE` unset, matching go1.26.6 SDK — see the
  toolchain note above) for all six modules, one at a time:
  - `publisher/kafka`: build/vet clean; lint found 2 real pre-existing
    `revive` (unused `ctx` parameter in both `Publish` methods) and 2
    `staticcheck` `SA4006` (the reassigned `ctx` from
    `context.WithTimeout` in both `Close` methods is never used — the
    timeout was already dead code before this change) findings. Fixed
    minimally: renamed the unused `Publish` parameters to `_`, changed
    `ctx, cancel := context.WithTimeout(...)` to `_, cancel := ...` in
    both `Close` methods. Re-run: `0 issues`, no tests (module has none).
  - `publisher/nats`: `0 issues`, no tests.
  - `publisher/pulsar`: `0 issues`, no tests.
  - `publisher/websocket`: lint found the same unused-`ctx`-parameter
    pattern in `Publish`; fixed the same way. Re-run: `0 issues`, no
    tests.
  - `benchmark`: `0 issues`; `go test ./...` → `ok ... [no tests to run]`
    (its one `_test.go` holds only `Benchmark*` functions).
  - `example/cluster`: `0 issues`; `go test ./...` → `ok` (its two test
    files ran and passed without needing a live Postgres — no skip or
    failure observed).
- `actionlint`: not installed in this environment (`which actionlint`
  found nothing); not installed per instructions. Validated both
  workflow files instead with `python3 -c 'import yaml; yaml.safe_load(...)'`
  — both parse as valid YAML.
- `pull_request.yml`'s root `go mod tidy && go mod vendor` step runs only
  in the `build` job, against the root module; the new `modules` job is a
  separate job/runner that never sets `GOFLAGS=-mod=vendor` and never
  touches the root `vendor/` directory, so it does not interfere with
  nested-module verification (confirmed by reasoning about job isolation,
  not by running the workflow in Actions).

### T4 (commit fc0b144)

- `release.yml`: replaced both hardcoded `PUBLISHERS="kafka nats pulsar
  websocket"` lines with a `Discover publishers` step that lists
  `publisher/*/go.mod` and passes its output through
  `steps.discover-publishers.outputs.publishers`. Verified the discovery
  shell logic locally against the real tree: it produces exactly
  `kafka nats pulsar websocket`, the same set as before.
- `scripts/ci/verify-published.sh <module-dir> <ego-version>`: copies the
  module into a scratch directory, `go mod edit -dropreplace -require=` in
  one edit (dropping the replace and pointing at the target version
  together, so the module graph is never resolved against the old,
  unpublished require line before the update applies — an earlier
  `-dropreplace` then `go get` ordering failed on exactly that), then
  `go mod tidy && go build ./...`. `go list -m <root>@<version>` is
  checked first and prints one `::error::` line and exits 1 if it fails.
  Called from `release.yml` for each publisher right after its own
  `go get`/`go mod tidy`, before the tag-creation loop.
- Honest finding: `go list -m -versions github.com/pablogore/ego/v4`
  against the real proxy returns **no tagged version at all** — this repo
  has not cut its first release yet, exactly as
  `openspec/changes/ego-arch-001/design.md` §8 and §9 already say
  (`require v4.4.3` "does not exist; the first release must fix that").
  The proxy's `@latest` endpoint does resolve a pseudo-version off the
  current `main` tip, `v4.0.0-20260926225153-3b15be0c4306` — there is no
  real "latest published tag" to test against.
- `scripts/ci/verify-published.sh publisher/kafka v4.4.3` →
  `::error::root module github.com/pablogore/ego/v4 v4.4.3 is not
  published; local replace cannot be used for release verification`,
  exit 1, as required.
- `scripts/ci/verify-published.sh publisher/kafka
  v4.0.0-20260926225153-3b15be0c4306` (the one resolvable version found)
  → downloads it and its transitive deps, `go build ./...` succeeds,
  prints `verify-published.sh: publisher/kafka builds against published
  github.com/pablogore/ego/v4@v4.0.0-...`, exit 0. Since this pseudo-version
  is the current `main` tip, it already contains `port/publishing`
  (merged in #116), so the "may fail to build because the published root
  lacks `port/publishing`" risk the task named does not apply to this
  particular version — that risk is real for an OLDER root version,
  which is not reachable here since none exists yet.

### T5 (commit c3a2623)

- `docs/ci.md`: replaced the "Multi-module path (#104)" section with
  "Nested module CI (#111)" — the selection rules, `modules.json`, what
  `verify-module.sh` runs, the matrix job, the integrated-vs-published
  verification distinction, the version policy, and the toolchain table.
  Readback: cross-checked every concrete claim (rule order, file names,
  flags, reasons) against the actual code in `select.go`/`main.go`/the
  scripts; all matched.
- `CHANGELOG.md`: one bullet under the existing `[Unreleased]` →
  `### 🧹 Improvements` section, in the same style as the neighboring
  #107/#108 entries.
- `internal/cmd/archcheck/baseline.go`: left unchanged. Its
  `RemovalCriterion` text ("S1b: switch to port/publishing once #111
  builds and verifies nested modules in CI") stays true after this
  change — #111 now does exactly that — so nothing needed editing.
- Final full check: `go run ./internal/cmd/archcheck` → `15 packages
  checked, 70 edges checked, 5 baselined, 0 violation(s), 0 stale
  entries`; `go test ./internal/cmd/ciselect/...` → both packages `ok`;
  `golangci-lint run ./internal/cmd/...` → `0 issues` (all with the
  matching go1.26.6 SDK, see the toolchain note under T1+T2).

## Close-out

All five tasks done. Acceptance re-checked against #111's own list:
Kafka-only changes now select and verify Kafka (T1/T2, manually
reproduced); all six modules are discovered and verified with no static
list (T3, `findSatelliteDirs` + `modules.json` feed the matrix, and
`release.yml`'s publisher list is now discovered too, T4); root-only leaf
changes keep the fast lane (`TestSelect_Modules_RootLeafChangeNot...`);
a failing nested build/vet/lint/test fails the module's own job, and
therefore the check, since `verify-module.sh` runs each step under
`set -euo pipefail`; selection and reasons are visible in the job summary
(`## Nested modules` section); release verification fails clearly for an
unpublished root version (reproduced with `v4.4.3`); tests cover
satellite-only changes (`TestSelect_SatelliteOnlyIsNone`, updated) and
module discovery (`TestModuleDiscovery_FindsNestedModulesAndTheirRootImports`).

Not run: the actual GitHub Actions workflows (no CI trigger available
from this session) — the workflow YAML was validated for syntax only
(`python3 -c 'yaml.safe_load(...)'`) and reasoned through against the
`build`/`modules` job wiring; `actionlint` was not available and was not
installed, per instructions. No PR was opened; pushing and opening one
remain the user's decision.
