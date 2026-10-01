# internal utility tests on go-specs v0.3.3 (#220)

## Problem

PR #220 (branch `test/205-migrate-internal-utils`) moved the unit tests of `internal/queue`,
`internal/runner`, `internal/syncmap` and `internal/ticker` to go-specs. The conventions in
`docs/testing/go-specs.md` (#203) ask for more:

- `internal/runner/runner_test.go` records calls with hand-written `called*` booleans and compares
  `errText(err)` strings. The conventions ask for `mock.Controller` for replaced dependencies.
- `internal/syncmap/map_test.go` checks `Range` and `Values` with `len`, `slices.Contains` and
  sort + `ToEqual`.
- `internal/ticker/ticker_test.go` blocks on `<-ticker.Ticks`: a ticker that never ticks hangs the case.

## What changes

Only `*_test.go` files change.

- `runner_test.go`: each runner step is a method of one `mock.Controller`, reached through a small `steps`
  adapter. "Not called" becomes `Expect().Never()` and "called" becomes the default `Times(1)`. The
  `AddContextRunner*` cases also check the context the chain passes (`mock.Equal(bg)` for `WithContext`).
  Errors are checked with `MatchError` (`errors.Is`), plus the joined message for the ReturnAll order.
  The `errText` helper is gone.
- `map_test.go`: `ContainTheSameElementsAs` replaces `len` + `slices.Contains` and sort + `ToEqual`. It is
  also stricter: a key visited twice now fails.
- `ticker_test.go`: the five ticks are collected with `ctx.Eventually` (5 s timeout), the ticker is stopped
  with `ctx.Cleanup`, and a new check asserts `Ticking()` is true after `Start`.

## What does not change, and why

- **Production code.** None changes.
- **`queue_test.go`.** It has no dependency to replace, no waits and no collections: each case is a short
  sequence of `Length`/`IsEmpty` checks that already use the exact matchers. Tables or mocks would add
  noise.
- **`syncmap` cases as single `It`s.** Each tests a different operation, so `specs.Table` does not fit.
- **The ticker's real-time wait.** `Ticker` builds its own `time.NewTicker`, so the case still takes about
  0.5 s (five 100 ms ticks). A manual clock needs a production change; see the follow-up.

## Constraints

- Every case stays, subtest names stay. 36 `--- PASS` before and after, names identical.
- No `-race`, no workbench. Strict TDD: RED by a deliberate production mutation per task, then reverted.
  Runner: `go test ./internal/queue ./internal/runner ./internal/syncmap ./internal/ticker`.
- No force-push: `develop` is merged into the branch.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3). Route: inline. Evidence: merge commit `7407026`, no
      conflicts, `go build ./...` clean.
- [x] T2 `runner_test.go` on `mock.Controller`. Route: inline (one file). Evidence: `3eec3e6`. RED: the
      FailFast guard removed from `AddRunner`/`AddRunners`, failing with `mock: forbidden call fn2():
      expectation fn2() declared at runner_test.go:58 says never`.
- [x] T3 `map_test.go` collection matchers. Route: inline. Evidence: `9ff62e2`. RED: `Range` calls `f` twice
      and `Values` appends twice; failure `expected [1 1 2 2] to contain the same elements as [1 2]`.
- [x] T4 `ticker_test.go` with `Eventually` and cleanup. Route: inline. Evidence: `c43fb06`. RED: the loop
  reads a channel that never fires; failure `Eventually: timed out after 5.0s ... expected 0 to equal 5`,
  where the old test hung forever. A first draft polled non-blockingly and failed on the unmutated code,
  because the ticker drops ticks nobody is waiting for; each poll now waits for one tick.
- [x] T5 Verify and deliver. Route: inline. Evidence: vet, golangci-lint (0 issues), gofmt and
  `go test -count=5` clean. Coverage unchanged: queue 93.9%, runner 95.6%, syncmap 100.0%, ticker 86.4%.

## Follow-up spec (not in this document)

`ticker-clock`: let `ticker.New` accept a clock (production change), so the tick count can be driven by
`specs.NewManualClock()` and the case runs without real time.

## Progress

- 2026-09-30: T1-T5 done. Engram mirror: not written from this worker.
