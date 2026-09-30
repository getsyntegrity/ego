# projectionrunner tests on go-specs v0.3.3 (#236)

## Problem

PR #236 (`test/205-migrate-projectionrunner`) moved `internal/projectionrunner/runner_test.go` to go-specs
v0.3.1 and replaced its 27 fixed waits with a hand-written `waitUntil` poll. It still mocks every
collaborator with testify/mockery (`mocksoffsetstore.OffsetStore`, `mockencryption.Encryptor`,
`mockadapter.EventAdapter`, the events store mocks), and it writes near-identical cases one by one.

The unit-test conventions (#219) now require go-specs `mock.Controller` for every replaced dependency and
recommend `specs.Table`, `ctx.Eventually` and the v0.3.2 matchers. go-specs v0.3.3 is on `develop` since #242.

## What changes

Only `internal/projectionrunner/*_test.go`. Production code is untouched in this spec.

- testify mocks are replaced with small typed `mock.Controller` adapters (a new `mocks_test.go` in the
  package). The in-memory `testkit` stores stay: they are fakes with real behavior, not external resources.
- `waitUntil`, `awaitStopped`, `awaitCalls`, `awaitOffset` and `awaitFailure` become `ctx.Eventually` calls
  (or thin helpers over it), so a timeout reports the last observed value.
- Cases that differ only by the stubbed collaborator or the expected offset become `specs.Table` rows.
- Assertions use the specific matchers (`HaveLen`, `Project`, `MatchError`, ...) where they say more.

`TestProjectionRunnerStaysRuntimeNeutral` runs `go list` in a subprocess, so it is not a unit test and stays
as it is (out of phase, testify kept for it only).

## Why this scope

The runner has no clock seam: `*ticker.Ticker` (`runner.go:106`), the ping retrier (`runner.go:244-252`) and
the `time.After` backoff (`runner.go:462`) use real time. Driving them with `specs.NewManualClock()` needs a
production option, which is a behavior change and does not belong in a test-only migration PR. The
alternative, adding that seam here, was rejected because it would mix a production change into #236 and block
it on a separate review. That work is the follow-up below.

## Constraints

- Every case and invariant the current test asserts is kept; case counts per test do not drop.
- No force-push: `develop` is merged into the branch, not rebased.
- No `-race`, no workbench.
- TDD: strict (user CLAUDE.md). Runner: `go test ./internal/projectionrunner/`. For a test rewrite, RED
  is shown by a deliberate mutation per task that the rewritten cases must catch, then reverted.
- Delivery strategy: `ask-on-risk`. The diff is one test file plus one adapter file.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3) into the branch. Route: inline. Evidence: merge commit
      `34cac68`, `go.mod` pins v0.3.3.
- [x] T2 Replace testify mocks with `mock.Controller` adapters in `mocks_test.go`, and port every
      `EXPECT()`/`.Run(...)`/`AssertNotCalled` to `Expect/Return/Do/Never`. Route: delegated writer (one
      non-trivial file of ~2000 lines plus a new file). Evidence: `288ae4a`. No testify `mock` or
      `ego/mocks/*` import left. RED: the decrypt stub was changed to expect `key-2`, and the case failed
      with `mock: unexpected call Decrypt(... "key-1") ... argument 3`. The change was reverted.
- [x] T3 Replace the `await*` helpers with `ctx.Eventually`. Route: same writer. Evidence: `6f60262`. RED:
      `committedAt(ts+1)` failed with `Eventually: timed out after 10s (4470 attempts) ... Value: expected
      1790800783 to equal 1790800784`. The change was reverted.
- [x] T4 Fold near-identical cases into `specs.Table` and use specific matchers. Route: same writer.
      Evidence: `90b33e6`. There are 58 `--- PASS` before T2 and 58 after, with identical subtest names.
      `-count=5` is green. The constructor-guard and ping-failure pairs stay separate `It` blocks, because
      folding them needed per-row branches.
- [ ] T5 Verify and deliver: `go vet`, `golangci-lint`, coverage not below 92.6%, push to
      `test/205-migrate-projectionrunner`, update the PR description. Route: inline.

## Follow-up spec (not in this document)

`projectionrunner-clock-seam`: add a clock/ticker option to `internal/projectionrunner` (ticker, ping
retrier, store retry backoff, `time.Now()` for lag) so its tests drive time with `specs.NewManualClock()`,
remove the ~14 s of real ping backoff, and stop synchronizing on real time.

## Progress

- 2026-09-30: T1 done. Engram mirror: pending (Engram reports several active sessions for the project).
- 2026-09-30: T2-T4 done by the delegated writer. Parent spot check with `go test -count=1 -cover`: pass
  in 16.8 s, coverage 93.3% (92.6% before). `go vet`, `golangci-lint` and `gofmt` are clean. RDD is off
  (global).
- Next: T5 (push and PR description), then the follow-up `projectionrunner-clock-seam`.
