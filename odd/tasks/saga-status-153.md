# Feature: SagaStatus reports the saga lifecycle status (#153)

Branch: `fix/153-saga-status` · Base: `origin/main` `beed644` · Epic: #10 · Issue: #153 ·
Design: `openspec/changes/ego-runtime-001` (follow-up FU-1)

## Problem

`Engine.SagaStatus` (`engine.go`) built `&SagaInfo{ID, State}` and never set `Status`, so a caller
always read the zero value, `SagaRunning`, even for a saga that had completed or failed. The saga
actor tracks its lifecycle status (`SagaActor.status` in `saga_actor.go`) and moves it to
`SagaCompensating`, `SagaCompleted` and `SagaFailed`, but its reply to the status query
(`egopb.GetStateCommand` answered with an `egopb.CommandReply` carrying a `StateReply`) had no field for
it, so the status never left the actor.

## What changes

- `protos/ego/ego.proto` gains a new enum, `SagaLifecycleStatus` (`NONE`, `RUNNING`, `COMPLETED`,
  `COMPENSATING`, `FAILED`), and a new field on `StateReply`, `saga_status = 5`. Additive: no field
  renumbered, removed or retyped; `buf lint` and `buf breaking` (against `origin/main`'s `ego.proto`)
  pass. `egopb/ego.pb.go` is regenerated with the repository's `buf.gen.yaml` (buf v1.69.0, the
  version `Dockerfile.ci` pins, and protoc-gen-go v1.36.12, the version the existing file names).
- `SagaActor.replyWithState` fills `saga_status` from `s.status`.
- `Engine.SagaStatus` reads it back into `SagaInfo.Status`. `NONE` (a reply from a node built before
  the field existed, or any non-saga reply) and any value this build does not know read as
  `SagaRunning`, the previous behavior.
- Docs: `Engine.SagaStatus` describes what each status means and when it is seen; the known-gap notes
  on `runtime.Sagas.SagaStatus` (`port/runtime/runtime.go`) and `runtime.SagaInfo`
  (`port/runtime/saga.go`) are gone. CHANGELOG Bug Fixes entry.

Rejected alternative: a Go-only (non-protobuf) status query message. A saga can be reached by name
across a cluster, where the message must be serializable; the existing query is a protobuf message,
so the status rides on its reply instead of on a new message pair.

Rejected alternative: reusing `StateReply.timestamp` (unset by the saga actor). It would change the
meaning of an existing field.

## Behavior notes (not changed here; saga lifecycle is #18)

- `SagaCompensating` is not observable through `Engine.SagaStatus`: the actor runs a whole
  compensation (behavior `Compensate` plus every compensation command) inside one message, so a query
  queued behind it is answered after the status has moved to `SagaCompleted` or `SagaFailed`.
- A completed or failed saga actor is not stopped; it stays alive (long-lived, ignoring further stream
  events) and keeps answering with its final status.
- The status is not persisted. A saga actor that restarts (supervisor restart, relocation, process
  restart) recovers its state from its events but reports `SagaRunning` again.

## Constraints

- No signature changes; additive protobuf only; apidiff additions only.
- File ownership: `engine.go` only inside `SagaStatus`; `saga_actor.go`; new `saga_status_test.go`;
  `protos/ego/ego.proto` and `egopb/ego.pb.go`; `port/runtime` doc comments; `CHANGELOG.md`.
- TDD: strict (user global configuration); runner `go test` with `GOROOT` unset (no `-race`, no
  workbench).
- Route: direct inline (two small source edits, one generated file, one new test file).

## Tasks

- [x] **T1** RED: `TestEngineSagaStatusReportsLifecycleStatus` (running, completed, compensation
  failed, compensation succeeded) through the public `SpawnSaga`/`Entity`/`SendCommand`/`SagaStatus`.
- [x] **T2** Additive proto field + regeneration; actor reply; engine mapping. GREEN.
- [x] **T3** `TestEngineSagaStatusMapsWireStatus` (stub actor, every wire value including
  `COMPENSATING` and `NONE`) and `TestSagaStatusWireRoundTrip`.
- [x] **T4** Docs (engine, port/runtime), CHANGELOG, apidiff, archcheck, lint, ciselect, full root
  suite, nested modules.

## Evidence

**T1 RED** (`beed644` + test, before the fix):

- `a_saga_whose_action_completes_reports_SagaCompleted`: `expected: 1 actual: 0` (Completed vs Running)
- `a_saga_whose_compensation_fails_reports_SagaFailed`: `expected: 3 actual: 0`
- `a_saga_whose_compensation_succeeds_reports_SagaCompleted`: `expected: 1 actual: 0`
- `a_saga_that_has_not_finished_reports_SagaRunning`: passes on `main` too, since `SagaRunning` is
  the zero value; it pins that the fix does not report a terminal status early.

**T2/T3 GREEN**: all subtests pass; `-count=30` of both engine-level tests passes (1.3s). No sleeps:
the running case waits on a channel the behavior closes, the other cases poll with
`require.EventuallyWithT` (10s bound, 20ms tick).

**T4** see the PR body for apidiff, archcheck, lint, ciselect and suite results.

## Next step

PR against `main`; maintainer review and merge.
