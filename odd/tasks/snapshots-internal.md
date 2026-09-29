# Move snapshot writing and its retention out of the engine package

## Objective

Next cut of the reorganization of the former root package (now `engine`, after #184 to
#187 and #193): move the child actors that persist an event-sourced entity's snapshots
and then apply its retention policy into one internal package that owns that work, and
leave in `engine` only the decision of *when* to snapshot.

Base: `main` at `aa2d454` (the squash merge of #193).

## Problem

The request asked to extract `engine/snapshots_writer_actor.go` on its own. Tracing its
dependencies showed that this boundary is not cohesive:

- The snapshot writer carries the events janitor's private message. `persistSnapshotRequest`
  holds an `*applyRetentionRequest` and the janitor's PID, and the writer forwards that
  request to the janitor with `ctx.Tell` only after `WriteSnapshot` succeeds
  (`engine/snapshots_writer_actor.go`, `handlePersistSnapshot`). The ordering "snapshot
  confirmed, then retention" is a protocol between the two actors, not between the writer
  and `EventSourcedActor`.
- The writer and the janitor share `retryWithBackoff` and `defaultMaxRetries`
  (`engine/retry.go`), which nothing else in `engine` uses.
- Moving the writer alone would force exporting `applyRetentionRequest` and its seven
  fields across packages, or sending it as `any`, or adding an interface only so the file
  can move. All three were rejected.

So the cut is the writer **and** the janitor, with their protocol and retry helper, in
one package: `internal/snapshots`.

## What changes

- New package `internal/snapshots`:
  - `NewWriter() goakt.Actor` and `NewJanitor() goakt.Actor` create the two actors; their
    structs stay unexported.
  - `Tell(ctx *goakt.ReceiveContext, writer *goakt.PID, snapshot *egopb.Snapshot, scope persistence.Scope, retention *Retention)`
    builds the unexported snapshot request (and, when `retention` is not nil, the
    unexported retention request with the same scope) and sends it with `ctx.Tell`.
    It plays the role `eventswriter.Ask` plays for the events writer: the protocol stays
    unexported and owned by the package.
  - `Retention` carries the janitor PID and the values the parent already computes:
    persistence ID, events counter, snapshot interval, the two delete flags and the
    events retention count.
  - `retryWithBackoff` and its constants move with the actors, unexported.
- `internal/extensions` gains `Optional[T]` next to `Require[T]`, with the same messages
  and the same wrapped sentinel as `engine.optionalExtension`, which now delegates to it.
- `EventSourcedActor` keeps the trigger (`triggerSnapshotAndRetention`,
  `crossedSnapshotBoundary`, `snapshotAndRetain`), `newSnapshotEnvelope`, the spawn in
  `spawnChildren` with `childSpawnOptions()` and the child names `snapshots-writer` and
  `events-janitor`.

Dependency direction: `engine -> internal/snapshots -> {internal/extensions,
internal/goaktlog, persistence, encryption, egopb, goakt}`. No import back to `engine`.

## Behavior that must not change

- A snapshot is sent only after the events store confirmed the write and the state was
  applied, on the interval boundary (direct path) or when a batch crosses a boundary.
- A failed events write sends no snapshot.
- Order: events written, then snapshot written, then retention deletes. Retention runs
  only if the snapshot write succeeded.
- The entity's `persistence.Scope` reaches `WriteSnapshot` and the janitor's deletes
  unchanged.
- Encryption: the state is encrypted with the registered encryptor before the write; an
  encryption failure is logged, writes nothing and forwards no retention.
- Errors are logged and never propagate to the entity or the caller; the actors stay alive.
- Supervision: the three children keep `childSpawnOptions()` (long-lived, restart on any
  error).
- Public API and `ClusterKinds()` unchanged (neither actor is a cluster kind).

## Scope

In: `engine/snapshots_writer_actor.go`, `engine/events_janitor_actor.go`, `engine/retry.go`
and their tests moved with history to `internal/snapshots`; `extensions.Optional`; the
`EventSourcedActor` call site; new invariant tests; this document.
Out: the trigger logic, the events writer, persistence and encryption contracts,
protobuf, public API, metric or span names, CI, tags, releases, merge.

## Tasks

TDD: strict (from the user's global configuration). Runner: `go test`, offline
(`GOPROXY=off`, `GOTOOLCHAIN=local`), no `-race` locally (user rule; CI keeps its own).

- [x] T1 (delegated writer, commit 8cdc029): characterization test at the `EventSourcedActor` level that
      records store calls in order and passes on unchanged `main`: success is
      events write, then snapshot write, then retention deletes; a failed events write
      sends no snapshot; a tenant scope reaches `WriteSnapshot` and the deletes.
      Evidence: `engine/snapshots_sequence_test.go` passes on unchanged code (a
      characterization test has no RED by design), 8 runs in a row.
- [x] T2 (delegated writer, commit 8a016e9): `extensions.Optional[T]` test-first, then
      `engine.optionalExtension` delegates.
      RED: `internal/extensions/lookup_test.go:82:15: undefined: Optional` (also :98, :111).
- [x] T3 (delegated writer, commit 3e75aa3): contract tests for `internal/snapshots` first (RED), then
      move the two actors, `retry.go` and their tests, add `Tell` and `Retention`, rewire
      `snapshotAndRetain`; GREEN.
      RED: `internal/snapshots/contract_test.go`: undefined: Retention, Tell, NewWriter,
      NewJanitor, defaultMaxRetries (build failed).
      Route: delegated direct, one writer.
- [ ] T4 (parent): verification, PR.

Route: delegated direct, because T1 to T3 touch more than two non-trivial files
(writer trigger).

## Acceptance criteria and checks

- The T1 test passes before and after, unchanged.
- `apidiff -m` over the root module, `aa2d454` vs branch: no change.
- Full root-module tests, `go build` and `go vet` in every nested module, `golangci-lint`
  on the touched packages and `archcheck` pass.
- `go list -deps ./internal/snapshots` contains no `engine`.

## Progress and evidence

Recorded in the PR description.

## Next step

Next cut is requested separately by the user.
