# Spec 4 of 6 — Clock and passivation (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 4 of 6.** Previous: [Spec 2 — durable state, publishers, tenancy, erasure](../inmem-runtime-state/spec.md). Next: [Spec 5 — `compose/inmem`](../compose-inmem/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D11; maintainer decision on passivation (2026-09-27): implement, with a typed error only as a fallback |

## Purpose

On a single node, GoAkt honors `WithPassivateAfter`. An entity idle for that long is stopped, `EntityExists` then reports false, and `Dispatch` does not re-spawn it (`engine.go:1910-1911`). The maintainer decided that the in-memory runtime must not ignore the setting silently. This spec implements it with the same observable semantics. To keep tests deterministic, all of the runtime's time reads go through one clock that tests advance by hand.

## Requirements

### Requirement: one clock

`internal/inmemruntime` MUST read time only through `Clock` (`Now`, `AfterFunc`, design §D11). This covers event and state timestamps, idle timers and saga timeouts. A nil `Config.Clock` means the wall clock. An internal manual clock MUST run every due `AfterFunc` callback synchronously, inside `Advance(d)`.

#### Scenario: no wall time in tests

- GIVEN the manual clock
- WHEN a test advances it past an entity's idle period
- THEN the entity passivates without any real time passing (the test has no sleep and no ticker)

### Requirement: passivation

An event-sourced or durable-state entity spawned with `WithPassivateAfter(d)`, `d > 0`, MUST be passivated after being idle for `d`:

- each completed mailbox turn resets its idle timer;
- the timer enqueues a passivate item; when the item runs, it re-checks idleness against the clock;
- a durable-state entity writes its state once more first;
- then the entity is removed.

After that, `EntityExists` MUST report false, `SendCommand`/`Dispatch` MUST fail as for an unknown ID without re-spawning, and a new spawn MUST recover the state from the stores. Sagas MUST ignore the setting. `Stop` MUST stop every idle timer.

#### Scenario: a command resets the timer

- GIVEN an entity with `WithPassivateAfter(10s)`
- WHEN the clock advances 6 s, a command runs, and the clock advances 6 s more
- THEN the entity is still alive; after 4 s more it passivates

#### Scenario: the item races a command

- GIVEN a passivate item queued behind a command
- WHEN the command runs first
- THEN the item finds the entity no longer idle and does nothing

## Tasks (3)

1. **Clock** (RED first): the `Clock` interface, the wall clock, the internal manual clock, and switching timestamps and spec 3's saga timeout to it. *Check:* manual-clock unit tests (callbacks run in `Advance`, stop prevents a callback); a timestamp test with a pinned clock.
2. **Passivation**: idle timer, passivate item, durable-state final write, removal. *Check:* the two scenarios above; `EntityExists` false; `Dispatch` does not re-spawn; re-spawn recovers the state; sagas ignore the option.
3. **Timers at `Stop`**. *Check:* `Stop` with pending idle timers leaves no goroutine or timer behind (checked with `awaitCondition` on the goroutine count, and the manual clock reports no pending callbacks).

## Checks

- `go test ./internal/inmemruntime/` with no sleeps: a review check that the spec's tests contain no `time.Sleep`, `pause.For` or wall-clock ticker
- closure test and `go run ./internal/cmd/archcheck`
- apidiff: no report for any public package

## File ownership

`internal/inmemruntime/**`: new clock and passivation files; one call in the mailbox's turn-completion path.

## Dependencies

Specs 1 and 2 merged (the durable-state final write comes from spec 2). If spec 3 is still open, rebase its saga timeout onto the clock in whichever pull request lands second.

## Next in the chain

[Spec 5](../compose-inmem/spec.md), which exposes the clock as `WithClock`.
