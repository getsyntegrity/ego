# Feature: `test/compat` integration module (#102, slice S1)

Branch: `ci/102-s1-test-compat` · Base: `origin/main` `9084b80` · Epic: #10 · Issue: #102 ·
Design: `openspec/changes/ego-arch-006/design.md` (§2.1, §2.2 row S1, §3 D5, §5.2, §6 S1, §7)

## Problem

Each of the four publisher modules (`publisher/kafka`, `nats`, `pulsar`, `websocket`) carries a
`compat_test.go` (#130, #122) that checks the historical compatibility aliases in the root package
`ego`: `ego.EventPublisher`, `ego.StatePublisher` and `ego.ErrPublisherNotStarted`. Because those
files import package `ego`, every publisher module has to keep requiring the root module, which pulls
the GoAkt runtime and Olric into its module graph. #122 hid the file behind a `compat` build tag so
that the default test closure stays clean, but the requirement itself remains, and S3 of the design
cannot drop it without losing the check.

## What changes in this slice

A new nested module, `test/compat` (module path `github.com/pablogore/ego/v4/test/compat`, the same
path scheme `benchmark` uses; it is never released, so no D1–D3 decision applies), requires the root
and the four publishers through local `replace` directives and holds the alias and sentinel
assertions for all four publishers. The four `compat_test.go` files are deleted, so the publishers no
longer need the `compat` build-tag lane, and `scripts/ci/verify-module.sh` drops that lane.

`internal/cmd/archcheck` gains a module table (each module's path and its in-repository
requirements, read with `go mod edit -json`) and two module-aware checks:

- `no-module-cycle` (new): an in-repository requirement edge that lies on a cycle fails the build.
- `no-cross-module-internal` (generalized): any package in module A importing an `internal/` package
  owned by a different in-repository module B fails, not only "nested module to root internal/".

## Why this shape

Judgement call on the sentinel assertion. The old test built a stopped publisher with the unexported
field `started`, which an external module cannot reach, and every constructor dials a broker. The
alternatives were (a) a real broker or fake server per publisher (Pulsar has no embeddable server),
(b) adding an exported test hook to the publishers' production code (out of S1's scope), or (c)
splitting the check into "publisher returns `publishing.ErrPublisherNotStarted`" (already in each
publisher) plus "`ego.ErrPublisherNotStarted` is the same value" (a weaker, transitive form). We chose
(d): `test/compat` builds the zero-value publisher and sets its `started` field through
`reflect` + `unsafe`, exactly the state the old test built, so the runtime assertion stays
one-to-one. If the field is renamed or retyped the helper fails the test loudly instead of passing.

## Constraints

- Only S1. No module-path migration (D1), no contracts module (S2), no change to the publishers'
  imports or `closure_test.go` semantics (S3).
- No new archcheck baseline entry; no alias assertion weakened.
- No workflow or branch-protection change: CI must discover `test/compat` on its own.
- Root production `.go` files, `engine.go`, `compose/` and `internal/cmd/ciselect` untouched.

## Resolved TDD mode

Strict TDD enabled (user session configuration). Runner: `go test` (`env -u GOROOT`, Go 1.27.1 local;
never `-race`).

## Tasks

- [x] T1 `test/compat` module with the alias and sentinel assertions for all four publishers
  (route: inline, writer agent; RED: assertions shown failing against a mutated alias).
  Commit `985ce6c`.
- [x] T2 Delete the four `compat_test.go`, tidy the publishers, drop the compat lane in
  `verify-module.sh`, update the publishers' comments (route: inline). Commit `85aac0e`.
- [x] T3 archcheck module table, `no-module-cycle`, generalized `no-cross-module-internal`
  (route: inline; RED: fixtures fail before the rules exist). Commit `c4272bd`.
- [x] T4 `docs/ci.md` and `CHANGELOG.md`: `test/compat` listed as unreleased, measured selection
  recorded (route: inline). Commit: the docs commit on this branch.

## Acceptance criteria and checks

- `scripts/ci/verify-module.sh` passes for `test/compat` and the four publishers (plus `benchmark`,
  `example/cluster`), including `go mod tidy -diff`.
- The four publishers' closure tests still pass.
- archcheck passes on the real repository with no new baseline entry; cycle and cross-module
  `internal/` fixtures are rejected.
- `ciselect -base origin/main` selects `test/compat` for a `port/publishing` change and a publisher
  change, and not for an unrelated change.

## Progress and evidence

Toolchain: `env -u GOROOT` gives Go 1.27.1 for build and test. golangci-lint 2.13.1 is built with
go1.26.6 and fails type-checking against the 1.27.1 standard library, so every `verify-module.sh`
run and the root lint used the fallback `GOROOT=/home/pablog/sdk/go1.26.6 GOTOOLCHAIN=local`.

- **Assertion mapping (T1).** Per publisher P in kafka, nats, pulsar, websocket, the old
  `publisher/P/compat_test.go` held `_ ego.EventPublisher = (*EventsPublisher)(nil)`,
  `_ ego.StatePublisher = (*DurableStatePublisher)(nil)` and
  `TestPublishBeforeStartMatchesEgoSentinel` (events and state). `test/compat/publisher_compat_test.go`
  holds `_ ego.EventPublisher = (*P.EventsPublisher)(nil)`, `_ ego.StatePublisher =
  (*P.DurableStatePublisher)(nil)` and subtests `P/events`, `P/state` of
  `TestPublishBeforeStartMatchesEgoSentinel`: 8 compile-time plus 8 runtime assertions, 16 of 16.
- **RED (T1).** Temporarily set `ego.ErrPublisherNotStarted = errors.New(...)` in `publisher.go`:
  all 8 subtests FAIL. Temporarily widened `ego.EventPublisher`/`StatePublisher` with an extra method:
  `go vet`/`go test` report 8 "does not implement" errors, one per assertion. `publisher.go`
  restored with `git checkout` both times (never committed). GREEN: 8/8 subtests pass.
- **T2.** Publisher closure tests (`TestUnitTestClosureExcludesRuntimeAndRoot`) and contract tests
  pass. `go mod tidy` was required by the tidy gate: it dropped the indirect GoAkt, Olric and OTel
  lines from each publisher's `go.mod`/`go.sum` (S3 still owns dropping the root requirement).
- **RED (T3).** New `rules/modules_test.go`: with only the `Module`/`Graph.Modules` types added, 9
  tests fail (unknown rule `no-module-cycle`; no violation for nested-to-nested and root-to-nested
  `internal/` imports; no error for a package outside every module). e2e tests failed to compile
  (`loadModuleTable` undefined). GREEN: `go test ./internal/cmd/archcheck/...` ok, also with
  `GOFLAGS=-mod=vendor`.
- **archcheck summary.** Before: `37 packages checked, 157 edges checked, 1 baselined, 0
  violation(s), 0 stale entries`. After: `8 modules checked, 44 packages checked, 182 edges checked,
  1 baselined, 0 violation(s), 0 stale entries`. No baseline entry added.
- **verify-module.sh.** Pass for `publisher/kafka`, `nats`, `pulsar`, `websocket`, `test/compat`,
  `benchmark`, `example/cluster` (tidy, build, vet, lint, test).
- **ciselect `-base origin/main`.** `port/publishing/publishing.go`: root `affected`, all seven
  modules, `test/compat ← .`. `publisher/kafka/kafka.go`: `publisher/kafka`, `test/compat`
  (`test/compat ← publisher/kafka`). `migration/migration.go`, `compose/spec.go`, `docs/ci.md`:
  `test/compat` not selected. Deviation from the design text: the design expected the chain
  `test/compat ← publisher/… ← .` for a `port/publishing` change; the walk records the shorter
  `test/compat ← .` because `test/compat` imports package `ego` directly. Selection is identical.
- **CI discovery.** `modules.json` comes from discovery; `test/compat` appears in it with no
  workflow edit. No workflow or branch-protection change needed.
- **Pending.** CI measurement on a draft PR (design §6 S1 "CI measurement") needs maintainer
  authorization and was not done. Native review (RDD) not run by the writer.

## Next step

Open the PR; CI on the PR is the authoritative lint and race run.
