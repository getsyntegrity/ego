# Spec 6 of 6 — Neutrality proof and the shared scenario table (#105, #148)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` (umbrella: [`proposal.md`](../../proposal.md), [`design.md`](../../design.md)) |
| Chain position | **Spec 6 of 6.** Previous: [Spec 3](../inmem-runtime-sagas/spec.md) and [Spec 5](../compose-inmem/spec.md). Next: none; follow-ups FU-A to FU-E are in design §8 |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148), [`#105`](https://github.com/getsyntegrity/ego/issues/105) |
| Baseline | `main` at `57c4b11` |
| Decisions applied | design §D9, §D10, §D11; maintainer decision on Q8 (2026-09-27) |

## Purpose

This spec delivers the evidence #105 has been waiting for. One behavior value and one consumer function, both written only against `port/runtime` and `port/behavior`, run on `compose/goakt` and on `compose/inmem` with the same `Spec`, and they must produce the same observable state, journal and events. The same mechanism, a small table of scenarios, also turns the GoAkt behaviors this design could only read about (panics, queued commands after a failure) into measured ones. Later, it is what catches drift between the two runtimes.

## Requirements

### Requirement: the scenarios are runtime-neutral code

`internal/runtimeconsumer` MUST gain `Trace`, `Stores`, `Observe`, `Scenarios` and the two wait helpers `awaitStream` and `awaitCondition` (design §D9, §D10), in production files. Its production closure test keeps rejecting the root package and GoAkt. Its `allowedFirstParty` list (`internal/runtimeconsumer/closure_test.go:59-68`) grows by **exactly two entries**, `persistence` and `egopb`, which `Stores` and the trace normalizer need.

### Requirement: the same observations on both roots

`internal/runtimeconsumer/neutrality_test.go` (package `runtimeconsumer_test`) MUST, for every entry of `Scenarios`:

1. build one `compose.Spec` shape, with fresh `testkit` stores for each root;
2. start `compose/goakt` and `compose/inmem`;
3. run the scenario on each `App.Runtime()`;
4. require equal normalized traces.

Design §D9 lists what is compared and what is removed. Stream messages are compared as a **multiset** (a sorted slice). Collection stops when the count implied by the journal and the latest states is reached, or when the context deadline passes, which is a test failure.

The table MUST include the scenarios of design §D10:

- the duplicated rules;
- family before store;
- liveness after a failed write, an out-of-sync conflict and an in-sync conflict;
- a panic under `RestartDirective` and under `StopDirective`;
- spawning with `WithPlacement(Random)`, `WithPlacement(LeastLoad)`, `WithPlacement(Local)` and `WithRelocation(true)`;
- passivation, with the `advance` hook of design §D11;
- `EntityExists` on a saga ID.

#### Scenario: event-sourced, durable state and saga

- GIVEN `Observe` with the account behavior, one durable-state behavior and one saga
- WHEN it runs on both roots
- THEN the command results, journals, latest states, stream-message multisets, saga status and `EntityExists` answers are equal

#### Scenario: drift is caught

- GIVEN a deliberate change to the in-memory no-event reply (returning a nil state), made only in a local experiment
- WHEN the neutrality test runs
- THEN it fails, naming the scenario and the differing field (recorded as evidence in the pull request, not committed)

### Requirement: no dependence on saga delivery order

Per the maintainer decision on Q8, no scenario's result may depend on the order in which a saga receives events. The test MUST run every saga scenario a second time on `compose/inmem`, with delivery order to sagas reversed through an internal test hook of `internal/inmemruntime`, and require the same trace.

### Requirement: no sleeps

Every wait MUST go through `awaitStream` or `awaitCondition` under a context deadline. On the in-memory side, time moves only through the manual clock.

## Tasks (5)

1. **`Trace`, normalization and the wait helpers** (RED first: the neutrality test fails to build). *Check:* unit tests of the normalizer (timestamps, shard, key IDs and failure text removed; multiset comparison catches a duplicate); `awaitStream` fails on a deadline instead of returning a short slice; the closure allowlist grows by exactly `persistence` and `egopb`.
2. **`Observe` and the duplicated-rule scenarios.** *Check:* the closure test passes; each scenario runs on `compose/inmem` alone.
3. **Measured-behavior scenarios**: family before store, liveness after failures, panics, ignored placement and relocation, passivation, `EntityExists` on a saga. *Check:* each one passes on both roots. If a GoAkt measurement contradicts design §D4 or §D5, the in-memory rule is corrected in the same pull request, and the design is amended.
4. **`neutrality_test.go`** on both roots, plus the reversed-delivery-order run. *Check:* `go test ./internal/runtimeconsumer/` is green; the drift experiment is recorded in the pull request.
5. **Records.**
   - `CHANGELOG.md`: one line saying the in-memory composition runs the same behavior and consumer as the GoAkt one.
   - ego-arch-003 §6: the IMPL-6 row marked done, with the §5.2 departure of design §D8.
   - ego-arch-001 §4: the map rows for `internal/inmemruntime` and `compose/inmem`.
   
   *Check:* the documentation is read back, and the pull request body maps each #148 criterion to its test or to the maintainer decision that covers it.

## Checks

- `go test ./internal/runtimeconsumer/ ./compose/...`
- the root lane `ciselect` picks for the pull request (it selects this test through `compose/goakt`'s test imports, with no selector change)
- `go run ./internal/cmd/archcheck`; apidiff: no report for any public package

## File ownership

`internal/runtimeconsumer/**`; `CHANGELOG.md`; `openspec/changes/ego-arch-003/design.md` (§6 IMPL-6 row, §7 last row); `openspec/changes/ego-arch-001/design.md` (§4 map rows); the reversed-order test hook in `internal/inmemruntime` (one unexported function).

## Dependencies

Specs 3 and 5 merged.

After this spec lands, the neutrality criterion of #148 and the in-memory criterion of #105 have their evidence. #148's typed-error criterion is covered only for the projection capability (Q3). For spawn settings, it depends on the maintainer's answer to Q2 and on the amendment of #148's wording that Q2 asks for. Closing either issue is the maintainer's call.

## Next in the chain

None. The follow-ups are FU-A (projection runner), FU-B (extract shared rules), FU-C (ordered event stream), FU-D (neutral entity-not-found error) and FU-E (the §2.6 contract mismatches), listed in design §8.
