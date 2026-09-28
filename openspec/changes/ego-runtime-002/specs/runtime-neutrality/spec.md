# Spec 5 of 5 — Neutrality proof and the shared scenario table (#105, #148)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 5 of 5.** Previous: [Spec 3](../inmem-runtime-sagas/spec.md) and [Spec 4](../compose-inmem/spec.md). Next: none; follow-ups FU-A to FU-E in design §8 |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148), [`#105`](https://github.com/getsyntegrity/ego/issues/105) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D9, §D10 |

## Purpose

This spec delivers the evidence #105 has been waiting for: one behavior value and one consumer function, both written only against `port/runtime` and `port/behavior`, run on `compose/goakt` and on `compose/inmem` with the same `Spec`, and produce the same observable state, journal and events. The same mechanism, a small table of scenarios, is what later catches drift between the two runtimes.

## Requirements

### Requirement: the scenarios are runtime-neutral code

`internal/runtimeconsumer` MUST gain `Trace`, `Stores`, `Observe` and `Scenarios` (design §D9, §D10) in production files. Its existing production closure test MUST keep passing unchanged: no root package, no GoAkt.

### Requirement: the same observations on both roots

`internal/runtimeconsumer/neutrality_test.go` (package `runtimeconsumer_test`) MUST, for every entry of `Scenarios`, build one `compose.Spec` shape with fresh `testkit` stores per root, start `compose/goakt` and `compose/inmem`, run the scenario on each `App.Runtime()`, and require equal normalized traces (design §D9 lists what is compared and what is removed).

#### Scenario: event-sourced, durable state and saga

- GIVEN `Observe` with the account behavior, one durable-state behavior and one saga
- WHEN it runs on both roots
- THEN the command results, journals, latest states, stream-message sets, saga status and `EntityExists` answers are equal

#### Scenario: drift is caught

- GIVEN a deliberate change to the in-memory no-event reply (returning nil state), made only in a local experiment
- WHEN the neutrality test runs
- THEN it fails naming the scenario and the differing field (recorded as evidence in the pull request, not committed)

### Requirement: no sleeps

Every wait in the scenarios MUST be condition-based under a context deadline (stream receive, `SagaStatus` re-query).

## Tasks (4)

1. **`Trace` and normalization** (RED first: the neutrality test fails to build). *Check:* unit tests of the normalizer (timestamps, shard, key IDs and failure text removed; stream messages compared as a set).
2. **`Observe` and `Scenarios`** covering design §D10's duplicated rules. *Check:* the closure test still passes; each scenario runs on `compose/inmem` alone.
3. **`neutrality_test.go`** on both roots. *Check:* `go test ./internal/runtimeconsumer/` green; the drift experiment recorded in the pull request.
4. **Records.** `CHANGELOG.md` line (the in-memory composition meets #105's criterion); ego-arch-003 §6 IMPL-6 and §7 marked done; ego-arch-001 §4 map row for `internal/inmemruntime` and `compose/inmem`. *Check:* documentation read back; the pull request body maps each #148 criterion to its test.

## Checks

- `go test ./internal/runtimeconsumer/ ./compose/...`
- the root lane selected by `ciselect` for the pull request (it selects this test through `compose/goakt`'s test imports; no selector change)
- `go run ./internal/cmd/archcheck`; apidiff: no report for any public package

## File ownership

`internal/runtimeconsumer/**`; `CHANGELOG.md`; `openspec/changes/ego-arch-003/design.md` (§6 IMPL-6 row, §7 last row); `openspec/changes/ego-arch-001/design.md` (§4 map rows).

## Dependencies

Specs 3 and 4 merged. After it lands, #148's criteria are met and #105's in-memory criterion is satisfied; closing either issue is the maintainer's call.

## Next in the chain

None. Follow-ups FU-A (projection runner), FU-B (extract shared rules), FU-C (ordered event stream), FU-D (neutral entity-not-found error), FU-E (documentation mismatches) are listed in design §8.
