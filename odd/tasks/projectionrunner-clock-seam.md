# projectionrunner clock seam

Follow-up of `projectionrunner-go-specs-v033.md` (#236). This is a stacked PR on
`test/205-migrate-projectionrunner`.

## Problem

`internal/projectionrunner.Runner` reads real time in five places, and none of them can be replaced:

1. The pull loop ticks on `internal/ticker.Ticker`, which wraps `time.NewTicker` (`runner.go:277`, `:340`).
2. `Start` pings both stores through `flowchartsman/retry` with a 1 s delay, and it tries up to 5 times
   (`runner.go:244-258`).
3. Handler recovery (`RetryAndFail`/`RetryAndSkip`) retries through a second `flowchartsman/retry` with
   `RetryDelay` (`runner.go:215`, `:743`).
4. After a store failure, the pass loop waits `storeRetryDelay(n)` with `time.After` (`runner.go:462`).
5. `time.Now()` stamps committed offsets (`runner.go:801`) and computes the lag metric (`runner.go:611`).

Because of this, its unit tests synchronize on real time. The two "max retry to ping" cases alone sleep about
14 s, and the store backoff and lag cases depend on the wall clock.

## What changes

The runner gets one injectable source of time. It is a small package-private interface:

```go
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) timer
}

type timer interface {
	C() <-chan time.Time
	Stop() bool
}
```

The interface is set with a new option, `WithClock(clock)`. The default is the real clock, so production
behavior does not change. All five sites above go through it:

- The pull loop waits on a timer re-armed every `pullInterval`.
- The ping and recovery retries become a small clock-driven retry loop with the same attempt count and delays.
- The store backoff and both `Now()` calls use the clock.

In tests, a tiny adapter over `specs.NewManualClock()` satisfies `clock`. The ping-retry, store-backoff and
lag cases then advance time explicitly instead of sleeping.

`internal/projectionrunner` is internal, so this does not change the public API. `WithClock` takes the
package-private interface. A caller outside the package cannot construct one, and that is intended: the
option exists for the tests.

## Judgement calls

- **Timer re-arm instead of `time.Ticker`.** A ticker keeps a fixed rate and drops ticks when a pass is
  slow. A re-armed timer waits `pullInterval` after each pass. Both produce at most one pass per interval,
  and the difference is only visible when a pass takes longer than the interval. We still accept it because
  a manual clock has no ticker, and a ticker built on a timer would be the same loop in another file. The
  alternative was to give `internal/ticker` a clock, which would keep a second abstraction just for this. We
  rejected it for that reason.
- **Replacing `flowchartsman/retry` in the runner.** The library sleeps internally, so it cannot follow a
  manual clock. Both call sites use an initial delay equal to the maximum delay, so their schedule is a
  constant delay. The writer must check whether the library adds jitter. If it does, the difference is
  recorded in the PR. The retry count and the error returned after the last attempt must stay the same.
- **Only the slow and wall-clock cases move to the manual clock.** The other cases use
  `WithPullInterval(time.Millisecond)` with `ctx.Eventually`. Moving each pull to an explicit `Advance`
  would rewrite most of `TestRunner` for little gain, so it is left out of this spec.

## Constraints

- There is no behavior change with the default clock. The existing tests pass unchanged, with the same case
  count (58 `--- PASS`).
- No `-race`, no workbench, no force-push.
- TDD: strict (user CLAUDE.md). The runner is `go test ./internal/projectionrunner/`. RED must be observed
  before each production change.
- Delivery: a stacked PR with base `test/205-migrate-projectionrunner`, strategy `ask-on-risk`.

## Tasks

- [x] T1 Add the `clock`/`timer` interfaces, the real clock, and `WithClock`. The default is set in `New`.
      Route: delegated writer. Evidence: `72782b1`. RED was a compile failure (`undefined: clock`,
      `undefined: WithClock`). GREEN: `TestWithClock`, 4 cases.
- [x] T2 Route the five sites through the clock: the pull loop, the ping retry, the recovery retry, the
      store backoff and `Now()`. Route: the same writer. Evidence: `b9e7a89` (`retry.go`/`retryOn`). RED:
      six manual-clock tests timed out because no timer was ever armed. GREEN: all six pass, and they stayed
      clean over 20 repeats.
- [x] T3 Move the two ping-retry cases, the store-backoff cases and the lag-metric cases to the manual clock.
      Route: the same writer. Evidence: `fcb9f73`. The package now takes 0.92 s instead of 16.7 s.
      `--- PASS` goes from 58 to 76: the 58 existing cases plus 18 new ones. Coverage is 93.2%. Two
      mutations were caught: changing the ping delay from 1 s to 2 s, and changing the `storeRetryDelay`
      shift. A tidy follow-up in the parent, `chore(deps)`, makes `flowchartsman/retry` indirect.
- [ ] T4 Verify and deliver: `go vet`, `golangci-lint`, coverage not below 93%, `-count=5`, the native
      assessment and, if `high`, an independent verifier. Then push and open the stacked PR. Route: inline
      (parent).

## Follow-up spec (not in this document)

`projectionrunner-manual-pulls`: drive the remaining pull-interval cases with `Advance` instead of a 1 ms
real interval, if the team wants the whole package off real time.

## Progress

- 2026-09-30: document created on branch `refactor/projectionrunner-clock-seam`, on top of `05bff67` (PR
  #236 head). Engram mirror: pending.
- 2026-09-30: T1-T3 are done. This corrects the judgement call above: `flowchartsman/retry` is NOT constant
  when initial == max. It waits `jitter(max/2) + initial`, which is about 1.5x to 2x the delay. `retryOn`
  keeps the attempts, the defaults, the last-error return and the context behavior, and it drops the jitter
  (a constant delay). The pull loop re-arms its timer after every pass, including a pass triggered by a
  nudge. After a store failure, the next pull therefore comes at backoff + interval. The test-only
  `go.opentelemetry.io/otel/sdk/metric` require was added to read the lag gauge.
- The native assessment returned `high`, so an independent verifier is running. The PR opens as a draft
  until it reports.
