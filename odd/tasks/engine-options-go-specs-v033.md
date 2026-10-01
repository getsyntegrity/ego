# engine option, runtime compatibility and logger tests on go-specs v0.3.3 (#240)

## Problem

PR #240 (branch `test/205-migrate-engine-options`) moved `engine/option_test.go`,
`engine/runtime_compat_test.go` and `engine/logger_test.go` to go-specs v0.3.1. Four things were still
weak or off-convention:

- Many assertions were written as `ctx.Expect(x == y).To(specs.BeTrue())` or `len(x) ToEqual n`. On failure
  they say only "expected true", not which value was wrong.
- Six `TestConfigGoaktOptions*` tests and the logger wiring test still used `testing.T`, `testify/require`
  and `assert.Same`, so their failures bypassed the spec.
- The ten-sentinel check in `TestRuntimeSentinelsAreTheSameValues` was a `for` loop of `s.It`.
- `buildActorSystem` called `require.NoError` and `t.Cleanup` directly.

## What changes

Only `*_test.go` files in `engine/` change.

- A helper `beTheSame(want)` (in `option_test.go`) is a matcher for "the very same instance". It replaces
  `a == b` with `BeTrue`. `ToEqual` is deep equality and would accept a second, equal object, which is not
  what "stores the given store" means. `beNilInterface()` replaces `iface == nil`, because `specs.BeNil` also
  accepts a typed nil inside a non-nil interface, and the typed-nil tests exist to tell those apart.
- Collection checks use `HaveLen`, `BeEmpty` and `Not(BeEmpty())`, `EveryElement` and `BeNil` for maps and pointers.
- The six `TestConfigGoaktOptions*` tests and `TestGoaktOptionsCarryTheResolvedLogger` become specs.
  `buildActorSystem` takes a `*specs.Context` and registers `ctx.Cleanup`.
- The ten sentinels are a `specs.Table` (same row names). The three "falls back to the default logger" cases
  are a table too.
- `TestRuntimeMovedTypesAreAliases` compares the two type lists as a whole, so a failure shows both lists.

## What does not change, and why

- **Production code.** Untouched.
- **Test function names.** All 85 existing passing names stay. The six `TestConfigGoaktOptions*` tests had no
  subtests; as specs they gain one each, so the count is 91 (additions only).
- **`TestGoaktOptionsCarryTheResolvedLogger` uses `Describe(t, "", ...)`.** An empty name adds no subtest
  segment, so its five subtest names stay exactly as before. The convention asks for a real description;
  keeping the names was the stronger constraint. This is a judgement call, the alternative was renaming.
- **The tenant resolver stubs** (`stubTenantResolver`, `countingTenantResolver`, ...). They are inputs with
  real behavior and are shared with `engine_test.go`; they do not stand in for an external dependency.
- **`optRecovered`.** go-specs v0.3.3 has no panic matcher, so it stays.
- **One-`It` tests.** Folding them into a table would rename their top-level `Test`.

## Constraints

- Every case and invariant stays; no `-race`; no workbench; no force-push (`develop` was merged).
- Strict TDD, runner `go test ./engine`. RED is a deliberate production mutation per task, then reverted.
- Delivery: push to `test/205-migrate-engine-options` and update the #240 description.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge commit `9ea43b4`; clean build, no
      `go.mod`/`go.sum` change from `go mod tidy`.
- [x] T2 `option_test.go`: identity and collection matchers, specs for the GoaktOptions tests. Route:
      inline (one file). Evidence: `a7f935e`. RED: removing `reflect.Func` from `isNilResolver` failed
      `TestOptionWithTenantResolverFuncTypedNil` (`expected <nil> to satisfy "a nil interface"`).
- [x] T3 `runtime_compat_test.go`: sentinel table and whole-list type comparison. Route: inline. Evidence:
      `c73df68`. RED: giving `ErrNotACommand` its own `errors.New` failed the `ErrNotACommand` row
      (`expected x to satisfy "the same instance"`).
- [x] T4 `logger_test.go`: matchers, logger wiring as a spec with a fallback table. Route: inline. Evidence:
      `26ab793`. RED: `ResolveLogger` returning `DiscardLogger` failed 8 cases.
- [x] T5 Verify and deliver. Evidence: 85 `--- PASS` before, 91 after (6 added, none lost). Coverage of
      `engine` 94.8% before and after. `go build ./...`, `go vet`, `golangci-lint run ./engine/...` (0 issues),
      `gofmt -l` and `go test -count=5 ./engine` are clean.

## Follow-up spec (not in this document)

`engine-actor-system-unit-boundary`: `buildActorSystem` and the logger wiring tests start a real in-process
GoAkt actor system. The conventions list "actor system" among external resources, so these cases belong to
the component lane. Moving them needs a decision about where that lane lives, not a rewrite of the assertions.

## Progress

- 2026-09-30: T1-T5 done. Engram mirror: pending.
