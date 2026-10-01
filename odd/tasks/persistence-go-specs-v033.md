# persistence tests on go-specs v0.3.3 (#222)

## Problem

PR #222 (branch `test/205-migrate-persistence`) moved the unit tests of the `persistence` package to
go-specs v0.3.1. They already meet the no-external-resource rule: no testify, no `mocks/*`, no waits, no
network or files. What they still lack is the v0.3.3 style:

- `conflict_test.go` and its helper loops call `t.Fatal` / `t.Fatalf` inside the spec body, so those failures
  bypass the spec. The fuzz target does the same.
- Several `for ... s.It(name, ...)` loops are textbook `specs.Table` cases: the canonical error grammar,
  the exact-inverse check, the malformed-message list and the adversarial round trip (143 cases).
- `errors.As(...)` followed by `BeTrue`, and `Scope.Equal(...)` followed by `BeTrue`, say less than
  `MatchErrorAs` and `ToEqual` do when they fail.

## What changes

Only the `*_test.go` files under `persistence/` change.

- `conflict_test.go`: the four loops become `specs.Table` with the same row names; the `t.Fatal` fixtures
  move out of the spec body; `errors.As` checks use `MatchErrorAs`; the fuzz target reports through a
  go-specs matcher instead of hand-built messages.
- `precondition_test.go` and `scope_test.go` are reviewed and left as they are (see below).

## What does not change, and why

- **Production code.** None of `persistence/*.go` is touched.
- **Single-`It` tests are not split into rows.** Splitting would rename subtests. The `precondition_test.go`
  and `scope_test.go` cases each describe one behavior with several assertions, so they stay as they are.
- **`precondition_test.go` and `scope_test.go`.** Each `Test` holds one `It` that checks one behavior with
  several matchers that already say what they mean (`BeTrue` on predicates, `ToEqual` on values,
  `MatchError`). `Scope.Equal` and `Scope.IsUnscoped` are the methods under test there, so replacing them
  with a struct comparison would stop testing them. Rewriting these would add churn and no signal.
- **The empty malformed message stays a plain `It`.** `specs.Table` panics on an empty row name, and the
  old subtest for `""` has an empty name segment that must stay, so that one case is not a table row.
- **No mocks, no clock.** The package has no dependency to replace and no waits. There is nothing for
  `mock.Controller` or `Eventually` to do, so neither is introduced for show.
- **`persistence/conformance`** has no unit tests of its own and is out of this change.

## Constraints

- Every case and invariant stays. Subtest names stay. The case count per `Test` does not drop (298
  `--- PASS` before).
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. The runner is `go test ./persistence/...`. RED is shown per task by a deliberate production
  mutation that the rewritten cases must catch, then reverted.
- Delivery: push to `test/205-migrate-persistence` and update the #222 description.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3). Route: inline. Evidence: merge commit `f067099`,
      `go mod tidy` clean in every module, build clean, 298 `--- PASS`, coverage 96.6%.
- [x] T2 `conflict_test.go`: the four loops become `specs.Table`, `errors.As` becomes `MatchErrorAs`,
      scope checks use `ToEqual`, `t.Fatal` fixtures move into `mustTenantScopeFor`, and the fuzz target
      reports through go-specs matchers. Route: inline (one already-understood file). Evidence: `5da1f90`.
      RED mutation: `ConflictError.Scope()` collapsed every scope to `Unscoped()`, and
      `TestConflictErrorScopeAccessor` failed with `expected unscoped to equal tenant:tenant-a`. The mutation
      was reverted.
- [x] T3 `precondition_test.go` and `scope_test.go`: reviewed; `precondition_test.go` is unchanged for the
      reason above. Moving the conflict checks onto `ToEqual` stopped `Scope.Equal` from being exercised for
      two unscoped scopes (coverage fell from 96.6% to 95.7%), so `scope_test.go` now asserts that case where
      `Equal` is the unit under test. Route: inline. Evidence: `d6c9cd6`. RED mutation: `Scope.Equal`
      returned false for two unscoped scopes, and `TestScopeTwoTenantScopesWithSameIDAreEqual` failed with
      `expected true, got false (bool)`. Reverted.
- [x] T4 Verify and deliver: build, vet, lint, gofmt, `-count=5`, coverage, push and PR body. Route: inline.
      Evidence: 298 `--- PASS` before and after with identical names. Coverage `persistence` 96.6% before and
      after. `-count=5`, vet and gofmt are clean (golangci-lint reports 0 issues).

## Follow-up spec

None. The package has no real-time waits or external dependencies.

## Progress

- 2026-09-30: T1 done. Engram mirror: pending.
