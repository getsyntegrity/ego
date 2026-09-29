# Test inventory and lane contract (#202)

Parent epic: #201. Related: #159 (modular selection), #203 (go-specs pilot), #210 (integration workflow).

## Problem

Epic #201 splits Ego's tests into lanes (unit, component, integration, architecture, example) so feature and
hotfix PRs run only fast, hermetic suites while integration and examples run in their own workflows. Nothing
can move until we know, for every test and subtest, what it actually touches. Today there is no such list:
CI only reports per-package timings, a test's name (`TestIntegration...`, `e2e`) says nothing reliable about
the resources it uses, and 19 Postgres tests silently `Skip` without a DSN.

The epic's starting evidence was taken on `main` `c41d025` (183 `_test.go` files). Since the #217 sync,
`develop` carries that layout, minus the 28 test files under `internal/cmd/*` that #196 removed with the old
pipeline: 155 `_test.go` files and 873 `Test` functions across 8 modules at `develop` `0de4249`.

## What changes

1. A written lane contract (`docs/testing/lanes.md`) that defines each lane by observable behavior — the
   resources a test touches — not by its name or package.
2. A small generator (Go, under `internal/tools/testinventory`) that produces the inventory from two sources:
   - static signals per `Test` function, read from its AST (for example `net.Listen`, `httptest.NewServer`,
     `exec.Command`, `sql.Open`, a GoAkt actor system or `testkit`, `pause.For`/`time.Sleep`, `t.Skip` behind
     an environment variable);
   - a real `go test -json` run in every module, which is the only way to enumerate subtests (they are
     created at run time) and to record per-package time and skips.
3. An overrides file with a written justification per entry, for what static analysis cannot see (a helper
   that starts a cluster, a mixed file).
4. The committed inventory (`docs/testing/inventory.json`) and a readable summary (`docs/testing/inventory.md`)
   with counts per lane and the precise list of tests that must leave the PR lane, each with the workflow or
   issue that will run it.

Why a generator instead of a hand-written table: about 900 tests plus their subtests cannot be kept exhaustive
by hand, and the epic's closing criterion is "no orphan tests". Rejected alternative: classifying by file name
or package, which the issue explicitly forbids and which would misclassify the in-memory "integration" tests.

## Scope and constraints

- Read-only with respect to tests: no test is moved, tagged, skipped or rewritten here (the epic forbids hiding
  tests behind build tags before their workflow exists, #210).
- The generator is a development tool: it must not add dependencies to the library's public API and must not
  run in the regular test lane in a way that slows it down (its own unit tests use fixtures).
- Timings come from one recorded local run and are labelled with machine and Go version; they are indicative,
  not a CI baseline.
- `-race` is not run locally (user rule); CI already runs the suites.

## Tasks

- [x] **T1 Lane contract.** `docs/testing/lanes.md`: each lane, the objective signals that put a test in it, how
  mixed files are handled, and where each lane runs (PR, main, manual). Check: doc reviewed against the epic's
  decisions; every signal named is detectable by T2 or listed as an override reason.
- [x] **T2 Generator.** `internal/tools/testinventory`: AST signal scan + `go test -json` enumeration per module →
  JSON. Check: TDD unit tests on fixtures (a test that listens, execs, opens SQL, starts an actor system, skips on
  env, has subtests); `go vet`, lint clean.
- [x] **T3 Overrides and review.** `docs/testing/inventory-overrides.json` with justification per entry; manual
  review of mixed files and the special lists named in #202 (19 Postgres skips, engine/compose cluster tests,
  `go list`/exec, local `httptest`, in-memory "integration"/"e2e" names). Check: every override has a reason and
  points at an existing test.
- [ ] **T4 Inventory and summary.** Generate and commit `inventory.json` + `inventory.md`: counts per module and
  lane, tests leaving the PR lane with their destination workflow/issue (#210–#214), skips with cause. Check:
  every `Test` function found by `go test -list` in every module appears exactly once; totals reconcile.
- [ ] **T5 Freshness command.** A documented command that regenerates the inventory and fails when a test is not
  classified (no orphans). Check: running it on the committed state is clean; adding a fixture test without
  classification fails. Wiring it into CI is left to #209.

## Follow-up chain (not in this spec)

- #204 turns the unit-lane contract into an enforced gate.
- #209 wires the freshness check and lane selection into CI.
- #210–#214 create the workflows that the "leaves the PR lane" list points to.

## TDD

Strict TDD on (source: user global configuration). Runner: `go test`.

## Route

Delegated direct: one writer (the work spans 2+ non-trivial files and needs a broad read of the test tree).

## Progress and evidence

Working notes (writer, strict TDD, runner `go test`, no `-race`):

- Overrides use JSON (`docs/testing/inventory-overrides.json`): the root `go.mod` has no direct YAML dependency and the
  tool must not add one. The file also holds `example_modules`.
- T1 (`fdd24df`): `docs/testing/lanes.md`. Check: every signal id named in its table is a constant in
  `inventory/model.go` (script over the table, none missing), so T2 detects each one.
- T2 RED: `go test ./internal/tools/testinventory/...` failed to build (`undefined: Signal, Lane, Module, Test`) for the
  scanner and classifier tests; later slices went RED with `undefined: RunData`, `undefined: Inventory` and `undefined: run`.
  One signal (`lifecycle.start`) and the variable-argument `go` toolchain rule were added after reading real data;
  their fixture tests were written first for the second one only in the sense that the fixture case fails without it.
- T2 GREEN: `go test ./internal/tools/...` ok (scanner, classifier, `go test -json` parser incl. a real run of a tiny
  module, overrides, check, renderer, CLI end to end); `go vet ./internal/tools/...` clean; `golangci-lint run
  ./internal/tools/...` 0 issues.
- Commits: `build(tools): scan Test functions and classify them into lanes (#202)`,
  `build(tools): merge go test -json results and overrides into entries (#202)`, then the summary/check/CLI commit.

- T3 review (static inventory, before the run). Reviewed by reading the tests:
  - 19 PostgreSQL tests of `example/cluster` are `integration` (#211) through the helper that reads
    `EGO_EXAMPLE_POSTGRES_DSN` and opens `pgxpool`; they need no override.
  - Cluster: 9 root tests are real single-node or two-node clusters on `127.0.0.1` ports from `dynaport` and stay
    `integration` (#212): eight in `engine`, one in `compose/goakt`. Three `compose/goakt` tests that only build a
    cluster configuration were false positives and were overridden (two to `unit`, one to `component`).
  - 12 `engine` and 12 `compose/goakt` tests reach a real actor system through a wrapper two helper levels deep or
    through `App.Start`; they are overridden to `component`. 26 more `Start` calls were reviewed and confirmed `unit`
    (in-memory step runner, fake stores, bare structs, span and timer tests).
  - `go list` and exec: 22 `architecture` tests (#208). `TestAssertionSitesNegativeControl` parses an in-memory
    string only and was overridden to `unit`.
  - In-memory tests named integration or end-to-end: none of them touches a real resource; the list is in
    `inventory.md`. No test named like integration is integration by behavior except none: the 9 cluster tests are
    named `...ClusterMode` or `...MultiNode`.
  - Check: `go run ./internal/tools/testinventory -update && ... -check` is clean, and `ApplyOverrides` rejects an
    override without reason, with an invalid lane, listed twice, or pointing at no test (unit tests).

