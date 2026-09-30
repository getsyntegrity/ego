# S1: engine test seams and mock.Controller adapters

This is spec 1 of `engine-actors-go-specs-chain.md`. It is a stacked PR on `test/205-migrate-engine-actors-unit` (#238).

## Problem

Two gaps block the rest of the chain.

The first is real time in the retry helper. `internal/engine/eventsource/retry.go:42` (`retryWithBackoff`) sleeps with `time.After`, using an exponential delay (100 ms base, 2 s cap) times a random jitter in [0.5, 1.5). So `retry_test.go` either waits in real time or asserts against `time.Now()` bounds. Both break the rule that unit tests do not synchronize on real time.

The second is that no `mock.Controller` adapters exist. The unit tests in S2 and S3 need typed adapters for `persistence.EventsStore`, `persistence.SnapshotStore`, `persistence.StateStore`, `encryption.Encryptor` and `eventadapter.EventAdapter`. Today only the testify mocks generated into `mocks/` exist.

## What changes

`retryWithBackoff` gets an injectable time source and jitter source. The default keeps today's behavior exactly: the same delay formula, the same random jitter, the same number of attempts, and the same context handling.

There are two ways to inject them. The recommended one is to pass a small policy struct or clock. The alternative is a package-level variable that tests swap. We reject the variable because it is shared mutable state, which breaks tests that run in parallel.

`retry_test.go` then drives the backoff with go-specs `specs.NewManualClock()` and a fixed jitter, so it no longer uses real sleeps or `time.Now` bounds.

The adapters go into `internal/engine/enginetest`, next to the behaviors and probes that eventsource and durablestate tests already share. Each adapter forwards every interface method to `mock.Controller.Method(name).Call(args...)`. Each file also has a compile-time `var _ persistence.EventsStore = (*EventsStoreMock)(nil)` style check and a short example test.

## Constraints

- There is no behavior change with the default time and jitter sources.
- `enginetest` may import `github.com/getsyntegrity/go-specs/mock`, because go-specs is already a module dependency. The writer checks that no architecture test forbids this import.
- Strict TDD. Observe RED before each production change. The runner is `go test ./internal/engine/...`.
- No `-race` and no workbench.

## Tasks

- [x] T1 Merge `origin/develop` into #238's branch and push. Route: inline. Evidence: `go.mod` pins v0.3.3,
      `tidy` is clean, and both packages are green (225.6 s and 22.2 s).
- [x] T2 Add the backoff seam to `retryWithBackoff`, with the default preserved. Route: delegated writer.
      Evidence: `246579d`, which adds `backoff{clock, jitter}` and `defaultBackoff()`, and updates the 3
      callers. RED was a compile failure.
- [x] T3 Move `retry_test.go` to the manual clock and a fixed jitter, removing the real-time assertions.
      Route: the same writer. Evidence: `c5687d2`. The file went from 1.59 s with 8 cases to 0.01 s with
      16 cases, and every original name is kept. Two mutations were caught: `2^(attempt+1)`, and a wait
      after the last attempt.
- [x] T4 Add typed `mock.Controller` adapters for the five ports in `internal/engine/enginetest`. Route:
      the same writer. Evidence: `8c39f85`, which adds `EventsStoreMock` (10 methods), `SnapshotStoreMock`
      (6), `StateStoreMock` (5), `EncryptorMock` (2) and `EventAdapterMock` (1), with 15 adapter specs. No
      architecture rule restricts `enginetest` imports.
- [x] T5 Verify and deliver. Route: inline. Evidence:
      - `go test ./internal/engine/...` is green; eventsource takes 223.9 s, versus 225.6 s before.
      - vet, lint and gofmt are clean, and `go build ./...` succeeds.
      - The assessment returned `medium` with RDD off, so the writer's self-verification stands.
      - The parent also read the production diff and confirmed the default behavior is equivalent.

## Progress

- 2026-09-30: T1 done. Engram mirror pending.
