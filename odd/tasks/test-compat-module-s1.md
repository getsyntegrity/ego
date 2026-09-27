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

- [ ] T1 `test/compat` module with the alias and sentinel assertions for all four publishers
  (route: inline, writer agent; RED: assertions shown failing against a mutated alias).
- [ ] T2 Delete the four `compat_test.go`, tidy the publishers, drop the compat lane in
  `verify-module.sh`, update the publishers' comments (route: inline).
- [ ] T3 archcheck module table, `no-module-cycle`, generalized `no-cross-module-internal`
  (route: inline; RED: fixtures fail before the rules exist).
- [ ] T4 `docs/ci.md` and `CHANGELOG.md`: `test/compat` listed as unreleased, measured selection
  recorded (route: inline).

## Acceptance criteria and checks

- `scripts/ci/verify-module.sh` passes for `test/compat` and the four publishers (plus `benchmark`,
  `example/cluster`), including `go mod tidy -diff`.
- The four publishers' closure tests still pass.
- archcheck passes on the real repository with no new baseline entry; cycle and cross-module
  `internal/` fixtures are rejected.
- `ciselect -base origin/main` selects `test/compat` for a `port/publishing` change and a publisher
  change, and not for an unrelated change.

## Progress and evidence

(filled in as tasks close)

## Next step

T1.
