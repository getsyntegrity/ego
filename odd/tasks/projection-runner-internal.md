# Move projection execution out of the engine package

## Objective

Third cut of the reorganization of the former root package (now `engine`, after #184,
#185 and #186): move the projection runner, the code that actually executes a
projection, out of `engine` into one internal package, without changing its behavior
or the public v4 API.

## Problem

`engine/projection_runner.go` and `engine/projection_runner_option.go` held the whole
event-processing machinery (shard polling, worker pool, offsets cache, recovery
policies, panic recovery, dead letters, lag metrics) inside the package that also owns
the GoAkt actors and the `Engine` facade. The runner reached back into GoAkt for one
thing only: it held the host actor's `*goakt.PID` and sent it a `runnerFailed` message
when an unprocessable event stopped the loop. It also read three `engine` symbols
(`DiscardLogger`, `ZeroTime`, `eventsTopic`).

## What changes

- New package `internal/projectionrunner` owns projection execution: `Runner`, `New`,
  `Option` and the `With...` options (exported only inside `internal/`, so they are not
  public API). It imports the public contract packages (`projection`, `persistence`,
  `offsetstore`, `eventstream`, `encryption`, `eventadapter`, `egopb`) and the neutral
  internals `internal/instrumentation` and `internal/ticker`. It never imports `engine`,
  `internal/extensions` or GoAkt.
- The `*goakt.PID` field is replaced by a failure callback passed to
  `Run(ctx, onFailure func(error))`. `ProjectionActor` passes a closure that does exactly
  what the runner did before: `goakt.Tell(context.Background(), pid, &runnerFailed{...})`,
  ignoring a failed delivery.
- `eventsTopic` becomes a parameter: `WithEventsStream(stream, topic)`. The runner keeps
  a package-local discard logger as its construction default; the actor always passes
  the actor system's logger, as before.
- `ProjectionActor`, `runnerFailed`, `newProjectionSupervisor` and
  `projectionRunnerError` stay in `engine`. `Engine.StartProjection`, `StopProjection`,
  `RebuildProjection`, `IsProjectionRunning` and `ProjectionLag` are untouched.

Dependency direction: `engine -> internal/projectionrunner -> {contract packages,
internal/instrumentation, internal/ticker}`. No cycle, no new interface.

## Why this boundary

`projectionRunnerError` stays in `engine` on purpose. GoAkt keys supervisor directives by
the error's type name (`reflect.Type.String()`, here `engine.projectionRunnerError`) and
ships those names to peer nodes with singleton spawns (`internal/codec` in GoAkt). Moving
the type would rename the key and silently break the stop directive in a cluster running
mixed versions during a rolling upgrade. The runner therefore classifies unprocessable
events with its own unexported `eventError` and hands the host the underlying cause; the
actor wraps it in `projectionRunnerError` exactly once, so the error chain, the message
and the supervision key are the same as before.

Rejected: keeping `*goakt.PID` in the runner and exporting `runnerFailed` from the new
package (the execution package would still depend on the actor runtime); a `Notifier`
interface (an interface with one implementation, created only to move a file; a plain
function field states the same contract); exporting `projectionRunnerError` from the
internal package (changes the supervision key, see above).

## Scope

In: `engine/projection_runner*.go` moved with history to `internal/projectionrunner/`,
`engine/projection_actor.go` integration, tests, this document.
Out: entity actors, logging, persistence, CI or archcheck rule changes, `ProjectionActor`
and `Engine` methods, tags, merge.

## Tasks

TDD: strict (from the user's global configuration). Runner: `go test`.

- [x] T1 (inline, mechanical move with an already-decided design): move the runner tests
      to `internal/projectionrunner`, add the failure-callback assertions, observe RED
      (`undefined: Runner`).
- [x] T2 (inline): move `projection_runner.go` and `projection_runner_option.go`,
      replace the PID with the callback and the topic parameter; wire `ProjectionActor`;
      keep `projectionRunnerError` in `engine` and pin its supervision key with
      `TestProjectionSupervisorContract`. GREEN.
- [x] T3: verification (below).

Route note: the delegation writer trigger (2+ non-trivial files) was not applied; the
change is a `git mv` plus identifier renames whose design was settled during exploration,
and the parent already held the full context.

## Acceptance criteria and checks

- Public API identical: `apidiff -m` over the whole module before/after reports no change.
- Runner tests (stop during processing, store retry backoff, retry policies, panic
  recovery, dead letters, offsets, telemetry) and `engine` projection tests pass, also
  under `-race`.
- `archcheck` 0 violations; lint shows no finding in the touched packages.
- An external module compiles the public projection operations and runs
  start / running / rebuild / stop against this checkout.

## Progress and evidence

Work-unit commit: `0c2e1e4` (T1 + T2). Local results, Go 1.26.6:

- `apidiff -m` (whole module, before export taken on f327f63): no change.
- `go test ./internal/projectionrunner/`: ok. With `-race`: ok.
- `go test -run 'Projection|Rebuild|Supervisor|Telemetry' ./engine/`: ok (34.9 s).
  With `-race`: ok (36.1 s). The full `engine` suite was left to CI.
- `go run ./internal/cmd/archcheck`: 8 modules, 58 packages, 233 edges, 0 violations.
- `golangci-lint run ./...` (local 2.13.1, after `go mod vendor`): no finding in
  `engine` or `internal/projectionrunner`; 14 pre-existing `revive var-declaration`
  findings in untouched `command/errors.go` and `tenancy/errors.go`.
- External module with `replace` to this checkout: compiles the pinned signatures of
  `StartProjection`, `StopProjection`, `RebuildProjection`, `IsProjectionRunning`,
  `ProjectionLag`, `WithProjection`; runs start / running / rebuild / unknown name /
  stop: OK. Importing `internal/projectionrunner` from it fails as expected.

The Engram mirror
(`odd/projection-runner-internal/tasks`) is pending: the save was refused because several
active memory sessions matched this directory.

## Next step

Next cut is requested separately by the user.
