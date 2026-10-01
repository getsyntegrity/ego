# compose spec and adapters tests on go-specs v0.3.3 (#232)

## Problem

PR #232 (branch `test/205-migrate-compose-spec`) moved the unit tests of `compose/spec_test.go` and
`compose/internal/adapters/adapters_test.go` to go-specs v0.3.1. They still have these problems:

- `adapters_test.go` checks the start-and-probe order through hand-written recording fakes that append to a
  shared `recorder.calls` slice, and compares that slice by hand.
- `spec_test.go` asserts through `requireOneProblem`, `requireV8` and `problems`, which call `t.Fatalf` and
  `t.Errorf` and so bypass the spec. They also use `strings.Contains` loops, `len(x) ToEqual`, and a package
  `atomic.Int32` to count `Describe` calls.
- Many cases are hand-rolled `for _, c := range cases { s.It(...) }` loops instead of `specs.Table`.

The #219 conventions (`docs/testing/go-specs.md`) require `mock.Controller` for replaced dependencies and
recommend `specs.Table` and the specific matchers.

## What changes

Only `compose/spec_test.go` and `compose/internal/adapters/adapters_test.go` change.

- Adapters: `starter`, `pinger` and `both` become mock adapters that forward to a `mock.Controller`, passing
  their own name. Each case declares the calls it expects and `ctrl.InOrder` requires their order. Calls that a
  case does not declare (anything after the first failure) are reported by the controller as unexpected.
- Spec: `problems` takes the `*specs.Context`; a malformed error fails through `Satisfy` and `MatchErrorAs`.
  `requireOneProblem` and `requireV8` use `HaveElementsInOrder` with a `problem(rule, field)` matcher built from
  `Project`, and `ContainAllOf` for the message. `ctx.Cleanup` replaces `ctx.T.Cleanup`.
- Tables: minimal specs, V2, V4, V5, V6 nil publisher, V7 accepted values, V8b, and the start-failure family.
- `describeCalls` (an atomic counter) becomes a `mock.Controller` expectation `Describe().Never()`.

## What does not change, and why

- **Production code** (`compose/spec.go`, `compose/internal/adapters/adapters.go`).
- **The `fake*`, `declared*` and `described` types in `spec_test.go`.** They are the inputs of the validation
  rules (values with or without a descriptor or optional methods), not recorders standing in for a dependency.
  The `Describe` descriptor on the adapters' `both` mock is a fixed fixture for the same reason.
- **The V4 "offset store not required", V6 duplicate IDs and V8 single-case tests** stay as separate cases:
  each has its own setup, and a one-row table adds nothing.
- **`describeCtrl` is a package variable.** The typed-nil publisher has no state, since its `Describe` runs on a
  nil receiver, so the case sets the variable and restores it with `ctx.Cleanup`. A mutation that calls
  `Describe` on the nil value is caught (RED below). Rejected alternative: a package `mock.Spy`, which is not
  bound to the case and so would not verify at case end.

## Constraints

- Every case and invariant stays, subtest names stay, and the case count per `Test` does not drop.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- Strict TDD: the runner is `go test ./compose/...`. RED is a deliberate production mutation caught by the new
  cases, then reverted.
- About 400 changed lines per task is only a planning heuristic.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge commit `42392ab`, clean merge, no
      conflicts, `go build ./...` clean.
- [x] T2 `adapters_test.go`: mock adapters with `mock.Controller` and `InOrder`, `specs.Table` for the failures,
      `ContainAllOf` for the message. Route: inline (one file, already understood). Evidence: `625c7e5`. RED:
      `StartAndProbe` continuing after a Start error failed with `mock: unexpected call Start(..., "c")` and a
      nil error; swapping Ping before Start failed with `mock: order violation: expected Start(... "a") before
      Ping(... "a")`.
- [x] T3 `spec_test.go`: matcher-based helpers, tables, `ctx.Cleanup`, and the `Describe` controller. Route:
      inline. Evidence: `b5bb730`. No `t.Fatalf` is left. RED: calling `Describe` on a typed-nil publisher failed
      with `mock: forbidden call Describe(): expectation Describe() ... says never`; reporting V7 as V6 failed
      three tests with `Rule: expected V6 to equal V7`.
- [x] T4 Verify and deliver: vet, lint, gofmt, `-count=5`, coverage, push, PR body. Route: inline. Evidence: 93
      `--- PASS` before and after with identical names. Coverage `compose` 97.9% and `compose/internal/adapters`
      100.0%, both unchanged. `go vet`, `golangci-lint` and `gofmt -l` are clean.

## Follow-up spec (not in this document)

None needed: no production change was required to remove real time or other external resources.

## Progress

- 2026-09-30: T1 to T4 done. Engram mirror: pending.
