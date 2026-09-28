# Spec 1 of 5 — In-memory runtime core: event-sourced entities, commands, event stream (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 1 of 5.** Previous: none. Next: [Spec 2 — durable state, publishers, tenancy, erasure](../inmem-runtime-state/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D1, §D2, §D3, §D4, §D7; open questions Q1, Q2, Q3, Q4 as answered by the maintainer (this spec assumes the recommendations; if an answer differs, the affected requirement is revised before implementation) |

## Purpose

This spec creates the runtime package and the rule that keeps it free of GoAkt, and makes the smallest useful slice of it work: event-sourced entities that recover from the stores, handle commands one at a time, persist events, and publish them on the event stream. Everything the runtime does not do yet answers with a typed error, so the package implements the whole `port/runtime.Runtime` interface from its first pull request.

## Requirements

### Requirement: the package implements `port/runtime.Runtime` without GoAkt

`internal/inmemruntime` MUST declare `var _ runtimeport.Runtime = (*Runtime)(nil)`. Its production and test dependency closures (`go list -deps` and `go list -deps -test`) MUST contain neither the root package `github.com/pablogore/ego/v4` nor any `github.com/tochemey/goakt/v4` package nor `compose/goakt`.

#### Scenario: closure test

- GIVEN the package after this spec
- WHEN `closure_test.go` runs both `go list` commands
- THEN neither output contains a forbidden package

### Requirement: archcheck rule `inmem-no-runtime`

`internal/cmd/archcheck/rules` MUST gain `InMemoryRuntimeLayer` (matching `internal/inmemruntime/...` and `compose/inmem/...`) and the denylist rule `inmem-no-runtime` of design §D2. No baseline entry and no exception MAY be added; no existing rule MAY change.

#### Scenario: a forbidden edge

- GIVEN a test graph in which `internal/inmemruntime` imports the root package (and, separately, `internal/extensions`, a GoAkt package, `compose/goakt`)
- WHEN `rules.Evaluate` runs
- THEN it reports one `inmem-no-runtime` violation per edge with the matching reason

### Requirement: lifecycle and unsupported operations

Before `Start` and after `Stop`, every method MUST return `ErrEngineNotStarted`, except the five `Projections` methods, which MUST return `*runtimeport.UnsupportedError{Runtime: "inmem", Operation: <method name>}` in every lifecycle state and before any side effect.

#### Scenario: precedence

- GIVEN a runtime that was never started
- WHEN `StartProjection` is called
- THEN the error matches `runtimeport.ErrUnsupported` and `errors.ErrUnsupported`, not `ErrEngineNotStarted`

### Requirement: event-sourced entities

`SpawnEventSourced` MUST follow design §D4's check order, be idempotent for a live ID, and recover synchronously from the snapshot store (when set) and the events store. `SendCommand` and `Dispatch` MUST run inside the entity's mailbox, one command at a time, with the deadline checks, handler preference, precondition mapping, conflict result and no-event reply (current state and revision) of design §D4. Events MUST be written with one `WriteEvents` per command and published to `topic.events` after the write succeeds. Spawn options MUST be read through `runtimeport.ResolveSpawnOptions`; placement, relocation and passivation are ignored (Q2).

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
- THEN the result is `OutcomeRejected` with `command.CodeConcurrencyConflict`, `errors.As` recovers a `*persistence.ConflictError`, and the store is unchanged

#### Scenario: serialized mailbox

- GIVEN ten goroutines sending one command each, each emitting one event, to the same entity
- WHEN all return
- THEN the revisions are exactly 1 through 10, each once

## Tasks (5)

1. **Package and rule.** `internal/inmemruntime` with `Config`, `New`, `Runtime`, `Start`, `Stop`, the compile-time assertion, `closure_test.go`; the `inmem-no-runtime` layer and rule with `evaluate_test.go` cases; the `docs/ci.md` rule-table row. *Check:* `go test ./internal/inmemruntime/ ./internal/cmd/archcheck/...`; `go run ./internal/cmd/archcheck` reports 0 violations and the unchanged baseline count.
2. **Lifecycle and unsupported operations** (RED first). *Check:* table test over all 14 methods before `Start`, after `Start`, after `Stop`.
3. **Spawn and recovery** for event-sourced entities, `EntityExists`, family guard. *Check:* recovery, idempotent re-spawn, missing events store, undeclared family tests.
4. **Commands.** Mailbox, `SendCommand`, `Dispatch` with deadlines, preconditions, no-event reply, conflict re-hydration, handler panic. *Check:* the scenarios above plus a deadline-already-passed and a canceled-context test; no sleeps.
5. **Event stream.** Publish to `topic.events` after the write; `Subscribe`; `Config.Clock` timestamps. *Check:* a subscriber receives every event of a command sequence (as a set, design §2.3); a failed write publishes nothing.

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
