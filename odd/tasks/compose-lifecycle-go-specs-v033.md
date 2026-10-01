# compose/internal/lifecycle tests on go-specs v0.3.3 (#231)

## Problem

PR #231 (branch `test/205-migrate-compose-lifecycle`) moved the unit tests of `compose/internal/lifecycle`
to go-specs v0.3.1. After #242 and the precedent of #229 they still have these problems:

- `recorder` keeps its ordered call log in a hand-written mutex plus slice.
- `cleanupProbe` is a hand-written recorder of the cleanup context, and its `check` calls `t.Fatal`, which
  bypasses the spec. `mustNew` and `mustStart` do the same with `t.Fatalf`.
- Two tests collect "missing" errors with a manual loop, and `errText` exists only to feed `Contain`.
- Near-identical cases (`missing name`/`missing start`, one case per step name, the single-use cases) are
  written as `for` loops around `s.It`.
- `len(x) ToEqual 0` and `Not(BeNil)` on a context error say less than a specific matcher.

## What changes

Only `compose/internal/lifecycle/lifecycle_test.go` changes.

- The `recorder` log becomes a `mock.Spy`. The fake stays, because `failStart`, `failStop` and `failRel`
  are real behavior that decides which step fails: they are inputs of the test, not a stand-in for an
  external dependency.
- `cleanupProbe` becomes a `mock.Controller` method that must be called exactly once, with a
  `mock.Captor` keeping a snapshot of the cleanup context. The checks use `specs.All` and `specs.Project`,
  so a failure names the field (`Err`, `Value`, `HasDeadline`, time left until the deadline).
- `mustNew`, `mustStart` and the new `mustStop` take `*specs.Context` and assert through matchers.
- Tables: `missing name`/`missing start`, the per-step Start and Stop failure cases and the single-use cases.
- `specs.All(MatchError...)` replaces the two "collect the missing errors" loops. `Project` with
  `Contain` replaces `errText`. `BeEmpty` replaces `len(...) ToEqual 0`.
- `defer cancel()` becomes `ctx.Cleanup(cancel)`. The "context is released" check now expects
  `context.Canceled` instead of any error.

## What does not change, and why

- **Production code** (`lifecycle.go`).
- **Full-sequence `ToEqual` on the call log.** A `mock.Controller` with `InOrder` only compares the first
  call of each expectation, which is weaker than comparing the whole sequence, so the log is checked as a
  slice.
- **`TestStart_PanickingStepRollsBackReleasesAndFails`'s `recover`.** go-specs has no panic matcher.
- **`TestStartAndStop_AreSerialized`'s goroutines.** They only drive concurrency, never touch `ctx`, and are
  joined through channels. They use no real waits.
- There is no `time.Sleep` or `pause.For` in the file, so there is nothing to move to `Eventually`.

## Constraints

- Every case and invariant stays. Subtest names stay. The count of `--- PASS` lines is 54 before and after.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. The test runner is `go test ./compose/internal/lifecycle/`. RED is shown by a deliberate
  production mutation that the rewritten cases catch, then reverted.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge commit `731bb07`, clean build, no
      conflicts, no module needed `go mod tidy` changes.
- [x] T2 Rewrite `lifecycle_test.go`: spy log, mock-based cleanup probe, matchers, tables, `ctx.Cleanup`.
      Route: delegated writer (single file, one writer). Evidence: `cd53ffc`. RED mutations caught:
      cleanup in forward order (`expected [start probe ... stop probe stop runtime release] to equal
      [... stop runtime stop probe release]`); cleanup context inheriting the caller's cancellation
      (`Err: expected nil, got context canceled`); cleanup context without a deadline
      (`HasDeadline: expected true, got false`); Release error dropped from the join (`... to match publisher
      close failed ... errors.Is(actual, expected) is false`).
- [x] T3 Verify and deliver. Route: inline. Evidence: 54 `--- PASS` before and after with identical names;
      coverage 94.1% before and after; `go build ./...`, `go vet ./compose/...`, `gofmt -l` and
      `golangci-lint run` clean; `go test -count=5` green. Then push and update the PR description.

## Follow-up spec (not in this document)

None needed: no production change is required.

## Progress

- 2026-09-30: T1, T2 and T3 checks done. Engram mirror: pending.
