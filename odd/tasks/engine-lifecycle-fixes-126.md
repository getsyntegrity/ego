# Feature: engine lifecycle fixes (#126, slice IMPL-1 of #105)

Branch: `fix/126-engine-lifecycle` · Base: `origin/main` `9084b80` · Epic: #10 · Issue: #126 ·
Design: `openspec/changes/ego-arch-003/design.md` (§2.2, §4 V6, §6 row IMPL-1, D7)

## Problem

Issue #126 lists three defects in the engine's lifecycle on `main`:

1. `Engine.Stop` (`engine.go`) marks the engine stopped, then returns on the first publisher `Close`
   error. The other publishers, the event stream and the actor-system detach are skipped, and a second
   `Stop` returns `nil` at once, so they are leaked for good.
2. `Engine.Entity` and `Engine.Saga` on an engine built with `NewConfig(nil, ...)` (no events store,
   valid for durable-state-only deployments) reach a nil-interface `Ping` in the actor's `PreStart`.
   Reproduced on `9084b80`: it still panics after #136/#127, because that `PreStart` path does not go
   through the extension checks — the `EventsStore` extension is registered, it just wraps `nil`. GoAkt
   runs `PreStart` under `singleflight`, so the panic kills the whole test process, not only the spawn.
3. `AddEventPublishers`/`AddStatePublishers` key publishers by `ID()`; a duplicate ID overwrites the
   first entry, whose goroutine and subscriber are never signalled or closed.

## What changes

- `Engine.Stop` attempts every step and returns the `Close` errors joined with `errors.Join`, each
  wrapped with the publisher ID (`close events publisher "<id>": ...`).
- The unexported `spawnEventSourced` and `spawnSaga` (the single spawn paths behind `Entity`/`Saga`, and
  behind S3-3's future public entry points) return the new `ErrEventsStoreRequired` before anything is
  spawned. The name comes from the design (§4, §6 IMPL-1).
- `AddEventPublishers`/`AddStatePublishers` check the whole call before touching anything: IDs repeated
  within the call and IDs already registered for that kind. On a duplicate they return an error wrapping
  the new `ErrDuplicatePublisherID` that names the duplicate IDs, and nothing from the call is registered,
  subscribed or started. The design and issue asked only for "a typed error"; the maintainer chose the
  name and the all-or-nothing batch rule on 2026-09-27 (PR #140).

Rejected alternative for defect 2: a nil guard in the actors' `PreStart`. It would turn the panic into a
failed spawn with a less precise error, and the engine already knows at spawn time, as it does for
`ErrDurableStateStoreRequired`.

## Constraints

- IMPL-1 only: no `compose/`, no public `Spawn*`, no `option.go`, no actor files, no `engine_test.go`.
- Only additive API changes in package `ego` (apidiff).
- TDD: strict (user global configuration); runner `go test` with `GOROOT` unset (go1.27.1 local; no
  `-race`, no workbench).
- Route: direct inline (one source file plus one new test file, design already specifies the fix).

## Tasks

- [x] **T1** RED/GREEN defect 1: `TestEngineStopAttemptsEveryStep` in `engine_lifecycle_fixes_test.go`.
- [x] **T2** RED/GREEN defect 2: `TestSpawnWithoutEventsStore` (entity, saga, durable-state control).
- [x] **T3** RED/GREEN defect 3: `TestAddPublishersRejectsDuplicateIDs` (within a batch and against a
  registered publisher, for both kinds; one ID per kind still allowed).
- [x] **T4** apidiff, archcheck, ciselect, full root suite, nested modules, golangci-lint, CHANGELOG.
- [ ] **T5** One godoc sentence on `SpawnEventSourced`/`SpawnSaga` in `engine_spawn.go` once S3-3 (#141)
  is on `main`.

## Evidence

**T1 RED** (`9084b80` + test): `Error "close failed: events-1" does not contain "close failed: events-2"`;
second event publisher `Close` count 0; state publisher `Close` count 0; event stream still had 2
events-topic and 1 states-topic subscribers; both stream maps non-empty; actor-system reference not
detached. **GREEN**: passes.

**T2 RED**: both subtests crash the test binary with `panic: runtime error: invalid memory address or nil
pointer dereference` at `event_sourced_actor.go:512` (entity) and `saga_actor.go:187` (saga), re-panicked
through `singleflight`. **GREEN**: both return `ErrEventsStoreRequired`, `ActorExists` reports false;
the durable-state control subtest still spawns.

**T3 RED**: first a scratch proof on `9084b80`: `AddStatePublishers(p1, p2)` with equal IDs returned `nil`,
left 1 registered stream but 2 subscribers on the states topic, and after `Stop` `p1` was never closed.
Then the committed test, with only the error variable declared: all four duplicate subtests fail with
`Expected error with "duplicate publisher id" in chain but got nil`; the one-ID-per-kind control passes.
**GREEN**: all five subtests pass; no registration, no new subscriber, and `Stop` closes only the
publisher registered before the rejected call.

**apidiff** (package `ego`, `origin/main` vs head): `Compatible changes: - ErrDuplicatePublisherID: added,
- ErrEventsStoreRequired: added`.
**archcheck**: before and after `37 packages checked, 157 edges checked, 1 baselined, 0 violation(s), 0 stale entries`.

**Verification (T4).** `ciselect -changed ... -base origin/main`: mode `full` (shared root package), 26 of
26 packages, all six nested modules selected. `go vet ./...` clean. Full root suite
(`go test -count=1 ./...`, Go 1.27.1, no `-race`): every package passes, the root package in 370 s.
Nested modules (`scripts/ci/verify-module.sh`, Go 1.26.6, `GOTOOLCHAIN=local`): `benchmark`,
`example/cluster`, `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket` all exit 0.
Root golangci-lint (`--new-from-rev=origin/main .`, Go 1.26.6, local `go mod vendor`, not committed):
`0 issues`. CI is authoritative for lint and the race lane. Rerun after T3 (same commands and
toolchains): full root suite all 23 packages pass (root 370 s), the six nested modules exit 0,
golangci-lint `0 issues`, archcheck unchanged.

## Next step

T5 once #141 is merged: merge `origin/main` (normal merge commit) and add the godoc sentence. PR #140 now
says `Closes #126`.
