# Move the events writer actor out of the engine package

## Objective

Next cut of the reorganization of the former root package (now `engine`, after #184
to #187): move the GoAkt actor that writes an event-sourced entity's events out of
`engine` into an internal package dedicated to that write, and leave in `engine` only
the coordination `EventSourcedActor` needs.

This cut was first proposed in #188, which was closed without a stated reason when
#192 reset the module path from `github.com/getsyntegrity/ego/v4` to
`github.com/getsyntegrity/ego`. This document restarts it on the current `main`
(`335466f`, the merge of #192). #188's branch is not reused.

## Problem

`engine/events_writer_actor.go` held three things: the child actor that persists a batch
and publishes it only after the store confirms, the request and response messages of
its protocol, and (in `event_sourced_actor.go`) the client function `askEventsWriter`
that builds the request. Everything lived in `engine`, so the write protocol had no
owner of its own. The actor also looked up its extensions with `requireExtension`, whose
error wraps the public `engine.ErrMissingRequiredExtensions`, which a package below
`engine` cannot import.

## What changes

- New package `internal/eventswriter` owns the actor and its protocol:
  - `New() goakt.Actor` creates the actor; its struct stays unexported.
  - `Ask(pid, envelopes, topic, timeout, precondition, scope) (*Response, error)` is the
    former `askEventsWriter`, moved unchanged, so the request type stays unexported.
  - `Response{Err}` is exported because it is the one message that crosses the boundary:
    `Ask` runs inside the parent's `PipeTo`, which delivers the `Response` to
    `EventSourcedActor`'s mailbox.
- `internal/extensions` owns the missing-extension sentinel and the generic lookup
  `Require[T]` (new file `lookup.go`). `engine.ErrMissingRequiredExtensions` is now an
  alias of that value (the pattern `engine` already uses for `runtimeport` errors), so
  `errors.Is`, the message and the public declaration do not change. `engine`'s
  `requireExtension` delegates to it, which leaves the other actors untouched.
- `EventSourcedActor` keeps the spawn (`spawnChildren`, child name `events-writer`,
  `childSpawnOptions` with the restart supervisor), `persistAsync`, `flushBatch`, the
  routing of `*eventswriter.Response` and everything after confirmation (state, metrics,
  replies, shutdown after a failed write, the conflict keep-alive rule).
- `snapshots_writer_actor.go` and `events_janitor_actor.go` stay in `engine` for later cuts.

Dependency direction: `engine -> internal/eventswriter -> {internal/extensions,
persistence, eventstream, egopb, goakt}`. No cycle and no new interface.

## Why this boundary

Before choosing it, every dependency of the writer was traced: the actor uses only
`requireExtension` (and through it the sentinel) from `engine`; every other use of its
types sits in `EventSourcedActor` (spawn, the two `PipeTo` call sites, the `Receive`
switch). So the writer can move with a three-name surface, without taking any of
`EventSourcedActor` with it.

The writer package owns its protocol, so the only exported names are the ones a caller
must use: a constructor, the client call and the reply. Rejected: exporting the request
type so `engine` could keep `askEventsWriter` (it would export a detail only to move the
file, and split the protocol across two packages); injecting the store and stream into
the writer at spawn instead of the extension lookup (it would change when a missing
extension fails and what error it produces); a second sentinel in the new package
(`errors.Is(err, engine.ErrMissingRequiredExtensions)` would stop matching writer
failures).

## Scope

In: `engine/events_writer_actor.go` and its test moved with history to
`internal/eventswriter`, the lookup helper and sentinel in `internal/extensions`,
`EventSourcedActor` integration, tests, this document.
Out: snapshots writer, events janitor, persistence contracts, protobuf, public API,
metric or span names, CI, tags, releases, merge.

## Tasks

TDD: strict (from the user's global configuration). Runner: `go test`, run offline
(`GOPROXY=off`, `GOTOOLCHAIN=local`). No `-race` locally (user rule; CI keeps its own).

- [x] T1 (inline): port #188's sequence test (`engine/events_write_sequence_test.go`)
      with the new import path and see it pass on unchanged `main`. Commit `1ffb22d`.
- [x] T2 (inline): move the writer test to `internal/eventswriter` and add the contract
      tests (success order and topic, conflict, store failure, tenant scope, transport
      failure); RED observed with the production file absent (`undefined: New`,
      `undefined: Ask`).
- [x] T3 (inline): move the actor and `askEventsWriter`, move the sentinel and `Require`
      to `internal/extensions`, rewire `EventSourcedActor`; GREEN. Commit `d364a23`.
- [x] T4: verification (below and in the PR description).

Route note: the delegation writer trigger (2+ non-trivial files) was not applied to the
edits; they are #188's reviewed change re-applied by cherry-pick, with the only
conflicts being the `/v4` import path. The dependency map was delegated to one
read-only explorer.

## Acceptance criteria and checks

- `apidiff -m` over the whole module, base `335466f` vs branch: no change.
- The sequence test passes before and after: success is `write, publish, reply`; a store
  failure and a known conflict are `write, reply` with nothing published.
- Writer contract, extension and full root-module tests pass.
- `go build` and `go vet` pass in every nested module (benchmark, example/cluster,
  test/compat, the four publishers).
- `archcheck` 0 violations; `go list -deps` of the new package contains no `engine`.

## Progress and evidence

Recorded in the PR description.

## Next step

Next cut is requested separately by the user.
