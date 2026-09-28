# Spec 0 of 7 — Characterize the GoAkt runtime first (EGO-RUNTIME-005)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 0.** Previous: none. Next: [Spec 1 — runtime core](../inmem-runtime-core/spec.md) |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D12 (maintainer decision 2026-09-27), §D9, §D10 |

## Purpose

The in-memory runtime copies GoAkt's observable behavior. A few behaviors cannot be settled by reading the code:

- what a panic does under each supervisor directive;
- what happens to a command queued behind a failed write;
- which messages count as passivation activity.

This spec measures them on GoAkt before any in-memory code exists, and writes the results into the design. It also builds the harness that spec 6 later reuses for the neutrality proof. It touches no root-package file and no hot spot.

## Requirements

### Requirement: a neutral harness

`internal/runtimeconsumer` MUST gain, in production files:

- `Scenarios`, a list of `{Name, Run func(ctx, runtimeport.Runtime, Stores) (Trace, error)}`;
- `Trace` and its normalizer;
- `Stores`;
- the wait helpers `awaitStream` and `awaitCondition` (design §D10).

Its production closure test keeps rejecting the root package and GoAkt. Its `allowedFirstParty` list (`internal/runtimeconsumer/closure_test.go:59-68`) grows by **exactly two entries**, `persistence` and `egopb`.

### Requirement: a GoAkt-only runner

`internal/runtimeconsumer/characterization_test.go` (package `runtimeconsumer_test`) MUST start `compose/goakt` from a `compose.Spec` with fresh `testkit` stores for each scenario, run the scenario on `App.Runtime()`, and assert the measured trace. It uses no sleeps; the only waits are the two helpers, under a context deadline.

### Requirement: results recorded before spec 1

Each task's pull-request commit MUST write the measured result into the design (§2, §D4, §D11 or the Q2 table, as named in the task), replacing the word "provisional". Spec 1 MUST NOT start until all four are recorded.

## Tasks (4)

1. **Harness.** `Scenarios`, `Trace`, `Stores`, `awaitCondition`, `awaitStream`, and the allowlist growing by exactly `persistence` and `egopb`. *Check:* normalizer unit tests (timestamps, shard, key IDs and failure text removed; a multiset comparison catches a duplicate); `awaitStream` fails on a deadline instead of returning a short slice; the closure test passes.
2. **Liveness after failures.** `EntityExists` after a failed write (a `mocks/` events store), after an out-of-sync conflict and after an in-sync conflict. Also a command queued behind a failed write: its reply, and whether it runs. *Check:* the scenario passes on `compose/goakt`; the results are recorded in design §2.1 and §D4.
3. **Panics.** A handler panic under `RestartDirective` and under `StopDirective`: the reply, `EntityExists`, and the state recovered by the next command or spawn. *Check:* the scenario passes on `compose/goakt`; the results are recorded in design §2.5, §D4 and the Q2 table.
4. **Passivation activity and ignored options.**
   - Passivation: a refused command (tenant mismatch) and a no-event command each keep the entity alive past the idle period; a passivating durable-state entity writes **and** publishes its state.
   - Ignored options: spawning with `WithPlacement(Random|LeastLoad|Local)` and `WithRelocation(true)` behaves as without them; a saga spawned with options other than `WithTenant` behaves as without them.
   
   *Check:* the scenarios pass on `compose/goakt` (with `WithPassivateAfter(100 ms)` and `awaitCondition`, no sleeps); the results are recorded in design §2.5, §D11 and the Q2 table.

## Checks

- `go test ./internal/runtimeconsumer/` (no `-race` locally)
- `go run ./internal/cmd/archcheck`; apidiff: no report for any public package

## File ownership

`internal/runtimeconsumer/**`; `openspec/changes/ego-runtime-002/design.md` (the recorded results only).

## Dependencies

This design approved.

## Next in the chain

[Spec 1](../inmem-runtime-core/spec.md), which starts only after this spec's results are recorded.
