# Separate telemetry instrumentation from the engine implementation

## Objective

Second cut of the reorganization of the former root package (now `engine`, after #184
and #185): move the construction of OpenTelemetry instruments and the helpers that
record operations out of `engine` into one internal package, and leave in `engine`
only the public configuration (`Telemetry`, `WithTelemetry`) and the call sites.

## Problem

`engine/telemetry.go` mixed the public configuration type with the private instrument
catalog (`metrics`, `newMetrics`). Span names, attribute keys and the propagator setup
were spelled inline in `engine.go`, `durable_state_actor.go`, `event_sourced_actor.go`
and `projection_runner.go`, each repeating `if x.metrics != nil { x.metrics.<field>... }`.
The telemetry contract (names, descriptions, attribute keys) had no single owner and no
test pinning it end to end.

## What changes

- New package `internal/instrumentation` owns ego's telemetry contract: the instrument
  catalog (`New(meter) *Instruments`), nil-safe recording methods (`CommandReceived`,
  `CommandCompleted`, `EventsPersisted`, `EntityStarted/Stopped`,
  `ProjectionStarted/Stopped`, `ProjectionEventHandled`, `Shard(...).Record`), the span
  helpers (`StartCommandSpan`, `StartSendCommandSpan`, `EndSendCommandSpan`) and
  `InstallPropagator`. It imports only OpenTelemetry and protobuf; never `engine`,
  `internal/extensions` or GoAkt.
- `engine/telemetry.go` keeps only the public `Telemetry` type. `WithTelemetry` in
  `option.go` is unchanged.
- The GoAkt integration (`internal/extensions.TelemetryExtension`, registered by
  `Config.GoaktOptions` and looked up by the actors) stays where it is: it already
  lives in the GoAkt adapter's internal package.

Dependency direction: `engine -> internal/instrumentation -> otel`,
`engine -> internal/extensions -> otel`. No cycle and no new interface.

## Why this boundary

Rejected: moving `TelemetryExtension` into the new package (it implements a GoAkt
interface, so the neutral package would import GoAkt); caching one `Instruments` in the
extension (it would change how many times the configured `Meter` is asked for
instruments, which a custom meter can observe); keeping the fields exported and the
`if != nil` checks at the call sites (it exports the catalog layout instead of the
operations).

## Scope

In: `engine/telemetry.go`, the call sites above, `internal/instrumentation`, tests, this
document. Out: moving logging, actors, projections or `Engine`; a generic utils package;
tags; merge.

## Constraints

- TDD: strict (source: user global CLAUDE.md), runner `go test` (race only in the
  dedicated gate step, never added to scripts).
- RDD: off (global). No native review.
- Delivery: single PR.

## Tasks

- [x] T1 Characterization: `engine/telemetry_contract_test.go` pins spans (names,
  attributes, parentage, error status), instrument catalog, measurement attribute keys,
  creation counts, propagator, and the no-telemetry case; green on `origin/main`.
  Route: inline.
- [x] T2 `internal/instrumentation` with its own tests (RED first) and a neutrality guard.
  Route: inline (one package, already mapped).
- [x] T3 Rewire `engine` call sites; reduce `engine/telemetry.go` to the public type.
  Route: inline (mechanical, mapped call sites).
- [ ] T4 Gates: contract diff main vs branch, full tests, race on touched packages,
  archcheck, lint, apidiff, external consumer; open PR.

## Verification evidence

See the pull request description.
