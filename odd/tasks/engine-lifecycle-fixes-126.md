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
- Defect 3 is **not fixed in this slice yet**: the design and the issue ask for "a typed error" but do not
  name one, and adding an exported error needs a decision (see Next step).

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
- [ ] **T3** Defect 3 (duplicate publisher IDs): blocked on naming the exported error.
- [x] **T4** apidiff, archcheck, ciselect, full root suite, nested modules, golangci-lint, CHANGELOG.

## Evidence

**T1 RED** (`9084b80` + test): `Error "close failed: events-1" does not contain "close failed: events-2"`;
second event publisher `Close` count 0; state publisher `Close` count 0; event stream still had 2
events-topic and 1 states-topic subscribers; both stream maps non-empty; actor-system reference not
detached. **GREEN**: passes.

**T2 RED**: both subtests crash the test binary with `panic: runtime error: invalid memory address or nil
pointer dereference` at `event_sourced_actor.go:512` (entity) and `saga_actor.go:187` (saga), re-panicked
through `singleflight`. **GREEN**: both return `ErrEventsStoreRequired`, `ActorExists` reports false;
the durable-state control subtest still spawns.

**T3 RED only** (scratch test, not committed): `AddStatePublishers(p1, p2)` with equal IDs returns `nil`,
leaves 1 registered stream but 2 subscribers on the states topic; after `Stop`, `p1` was never closed and
`p2` was.

**apidiff** (package `ego`, `origin/main` `9084b80` vs head): `Compatible changes: - ErrEventsStoreRequired: added`.
**archcheck**: before and after `37 packages checked, 157 edges checked, 1 baselined, 0 violation(s), 0 stale entries`.

**Verification (T4).** `ciselect -changed ... -base origin/main`: mode `full` (shared root package), 26 of
26 packages, all six nested modules selected. `go vet ./...` clean. Full root suite
(`go test -count=1 ./...`, Go 1.27.1, no `-race`): every package passes, the root package in 370 s.
Nested modules (`scripts/ci/verify-module.sh`, Go 1.26.6, `GOTOOLCHAIN=local`): `benchmark`,
`example/cluster`, `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket` all exit 0.
Root golangci-lint (`--new-from-rev=origin/main .`, Go 1.26.6, local `go mod vendor`, not committed):
`0 issues`. CI is authoritative for lint and the race lane.

## Next step

Decide the exported error for defect 3 (proposal: `ErrDuplicatePublisherID`, returned by both
`AddEventPublishers` and `AddStatePublishers`, validating the whole batch before starting any
publisher), then add T3 to this branch and switch the PR from `Refs #126` to `Closes #126`.
