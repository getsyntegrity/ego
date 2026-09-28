# Separate the GoAkt logging adapter from the engine package

## Objective

First cut of the reorganization of the former root package (now `engine`, after #184):
move the adapter that implements GoAkt's `log.Logger` out of `engine/logger.go` into
an internal, GoAkt-specific package, and leave only the public logging facade in
`engine`.

## Problem

`engine/logger.go` mixed two things: the public facade every consumer uses
(`DiscardLogger`, `DefaultLogger`, `ResolveLogger`, with `WithLogger` in `option.go`)
and roughly 250 lines of GoAkt glue (`loggerAdapter`, level mapping, argument
splitting, `Flush`, `StdLogger`, `kitLoggerFrom`). The glue is runtime-specific; the
facade is not. Keeping them in one file ties any later split of `engine` to the
adapter.

## What changes

- New package `internal/goaktlog` (`adapter.go`) holds the adapter, unchanged in
  behavior. It exports two functions: `New(kitlog.Logger) log.Logger` (was
  `newLoggerAdapter`) and `Backend(log.Logger) kitlog.Logger` (was `kitLoggerFrom`).
  The adapter type stays unexported.
- `engine/logger.go` keeps only the public facade, delegating to `internal/logging`.
- `engine/option.go` (`Config.GoaktOptions`) and the four actors that recover the
  backend (`saga_actor.go`, `events_janitor_actor.go`, `snapshots_writer_actor.go`,
  `projection_actor.go`) call `goaktlog.New` / `goaktlog.Backend`.
- `internal/logging` is unchanged in code; it gains tests, including a guard that its
  transitive dependency closure never contains GoAkt or `internal/goaktlog`.
- `TestKitLoggerIsTheOnlyLoggingBackend` now names `internal/goaktlog/adapter.go` as
  the only file allowed to import `goakt/v4/log`.

Dependency direction: `engine -> internal/goaktlog -> internal/logging`, and
`migration -> internal/logging`. `internal/goaktlog` never imports `engine`, so there
is no cycle.

## Why `internal/goaktlog`

`ego-runtime-001` design §3 ("Internals" row) sends `loggerAdapter` to the GoAkt
adapter and leaves the final package name to #124. An `internal/` package keeps the
choice reversible and invisible to consumers. Rejected: `internal/logging/goakt`
(a subpackage reads as if the neutral package owned GoAkt code), and keeping a root
facade (#184 removed every Go file from the module root and `archcheck` now enforces
it, so the public facade stays in `engine`).

## Scope

In: the files above, their tests, this document.
Out: moving `Engine`, actors, projections, telemetry or any other file; a generic
utils package; tags; merge.

## Constraints

- TDD: strict (source: user global CLAUDE.md), runner `go test` (no `-race`).
- RDD: off (global). No native review.
- Delivery: single PR.

## Tasks

- [x] T1 Move the adapter tests to `internal/goaktlog` (RED: `undefined: loggerAdapter`),
  move the adapter (GREEN). Route: inline (mechanical relocation, already mapped).
- [x] T2 Reduce `engine/logger.go` to the facade; rewire `option.go` and the four
  actors; keep an `engine` test helper `newLoggerAdapter` so ~200 test call sites stay
  untouched. Route: inline.
- [x] T3 Facade and wiring tests in `engine` (default, explicit, nil, typed-nil via
  `GoaktOptions`); neutrality tests in `internal/logging`; retarget the logger
  architecture seam. Route: inline.
- [x] T4 Gates: full root-module tests, archcheck, lint, apidiff, external consumer.

## Verification evidence

See the pull request description for command output.
