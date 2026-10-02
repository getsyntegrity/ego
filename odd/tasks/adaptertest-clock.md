# adaptertest-clock

Follow-up to `port-adapter-go-specs-v033` (origin PR #229).

## Problem

The conformance harness `port/adapter/adaptertest` checks AT-4 ("release honors its deadline while the backend is stalled") by giving release a 200 ms deadline and then waiting `stallDeadline + grace` (1.2 s) of real time for a release that never returns. So `TestCapture_CloseIgnoringTheDeadlineFailsAT4` took 1.20 s, and any adapter conformance test depends on wall-clock time.

## What changes

`port/adapter/adaptertest/adaptertest.go` gets a small unexported seam: a `clock` interface (`Now`, `NewTimer`) and a package variable `clk` that defaults to `realClock{}`. `bounded` (the grace wait) and the AT-4 elapsed-time log read time through `clk`. Nothing exported changes: `Run`, `Capture` and `Target` still take `*testing.T`.

The AT-4 self-check moves from `adaptertest_test.go` (external package, cannot see `clk`) to a new `clock_internal_test.go` in package `adaptertest`. It swaps `clk` for `instantClock`, which fires only the `stallDeadline+grace` timer immediately, and asserts the check still fails with "deadline" while taking under half a second of real time. Two small tests cover `realClock` itself.

## What does not change, and why

- Public API: the `api` CI job runs apidiff; the seam is unexported.
- Standard-library-only rule (`architecture_test.go`): the seam imports only `time`, not go-specs.
- The request context still uses `context.WithTimeout` (real 200 ms): it is never waited on in the stalled case, and replacing it with a clock-driven cancel would change the `DeadlineExceeded` error adapters see. Rejected for that reason.
- `publisher/websocket` conformance: untouched, it runs against a live backend and is not part of this spec.

## Constraints

Strict TDD (go-specs v0.3.3, `go test`), no `-race`, no workbench, unit tests touch no network.

## Tasks

- [x] T1 Clock seam plus the instant-clock AT-4 test. Route: inline (one production file, one test file; small and understood). RED: the new test first failed to compile (`undefined: clk, clock, realClock`); after GREEN, a mutation making `bounded` use `time.NewTimer` directly failed the test (`expected false to equal true` on the elapsed-time assertion, 1.21 s) and was reverted. GREEN: the test passes in 0.00 s. Also removed the now-unused `owned.closeBlock` field. Commit: see git log (`test(adaptertest): add an internal clock seam ...`).
- [x] T2 Verify and deliver. Route: inline. Evidence: `go build ./...`, `go vet`, `golangci-lint run ./port/adapter/...` (0 issues), `gofmt -l` clean, `go test -count=5` on `port/adapter/...`, `testkit` and `port/...` green. Coverage `adaptertest` 94.0% before, 94.1% after; `port/adapter` 95.0% unchanged. AT-4 test 1.20 s before, 0.00 s after.

## Follow-up

None required. Optional: drive the request context through `clk` as well, if a conformance test ever needs to wait on the 200 ms deadline.

## Progress

- 2026-09-30: T1 and T2 done; Engram mirror not written (tooling-limited writer).
