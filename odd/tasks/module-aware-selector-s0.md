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
Global paths force the full gate, a nested `go.mod` change fully changes
its parent module, and a module the root requires seeds the root package
lane. `modules.json` keeps its shape; `plan.json` and a summary table are
new.

Why: it is slice S0 of the ego-arch-006 design (PR #132, §5 and §6 S0),
the prerequisite for every later module slice, and it needs none of the
maintainer decisions D1 to D8.

Rejected alternative: keeping import-based edges for the root-to-nested
case to preserve today's cheaper selection for tooling-only root changes.
The design rejects import-only edges (§5.1, §8) because they miss
requirement-only effects; the cost is recorded below as a decision for the
maintainers rather than silently worked around.

## Constraints

- TDD: strict (source: user global CLAUDE.md). Runner:
  `env -u GOROOT go test ./internal/cmd/ciselect/...` (go1.27.1 via
  GOTOOLCHAIN=auto); also run with go1.26.6, `GOTOOLCHAIN=local`.
- No `-race` locally, no workbench.
- No new Go module, no module path change, no committed `go.work`, no
  workflow change, no edit to `scripts/ci/verify-module.sh`.
- RDD: not run by this writer; delivery follows ordinary repository policy.

## Tasks

- [x] T1 `ModuleInfo` discovery through `go mod edit -json`, `GOWORK=off`
      on every `go` subprocess, fail closed on unreadable `go.mod` or a
      mismatched `replace`; `-all` degrades to directory-only discovery.
      Check: `modules_test.go` (discovery, broken go.mod, wrong replace,
      GOWORK, run-level fail-closed). Route: inline writer (this agent is
      the delegated writer).
- [x] T2 Closure, ownership, boundary-change and global-path rules in
      `selector`, driven by the design §5.5 fixtures, RED first.
      Check: `selector/modulegraph_test.go`.
- [x] T3 Root-lane seeding through dependency modules; `plan.json`; the
      summary "module | selected | why" table.
      Check: seeding, fallback and summary tests; `TestWriteOutputs_PlanAndModulesJSON`.
- [x] T4 `docs/ci.md` (selection rules, global list, `plan.json`, the why
      table, before/after table) and `CHANGELOG.md`. Check: readback.
- [ ] T5 CI measurement with two throwaway draft PRs (design §6 S0).
      Pending: the design requires the maintainers' authorization to open
      them; not done by this slice's writer.

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

**Changed expectations in existing tests** (the design changes these
behaviors on purpose):

- `TestSelect_Modules_RootLeafChange...`: a root leaf change now selects
  the modules that require the root (was: none, because no module
  imported the package). Design §5.1: requirements, not imports, are edges.
- `TestSelect_Modules_RootChangeSelectsModuleRequiringRoot`: reason is now
  the chain `publisher/kafka ← .` (was `imports affected root package …`).
- `TestSelect_Modules_AllSelectsEveryModule`: reason
  `global: -all requested` (was `full gate: -all requested`).
- `TestSelect_Modules_RootGoModChangeSelectsModulesRequiringRoot`: the root
  `go.mod` is not global (design §5.3); only modules requiring the root.
- `TestModuleDiscovery_FindsNestedModulesAndTheirRootImports` became
  `TestModuleDiscovery_FindsNestedModules`: the `go/parser` import scan
  (`discoverModuleImports`) is gone, replaced by `go.mod` requirements.

**Real repository, before (main `77beda6`) and after**:

| Changed path | Root lane before → after | Modules before → after |
|---|---|---|
| (a) `internal/ticker/ticker.go` | affected 3 → affected 3 | 6 → 6 |
| (b) `port/publishing/publishing.go` | affected 3 → affected 3 | 6 → 6 |
| (c) `publisher/kafka/kafka.go` | none → none | 1 → 1 |
| (d) `go.work` | affected 2 → full 23 (global) | 6 → 6 |
| (e) `docs/ci.md` | none → none | 0 → 0 |
| `internal/cmd/archcheck/main.go` | affected 1 → affected 1 | 0 → 6 |
| `migration/migration.go` | affected 1 → affected 1 | 0 → 6 |
| `publisher/kafka/go.mod` | none → full 23 | 1 → 6 |

`coverpkg` sha256 prefix `ce183755` in every row and in `-all`.

**Other checks.** archcheck: `15 packages checked, 70 edges checked, 1
baselined, 0 violation(s), 0 stale entries`. Full root suite
(`go test -count=1 -timeout 30m ./...`, `GOWORK=off`, no `-race`): 20
packages ok, 0 failures, 450 s. `scripts/ci/verify-module.sh` with
`GOWORK=off GOFLAGS=` and go1.26.6 on all six modules: all pass.
`golangci-lint run --new-from-rev=origin/main ./internal/cmd/ciselect/...`:
0 issues with go1.26.6 (`GOTOOLCHAIN=local`); with go1.27.1 it fails on a
stdlib typecheck in `internal/poll` (environmental, also on untouched
packages). CI lint is authoritative.

## Needs a maintainer decision

1. **The design's S0 check is wrong for two change classes.** §6 S0 says
   the output on the exploration change set "only differs for `go.work`".
   Following §5.2 literally, a root change that no nested module imports
   (`internal/cmd/archcheck`, `migration`) now selects all six modules,
   because they all require the root. This reverses #111's accepted
   choice ("root-only leaf changes keep the fast lane" for modules).
2. **A nested `go.mod` edit now runs the full root lane.** §5.2 step 3
   says "added, edited or deleted", and the selector cannot tell an edit
   from an add from paths alone. A publisher dependency bump, which
   Renovate can open, costs the full root suite (about 8.5 minutes) plus
   all modules, instead of one module job.

Either can be narrowed later (for example, only treat a `go.mod` as a
boundary change when its directory is not already a discovered module on
the base commit), but that needs the base tree and is a design change.

## Next step

Maintainers review the PR and decide items 1 and 2; authorize (or not) the
two draft measurement PRs of T5.
