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

The sentinel assertion. The old test built a stopped publisher with the unexported field `started`,
which a separate module cannot reach, and every constructor dials a broker. The options were (a) a
real broker or fake server per publisher (Pulsar has no embeddable server), (b) an exported test hook
in the publishers' production code (outside S1's scope), (c) splitting the check into "publisher
returns `publishing.ErrPublisherNotStarted`" (already in each publisher's `publisher_contract_test.go`,
events and state, all four) plus "`ego.ErrPublisherNotStarted` is the same value" in `test/compat`,
or (d) setting the field from `test/compat` through `reflect` + `unsafe`.

The first push used (d). The maintainers chose (c) on 2026-09-27 (PR #142), and the branch now
implements it. (c) is not weaker: `ego.ErrPublisherNotStarted` is defined as
`publishing.ErrPublisherNotStarted`, so an error matches one exactly when it matches the other, and
the two halves together prove the original assertion for every publisher and kind. It also keeps
`reflect`/`unsafe` out of the tests. The ADR records the split in §6 S1, task 1.

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
  recorded (route: inline). Commit `0fa4dab`.
- [x] T5 Maintainer decision on PR #142: option (c) for the sentinel check (drop `reflect`/`unsafe`,
  add the identity check, ADR note), plus review nits: tidy wording, `test/compat` job cost, loader
  fail-closed tests (route: inline). Commit: the T5 commit on this branch.

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

- **Assertion mapping (T1, revised in T5).** Per publisher P in kafka, nats, pulsar, websocket, the
  old `publisher/P/compat_test.go` held `_ ego.EventPublisher = (*EventsPublisher)(nil)`,
  `_ ego.StatePublisher = (*DurableStatePublisher)(nil)` and `TestPublishBeforeStartMatchesEgoSentinel`
  (events and state). Now: the 8 compile-time assertions are in
  `test/compat/publisher_compat_test.go` as `(*P.EventsPublisher)(nil)` / `(*P.DurableStatePublisher)(nil)`;
  the 8 runtime checks are `TestPublishBeforeStartMatchesPublishingSentinel` (events and state) in
  each `publisher/P/publisher_contract_test.go`, plus one identity check
  `TestEgoSentinelIsThePublishingSentinel` in `test/compat`.
- **RED (T1).** Temporarily set `ego.ErrPublisherNotStarted = errors.New(...)` in `publisher.go`:
  all 8 subtests FAIL. Temporarily widened `ego.EventPublisher`/`StatePublisher` with an extra method:
  `go vet`/`go test` report 8 "does not implement" errors, one per assertion. `publisher.go`
  restored with `git checkout` both times (never committed). GREEN: 8/8 subtests pass.
- **T2.** Publisher closure tests (`TestUnitTestClosureExcludesRuntimeAndRoot`) and contract tests
  pass. `go mod tidy` was required by the tidy gate: it dropped the indirect GoAkt, Olric and OTel
  lines from each publisher's `go.mod`/`go.sum` (S3 still owns dropping the root requirement), and
  added indirect lines pinning already-resolved versions: 1 in kafka (`prometheus/client_golang`),
  12 in pulsar (`testcontainers-go`, `moby`/`docker` clients, `gopsutil`, `x/crypto`, ...); nats and
  websocket only lose lines. The module versions behind `go list -deps -test ./...` are identical to
  `main` in all four (kafka 21, nats 13, pulsar 72, websocket 7 modules).
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
- **RED (T5).** Identity check: with `ego.ErrPublisherNotStarted = errors.New(...)` in a throwaway,
  uncommitted edit of `publisher.go`, `TestEgoSentinelIsThePublishingSentinel` fails all three
  checks (`==`, `errors.Is` both ways); restored with `git checkout`, it passes. Loader tests: with the
  error and empty-path guards in `loadModuleTable` temporarily removed, the new
  `TestLoadModuleTable_MalformedGoModFailsClosed` and `TestLoadModuleTable_EmptyModulePathFailsClosed`
  fail; restored, they pass.
- **CI cost.** PR run 36331397582: `test/compat` job about 195 s, the slowest module job
  (`publisher/pulsar` about 154 s). It runs on every leaf publisher PR.
- **Pending.** The throwaway draft-PR CI measurement (design §6 S1) needs maintainer authorization
  and was not done. Native review (RDD) was not run by the writer.

## Next step

PR #142 is open. Waiting on its CI for the T5 commit and on maintainer review; merging is the
maintainers' call.
