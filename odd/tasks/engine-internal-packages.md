# Split the engine package into internal packages by behavior

## Objective

Reorganize `engine` so each actor's implementation lives in its own internal package and
the dependency direction is obvious, without changing behavior, observable errors, cluster
wire identity or the public API.

## Problem

`engine` holds about 8k lines of production code in one package: the `Engine` facade
(`engine.go`, 2034 lines), four GoAkt actors, three persistence child actors of the event
sourced actor, and the helpers they share. To find how an event gets persisted you have to
read past saga, durable state and projection code; nothing states which pieces depend on
which.

## Constraint that shapes the design: GoAkt names types by package name

GoAkt v4.5.4 names an actor kind `lower(reflect.TypeOf(actor).Elem().String())`, so
`engine.EventSourcedActor` travels as `engine.eventsourcedactor` in spawn, relocation and
singleton records (`internal/types/registry.go:114` in GoAkt). It also keys supervisor
directives by the error's `reflect.Type.String()` (`engine.projectionRunnerError`) and ships
them to peers. Moving those types to another package would rename them and break a cluster
running mixed versions during a rolling upgrade.

So the four cluster kinds and `projectionRunnerError` stay declared in `package engine`.
Each kind becomes a small type with one unexported field holding the internal
implementation and the three `goakt.Actor` methods (`PreStart`, `Receive`, `PostStop`)
delegating to it. Its public shape (a struct with only unexported fields, same method set)
does not change. Rejected: embedding the internal type (adds an exported embedded field of
an internal type to the public API); naming the internal packages `engine` (misleading
imports); accepting the wire rename (breaks rolling upgrades).

The three persistence children (`eventsWriterActor`, `snapshotsWriterActor`,
`eventsJanitorActor`) move with the event sourced actor. They are not cluster kinds, are
spawned locally by their parent and are never resolved by name on a peer. The only visible
effect is the kind label on GoAkt's optional lifecycle metrics (`spawned`, `stopped`,
`passivated`), which changes from `engine.eventswriteractor` to
`eventsource.eventswriteractor` if the user enables GoAkt metrics. Ego does not enable
them. Keeping those labels would require declaring the children in `engine` and injecting
them back into `eventsource`, which reverses the dependency this change exists to clarify.

## Package map

| Package | Responsibility |
|---|---|
| `engine` | Public API: `Engine`, `Config` and options, aliases, public errors, `ClusterKinds()`, the four cluster-kind types and `projectionRunnerError` |
| `internal/engine/protocol` | The command protocol between `Engine` and its actors: command metadata carrier in the context, deadline gate, command reply parsing and error classification, tenant binding answer, event and state stream topics |
| `internal/engine/eventsource` | Event sourced actor and its children: events writer, snapshots writer, events janitor (retention), retry with backoff. They share private messages, so they move together |
| `internal/engine/durablestate` | Durable state actor |
| `internal/engine/saga` | Saga actor and saga status conversion to and from protobuf |
| `internal/engine/projection` | Projection host actor around `internal/projectionrunner`; the escalation error is built by a hook that `engine` sets |
| `internal/extensions` (extended) | Typed extension lookup, `behaviorFrom`, and the sentinels `ErrMissingRequiredExtensions` and `ErrEntityTenantScopeMissing` (re-exported by `engine` as the same values) |

Dependency direction:

```
engine ──> internal/engine/{eventsource, durablestate, saga, projection}
                 └──> internal/engine/protocol, internal/extensions,
                      internal/{instrumentation, goaktlog, projectionrunner, runner, ...},
                      port/*, contract packages, goakt
engine ──> internal/engine/protocol, internal/extensions
```

No internal package imports `engine`; the compiler forbids it (engine imports them). No
generic `common` package: every shared helper has a named home.

## Scope

In: moving actor code and its package-internal tests, the shared helpers, splitting
`engine.go` by responsibility, an archcheck extension so application, composition and
adapter layers cannot import `internal/engine/...`, architecture docs.
Out: behavior changes, public API changes, `internal/projectionrunner` layout, CI workflow
changes, tags, releases, merge.

## Tasks

TDD: strict (user global configuration). Runner: `go test` (no `-race`, user rule).
A behavior-preserving move has no meaningful RED; the evidence is the characterization
suite (unchanged assertions) green before and after each slice, plus boundary pins.
Route: delegated direct (writer trigger: every task touches 2+ non-trivial files).
Delivery: single PR with one work-unit commit per slice (explicit user request).

- [x] T1 `internal/engine/protocol` and the `internal/extensions` helpers; `engine` rewired.
- [x] T2 `internal/engine/eventsource` with the cluster-kind wrapper and a wire-name pin.
- [x] T3 `internal/engine/durablestate` and `internal/engine/saga` (one commit each).
- [x] T4 `internal/engine/projection` (supervision key pinned) and `engine.go` split by responsibility.
- [ ] T5 archcheck rule for `internal/engine/...`, architecture docs, full verification, PR.

## Acceptance criteria

- `go build ./...`, `go vet ./...`, `go run ./internal/cmd/archcheck`, root tests and the
  nested consumers (`compose`, `example/*`, `publisher/*`, `benchmark`, `test/compat`)
  build and pass.
- `apidiff` of `engine` against the base reports no incompatible change.
- `ClusterKinds()` names stay `engine.eventsourcedactor`, `engine.durablestateactor`,
  `engine.sagaactor`, `engine.projectionactor`; the projection supervisor key stays
  `engine.projectionRunnerError`.

## Progress

Base: `origin/main` at `335466f`. Baseline: build, vet, archcheck (59 packages, 0
violations) and `go test ./engine/... ./internal/...` green (engine 341 s).

T1 (commit: work unit 1, moves the command protocol and extension helpers; no actor moved): `go build ./...`, `go vet ./engine/... ./internal/...`, archcheck (60 packages, 0 violations), `go test -count=1 ./internal/... ./engine/...` (engine 341 s) and `gofmt -l` all clean. `golangci-lint` could not run (vendor/modules.txt inconsistent with go.mod, tooling failure). Route: delegated direct (one writer). Review tier: not assessed here (RDD switch is user-owned).

T2 (work unit 2): the event sourced actor and its three children moved to `internal/engine/eventsource` as `eventsource.Actor` (exported methods `PreStart`, `Receive`, `PostStop`; constructor `eventsource.New`). `engine/cluster_kinds.go` keeps `EventSourcedActor` as a wrapper around it and now holds `ClusterKinds()`. The wire-name pin `TestClusterKindsExposesEgoActors` (`engine.eventsourcedactor`, `engine.durablestateactor`, `engine.sagaactor`, `engine.projectionactor`) passed on the old code first and after the move. Shared fixtures went to `internal/engine/enginetest` (account and tenancy probe behaviors, failing-HandleEvent behavior, envelope-capturing behavior, mistyped extension, discard logger); `ExpectedRevisionFromContext` and `PreconditionFromRevision` moved to `protocol` because durable state also uses them. Checks: `go build ./...`, `go vet ./engine/... ./internal/...`, archcheck (62 packages, 0 violations), `go test -count=1 ./...` (engine 121 s), `gofmt -l` and `golangci-lint run ./engine/... ./internal/...` (0 issues, after `go mod vendor`) clean; nested modules build and vet. Route: delegated direct (one writer).

T3 (work units 3 and 4, one commit each): `durablestate.Actor` and `saga.Actor` moved out of `engine`; `DurableStateActor` and `SagaActor` are wrappers in `engine/cluster_kinds.go`, and the wire-name pin still passes. The saga package also owns `StatusToProto`/`StatusFromProto` and the no-op action check. `saga.Actor.PreStart` now creates its stop channel when `New` did not, because the wrapper spawns the zero value. More shared fixtures went to `internal/engine/enginetest` (durable account and tenancy probe, bad version, envelope capturing durable, callback saga, simple reply actor, `MustAny`); `noTenantContext` is gone from `engine`. Checks per commit and at the end: `go build ./...`, `go vet ./engine/... ./internal/...`, archcheck (64 packages, 0 violations), `go test -count=1 ./internal/... ./engine/...`, `gofmt -l`, `golangci-lint run ./engine/... ./internal/...` (0 issues).

T4 (work units 5 and 6, one commit each): `projection.Actor` moved to `internal/engine/projection`; `ProjectionActor`, `projectionRunnerError`, `newProjectionSupervisor` and `NewProjectionActor` stay in `engine`, and `ProjectionActor.PreStart` binds the escalation hook (`Actor.SetEscalation`) before delegating, so the runner cause is wrapped once in `projectionRunnerError` as on the base. The supervision and escalation tests stayed in `engine`; `TestProjectionSupervisorContract` still pins the `engine.projectionRunnerError` key. Then `engine/engine.go` was split into `engine.go`, `errors.go`, `entities.go`, `spawn_tenancy.go`, `commands.go`, `sagas.go`, `projections.go`, `streams.go` and `spawn_options.go` as a pure move: a script that hashes every top-level declaration of package `engine` (source bytes including doc comment) gave identical output before and after (148 declarations), and `go doc -all ./engine` is byte-identical. Checks: `go build ./...`, `go vet ./engine/... ./internal/...`, archcheck (65 packages, 0 violations), `go test -count=1 ./...`, `gofmt -l`, `golangci-lint run ./engine/... ./internal/...` (0 issues), nested modules build and vet.

## Pending risks

- The three persistence child actors (events writer, snapshots writer, events janitor) now report the GoAkt lifecycle metric kind label `eventsource.<name>` instead of `engine.<name>`. It only shows if a user enables GoAkt's optional metrics; eGo does not.
- A saga actor built by reflection (cluster relocation) has a nil `stopCh`, so `PostStop` runs `close(nil)` and panics. This exists on the base commit and is preserved on purpose (`newSagaActor` in `engine/cluster_kinds.go` builds local sagas through `saga.New` as before). Fix it separately.

## Next step

T5.
