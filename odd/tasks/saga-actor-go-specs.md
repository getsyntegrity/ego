# saga actor tests on go-specs v0.3.3 (follow-up of #237)

## Problem

PR #237 moved most of `internal/engine/saga` to go-specs but left `TestSagaActor` (32 `t.Run` cases) and
`TestSagaFailsClosed` (1 case) in `saga_test.go` on testify `assert`/`require`. They start a real in-process
goakt actor system, which is why #237 stopped there. The user's rule is that every test uses go-specs
`Describe`/`It`, go-specs mocks and the v0.3.3 features, so these 33 cases were the last testify holdout in
the package.

Reading them also showed three habits the conventions replace:

- Most cases waited for a callback with a buffered channel and a `select` on `time.After`, or checked "the
  actor is still alive" with `require.Never`. A failed `require` left the actor system running.
- Every case repeated about 20 lines to build the store, the stream and the actor system.
- `consumeEvents: HandleEvent error is logged and skipped` counted calls in a plain `int` that the saga
  goroutine wrote and the test goroutine did not read, which is only safe by luck.

## What changes

Only `internal/engine/saga/saga_test.go` changes. Production code does not.

- `TestSagaActor` and `TestSagaFailsClosed` keep their names and every case name; each now holds one
  `specs.Describe`. The case names are the last segment of the subtest path, and `go test -v` prints the same
  92 `--- PASS` leaf names before and after.
- A small rig (`newSagaRig`, `spawnSaga`, `spawnReplyTarget`, `newTestkitStore`) starts the real actor system
  and registers its shutdown with `ctx.Cleanup`, so a failed assertion no longer leaks a running system. It
  reuses the `sagaStoreMock` adapter over `mock.Controller` that #237 introduced.
- Waiting for a callback is `ctx.Eventually` on an atomic counter. "Must not happen" and "must stay alive"
  are `ctx.Consistently`. Cases that only checked survival now first wait for the behavior callback that
  proves the event was processed, so the survival window starts after the work, not before it.
- `specs.Table` replaces near-identical cases: the five `PreStart` failures, the five commands that end in
  `HandleError` (a missing target or an error reply, with the failing and the completing outcome), and the
  two `HandleResult` outcomes.
- `assert.AnError` becomes a local sentinel; `require.NotNil` and friends become `Not(BeNil())`, `BeZero()`
  and `ToEqual`. testify is no longer imported anywhere in the package.

## What does not change, and why

- **The real actor system.** The user asked to keep it (no move to a component lane). The cases still start
  goakt and wait on real time.
- **Time spent.** The package still takes about 12 s. The cost is the observation windows of
  `Consistently` ("this must not happen for 500 ms", 2 s for two compensation cases), not sleeps. A negative
  check needs a window, and shortening them would weaken the checks, so they stay as they were.
- **The 5 s default-timeout case.** `sendCommand` hard-codes `5 * time.Second` when a command has no timeout
  and there is no option to change it. Reading the case showed that it never reached that timeout: its target
  `nonexistent-entity` makes `SendSync` fail at once (`0.00s` before and after). It proves a zero timeout
  still dispatches and reports the failure; it does not prove the 5 s value. Exercising it needs a seam, which
  this spec does not add (see the follow-up).
- **Production code and the public API.**

## Constraints

- Every case stays. 92 `--- PASS` lines before and after; leaf names identical.
- Strict TDD, runner `go test ./internal/engine/saga/...`. For test-only work the RED evidence is a
  deliberate production mutation, reverted each time. No `-race`, no workbench.
- Release note: NONE.

## Tasks

- [x] T1 shared rig and the `PreStart` cases. Route: inline (one file, already understood). Evidence:
      `2bcee34`. RED: the Ping error ignored failed `events store ping failure` (`unexpected call
      GetLatestEvent`); the replay upper limit off by one failed `ReplayEvents failure` (`unexpected call
      ReplayEvents(..., 1, 3, 4)`); removing the missing-behavior check crashed the spawn with a nil
      dereference in `recover`.
- [x] T2 `Receive` and `consumeEvents` cases. Route: inline. Evidence: `6eb0aed`. RED: dropping the
      `status == Running` guard failed `sagaTimeoutMsg when not running is no-op` (`expected 2 to equal 1`);
      not skipping own events failed `skips own saga events` (`expected 1 to equal 0`).
- [x] T3 `compensate` and `sendCommand` cases, with two tables. Route: inline. Evidence: `a29b4af`. RED: an
      always-false `SendSync` error branch failed the `HandleError` rows (`expected 0 to be greater than or
      equal to 1`); an always-false `ParseCommandReply` error branch failed the error-reply rows; skipping
      `HandleResult` failed both `HandleResult` rows.
- [x] T4 `TestSagaFailsClosed`, testify removed. Route: inline. Evidence: `2bd982a`. RED: turning the tenant
      gate off in both `eventContext` and `bindOrVerify` failed the case (`expected 1 to equal 0`). Turning
      it off in `eventContext` alone did not fail it, because `bindOrVerify` is an independent second gate.

## Follow-up spec (not in this document)

`saga-command-timeout-seam`: give `sendCommand` an unexported default-timeout seam (a package variable or a
field on `Actor`) so a test can prove the 5 s default and the `compensate` default without waiting, and so
the observation windows can be driven by a manual clock. This needs a production change, so it is its own
spec.

## Progress

- 2026-09-30: T1-T4 done. Checks: `go build ./...`, `go vet`, `golangci-lint run ./internal/engine/saga/`
  (0 issues) and `gofmt -l` clean; `go test -count=5` passes (about 12 s per run). Coverage of
  `internal/engine/saga` is 89.3% before and after (three runs each side). Engram mirror: pending.
