# Spec 4 of 5 — `compose/inmem` composition root (#105 IMPL-6)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 4 of 5.** Previous: [Spec 2](../inmem-runtime-state/spec.md) (spec 3 runs in parallel). Next: [Spec 5 — neutrality proof](../runtime-neutrality/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148), [`#105`](https://github.com/getsyntegrity/ego/issues/105) IMPL-6 |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D8; ego-arch-003 §D4–§D7; ego-arch-004 §D4, §D6 |

## Purpose

This spec adds the only new public package of the chain: a composition root that takes the same `compose.Spec` as `compose/goakt`, validates it with the same rules plus one of its own (M1, no projections), and starts, rolls back and stops the in-memory runtime with the same shared sequencer. Consumer code gets the runtime through `App.Runtime()`, typed as `port/runtime.Runtime`, exactly as it does from `compose/goakt`.

## Requirements

### Requirement: validation at `New`

`New` MUST run `spec.Validate()` (V1–V8) and rule M1 (`Spec.Projections` empty) and return every problem joined, each a `*compose.ValidationError`, with nothing started. It MUST apply no naming rule to `Spec.Name`.

#### Scenario: several problems at once

- GIVEN a `Spec` with no families and one projection
- WHEN `New` runs
- THEN the error lists V1, V2 (no events store for the projection), V4 and M1

### Requirement: start order and rollback

`Start` MUST run the steps `probe stores`, `start runtime`, `attach publishers` through `compose/internal/lifecycle`, use `compose/internal/adapters.StartAndProbe` for publishers, and on failure return a `*compose.StartError` naming the step after undoing earlier steps and closing every publisher never attached (design §D8 table). The `App` MUST be single-use.

#### Scenario: failure at each step

- GIVEN a failure injected after each step in turn (through the `afterStep` hook)
- WHEN `Start` runs
- THEN the `StartError` names that step, the runtime is stopped, the stream is closed and every publisher is closed

### Requirement: stop

`Stop` MUST undo the steps in reverse under the cleanup context (`context.WithoutCancel` bounded by `ShutdownTimeout`), attempt every undo and join errors, be idempotent, and close the publishers of an `App` that never started.

### Requirement: accessor

`Runtime()` MUST return an untyped nil before a successful `Start` and for good after a failed one, and the stopped runtime after `Stop`, whose methods return `ErrEngineNotStarted`.

### Requirement: GoAkt-free closure

`compose/inmem`'s production and test closures MUST contain neither the root package nor GoAkt nor `compose/goakt`; `inmem-no-runtime` (spec 1) covers its direct imports.

## Tasks (5)

1. **`New`, options, M1** (RED first). *Check:* tests mirroring `compose/goakt/app_test.go`'s `TestNew_*` (missing dependency, starts nothing, negative timeout, reports every problem, V8 lying publisher) plus M1.
2. **Start steps and rollback.** *Check:* failure at each step, probe failure names the store, cancelled context starts nothing, publisher failure at *k*.
3. **Stop.** *Check:* never-started `App` closes publishers, `Stop` after `Stop` is a no-op, recorded stop order, the durable-state flush happens after publishers close.
4. **`Runtime()` and closure test.** *Check:* nil before `Start`, nil after a failed `Start`, the same runtime after `Start` and after `Stop`; `closure_test.go` over `go list -deps` and `-deps -test`.
5. **Docs and changelog.** Package documentation (in-memory for tests and local development; which guarantees are in-memory-only, design §D7; what differs from `compose/goakt`); `CHANGELOG.md` Features entry. *Check:* apidiff on `compose/inmem` reports additions only; `go vet`, `golangci-lint`.

## Checks

- `go test ./compose/...` (the shared `compose` and `compose/internal/...` tests must pass unchanged)
- `go run ./internal/cmd/archcheck`
- apidiff: additions only for `compose/inmem`; no report for `compose`, `compose/goakt` or `ego`

## File ownership

`compose/inmem/**` (new); `CHANGELOG.md`. It MUST NOT edit `compose/spec.go`, `compose/errors.go`, `compose/internal/**` or `compose/goakt/**`; if a shared helper needs a change, that is a separate pull request.

## Dependencies

Specs 1 and 2 merged (publishers and `Stop` order come from spec 2).

## Next in the chain

[Spec 5](../runtime-neutrality/spec.md).
