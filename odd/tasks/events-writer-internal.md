# Move the events writer actor out of the engine package

## Objective

Fourth cut of the reorganization of the former root package (now `engine`, after #184
to #187): move the GoAkt actor that writes an event-sourced entity's events out of
`engine` into an internal package dedicated to that write, and leave in `engine` only
the coordination `EventSourcedActor` needs.

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
  `Require[T]`. `engine.ErrMissingRequiredExtensions` is now an alias of that value (the
  pattern `engine` already uses for `runtimeport` errors), so `errors.Is`, the message and
  the public declaration do not change. `engine`'s `requireExtension` delegates to it,
  which leaves the other actors untouched.
- `EventSourcedActor` keeps the spawn (`spawnChildren`), `persistAsync`, `flushBatch`, the
  routing of `*eventswriter.Response` and everything after confirmation (state, metrics,
  replies, shutdown after a failed write).
- `snapshots_writer_actor.go` and `events_janitor_actor.go` stay in `engine` for later cuts.

Dependency direction: `engine -> internal/eventswriter -> {internal/extensions,
persistence, eventstream, egopb, goakt}`. No cycle and no new interface.

## Why this boundary

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
metric or span names, CI, tags, merge.

## Tasks

TDD: strict (from the user's global configuration). Runner: `go test`.

- [x] T1 (inline): pin the observable sequence of a write at the `EventSourcedActor`
      level (`engine/events_write_sequence_test.go`) and see it pass on unchanged `main`.
      Commit `22dd6c3`.
- [x] T2 (inline): move the writer test to `internal/eventswriter`, add the contract
      tests (success order and topic, conflict, store failure, tenant scope, transport
      failure), observe RED (`undefined: New`).
- [x] T3 (inline): move the actor and `askEventsWriter`, move the sentinel and `Require`
      to `internal/extensions`, rewire `EventSourcedActor`. GREEN.
- [x] T4: verification (below).

Route note: the delegation writer trigger (2+ non-trivial files) was not applied; the
change is a `git mv` plus a mechanical rewire whose design was settled during exploration,
and the parent already held the full context.

## Acceptance criteria and checks

- `apidiff -m` over the whole module, base `be2f125` vs branch: no change.
- The sequence test passes before and after: success is `write, publish, reply`; a store
  failure and a known conflict are `write, reply` with nothing published.
- Writer contract, extension and focused `engine` tests pass, also under `-race`.
- `archcheck` 0 violations; lint shows no finding in the touched packages.

## Progress and evidence

Recorded in the PR description.

## Next step

Next cut is requested separately by the user.
