# Spec 1 of 6 — In-memory runtime core: event-sourced entities, commands, event stream (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 1 of 6.** Previous: none. Next: [Spec 2 — durable state, publishers, tenancy, erasure](../inmem-runtime-state/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D1, §D2, §D3, §D4, §D7. It assumes the recommendations for open questions Q1, Q3, Q4 and the placement and relocation part of Q2; if the maintainer answers differently, the affected requirement is revised before implementation |

## Purpose

This spec creates the runtime package and the rule that keeps it free of GoAkt. It then makes the smallest useful slice work: event-sourced entities that recover from the stores, handle commands one at a time, persist events, and publish them on the event stream. The package implements the whole `port/runtime.Runtime` interface from its first pull request. What is not built yet answers with a typed error, as the table below says.

## Requirements

### Requirement: the package implements `port/runtime.Runtime` without GoAkt

`internal/inmemruntime` MUST declare `var _ runtimeport.Runtime = (*Runtime)(nil)`. Its production and test dependency closures (`go list -deps` and `go list -deps -test`) MUST contain none of these: the root package `github.com/pablogore/ego/v4`, any `github.com/tochemey/goakt/v4` package, or `compose/goakt`.

#### Scenario: closure test

- GIVEN the package after this spec
- WHEN `closure_test.go` runs both `go list` commands
- THEN neither output contains a forbidden package

### Requirement: archcheck rule `inmem-no-runtime`

`internal/cmd/archcheck/rules` MUST gain `InMemoryRuntimeLayer`, matching `internal/inmemruntime/...` and `compose/inmem/...`, and the denylist rule `inmem-no-runtime` of design §D2. No baseline entry and no exception MAY be added, and no existing rule MAY change.

#### Scenario: a forbidden edge

- GIVEN a test graph in which `internal/inmemruntime` imports the root package (and, in separate cases, `internal/extensions`, a GoAkt package, and `compose/goakt`)
- WHEN `rules.Evaluate` runs
- THEN it reports one `inmem-no-runtime` violation per edge, with the matching reason

### Requirement: lifecycle, unsupported and not-yet-built methods

The runtime MUST answer all 14 methods of `port/runtime.Runtime` from this spec on, as follows:

| Methods | After spec 1 | Changed by |
|---|---|---|
| `SpawnEventSourced`, `EntityExists`, `SendCommand`, `Dispatch` (event-sourced targets), `Subscribe` | implemented | — |
| `StartProjection`, `StopProjection`, `IsProjectionRunning`, `RebuildProjection`, `ProjectionLag` | `*UnsupportedError{Runtime: "inmem", Operation: <method>}` in every lifecycle state, before any side effect | nobody in this chain (Q3) |
| `SpawnDurableState`, `EraseEntity` | `*UnsupportedError{Runtime: "inmem", Operation: <method>}` in every lifecycle state, before any side effect, as a placeholder | spec 2 implements them |
| `SpawnSaga`, `SagaStatus` | `*UnsupportedError`, as a placeholder | spec 3 implements them |

Otherwise, before `Start` and after `Stop` every method MUST return `ErrEngineNotStarted`. The table test of task 2 is keyed by method, and each later spec updates its own rows from "unsupported placeholder" to the real behavior. At the end of the chain only the five projection rows remain unsupported.

#### Scenario: precedence

- GIVEN a runtime that was never started
- WHEN `StartProjection` is called
- THEN the error matches `runtimeport.ErrUnsupported` and `errors.ErrUnsupported`, not `ErrEngineNotStarted`

### Requirement: event-sourced entities

- **Spawn.** `SpawnEventSourced` MUST check, in order: started, then family, then events store, then tenancy (design §D4). It MUST be idempotent for a live ID, and it MUST recover synchronously from the snapshot store (when set) and the events store.
- **Commands.** `SendCommand` and `Dispatch` MUST run inside the entity's mailbox, one command at a time. They MUST apply the deadline checks, handler preference, precondition mapping and conflict result of design §D4, and the no-event reply (current state and revision).
- **Writes.** Events MUST be written with one `WriteEvents` per command, and published to `topic.events` only after the write succeeds.
- **Failed writes.** After a failed write, and after an out-of-sync conflict, the entity MUST be removed (design §D4, GoAkt behavior §2.1).
- **Spawn options.** They MUST be read through `runtimeport.ResolveSpawnOptions`. Placement and relocation are ignored, a provisional choice under Q2 pending RUNTIME-003. Passivation is spec 4.

#### Scenario: family before store

- GIVEN a runtime that declares only `DurableState` and has no events store
- WHEN `SpawnEventSourced` is called
- THEN the error wraps `ErrEntityFamilyNotDeclared`, not `ErrEventsStoreRequired`

#### Scenario: recovery

- GIVEN an events store holding two events for ID `a`
- WHEN `a` is spawned and sent a command that emits one event
- THEN the returned revision is 3 and the state reflects all three events

#### Scenario: no event

- GIVEN a live entity at revision 2
- WHEN a command emits no events
- THEN `SendCommand` returns the current state (not nil) and revision 2

#### Scenario: conflict

- GIVEN a live entity at revision 2
- WHEN `Dispatch` carries an expected revision of 1
- THEN the result is `OutcomeRejected` with `command.CodeConcurrencyConflict`, `errors.As` recovers a `*persistence.ConflictError`, and the store is unchanged; and whether `EntityExists` is still true follows the in-sync rule of design §D4

#### Scenario: failed write

- GIVEN a live entity whose events store fails the next `WriteEvents` (a `mocks/` store)
- WHEN a command emits an event
- THEN the result is `OutcomeFailed`, `EntityExists` is false, and a later `SendCommand` does not re-spawn it

#### Scenario: serialized mailbox

- GIVEN ten goroutines each sending one command, each emitting one event, to the same entity
- WHEN all return
- THEN the revisions are exactly 1 through 10, each once

## Tasks (5)

1. **Package and rule.** `internal/inmemruntime` with `Config`, `New`, `Runtime`, `Start`, `Stop`, the compile-time assertion and `closure_test.go`. The `inmem-no-runtime` layer and rule, with `evaluate_test.go` cases. The `docs/ci.md` rule-table row. *Check:* `go test ./internal/inmemruntime/ ./internal/cmd/archcheck/...`; `go run ./internal/cmd/archcheck` reports 0 violations and the unchanged baseline count.
2. **Lifecycle and the 14-method table** (RED first). *Check:* the method-keyed table test before `Start`, after `Start` and after `Stop`, with the rows of the requirement above.
3. **Spawn and recovery** for event-sourced entities, `EntityExists`, the family guard in GoAkt's order. *Check:* recovery, idempotent re-spawn, missing events store, family-before-store and undeclared-family tests.
4. **Commands.** Mailbox, `SendCommand`, `Dispatch` with deadlines, preconditions, no-event reply, removal after a failed write or an out-of-sync conflict, and panics under each supervisor directive. *Check:* the scenarios above, plus a deadline-already-passed test, a canceled-context test, and a command queued behind a failed one (design §D4, provisional). No sleeps.
5. **Event stream.** Publish to `topic.events` after the write, `Subscribe`, and clock timestamps. *Check:* a subscriber receives every event of a command sequence, compared as a multiset (design §2.3); a failed write publishes nothing.

## Checks

- `go test ./internal/inmemruntime/ ./internal/cmd/archcheck/...` (no `-race` locally)
- `go run ./internal/cmd/archcheck`
- `golangci-lint run ./internal/inmemruntime/... ./internal/cmd/archcheck/...`
- apidiff: no report for any public package

## File ownership

`internal/inmemruntime/**` (new); `internal/cmd/archcheck/rules/layers.go`, `rules.go`, `evaluate_test.go`; `docs/ci.md` (one row in the rule table).

## Dependencies

This design approved. No code dependency: `port/runtime` (#147) and `port/behavior` (#123) are on `main`. Rebase the archcheck files onto any open archcheck change.

## Next in the chain

[Spec 2](../inmem-runtime-state/spec.md).
