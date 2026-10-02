# Deterministic ticker tests (follow-up of #220)

## Problem

After #220, `internal/ticker/ticker_test.go` still waited on real ticks. The case "stops ticking after five
ticks and Stop" used a 100 ms interval and polled `Eventually` with real time, so it took about 0.5 s and
depended on the scheduler. `ticker.New` built a `time.Ticker` internally, so a test had no way to drive it.

## What changes

`internal/ticker/ticker.go` gets a small unexported seam. A `tickSource` is a function that, given an
interval, returns a channel of ticks and a function that releases it. `realSource` wraps `time.NewTicker`.
`New(intervals)` is unchanged for callers: it calls the new unexported `newWithSource(intervals, realSource)`.
The ticking loop reads from the source's channel instead of from a `time.Ticker` it owns.

`ticker_test.go` passes a hand-driven source. Each attempt of the poll fires one source tick, lets a
receiver goroutine forward it, and counts at most one delivered tick. The poll runs on go-specs'
`specs.NewManualClock()` (`WithClock`), which the callback advances, so no real time passes. The test
name and subtest name are unchanged. Two cases were added so coverage does not drop: the panic for a
non-positive interval, and Start/Stop through `New` on the real clock with a one-hour interval (no tick
is awaited).

## What does not change, and why

- **The public API.** `New`, `Start`, `Stop`, `Ticking` and the exported `Ticks` field keep their
  signatures. Callers (only `internal/projectionrunner/runner.go`) build unchanged.
- **go-specs' `ManualClock` as the production seam.** It exposes `Now` and `NewTimer`, not a ticker, and its
  timer type is go-specs' own, so it cannot satisfy a production interface without importing go-specs into
  non-test code. A tiny internal function type avoids that. The manual clock still drives the poll.
- **The back-pressure rule.** The loop still drops a tick nobody is waiting for. The test accounts for this
  by firing one tick per attempt and counting deliveries, instead of assuming every tick arrives.

## Constraints

- Strict TDD. Runner: `go test ./internal/ticker/`. No `-race`, no workbench, no DB or network.
- Production change only in `internal/ticker/ticker.go`, unexported.

## Tasks

- [x] T1 Add the `tickSource` seam and rewrite the test on it. Route: inline (two small files, already
  understood). RED: the test first failed to compile (`undefined: newWithSource`). After the seam, a
  mutation that stops the loop forwarding ticks failed with `Eventually: timed out after 1m0s (60000
  attempts) ... expected 0 to equal 5`, then was reverted. GREEN: `go test -count=5` passes.
  Commit: test(ticker): drive ticks through an internal tick source instead of real time.
- [x] T2 Keep coverage. Route: inline. The seam moved `New`'s guard and the real wiring out of the old
  real-time path, so coverage fell from 95.5% to 84.0%. The two added cases bring it to 100.0%. Same commit as T1.
- [x] T3 Check callers. Route: inline. `go build ./...` and `go vet ./internal/ticker/ ./internal/projectionrunner/` pass.

## Follow-up

None needed. The projectionrunner tests are slower (about 40 s) but do not depend on this seam.

## Progress

All tasks done. Checks: gofmt clean, `golangci-lint run ./internal/ticker/...` 0 issues, coverage
95.5% -> 100.0%, test time 0.5 s -> 0.003 s.
