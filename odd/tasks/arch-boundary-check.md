# Feature: enforce architectural dependency boundaries in CI (#107, ADR slice S2)

Branch: `ci/107-arch-boundary-check` · Base: `origin/main` `a5265aa` · Epic: #10 · Issue: #107

## Problem

The layer rules of the ego-arch-001 ADR (`openspec/changes/ego-arch-001/design.md` §3) are review
rules today. Three packages (`tenancy`, `command`, `port/publishing`) have their own hand-written
architecture test, but nothing stops a new contract package, the `migration` application or a
publisher module from importing the GoAkt runtime. A regression is caught only if a reviewer notices.

## What changes

A new tool, `internal/cmd/archcheck`, reads the import graph and checks every import edge against a
table of layers. Each layer names its packages and what they may import. A violation prints the
importing package, the forbidden import and the rule it breaks, then the tool exits non-zero.

Known violations that cannot be fixed yet live in an explicit **baseline**. Every baseline entry has
an owner, a justification and a removal criterion. An entry that no longer matches a real violation
also fails the check, so the baseline can only shrink.

The tool runs as its own step in `pull_request.yml` and `build.yml`, before the test suite, so a
boundary break fails in seconds instead of after the full run.

### How the graph is read

- **Root module:** `go list -e -json ./...` gives each package's production `Imports`, with build
  constraints resolved by the toolchain. A direct-edge check is enough: contract layers use a closed
  allowlist, and every allowed target (stdlib, `egopb`, the protobuf runtime, `uuid`, `atomic`,
  `internal/queue`, `internal/syncmap`) is itself runtime-free, so no transitive path to GoAkt can
  open without adding a new, visible edge.
- **Nested modules** (`publisher/*`, `benchmark`, `example/cluster`): parsed with `go/parser` in
  imports-only mode. This needs no module download and no network, which keeps the step fast. Only
  direct imports matter for the nested-module rules.

Rejected alternative: `go list -deps` over every module. It needs every nested module's dependencies
downloaded (Kafka, Pulsar and NATS clients) and turns a seconds-long check into minutes.

Rejected alternative: extend `ciselect`. Selection decides which tests run; the rule check decides
whether the graph is legal. Mixing them would make one tool's fallback hide the other's failure.

## Rules (first version)

| Rule | Layer | Constraint | Source |
|---|---|---|---|
| `contract-allowlist` | Contract packages: `tenancy`, `command`, `persistence`, `offsetstore`, `projection`, `eventstream`, `encryption`, `eventadapter`, `port/...` | May import only stdlib, other contract packages, `egopb`, `google.golang.org/protobuf/...`, `internal/queue`, `internal/syncmap`, `github.com/google/uuid`, `go.uber.org/atomic` | design.md §3 |
| `application-no-runtime` | Application: `migration` | Must not import the runtime adapter (`ego`, `internal/extensions`) or GoAkt | #107 scope |
| `external-adapter-no-runtime` | Nested adapter modules: `publisher/*` | Must not import the root package `ego` or GoAkt | design.md §3 |
| `no-cross-module-internal` | Every nested module | Must not import root-module `internal/...` | design.md §3 |

## Baseline at the start

| Importer | Import | Rule | Owner | Removal criterion |
|---|---|---|---|---|
| `publisher/kafka`, `nats`, `pulsar`, `websocket` | `github.com/pablogore/ego/v4` | `external-adapter-no-runtime` | @pablogore | S1b: publishers import `port/publishing` once #111 builds nested modules |
| `migration` | `github.com/pablogore/ego/v4` | `application-no-runtime` | @pablogore | S3/S4 (#103, #11): runtime-neutral contracts for what `migration` uses |

## Constraints

- TDD strict (global configuration); runner `go test`; no `-race` locally.
- No new third-party dependency.
- Existing architecture tests stay; this tool is a superset for layers, not a replacement for the
  stricter `tenancy` stdlib-only test or the logger source scan.
- Route: **delegated direct** — one writer for T1–T2 (writer trigger: 2+ non-trivial files);
  T3–T4 inline.

## Tasks

- [x] **T1** Rule engine (`internal/cmd/archcheck/rules`): layer table, edge evaluation, baseline
  with stale-entry detection, deterministic sorted output.
  Check: RED then GREEN unit tests with in-memory graphs — forbidden import fails with importer,
  import and rule in the message; allowed graph passes; baselined violation passes; stale baseline
  entry fails.
  Route: delegated direct (writer trigger). Commit `05a49dc`.
- [x] **T2** Loaders and CLI (`internal/cmd/archcheck/main.go`): root module via `go list`, nested
  modules via `go/parser`; repository baseline.
  Check: `go run ./internal/cmd/archcheck` exits 0 on the branch; a throwaway GoAkt import in a
  contract package makes it exit 1 with an actionable message.
  Route: delegated direct (writer trigger). Commit `1866d0a`.
- [x] **T3** Wire the step into `pull_request.yml` and `build.yml`.
  Check: workflow YAML readback; CI run on the PR.
  Route: inline. Step "Check architecture boundaries" added after "Install dependencies", before the
  linter, in both workflows, with `GOFLAGS=-mod=vendor` like the selector step. YAML parses.
- [x] **T4** Document it in `docs/ci.md` (rules, baseline policy, how to add a layer) and
  `CHANGELOG.md`. Check: structural readback.

## Acceptance criteria (#107)

Fails on a forbidden fixture; passes on the allowed graph; runs fast and separately from the suite;
the message names importer, import and rule; every baseline entry has owner, justification and
removal criterion; automated, not review-only; documented update procedure.

## Progress and evidence

### T1 (rule engine) — done, commit `05a49dc`

- Files: `internal/cmd/archcheck/rules/{graph,layers,rules,baseline,evaluate,report}.go`,
  `evaluate_test.go`.
- TDD: wrote `evaluate_test.go` against the not-yet-existing package, moved the implementation
  files aside, `go test` failed with `undefined: Graph/Package/RootModule/...` (RED, build
  failure), restored the implementation, `go test` passed all 16 test functions (GREEN).
- `go vet ./internal/cmd/archcheck/...`: no issues. `gofmt -l`: clean.
- The `contract-allowlist` layer matches the 8 named contracts and `port/...` (and their
  subpackages), explicitly excluding `persistence/conformance` (test support, not a contract),
  per the task spec.

### T2 (loaders + CLI) — done, commit `1866d0a`

- Files: `internal/cmd/archcheck/{main,loader,baseline}.go`, `loader_test.go`, plus a 1-line
  staticcheck fixup in `rules/rules.go` and a wording fixup in `rules/report.go`.
- TDD: wrote `loader_test.go` first (t.TempDir fixtures for `parseGoModModulePath`,
  `loadNestedModule`, `discoverNestedModuleDirs`); `go test` failed with `undefined:
  parseGoModModulePath/loadNestedModule/discoverNestedModuleDirs` (RED), then implemented
  `loader.go` until GREEN.
- Verification (all observed on this branch, from the repository root):
  1. `go build ./... && go vet ./internal/cmd/archcheck/...` → both clean, no output.
  2. `go test -count=1 ./internal/cmd/archcheck/...` (no `-race`) → `ok` both packages
     (`archcheck`, `archcheck/rules`).
  3. `go run ./internal/cmd/archcheck` → exit 0,
     `archcheck: 14 packages checked, 94 edges checked, 5 baselined, 0 violation(s), 0 stale entries`,
     wall time ≈0.39s (`time` output: `0,39s total`).
  4. Fixture proof: added `tenancy/zz_violation.go` importing
     `github.com/tochemey/goakt/v4/actor`; `go run ./internal/cmd/archcheck` → exit 1, reported
     `github.com/pablogore/ego/v4/tenancy imports github.com/tochemey/goakt/v4/actor: rule
     contract-allowlist (design.md §3): ...`; file deleted, `git status` confirmed clean.
  5. Stale proof: temporarily appended a bogus baseline entry
     (`tenancy -> github.com/tochemey/goakt/v4/nonexistent`) to `repoBaseline`; `go run
     ./internal/cmd/archcheck` → exit 1, reported "baseline entry ... no longer matches a
     violation; delete it"; reverted, re-ran → exit 0 again.
  6. `GOFLAGS=-mod=vendor go run ./internal/cmd/archcheck` (after `go mod vendor`) → exit 0, same
     summary line.
  7. `golangci-lint run --config .golangci.yml ./internal/cmd/archcheck/...` → `0 issues.` (one
     staticcheck S1008 finding was fixed in `rules.go` before this final run).
  8. `gofmt -l internal/cmd/archcheck` → empty output (clean).
- Real baseline used matches the spec exactly: `publisher/kafka`, `publisher/nats`,
  `publisher/pulsar`, `publisher/websocket` (each a single-package nested module, one production
  import of the root package) under `external-adapter-no-runtime`, and `migration` under
  `application-no-runtime`. No deviation from the task's proposed baseline was found; the real
  import graph matches it exactly (verified via `rg` before writing loaders, and confirmed by the
  tool's own 0-violation/0-stale run above).
- Open note: `example/cluster` (a nested module, not under `publisher/`) imports the root package
  `github.com/pablogore/ego/v4` directly. This is **not** a violation under the current rule
  table: `external-adapter-no-runtime` only covers `publisher/*`, and
  `no-cross-module-internal` only forbids `internal/...` imports, not the root package itself.
  This matches design.md's classification of `example/cluster` as an "unreleased consumer,
  unchanged" rather than an external adapter, so no rule was written for it — flagging this in
  case the next slice wants to tighten that.

T3 (workflow wiring) and T4 (docs/CHANGELOG) are not part of this writer's scope and remain
unstarted.

### T3 — done, commit `260c778`
Step "Check architecture boundaries" in `pull_request.yml` and `build.yml`, after "Install
dependencies" and before the linter, `GOFLAGS=-mod=vendor`. Both files parse as YAML. The CI run on
the PR is the remaining proof.

### T4 — done
`docs/ci.md` gains "Architecture boundary check" (rules table, baseline policy, how to add a layer);
`CHANGELOG.md` Improvements entry. Check: structural readback; names cited in the doc
(`DefaultRules`, `contractRoots`, `baseline.go`) verified against the code.

### Delivery note
The feature is ~1,200 production lines plus ~540 test lines, above the ~400-line heuristic. It ships
as one PR with one commit per task: the engine, the loaders and the wiring are only useful together,
and splitting them would land an inert half. Review commit by commit.

**Review:** RDD is off (global); no native review ran.

## Review round 1 (PR #117)

Review fixes requested by the user after the first PR round. Rule semantics (design.md §3) and the
baseline contents stay unchanged. Contract-subpackage classification is out of scope (ADR question).
Seven tasks because the user scoped the round explicitly; each is atomic and has its own commit.
Route: delegated direct, one writer (writer trigger: 2+ non-trivial files). TDD strict, runner
`go test`, no `-race`.

- [x] **R1** Fail when a rule checks nothing: per-rule match counts, `PackagesChecked`/`EdgesChecked`
  from `rules.Evaluate`, unique-edge counting, module path read from root `go.mod`,
  `DefaultRules(rootModulePath)`; drop `countCheckedEdges`. Commit `ae9862b`.
- [x] **R2** Do not merge a nested module into its outer module (loader + discovery + doc comments).
  Commit `9345cbe`.
- [x] **R3** Reject duplicate and empty baseline entries. Commit `568729c`.
- [x] **R4** Robust `go.mod` module-path parsing (quoted, trailing comment, `module (` block).
  Commit `58e1911`.
- [x] **R5** End-to-end tests for `loadRootModule` (real `go list`) and `run()` exit behavior.
  Commit `02145a4`.
- [x] **R6** Rule-specific `Violation.Reason` (or honest doc comment); remove `isVendoredImportPath`
  if `go list` can never return a vendored package. Commit `4213514`.
- [x] **R7** Document the build-constraint gap in `docs/ci.md`. Commit `2e5660a`.

### Review round 1 — progress and evidence

Route actually used: delegated direct, one writer, as planned; each of R1–R7 landed as its own
commit on `ci/107-arch-boundary-check`, `3cd6a19..2e5660a`. TDD strict throughout: for every task
except R6's `isVendoredImportPath` removal (a dead-code deletion backed by empirical evidence, not a
new behavior) and R7 (docs only), a failing test was written first, `go test` was run against the old
production code to observe the RED failure (build error or failed assertion), then the implementation
was written to GREEN. `gofmt -l internal/cmd/archcheck` (empty), `go test -count=1
./internal/cmd/archcheck/...` (pass) and `golangci-lint run --config .golangci.yml
./internal/cmd/archcheck/...` (`0 issues.`) were run before every commit.

- **R1** (`ae9862b`): `rules.Evaluate` now returns `Result.PackagesChecked`, `Result.EdgesChecked` and
  a `RuleStats []RuleStat` (per-rule matched-package counts), and rejects the ruleset with an error
  naming every rule (and its layer) that matched zero packages. `DefaultRules(rootModulePath string)`
  reads the module path from the root `go.mod` instead of a package constant; layers
  (`ContractLayer`, `ApplicationLayer`, `ExternalAdapterLayer`) became functions of that path.
  `main.go`'s `countCheckedEdges` is gone; the summary line reads `Result`'s own counts. `run()` was
  split into a thin CLI wrapper plus a testable `runCheck(repoRoot, baseline, stdout)`, used again by
  R5. RED: reverting the four `rules/*.go` production files (via `git stash`) against the updated
  `evaluate_test.go` produced 9 compile errors (`too many arguments in call to DefaultRules`,
  `undefined: RuleStat`, etc.); GREEN after restoring the implementation, all 20 test functions in
  `rules` plus 5 in the `main` package pass. New tests: `TestEvaluate_ZeroMatchRulesFailClosed` (a
  graph under a different module path makes every rule match zero, and the error names all four
  rule IDs), `TestEvaluate_ContractAllowlistDoesNotNeedPortPackages` (a graph with no `port/...`
  package still passes `contract-allowlist`, proving the check is per rule, not per contract root),
  `TestEvaluate_SummaryCountsAreExactAndDeduped` (a `publisher/kafka`-shaped fixture proves an edge
  checked by two rules — `external-adapter-no-runtime` and `no-cross-module-internal` — is counted
  once, not twice). Existing tests that exercised one rule against a deliberately partial graph
  (e.g. only a `tenancy` package) were narrowed to a `rulesFor(t, "<rule-id>", ...)` subset of
  `DefaultRules(root)` instead of the full ruleset, since the new zero-match check would otherwise
  reject those intentionally partial fixtures.
- **R2** (`9345cbe`): `discoverNestedModuleDirs` used to `filepath.SkipDir` as soon as it found a
  `go.mod`, so a module nested inside another nested module could never be discovered on its own;
  `loadNestedModule` had no notion of a subdirectory being a separate module boundary and would have
  walked straight through it, merging its files into the outer module's package set under the outer
  module's import path. `discoverNestedModuleDirs` now keeps descending after adding a module
  directory; `loadNestedModule` now `SkipDir`s at any subdirectory (other than its own `moduleDir`)
  that holds a `go.mod`. RED: the two new tests
  (`TestDiscoverNestedModuleDirs_FindsModuleNestedInsideAnotherModule`,
  `TestLoadNestedModule_DoesNotMergeInnerModule`) failed against the pre-fix code — the first found
  only the outer module directory, the second returned 2 packages (`outer` and `outer/inner`) instead
  of 1. GREEN after the fix. This repository has no module nested inside another nested module today
  (`publisher/*`, `benchmark`, `example/cluster` are all single-level), so this is latent-bug
  prevention, not a fix to an observed production violation.
- **R3** (`568729c`): `ValidateBaseline` now also rejects an empty or blank-only `Importer`/`Import`,
  and rejects a second entry that repeats an already-seen `(Importer, Import, Rule)` key, reported
  alongside any other problems via the existing `errors.Join`. RED: 4 new subtests under
  `TestValidateBaseline_MissingFieldsRejected` (empty/blank importer, empty/blank import) and a new
  `TestValidateBaseline_DuplicateEntryRejected` failed (`ValidateBaseline() = nil error, want a
  rejection`) before the fix. A same-importer/import-but-different-rule case
  (`TestValidateBaseline_DuplicateEntryDifferentRuleIsNotADuplicate`) confirms that is not treated as
  a duplicate. The repository's real `repoBaseline` (`internal/cmd/archcheck/baseline.go`) has no
  duplicate or empty entries, so this is prevention, not a fix to a real baseline problem.
- **R4** (`58e1911`): `parseGoModModulePath` now handles every form the go.mod grammar allows for the
  `module` directive: a quoted path (Go string-literal syntax via `strconv.Unquote`), a trailing
  `// comment`, and the parenthesized block form (`module (`, one argument line, `)`). RED: a
  table-driven `TestParseGoModModulePath_Forms` covering 8 forms failed on 6 of them (quoted,
  trailing comment, quoted-with-comment, block form, block-form-quoted, block-form-with-comment)
  against the old single-line-only parser; GREEN after the rewrite. No dependency on
  `golang.org/x/mod` was added (it is not in the root `go.mod`), per the constraint.
- **R5** (`02145a4`): new `internal/cmd/archcheck/e2e_test.go`. `TestLoadRootModule_RealGoList` builds
  a tiny module in `t.TempDir()` (a `contract` package, a `runtimeish` package standing in for a
  forbidden dependency, and an `adapter` package importing both, plus a `_test.go` file with a
  test-only import) and loads it through the real `go list -e -json ./...`, confirming production
  imports are captured and the test-only import is excluded. Every import resolves offline (stdlib or
  within the temp module itself), so no network or module download is needed.
  `TestLoadRootModule_LoadErrorFailsClosed` uses an unterminated `import (` block — confirmed
  empirically that `go list -e -json` reports a non-nil `Error` for this case but not for a syntax
  error later in a file (which its lightweight prescan does not always catch) — and proves
  `loadRootModule` returns an error rather than silently dropping the broken package.
  `TestRunCheck_CleanGraphPasses`, `TestRunCheck_UnbaselinedViolationFails` and
  `TestRunCheck_StaleBaselineEntryFails` drive `runCheck` (the testable core `run()` was split into
  during R1) against a fixture repository covering all three exit behaviors the task named. Every
  test calls `t.Skip` when `go` is not on `PATH`. Confidence check: temporarily short-circuiting the
  `raw.Error != nil` branch in `loader.go` and re-running
  `TestLoadRootModule_LoadErrorFailsClosed` reproduces a failure, confirming the test is not vacuous;
  reverted before committing.
- **R6** (`4213514`): `Rule` gained an optional `Reason func(importPath string) string`; when set,
  `Evaluate` calls it instead of the generic `reasonFor` fallback. `application-no-runtime`,
  `external-adapter-no-runtime` and `no-cross-module-internal` each now name the specific matched
  import and which forbidden prefix it is (root package, `internal/extensions`, GoAkt, or the root
  module's `internal/` boundary) instead of only restating the rule's layer name.
  `contract-allowlist` keeps its existing generic "not on the ... allowlist" reason — deliberately:
  naming one specific allowed-list check that failed would be less informative than the allowlist
  itself, since a contract-allowlist violation can fail on any of several unrelated grounds at once.
  RED: `TestEvaluate_ViolationReasonNamesForbiddenPrefix` (5 subtests, one per forbidden-prefix case)
  failed against the old generic-only `reasonFor`, e.g. `Reason = "matches a forbidden import for
  application package migration"` instead of naming GoAkt or `internal/extensions`; GREEN after
  adding the `Reason` funcs. `isVendoredImportPath` (and its call site in `loadRootModule`) was
  removed as dead code: confirmed empirically, both in a throwaway fixture module and against this
  repository's own full package graph, that `go list -e -json ./...` never returns an `ImportPath`
  containing `vendor` under `GOFLAGS=-mod=vendor` — `-mod=vendor` only changes how *other* modules'
  imports resolve, not what `./...` itself enumerates for the main module — so the filter it
  implemented could never fire.
- **R7** (`2e5660a`): `docs/ci.md`, "Architecture boundary check", gained a paragraph naming the
  fail-safe asymmetry: the root module is checked via `go list` for the CI host's own `GOOS`/`GOARCH`
  only (a file restricted to another platform is invisible to the check), while nested modules are
  parsed with `go/parser` ignoring build constraints entirely (so they can only over-report, never
  under-report, across platforms). Also updated the "adding a layer or rule" walkthrough for R1's and
  R6's signature changes (`DefaultRules(rootModulePath string)`, a layer as a function of the root
  module path, a denylist rule's optional `Reason` func). No loader behavior changed for this task,
  per its constraint.

### Final verification (post-review-round-1)

All run from the repository root, `2e5660a` (branch head after R1–R7):

1. `go test -count=1 ./internal/cmd/archcheck/...` → `ok` both packages (`archcheck`, `archcheck/rules`).
2. `golangci-lint run --config .golangci.yml ./internal/cmd/archcheck/...` → `0 issues.` (run with a
   freshly generated `vendor/`, per `modules-download-mode: vendor` in `.golangci.yml`; the locally
   installed `golangci-lint 2.13.1` is built with go1.26.6, and the default `go1.27.1` toolchain in
   this environment trips an unrelated `typecheck` false positive in `internal/poll` — reproduced
   identically on the pre-review-round base commit with none of this round's changes, so it predates
   this work. Pinning `GOTOOLCHAIN=go1.26.6` for the lint invocation only, matching the linter's own
   build, avoids it and gets a real signal: `0 issues.`).
3. `gofmt -l internal/cmd/archcheck` → empty (clean).
4. `go run ./internal/cmd/archcheck` → exit 0:
   `archcheck: 14 packages checked, 69 edges checked, 5 baselined, 0 violation(s), 0 stale entries`.
   Delta from the pre-review-round line (`14 packages checked, 94 edges checked, 5 baselined, 0
   violation(s), 0 stale entries`): packages checked is unchanged (14) because the old
   `countCheckedEdges` already deduped packages into a `map[string]bool`; only edges were
   double-counted before. Edges dropped from 94 to 69 (−25) because the four `publisher/*` packages
   are nested modules rooted under `publisher/`, so each matches **two** rules at once
   (`external-adapter-no-runtime` and `no-cross-module-internal`); the old per-rule loop counted
   every one of their non-stdlib imports once per matching rule, while `Result.EdgesChecked` now
   dedups by the unique `(importer, import)` pair, per R1's explicit requirement.
5. `go mod vendor && GOFLAGS=-mod=vendor go run ./internal/cmd/archcheck` → exit 0, identical summary
   line; untracked `vendor/` removed afterward (`git status` confirmed clean).
6. Manual fixture: added `tenancy/zz_violation.go` importing `github.com/tochemey/goakt/v4/actor` →
   exit 1, reported `github.com/pablogore/ego/v4/tenancy imports
   github.com/tochemey/goakt/v4/actor: rule contract-allowlist (design.md §3): ...`; file deleted,
   re-run → exit 0 again.
7. Manual fixture: temporarily appended a bogus entry (`tenancy -> github.com/tochemey/goakt/v4
   /nonexistent`, rule `contract-allowlist`) to `repoBaseline` → exit 1, reported `baseline entry ...
   no longer matches a violation; delete it`; reverted with `git checkout --`, re-ran → exit 0 again.
8. `git status` clean except the doc-update commit this task list is part of;
   `git log --oneline 3cd6a19..HEAD` shows exactly the 7 commits above plus this doc-update commit.
