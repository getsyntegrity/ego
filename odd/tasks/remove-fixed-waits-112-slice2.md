# Replace async fixed waits with condition-based waiting (#112, slice 2)

## Problem

Slice 1 (PR #114) removed `pause.For(time.Second)` calls that followed a
provably synchronous call (`testkit` `Connect`, non-cluster `Start`), matched
mechanically on the inline call form `require.NoError(t, x.Connect(ctx))` /
`require.NoError(t, actorSystem.Start(ctx))`. The issue's remaining-density
list still showed `saga_test.go` (55) and `durable_state_actor_test.go` (33)
as slice-2 targets: waits meant to guard genuinely asynchronous readiness
(`Spawn`/`Tell`/`Publish`) before an assertion.

## What changed and why

**Scope chosen**: `saga_test.go` + `durable_state_actor_test.go`, the pairing
the issue itself suggested. Investigating both surfaced two different root
causes, not one:

- `durable_state_actor_test.go` has **no** `Tell`/`Publish` at all. Every one
  of its 33 waits follows `Connect`/`Start`/`Spawn`/`ReSpawn`/`Stop` — the
  *two-line* assignment form (`err = x.Start(ctx); require.NoError(t, err)`)
  that slice 1's inline-only regex didn't match. Reading goakt v4.5.4's
  source (`actor/spawn.go`, `actor/pid.go:325-399` `newPID`→`init`, and
  `actor/pid.go:1611` `Shutdown`, `actor/pid.go:3770` `restartSubtree`)
  confirms `Spawn`, `ReSpawn` and `Stop` are all fully synchronous — `Spawn`
  runs `PreStart` (with retry) to completion, including any state recovery,
  before returning; `Shutdown` publishes `ActorStopped` before `Stop`
  returns. These waits are the same category as slice 1, just missed by its
  narrower pattern. All 33 were deleted, not converted.
- `saga_test.go` genuinely uses `Tell`/`Publish` for async delivery:
  `consumeEvents` (`saga_actor.go:378`) runs on its own goroutine but only
  forwards each event into the actor's own mailbox via `Tell`
  (`saga_actor.go:407`); the actual processing (`handleStreamEvent` onward)
  runs on the actor's single-threaded mailbox, serialized with any `Ask`.
  That serialization makes some trailing waits redundant (an `Ask` issued
  after a channel signal from within the same processing chain doesn't need
  extra settling) but not others (a direct, unsynchronized read of a plain
  variable from the test goroutine, or an `IsRunning()` check for a crash
  that could still be a few lines away in that chain, does need one). Each
  of the 55 waits was classified individually against this model.

## Constraints

- TDD: strict (source: user global CLAUDE.md). Runner: `go test` (no
  `-race` locally; CI is the race gate).
- No public-semantics change; every existing readiness/failure assertion is
  preserved, several strengthened (point-check → continuous poll).
- RDD: off (global). Delivery follows ordinary repository policy.

## Tasks

- [x] T1 `durable_state_actor_test.go`: delete all 33 waits (proven
      synchronous). Check: `go test -run '^(TestDurableStateBehavior|TestDurableStateActorTenancyGate|TestDurableStateActorVerifyTenantForPersist|TestDurableStateActorTenancyWritePath)$' -count=3 .` — all green;
      33.18s → 0.07s (count=1).
- [x] T2 `saga_test.go`: delete/convert all 55 waits per the classification
      above (`require`/`assert` `Never`/`Eventually`, a `mock.Run` signal for
      one mock-backed case). Check: `go test -run '^(TestSagaStatus_String|TestSagaActor|TestSagaFailsClosed)$' -count=5 .` — all green;
      `TestSagaActor` 39.37s → 9.78s, `TestSagaFailsClosed` 3.52s → 2.00s.
- [x] T3 Mutation proof (TDD requirement for this slice): break one
      representative case per pattern, observe the new assertion fail, then
      revert. See PR body for the three transcripts (`Eventually`, `Never`
      on a channel, `Never` on `IsRunning` with a mid-window induced crash).
- [x] T4 Full-suite evidence: `ciselect` reports mode `full` for these two
      root-package files (expected — any root `.go` file forces full);
      archcheck unchanged (`15 packages checked, 70 edges checked, 1
      baselined, 0 violation(s), 0 stale entries`); full root package
      `go test -count=1 .` 423.1s → 360.1s (−63.0s, −14.9%) on the same
      machine, same conditions.

## Findings for a human decision

- `durable_state_actor_test.go`'s fix is really a slice-1 leftover
  (mechanical-pattern miss), not new async-readiness work. Flagged here
  rather than silently reclassified.
- `compensate: successful compensation sets SagaCompleted` had a
  `compensated` channel wired into an `applyEvent` hook that the actual
  compensation path (`saga_actor.go:805` `compensate`) never calls on
  success — it only calls `SendSync` and sets `SagaCompleted` directly. The
  channel was already dead code before this change (the original test never
  selected on it either, just blind-slept). Left as `require.Never` on
  `IsRunning` with a code comment; not fixing the dead channel itself, since
  that is a pre-existing test-authoring issue outside this slice's scope.
- No production-code races or bugs were found; every removed/converted wait
  guarded a call already proven synchronous, or was replaced by a real
  condition wait.
