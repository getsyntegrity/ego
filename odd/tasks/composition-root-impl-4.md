# Feature: GoAkt composition root `compose/goakt` (#105, slice IMPL-4)

Branch: `feat/105-impl-4-compose-goakt` · Base: `origin/main` `5a5621d` · Epic: #10 · Issue: #105 ·
Design: `openspec/changes/ego-arch-003/design.md` (§D1–§D7, §5.1, §6 row IMPL-4, §7)

## Problem

Every consumer of Ego assembles the runtime by hand in `main()` — `ego.NewConfig`, `goakt.NewActorSystem`,
`ego.NewEngine`, publishers, projections — and the examples leak a running actor system or discard errors
when a later step fails (design §2.2). IMPL-2 added the runtime-neutral `compose.Spec` and IMPL-3 the
ordered start/stop sequencer; nothing yet turns a `Spec` into a running GoAkt deployment.

## What changes in this slice

A new package, `compose/goakt` (import it as `egoakt`), is the GoAkt composition root:

- `New(spec, opts...)` runs `spec.Validate` (V1–V7) plus two GoAkt rules, G1 (`WithCluster` needs a
  cluster configuration and at least one behavior kind) and G2 (`Spec.Name` is a valid GoAkt actor-system
  name), and returns every problem at once. It does no I/O and starts nothing.
- `App.Start(ctx)` runs five steps on the IMPL-3 sequencer: probe the stores, start the actor system (it
  allocates the event stream and hands it in through `ego.WithEventStream`), start the engine, attach the
  publishers, start the projections. A failing step undoes its own half-start; the sequencer undoes the
  earlier steps in reverse and then closes every publisher that was never attached.
- `App.Stop(ctx)` stops projections, then the engine (publishers and stream), then the actor system (D7).
- Options: `WithLogger`, `WithTelemetry`, `WithCluster(cfg, kinds ...ego.BehaviorKind)`,
  `WithActorSystemOptions`.

Two additive `ego` options: `WithEntityFamilies` (the engine refuses to spawn an undeclared family, with
`ErrEntityFamilyNotDeclared`; the guard sits in the unexported `spawnEventSourced`/`spawnDurableState`/
`spawnSaga`, so the deprecated and new entry points share one copy) and `WithEventStream`.

Maintainer decisions carried in (2026-09-27): the sequencer checks `ctx.Err()` before each start step
(`compose/internal/lifecycle`); step 2 cleans up its own half-start; a negative `Spec.ShutdownTimeout` is
rule **V7** of `Spec.Validate`, so it fails at `New` (V-numbering continues the design's D4a list, since
the rule is runtime-neutral).

## Why this shape

- G1 and G2 errors: G2 is about a `Spec` field, so it is a `*compose.ValidationError{Rule: "G2", Field:
  "Name"}` like V1–V7. G1 is about an option, not a `Spec` field, so it is two sentinels
  (`ErrClusterConfigRequired`, `ErrClusterKindsRequired`); reusing `ValidationError` would print
  "Spec.WithCluster", which is not a field. G2 repeats GoAkt's name regex instead of calling
  `NewActorSystem` at `New`, so `New` constructs nothing; a test compares both on the same names.
- Test seams are two unexported hooks on `App` (`newEventStream`, `afterStep`), used only by the
  package's own tests to observe the stream and inject a failure at the end of each step. Rejected: an
  exported hook or option, which would leak test machinery into the public surface.
- `ego.EntityFamily` is its own bit set rather than `compose.Family`: package `ego` may not import
  `compose` (composition-leaf rule). `compose/goakt` maps one onto the other.

## Constraints

- Only IMPL-4. No example migration (IMPL-5), no `compose/inmem` (IMPL-6). No release tag; no v4 break.
- Do not touch `internal/cmd/ciselect`, `scripts/`, `.github/`, publishers, `port/`.
- No archcheck baseline entry.
- TDD strict (user global configuration); runner `go test` with `GOROOT` unset (go1.27.1), `-timeout`
  bounded; no `-race`, no workbench.
- Route: direct inline (one writer, design fully specified).
- Delivery: one PR. It exceeds the ~400-line heuristic mostly through tests; a split into (a) lifecycle
  ctx check + V7 and (b) `ego` options + `compose/goakt` was possible but not done, to keep the slice as
  the design's single IMPL-4 row.

## Tasks

- [x] **T1** Lifecycle `ctx.Err()` check before each step (decision 1). RED/GREEN.
- [x] **T2** `Spec` rule V7, negative `ShutdownTimeout` (decision 3). RED/GREEN.
- [x] **T3** `ego.WithEntityFamilies` + guard in the three unexported spawn functions, `ego.WithEventStream`.
  RED/GREEN.
- [x] **T4** `compose/goakt` (`New`, `App`, options, G1/G2), per-step failure tests including step 2's
  half-start (decision 2), D7 order and open-question tests. RED/GREEN.
- [x] **T5** archcheck, apidiff, lint, ciselect + full suite, nested modules, CHANGELOG, PR.

## Acceptance criteria (IMPL-4 row of design.md §6) and their tests

| Criterion | Test |
|---|---|
| End-to-end wiring with testkit stores | `compose/goakt` `TestApp_ValidSpecRunsAnEngine` |
| Missing required dependency fails at `New`, nothing started | `TestNew_MissingRequiredDependencyFailsWithNothingStarted`, `TestNew_StartsNothing` |
| Failure at each of the five steps: no running actor system, closed stream, every publisher closed | `TestStart_FailureAtEachStepReleasesEverything` (post-work injection, so step 2's half-start is covered), `TestStart_ProbeFailureNamesTheStore` |
| `Stop` on a never-started `App` closes its publishers | `TestStop_NeverStartedClosesPublishers` |
| Undeclared family returns the typed error (old and new entry points) | root `TestWithEntityFamilies_*`, `compose/goakt` `TestEngine_UndeclaredFamilyReturnsTypedError` |
| Shutdown order matches D7 | `TestStop_OrderMatchesD7` |
| `Stop` after `Stop` is a no-op | `TestStop_AfterStopIsNoOp` |
| Cluster `Spec` without entity kinds fails G1 | `TestNew_G1_ClusterRequiresEntityKinds`, `TestNew_ReportsEveryProblem` |
| D7 open question recorded, not decided | `TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown` |
| Decision 1 (ctx check) | lifecycle `TestStart_ChecksContextBeforeEachStep`, `compose/goakt` `TestStart_CancelledContextStartsNothing` |
| Decision 3 (V7) | `compose` `TestSpecValidate_V7_NegativeShutdownTimeout`, `compose/goakt` `TestNew_NegativeShutdownTimeoutFailsAtNew` |

## D7 open question — observed answer (not a policy)

A durable-state actor flushes its state in `PostStop`. Under D7's order the engine closes publishers and the
event stream (step 3) before the actor system stops (step 4). Observed on this branch: the flush **is
written to the state store** (exactly one extra `WriteState` during `Stop`) but **is not delivered to the
state publisher** (zero states delivered during `Stop`). The flush/drain policy stays with #24 (`LIFE-004`).

## Verification evidence

See the PR description for the command outputs; summary:

- RED: lifecycle ctx tests failed (`Start = <nil>`); V7 test failed (0 problems); family tests failed (16
  subtests spawned instead of rejecting); `compose/goakt` tests failed against a stub `New`/`Start`/`Stop`.
  `WithEventStream` tests failed to compile before the option existed.
- GREEN: `go test ./compose/...` and the root family tests pass; `-count=30` on `./compose/...` passes.
- archcheck before `8 modules, 44 packages, 186 edges, 1 baselined, 0 violation(s), 0 stale`; after
  `8 modules, 45 packages, 192 edges, 1 baselined, 0 violation(s), 0 stale`. No baseline entry.
- apidiff `ego` vs `origin/main`: compatible additions only (`EntityFamily`, `EventSourcedFamily`,
  `DurableStateFamily`, `SagaFamily`, `ErrEntityFamilyNotDeclared`, `WithEntityFamilies`,
  `WithEventStream`); `compose`: no change.

## Next step

IMPL-5 migrates `example/eventssourced` to `compose/goakt`.
