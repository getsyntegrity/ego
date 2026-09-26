# Feature: move publisher contracts to `port/publishing` (#103, ADR slice S1a)

Branch: `feat/103-port-publishing` · Base: `origin/main` `a5265aa` · Epic: #10 · Issue: #103

## Problem

The publisher contracts `EventPublisher`, `StatePublisher` and `ErrPublisherNotStarted` live in
`publisher.go`, inside the root package `ego`. That package also holds the GoAkt engine and actors,
so any code that only wants to implement a publisher must import the whole GoAkt runtime. The four
publisher modules (`publisher/kafka`, `nats`, `pulsar`, `websocket`) pay that cost today: the Kafka
publisher resolves 592 packages, 45 of them GoAkt (ADR `openspec/changes/ego-arch-001/design.md` §9).

## What changes

A new contract package `github.com/pablogore/ego/v4/port/publishing` owns the three declarations.
`publisher.go` keeps Go type aliases and a variable that point at the new package, so every existing
caller of `ego.EventPublisher` keeps compiling with the identical type. This is slice **S1a** of the
ADR (design.md §5): root-module files only. Moving the publishers to import `port/publishing`
(**S1b**) waits for #111, which makes CI build nested modules.

## Why this slice first

The ADR orders the work S1a → S2 (#107) → S3 (#103 behavior contracts) → S4 (#11). S1a is fully
specified, changes no public API, and is the smallest proof that a contract can leave package `ego`
with compatibility aliases. #103 has no dedicated S1 issue; this slice is filed under #103 because it
is exactly "extract a runtime-neutral contract from the GoAkt package".

Rejected alternative: start with S3 (`extension.Dependency` in behaviors). It changes public
signatures and needs its own compatibility design; doing it before the alias pattern is proven would
mix two risks in one PR.

## Constraints

- No incompatible change to package `ego` (verified with `apidiff`).
- `port/publishing` depends only on the standard library, `egopb` and the protobuf runtime
  (design.md §3, contract rules).
- Aliases are **not** marked `Deprecated:` in this slice (design.md §10 leaves the window open;
  deprecating before S1b would warn every publisher module while it still cannot migrate).
- TDD: strict, from the user's global configuration; runner `go test` (no `-race` locally).
- Route: direct inline (two small root files plus one test package; no trigger fired).

## Tasks

- [ ] **T1** Create `port/publishing` with the contracts and turn `publisher.go` into aliases.
  Check: RED test (alias identity, `errors.Is`, dependency allowlist) fails first, then
  `go build ./... && go vet ./... && go test ./port/...` pass.
- [ ] **T2** Record compatibility evidence required by design.md §5 items 1–3.
  Check: `apidiff` reports no incompatible change for package `ego`; nested-consumer script passes
  for the four publishers, `benchmark` and `mocks/ego`; `example/cluster` shows no new errors
  versus `main`.
- [ ] **T3** Document the move: `CHANGELOG.md` entry and package doc for `port/publishing`.
  Check: structural readback.

## Acceptance criteria

1. `go list -deps ./port/...` contains no GoAkt, OpenTelemetry or root `ego` package (enforced by a test).
2. `var _ ego.EventPublisher = publishing.EventPublisher(nil)` style identity holds, and
   `errors.Is(publishing.ErrPublisherNotStarted, ego.ErrPublisherNotStarted)` is true.
3. Evidence from T2 is recorded below.

## Follow-up specs (chain for #103)

1. **This spec** — S1a `port/publishing`.
2. `neutral-behavior-contracts` — S3: remove `extension.Dependency` from `EventSourcedBehavior`,
   `DurableStateBehavior` and `SagaBehavior` behind a documented bridge.
3. `inmemory-runtime-conformance` — the remaining #103 criteria: an in-memory runtime / test double
   running the same domain as GoAkt. Likely shaped together with #11's runtime SPI.

S1b (publishers import `port/publishing`) is blocked on #111.

## Progress and evidence

_(updated per task)_
