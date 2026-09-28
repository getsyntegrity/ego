# Spec 5 of 6 — `compose/inmem` composition root (#105 IMPL-6)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 5 of 6.** Previous: [Spec 4 — clock and passivation](../inmem-runtime-passivation/spec.md) (spec 3 can run in parallel). Next: [Spec 6 — neutrality proof](../runtime-neutrality/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148), [`#105`](https://github.com/getsyntegrity/ego/issues/105) IMPL-6 |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D8, §D11; ego-arch-003 §D4–§D7 (with the §5.2 departure of design §D8); ego-arch-004 §D4, §D6; maintainer decision on Q8 (2026-09-27) |

## Purpose

This spec adds the only new public package of the chain: a composition root that takes the same `compose.Spec` as `compose/goakt`. It validates the `Spec` with the same rules plus one of its own (M1: no projections), and it starts, rolls back and stops the in-memory runtime with the same shared sequencer. Consumer code gets the runtime through `App.Runtime()`, typed as `port/runtime.Runtime`, exactly as it does from `compose/goakt`.

## Requirements

### Requirement: validation at `New`

`New` MUST run `spec.Validate()` (V1–V8) and rule M1 (`Spec.Projections` empty), return every problem joined, each a `*compose.ValidationError`, and start nothing. It MUST apply no naming rule to `Spec.Name`.

#### Scenario: several problems at once

- GIVEN a `Spec` with no families and one projection
- WHEN `New` runs
- THEN the error lists V1, V2 (no events store for the projection), V4 and M1

### Requirement: start order and rollback

- **Steps.** `Start` MUST run `probe stores`, `start runtime`, `attach publishers` through `compose/internal/lifecycle`, and use `compose/internal/adapters.StartAndProbe` for publishers.
- **Failure.** On failure it MUST undo the earlier steps, close every publisher never attached, and return a `*compose.StartError` naming the step (design §D8 table).
- **Single use.** The `App` MUST be single-use.

#### Scenario: failure at each step

- GIVEN a failure injected after each step in turn (through the `afterStep` hook)
- WHEN `Start` runs
- THEN the `StartError` names that step, the runtime is stopped, the stream is closed and every publisher is closed

### Requirement: stop

`Stop` MUST undo the steps in reverse under the cleanup context (`context.WithoutCancel` bounded by `ShutdownTimeout`), attempt every undo and join the errors. It MUST be idempotent, and it MUST close the publishers of an `App` that never started. The runtime's own `Stop` behavior (waiting for the turn in progress, the provisional handling of queued items) is spec 2's.

### Requirement: accessor and clock

`Runtime()` MUST return an untyped nil before a successful `Start`, and for good after a failed one. After `Stop` it MUST return the stopped runtime, whose methods return `ErrEngineNotStarted`. `WithClock(inmem.Clock)` MUST pass the clock to the runtime (design §D11).

### Requirement: package documentation

The package documentation MUST state:

- that the runtime is for tests and local development;
- which guarantees are in-memory-only (design §D7);
- **that the order in which sagas receive events is not part of the contract**, even though this runtime produces persist order (maintainer decision on Q8);
- what differs from `compose/goakt`: three start steps, M1, and no `WithCluster`, `WithActorSystemOptions` or `WithTelemetry`.

### Requirement: GoAkt-free closure

`compose/inmem`'s production and test closures MUST contain neither the root package nor GoAkt nor `compose/goakt`. `inmem-no-runtime` (spec 1) covers its direct imports.

## Tasks (5)

1. **`New`, options, M1** (RED first). *Check:* tests mirroring the `TestNew_*` tests of `compose/goakt/app_test.go` (missing dependency, starts nothing, negative timeout, reports every problem, V8 rejects a lying publisher), plus M1.
2. **Start steps and rollback.** *Check:* failure at each step, probe failure names the store, cancelled context starts nothing, publisher failure at *k*.
3. **Stop.** *Check:* a never-started `App` closes its publishers; `Stop` after `Stop` is a no-op; the stop order is recorded; the durable-state flush happens after publishers close.
4. **`Runtime()`, `WithClock` and the closure test.** *Check:* nil before `Start`; nil after a failed `Start`; the same runtime after `Start` and after `Stop`; a pinned clock reaches event timestamps; `closure_test.go` over `go list -deps` and `-deps -test`.
5. **Docs and changelog.** The package documentation of the requirement above, and a `CHANGELOG.md` Features entry. *Check:* the documentation carries the saga-order sentence (review check); apidiff on `compose/inmem` reports additions only; `go vet`, `golangci-lint`.

## Checks

- `go test ./compose/...` (the shared `compose` and `compose/internal/...` tests must pass unchanged)
- `go run ./internal/cmd/archcheck`
- apidiff: additions only for `compose/inmem`; no report for `compose`, `compose/goakt` or `ego`

## File ownership

`compose/inmem/**` (new); `CHANGELOG.md`. It MUST NOT edit `compose/spec.go`, `compose/errors.go`, `compose/internal/**` or `compose/goakt/**`. If a shared helper needs a change, that is a separate pull request.

## Dependencies

Specs 1, 2 and 4 merged: publishers and the `Stop` order come from spec 2, and the clock from spec 4.

## Next in the chain

[Spec 6](../runtime-neutrality/spec.md).
