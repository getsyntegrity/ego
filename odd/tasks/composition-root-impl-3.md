# Feature: ordered lifecycle sequencer with rollback (#105, slice IMPL-3)

Branch: `feat/105-impl-3-lifecycle` · Base: `origin/main` `27848da` · Epic: #10 · Issue: #105 ·
Design: `openspec/changes/ego-arch-003/design.md` (§D1, §D5, §D6, §D7, §6 row IMPL-3, §7, §9)

## Problem

A composition root has to start several things in a fixed order and, when one of them fails, undo the
ones that already started. Today every consumer does this by hand in `main()`, and the examples leak a
running actor system when a later step fails (design §2.2). The design puts that sequencing in one
runtime-free package, `compose/internal/lifecycle`, shared by `compose/goakt` (IMPL-4) and the future
`compose/inmem` (IMPL-6).

## What changes in this slice

A new internal package, `compose/internal/lifecycle`, provides `Sequence`: an ordered list of named
steps, each with a `Start` function and an optional `Stop` function, plus an optional `Release`
function for resources no step owns yet (the "publishers not yet attached" of D5/D6).

- `Start(ctx)` runs the steps in order. When step *k* fails, it calls `Stop` on steps *k*−1 down to 1,
  then `Release`, collecting every cleanup error, and returns a `*compose.StartError` with `Step`,
  `Err` and `Rollback` filled in. The failing step is not stopped: it must clean up after itself.
- `Stop(ctx)` stops every started step in reverse order, attempting every one even when some fail, and
  joins the errors. On a sequence that never started it only calls `Release`.
- State is single-use: `New → Starting → Running → Stopping → Stopped`, plus a terminal `Failed`.
  `Start` outside `New` returns `ErrNotStartable`. `Stop` after `Stopped` or `Failed` is a no-op.
  `Start` and `Stop` are serialized by a mutex.
- Rollback and `Stop` both run under one cleanup context: `context.WithoutCancel(ctx)` bounded by the
  shutdown timeout. Caller values survive, caller cancellation does not.

## Why this shape

The shutdown timeout's default is an open decision (design §9). This slice does not decide it: it uses
the design's suggested 30s as `DefaultShutdownTimeout`, applied only when the configured timeout is
zero, so `compose/goakt` passes `Spec.ShutdownTimeout` straight through and the default stays one
constant to change. Flush/drain policy (#24, `LIFE-004`) is untouched: the sequencer only orders calls.

Originally rejected alternative, since adopted: checking `ctx.Err()` between start steps inside the
sequencer. IMPL-3 left it out because the design does not specify it and each step already receives the
caller's `ctx`. The maintainer approved it on 2026-09-27, and IMPL-4 (#105) adds it: `Start` checks
`ctx.Err()` before each step and, when the context is done, fails the step about to run without calling
it, returning a `*compose.StartError` naming that step and rolling back the steps already started
(`TestStart_ChecksContextBeforeEachStep`). The reason: a step that ignores its context would otherwise
start one more component that rollback must immediately undo.

## Constraints

- Only IMPL-3: no GoAkt, no `ego` import, no `compose/goakt`.
- No archcheck baseline entry; the package is covered by `composition-no-runtime` as it is.
- Do not touch `engine.go`, `option.go`, actors, `port/`, `internal/cmd/*`, `scripts/`, `.github/`,
  `docs/ci.md`.
- TDD: strict (user global configuration); runner `env -u GOROOT go test` (go1.27.1 local; no `-race`,
  no workbench).
- Route: direct inline (one package, already-specified by the design).

## Tasks

- [x] **T1** RED: ordered-fakes tests against a stub `Sequence`. Check: tests compile and fail.
- [x] **T2** GREEN: implement `Sequence`. Check: `go test ./compose/...` passes, gofmt, vet.
- [x] **T3** archcheck before/after, `go list -deps`, ciselect, golangci-lint.
- [x] **T4** `CHANGELOG.md`, this document, commit, PR. Evidence: the slice's work-unit commit on
  `feat/105-impl-3-lifecycle` (the PR head).

## Acceptance criteria (IMPL-3 row of design.md §6)

1. Ordered start with fakes.
2. A failure at each step rolls back exactly the earlier steps, in reverse, then releases.
3. Stop with a failure at each step still runs the rest; errors joined.
4. Cleanup runs under `WithoutCancel` plus the timeout even when the caller's context is cancelled.
5. Single-use state transitions; `Stop` idempotent.

## Verification evidence

- RED: all 15 tests in `compose/internal/lifecycle/lifecycle_test.go` failed against a stub whose
  `New`/`Start`/`Stop` returned nil without running anything.
- GREEN: `go test ./compose/...` passes (`compose`, `compose/internal/lifecycle`); `-count=50` on the
  lifecycle package passes (no sleeps; the serialization test uses channels only). `go vet`, gofmt clean.
- archcheck before (`origin/main` `27848da`): `36 packages checked, 156 edges checked, 1 baselined, 0
  violation(s), 0 stale entries`. After: `37 packages checked, 157 edges checked, 1 baselined, 0
  violation(s), 0 stale entries`. No baseline entry.
- `go list -deps ./compose/internal/lifecycle`: no GoAkt, no root `ego` package, no
  `internal/extensions` (it reaches only `compose`, the contract packages `compose` imports, and
  protobuf through `egopb`).
- ciselect `-base $(git merge-base origin/main HEAD)`: mode `affected`, 1 of 26 packages selected
  (`compose/internal/lifecycle`), no nested module selected.
- golangci-lint 2.13.1 (go1.26.6, `--modules-download-mode=mod --new-from-rev=origin/main
  ./compose/...`): 0 issues. CI is authoritative.
- Go: `go1.27.1` from `PATH` with `GOROOT` unset.

## Review round 1 (PR #137, approve with nits)

- [x] **R1** `New` rejects a negative `ShutdownTimeout` instead of mapping it to the default
  (`TestNew_RejectsNegativeShutdownTimeout`, RED then GREEN).
- [x] **R2** `Sequence` godoc: a `Stop` arriving during a blocked `Start` waits on the mutex, unbounded by
  the shutdown timeout (cancel the start context to unblock it); a step calling `Start`/`Stop` on its own
  sequence deadlocks.
- [x] **R3** A panicking step now rolls back, runs `Release` and marks the sequence `Failed` before the panic
  continues (a deferred check, no `recover`); cleanup errors on that path are dropped
  (`TestStart_PanickingStepRollsBackReleasesAndFails`, RED then GREEN, no timing). A panic inside an
  undo step during a normal rollback is not handled (the failure branch marks itself settled first so
  cleanup never runs twice).
- [x] **R4** `ShutdownTimeout` godoc: it bounds the whole cleanup (every undo plus `Release`), not each step.
- Not done in IMPL-3: no `ctx.Err()` check between start steps (maintainer decision pending then;
  approved 2026-09-27 and added in IMPL-4).

## Carried to IMPL-4 or a follow-up

- `compose.Spec.Validate` does not reject a negative `ShutdownTimeout`, while its godoc says zero means
  the default. `lifecycle.New` now rejects it, so IMPL-4 fails late (at `New` of the sequence) unless it
  adds a Spec rule or checks it itself. Candidate: a Spec rule in IMPL-4 or a follow-up; not added here
  because it is `compose`'s public surface.
- IMPL-4 step 2 (create and start the actor system, allocate the event stream) must clean up after
  itself when it half-starts (for example actor system created but `Start` failed, or started but a later
  part of the step failed): the sequencer never stops the step that failed. Add that half-start case to
  IMPL-4's per-step failure tests.

## Next step

IMPL-4 (`compose/goakt`) builds its five `App.Start` steps and the publisher `Release` on this
sequencer, passing `Spec.ShutdownTimeout` through as `Config.ShutdownTimeout`.
