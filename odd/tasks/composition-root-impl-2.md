# Feature: composition `Spec`, its validation and the composition archcheck rules (#105, slice IMPL-2)

Branch: `feat/105-impl-2-compose-spec` · Base: `origin/main` `965293a` · Epic: #10 · Issue: #105 ·
Design: `openspec/changes/ego-arch-003/design.md` (§D2, §D3, §D4a, §D6 `StartError`, §D8, §6 row IMPL-2, §7)

## Problem

Ego has no composition root. Every consumer wires the engine by hand in `main()`, and nothing checks the
whole dependency graph before something starts: a missing events store is accepted by `NewConfig` and
only panics at the first spawn (design §2.2). The design fixes this in slices. IMPL-2 is the
runtime-neutral foundation the later slices build on.

## What changes in this slice

A new root-module package, `compose`, declares `Spec` (the plain struct of already-built dependencies
plus the declared entity families), `Family` (`EventSourced`, `DurableState`, `Saga`), `Spec.Validate`
(rules V1–V6, every problem joined with `errors.Join` as a `*ValidationError` naming the rule and the
field) and `StartError` (the step that failed, its error and the rollback error). `compose` imports
contract packages only.

`internal/cmd/archcheck` gains two rules. `composition-no-runtime` has its own layer — `compose` and
`compose/internal/...` — and the same denylist as `application-no-runtime` (package `ego`,
`internal/extensions`, GoAkt), whose layer still matches only `migration`. `composition-leaf` lets only
packages under `compose/`, `main` packages and `example/...` import `compose/...`; tests never reach the
graph and `benchmark` is a nested module. To tell `main` packages apart, `rules.Package` gains `Name`,
filled by the root loader from `go list`'s `Name` and by the nested loader from the package clause.

## Why this shape

The design fixes the semantics; this slice settles the names. One judgement call: V5 rejects a typed
nil in every interface field, **optional fields included** (`SnapshotStore`, `Encryptor`,
`TenantResolver`, each `EventAdapters` element). The rejected alternative — reject a typed nil only where
a literal nil is rejected — would let a typed-nil `SnapshotStore` through, and the engine's own
`!= nil` guard would then call a method on it and panic, the very bug class V5 exists for. Loosening
this later is compatible; tightening it later would not be.

## Constraints

- Only IMPL-2: no lifecycle sequencer (IMPL-3), no `compose/goakt` (IMPL-4), no `ego` options.
- No new archcheck baseline entry; `application-no-runtime` is not widened.
- Do not touch `docs/ci.md` (another writer owns it right now); its rule table needs two new rows as a
  follow-up.
- TDD: strict (user global configuration); runner `go test` (no `-race`, no workbench locally).
- Route: direct inline, one writer (already-specified files).

## Tasks

- [x] **T1** RED tests for `compose`: one per rule V1–V6 with everything else valid, a multi-problem
  test, typed nil per interface field, duplicate publisher IDs per kind, `StartError`. Check: 10 tests
  fail against stubs. Evidence: commit `afcda08`.
- [x] **T2** Implement `Spec`, `Validate`, `ValidationError`, `StartError`. Check: `go test ./compose/`
  passes, `go vet`, gofmt clean. Evidence: commit `afcda08`.
- [x] **T3** RED archcheck tests (composition-no-runtime, composition-leaf, `application-no-runtime`
  still only `migration`, loader `Name`, e2e fixtures), then implement the layers, rules and loader
  field. Check: 13 tests fail first, then `go test ./internal/cmd/archcheck/...` passes. Evidence:
  commit `741200b`.
- [x] **T4** Real-repo archcheck before/after and the two throwaway violation demos (not committed).
- [x] **T5** `CHANGELOG.md` and this document; ciselect, full root suite, nested modules,
  golangci-lint.

## Acceptance criteria (IMPL-2 row of design.md §6)

1. Each rule V1–V6 has a test with everything else valid; a multi-problem test lists all problems.
2. V5 is tested for each interface field; V6 duplicate IDs are tested per kind.
3. `composition-no-runtime` rejects `compose` and `compose/internal/lifecycle` importing `ego`,
   `internal/extensions` and GoAkt; `application-no-runtime` still matches only `migration`.
4. A non-`main` production package outside `compose/` cannot import `compose/...`; a `main` package,
   examples, tests and `benchmark` can.
5. archcheck on the real repository: 0 violations, no baseline entry added, 0 stale.

## Verification evidence

- RED `compose`: `TestSpecValidate_V1..V6`, `_V5_*`, `_V6_*`, `_ReportsEveryProblem`, `TestStartError`
  failed against a stub `Validate` returning nil (10 failing tests).
- RED archcheck: 13 failing tests (rule not found, loader `Name` empty, zero-match and count checks).
- GREEN: `go test ./compose/ ./internal/cmd/archcheck/...` pass.
- archcheck before (`origin/main`): `16 packages checked, 72 edges checked, 1 baselined, 0 violation(s),
  0 stale entries`. After: `36 packages checked, 156 edges checked, 1 baselined, 0 violation(s), 0 stale
  entries`. The count grows by 20, not by 1: `compose` joins the new composition layer, and
  `composition-leaf`'s layer covers every non-`main` root-module package outside `compose/` and
  `example/`, most of which no earlier rule inspected.
- Demo (throwaway): a blank import of `github.com/pablogore/ego/v4` in `compose` fails with
  `composition-no-runtime`; a blank import of `compose` in `testkit` fails with `composition-leaf`.
- ciselect: mode `affected`, 3 packages (`compose`, `internal/cmd/archcheck`, `.../rules`).
- golangci-lint 2.13.1 (go1.26.6) `--new-from-rev=origin/main` on the changed packages: 0 issues
  (run with `--modules-download-mode=mod`; the repo config uses `vendor` and no vendor tree exists
  locally).

## Next step

IMPL-3: `compose/internal/lifecycle`, the ordered start/stop sequencer returning `compose.StartError`.
Follow-up for the `docs/ci.md` owner: add `composition-no-runtime` and `composition-leaf` rows to the
"Architecture boundary check" rule table.
