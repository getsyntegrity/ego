# eventsource snapshot tests on go-specs v0.3.3 (follow-up of #246)

## Problem

Three test files in `internal/engine/eventsource` still used testify and the generated `mocks/*` packages:

- `snapshots_writer_actor_test.go` (ten cases for the snapshots writer actor),
- `snapshots_writer_contract_test.go` (five cases on what the stores and the janitor observe),
- `snapshots_sequence_test.go` (three cases on the order of store calls after a command).

They also waited on real time: `pause.For(time.Second)` after every spawn and after every Tell, and a 150 ms `time.Sleep` inside a
logging store double to make a write "slow". The #219 conventions ask for go-specs `mock.Controller`, `Eventually`/`Consistently`
and `specs.Table`.

## What changes

Only these three `_test.go` files change.

- Every test is now one `specs.Describe`. The top-level `Test` names and every case name stay, so the last segment of every
  subtest name is identical to before (21 `--- PASS` lines before and after).
- `mocks/persistence` and `mocks/encryption` are gone. The cases use `enginetest.NewEventsStoreMock`,
  `NewSnapshotStoreMock` and `NewEncryptorMock` from #244; no new adapter was needed.
- `pause.For` is gone. After a Tell the cases poll for the observable result (`ctx.Eventually`), and "nothing else happens" checks use
  `ctx.Consistently`. The two mistyped-extension cases became one `specs.Table` with two rows.
- The 150 ms write delay became a `writeGate`: the logging snapshot store holds the write until the case releases the gate. The case
  first checks, while the write is held, that no retention was forwarded, then opens the gate and checks the order. This is a
  stricter check than sleeping and needs no fixed delay in the double.
- `assert.AnError` is replaced by local sentinel errors, because it comes from testify.

## What does not change, and why

- **Production code.** `retryWithBackoff` uses the real clock inside the writer actor and the actor cannot be given the injected
  clock from #244 without a production change. The retry cases therefore still take about 1.5 s of real backoff
  (100 ms, 200 ms, 400 ms with jitter). That is the follow-up below.
- **The real goakt actor system and the in-memory `testkit` stores.** The spec keeps them: they are the behavior under test.
- **Other eventsource files** (`event_sourced_actor_*`, `events_*`, `retry_test.go`, `clock_test.go`, `enginetest/mocks_test.go`).
- **"Nothing happens" waits.** `Consistently` still watches for 0.5 to 1 s of real time. A negative check has to observe a
  period; there is no event to wait for.
- **Rejected:** syncing the "nil snapshot store" case with an `Ask`. goakt does not reply to an unhandled message, so the `Ask`
  only times out; a `Tell` plus `Consistently(IsRunning)` is shorter and as strong.

## Constraints

- Strict TDD, runner `go test ./internal/engine/eventsource/`, source: the task instruction. RED is shown by deliberate production
  mutations that are reverted. No `-race`, no workbench. The package takes about 220 s, so `-run` filters are used for `-count=5`
  and the full package ran once.
- Release note: NONE.

## Tasks

- [x] T1 `snapshots_writer_actor_test.go`. Route: inline (delegation not available in this run; one writer). Commit: `test(eventsource): move the snapshots writer actor tests to go-specs`.
      RED mutations, all caught: the writer stops forwarding retention (case "forwards retention request to janitor" fails after
      10 s); the writer skips encryption (the "encrypts" and "encryption fails" cases fail); retention forwarded before the write
      and after a failed write ("does not forward retention when snapshot write fails" fails).
- [x] T2 `snapshots_sequence_test.go` and `snapshots_writer_contract_test.go`, in one commit because they share the logging
      store doubles. Route: inline. Commit: `test(eventsource): move the snapshot sequence and contract tests to go-specs`.
      RED mutations, all caught: retention forwarded before the write ("retention runs after the snapshot write"); the write
      drops the scope ("the scope reaches the snapshot write"); the entity drops its tenant scope on the snapshot request
      ("a tenant scope reaches ..."); retention dropped (sequence and contract cases fail).
- [x] T3 Verify and deliver. Evidence: `go build ./...`, `go vet ./internal/engine/...`, `golangci-lint run` (0 issues), `gofmt -l`
      clean; `-count=5` on the three tests passes; full package passes; 21 `--- PASS` names identical to `develop`; coverage of
      `internal/engine/eventsource` 92.0% before and 92.0% after. No testify, `mocks/*` or `pause.` left in the three files.

## Follow-up spec (not in this document)

`eventsource-retry-clock`: let the snapshots writer (and the events writer) take the injected `backoff` from `retry.go`, so
the retry cases advance a manual clock instead of waiting out real backoff.

## Progress

- 2026-10-01: T1 to T3 done. Engram mirror: pending (not attempted in this run).
