# Module-aware CI selector, slice S0 (#102)

## Problem

`internal/cmd/ciselect` knew only "the root module plus satellites". It
selected a nested module when a changed file sat under it, when the module
imported a root package in the root lane's affected set, or on a short
list of full-gate paths. Three gaps followed (ego-arch-006 exploration §6):
a nested module requiring another nested module would never be selected by
a change to it; a `go.work` change was not global; and adding a `go.mod`
inside a root directory could leave the root lane at `none`.

## What changes

The selector builds the repository's module graph from each module's
`go.mod` (`go mod edit -json`, offline, `GOWORK=off` on every `go`
subprocess). An in-repository requirement resolved through a local
`replace` is an edge; one pinned to a published version is reported, not
followed. A change selects the modules containing it plus every module
that transitively requires one of them, with the chain that reached each.
An edge is followed only if the consumer imports an affected package,
unless the required module's manifest changed or it is fully changed (the
import filter). Global paths force the full gate. An added or deleted
nested `go.mod` fully changes its parent module; `-base` tells those apart
from an edit. A module the root requires seeds the root package lane.
`modules.json` keeps its shape; `plan.json` and a summary table are new.

Why: it is slice S0 of the ego-arch-006 design (PR #132, §5 and §6 S0),
the prerequisite for every later module slice, and it needs none of the
maintainer decisions D1 to D8.

Rejected alternatives:

- Import-only edges, which is today's model. They miss requirement-only
  effects, so imports only filter an edge, and only when that is sound
  (design §5.1).
- Unfiltered requirement edges, which was this PR's first pass. They
  selected all six modules for tooling-only root changes and ran the full
  root lane for every publisher `go.mod` bump. The maintainers rejected
  that cost (R1 and R2 below).

## Constraints

- TDD: strict (source: user global CLAUDE.md). Runner:
  `env -u GOROOT go test ./internal/cmd/ciselect/...` (go1.27.1 via
  GOTOOLCHAIN=auto); also run with go1.26.6, `GOTOOLCHAIN=local`.
- No `-race` locally, no workbench.
- No new Go module, no module path change, no committed `go.work`, no
  edit to `scripts/ci/verify-module.sh`. The only workflow change is the
  `-base` merge-base pass-through in `pull_request.yml` (design §6 S0,
  amended at 1d8838a).
- RDD: not run by this writer; delivery follows ordinary repository policy.

## Tasks

The task list follows the amended design §6 S0 (ADR PR #132 at 1d8838a).
Route for every task: this agent, as the delegated writer.

- [x] T1 `ModuleInfo` discovery through `go mod edit -json`, `GOWORK=off`
      on every `go` subprocess, fail closed on unreadable `go.mod`, a
      mismatched `replace` or a `replace` into an undiscovered repository
      directory; `testdata/` skipped; `-all` degrades to directory-only
      discovery. Check: `modules_test.go`.
- [x] T2 Closure, ownership, boundary-change and global-path rules in
      `selector`, driven by the design §5.5 fixtures, RED first.
      Check: `selector/modulegraph_test.go`.
- [x] T3 The import filter (parser-based `Imports`, tests included, build
      tags ignored); root-lane seeding through dependency modules;
      `plan.json`; the summary table. Check: filter, seeding and summary
      tests; `TestWriteOutputs_PlanAndModulesJSON`.
- [x] T4 The `-base <rev>` flag (add or delete versus edit of a nested
      `go.mod`) and the `pull_request.yml` change, which passes the
      merge-base. Check: `TestGoModPresence_*`, the `WithBase` fixtures,
      and `TestRun_BaseFailsClosedButAllIgnoresIt`.
- [x] T5 `docs/ci.md` (selection rules, global list, `-base`, `plan.json`,
      the why table, before/after table) and `CHANGELOG.md`. Check: readback.

Outside this list: the design's S0 CI measurement (two throwaway draft
PRs) is still pending, because opening them needs the maintainers'
authorization.

## Acceptance

- The five #102 cases (leaf, shared contract, transitive consumer, new
  module, global change) and the design's extra cases pass as fixtures.
- On the real repository, the coverage denominator is identical in every
  mode (`coverpkg` sha256 prefix `ce183755` before and after).
- archcheck: no new violation, no baseline change.
- Full root suite and all six nested modules pass.

## Progress and evidence

Commits: `42e1532` (code and tests, T1 to T3), `64ea7ca` (docs, T4), plus
this document.

**RED.** First run failed to compile (`undefined: ModuleInfo`,
`undefined: discoverModules`, `undefined: goCommand`, ...). With type-only
stubs and the old logic, 26 tests failed on behavior, for example
`TestModuleGraph_SharedContract: got [port], want [adapter/a adapter/b it
port]`, `TestModuleGraph_TransitiveConsumer: got [adapter/a], want
[adapter/a it]`, `TestModuleGraph_GlobalChange/go.work: Global = false`,
`TestRun_ChangedFailsOnBrokenNestedGoMod: run() error = nil`. Already
green before the change (regression guards): docs-only, `odd/tasks/x.md`,
`TestRun_AllSurvivesBrokenNestedGoMod`.

**GREEN.** `go test -count=1 ./internal/cmd/ciselect/...`: both packages
`ok` on go1.27.1 and go1.26.6. gofmt clean; `go vet` clean.

**Changed expectations in existing tests, current state.** These are
behaviors the design changes on purpose. With R1 in place, the #111 test
`TestSelect_Modules_RootLeafChangeNotImportedByAnyModuleSelectsNone…` is
back to its original expectation.

- `TestSelect_Modules_RootChangeSelectsModuleRequiringRoot`: the reason is
  now the chain `publisher/kafka ← .` (was `imports affected root package …`).
- `TestSelect_Modules_AllSelectsEveryModule`: the reason is
  `global: -all requested` (was `full gate: -all requested`).
- `TestSelect_Modules_RootGoModChangeSelectsModulesRequiringRoot`: the root
  `go.mod` is not global (design §5.3), so only modules requiring the root
  are selected.
- `TestModuleDiscovery_FindsNestedModulesAndTheirRootImports` became the
  requirement-graph and import tests in `modules_test.go`.

**Real repository, before (main `77beda6`) and after, with
`-base origin/main`**:

| Changed path | Root lane before → after | Modules before → after |
|---|---|---|
| (a) `internal/ticker/ticker.go` | affected 3 → affected 3 | 6 → 6 |
| (b) `port/publishing/publishing.go` | affected 3 → affected 3 | 6 → 6 |
| (c) `publisher/kafka/kafka.go` | none → none | 1 → 1 |
| (d) `go.work` | affected 2 → full 23 (global) | 6 → 6 |
| (e) `docs/ci.md` | none → none | 0 → 0 |
| `migration/migration.go` | affected 1 → affected 1 | 0 → 0 |
| `internal/cmd/archcheck/main.go` | affected 1 → affected 1 | 0 → 0 |
| `publisher/kafka/go.mod` (edit) | none → none | 1 → 1 |
| the other exploration §6 rows | unchanged | unchanged |

Only `go.work` differs. Without `-base`, the kafka `go.mod` edit is
conservative: `full 23`, 6 modules. `coverpkg` sha256 prefix `ce183755`
in every row, with and without `-base`, and in `-all`.

The first-pass numbers below (before R1 and R2) are kept as history.

**Other checks.** archcheck: `15 packages checked, 70 edges checked, 1
baselined, 0 violation(s), 0 stale entries`. Full root suite
(`go test -count=1 -timeout 30m ./...`, `GOWORK=off`, no `-race`): 20
packages ok, 0 failures, 450 s. `scripts/ci/verify-module.sh` with
`GOWORK=off GOFLAGS=` and go1.26.6 on all six modules: all pass.
`golangci-lint run --new-from-rev=origin/main ./internal/cmd/ciselect/...`:
0 issues with go1.26.6 (`GOTOOLCHAIN=local`); with go1.27.1 it fails on a
stdlib typecheck in `internal/poll` (environmental, also on untouched
packages). CI lint is authoritative.

**Review follow-up (PR #133, approve with nits).** Findings 4 to 6 applied
on the same branch. R1 and R2 below and the parser-based `Imports` field
were left unchanged, as the review asked.

- 4: a local `replace` into a repository directory that is not a
  discovered module now fails discovery (fail closed; the workflow falls
  back to `-all`). A target outside the repository is still allowed.
  RED: `TestModuleDiscovery_ReplaceToUndiscoveredRepoDirFails: discoverModules()
  error = nil`. GREEN after `checkLocalReplaces`.
- 5: `plan.json` omits `path` when it is unknown (`-all` directory-only
  fallback), and `docs/ci.md` says so. RED: `TestRun_AllSurvivesBrokenNestedGoMod:
  plan.json has a path field`.
- 6: discovery skips `testdata/`. RED: `TestModuleDiscovery_SkipsTestdata:
  findSatelliteDirs() = [internal/tool/testdata/fixture moda modb modc]`.

After the fix, both test packages pass on go1.27.1 and go1.26.6, lint
reports 0 issues (go1.26.6), and the real-repository outputs (a) to (h) are
unchanged, with coverpkg `ce183755` throughout.

## R1 and R2 (maintainer decision 2026-09-27, ADR amended at 1d8838a)

The first pass followed the original §5.2 literally and flagged two cost
regressions. The maintainers decided to fix both inside this PR, and the
design was amended to match.

- **R1, the import filter.** Every module now carries parser-based
  `Imports` (#111's `discoverModuleImports`, generalized to any
  in-repository module, skipping `testdata/` and nested modules' files). A
  requirement edge M → D is followed unfiltered only when D's `go.mod` or
  `go.sum` changed, D is the parent of a boundary change, or D is the root
  with lane `full`. Otherwise M is selected only if its imports name an
  affected package of D. Root seeding counts only dependencies whose edge
  passed the filter, and the closure and seeding repeat until the root
  lane is stable.
- **R2, `-base`.** A changed nested `go.mod` is a boundary change only if
  it exists at exactly one of base and head. An edit marks the module
  changed with a changed manifest. Without `-base`, or for a `go.mod`
  missing from the presence map, the behavior stays conservative.
  `pull_request.yml` writes `git merge-base "$BASE_SHA" "$HEAD_SHA"` to
  `$RUNNER_TEMP/base.txt` in the step that already has both variables, and
  the select step passes `-base "$(cat …)"`. It must be the merge-base
  (review input), because the changed list is a three-dot diff.
  `TestGoModPresence_MergeBaseMatchesThreeDotDiff` shows the base branch
  tip would misclassify a pull request's add as an edit. An option-like
  `-base` (leading `-`) is rejected; the `-all` fallback never reads
  `-base`.

**RED** (type-only stubs, old logic): 11 behavioral failures, for example
`TestModuleGraph_ImportFilterSkipsConsumerNotImportingAffectedPackage:
modules = [{adapter/b …}], want none`,
`TestModuleGraph_WithBase_GoModEditHasNoBoundaryEffect: root Mode = full,
want none`, `TestModuleGraph_SharedContract: it chain = "it ← port", want
"it ← adapter/a ← port"`,
`TestSelect_Modules_RootLeafChangeNotImportedByAnyModule…: Modules =
[publisher/kafka]`, `TestGoModPresence_EditAddDelete: goModPresence() =
map[]`, and `TestModuleDiscovery_RootImportsSkipNestedModulesAndTestdata:
Imports:[]`. Later, the option-like base guard:
`TestGoModPresence_UnknownBaseFails: … exit status 1, want an option-like
base rejected`.

**GREEN**: both packages `ok` on go1.27.1 and go1.26.6. Lint reports 0
issues after the option guard; the two `exec.Command` lines carry
justified `//nolint:gosec` comments, following `retry.go`.

**Checks after R1 and R2:**

- archcheck is unchanged (0 violations, 0 stale entries).
- The full root suite (`GOWORK=off go test -count=1 -timeout 30m ./...`,
  no `-race`) passed: 20 packages ok, 0 failures, 427 s.
- `verify-module.sh` passed on all six modules (test results partly
  cached from the first pass).
- A local replay of the two workflow lines (`git diff … > changed.txt`,
  `git merge-base … > base.txt`, then `ciselect -base "$(cat base.txt)"`)
  on this branch gave `base.txt` = `77beda6`, and the run is global
  because `internal/cmd/ciselect/` changed.

## Next step

Maintainers review the PR, and authorize (or not) the two draft
measurement PRs for the S0 CI measurement.
