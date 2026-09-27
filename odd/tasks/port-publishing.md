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

- [x] **T1** Create `port/publishing` with the contracts and turn `publisher.go` into aliases.
  Check: RED test (alias identity, `errors.Is`, dependency allowlist) fails first, then
  `go build ./... && go vet ./... && go test ./port/...` pass.
- [x] **T2** Record compatibility evidence required by design.md §5 items 1–3.
  Check (revised during T2): `apidiff` was run and its output recorded, but it flags every
  cross-package alias, so criterion 1 is decided by a base-API consumer compiled against base and
  head (see evidence); nested-consumer script passes
  for the four publishers, `benchmark` and `mocks/ego`; `example/cluster` shows no new errors
  versus `main`.
- [x] **T3** Document the move: `CHANGELOG.md` entry and package doc for `port/publishing`.
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

S1b (publishers import `port/publishing`) was blocked on #111; #111 landed in #120, and S1b is
tracked in the section below.

## Progress and evidence

**T1 — done, commit `f616f29`.** RED observed first: `go test ./port/publishing/` failed
("no non-standard dependency was checked") and the root alias test failed to build (package missing).
GREEN: `go build ./...`, `go vet . ./port/...`, `go test ./port/publishing/` and
`go test -run Publisher .` (13.9 s) pass; `golangci-lint run --config .golangci.yml` on `./port/...`
and on the root package (`--new-from-rev=origin/main`, after `go mod vendor` like CI): 0 issues.

**T2 — done, evidence only (no code).** Go 1.26.6, linux/amd64, no `-race`.

- `apidiff` (golang.org/x/exp, `85c1c2202aba`) base `a5265aa` → head reports four "incompatible"
  changes: `EventPublisher`, `StatePublisher`, `(*Engine).AddEventPublishers`,
  `(*Engine).AddStatePublishers`, each "changed from X to X". This is a tool limitation, not a
  source break: `apidiff.go` line 1 reads `TODO: test exported alias refers to something in another
  package -- does correspondence work then?`
- Consumer comparison (the check that decides criterion 1): a program written against the base API
  — implements `ego.EventPublisher`, embeds both interfaces, assigns the mocks, binds
  `(&ego.Engine{}).AddEventPublishers` to `func(...ego.EventPublisher) error`, type-asserts and wraps
  `ego.ErrPublisherNotStarted` — builds, vets and prints identical results against base and head.
  The only difference is `reflect.TypeOf(...).String()`: `ego.EventPublisher` → `publishing.EventPublisher`.
  Documented in `CHANGELOG.md`.
- Nested-consumer check (design.md §5): `publisher/kafka`, `nats`, `pulsar`, `websocket`,
  `benchmark` and `mocks/ego` build and vet on head.
- `example/cluster`: fails identically on base and head (pre-existing #115 error,
  `*PostgresEventStore does not implement persistence.EventsStore (wrong type for method DeleteEvents)`);
  `diff` of the two outputs is empty, so S1a adds no error.

**T3 — done.** `CHANGELOG.md` Improvements entry; package doc in `port/publishing/publishing.go`.
Check: structural readback.

**Review:** RDD is off (global), so no native review ran; delivery follows ordinary repository policy.

**Next step:** push and open the PR for S1a, then spec 2 of the chain, or #107 (S2) which the ADR
allows in parallel.

## S1b — publishers import `port/publishing`

Branch: `feat/103-publishers-port-publishing` · Base: `origin/main` `c6b219c` (after #119 and #120).

The four publisher modules still imported package `ego` only for `EventPublisher`,
`StatePublisher` and `ErrPublisherNotStarted`, which pulled the whole GoAkt runtime into each
publisher's build. #111 now builds, vets, lints and tests a nested module whenever a PR touches it,
so the switch can land safely. Scope is the import switch only: no `go.mod` change, no public API
change, the `ego` aliases stay as they are and are not deprecated (design.md §10 keeps that window
open). #103's behavior contracts, #105 and the runtime SPI are out of scope.

TDD: strict (user global configuration), runner `go test`, no `-race` locally.
Route: direct inline — four one-line import switches, one baseline file and docs; no trigger fired.

- [x] **T4** Compatibility test per publisher (`publisher/*/compat_test.go`): compile-time
  assertions against `ego.*` and `publishing.*`, and `errors.Is` of a stopped publisher's
  `Publish` error against both sentinels. Check: passes on the old code; a temporary mutation of
  Kafka's sentinel makes it fail (RED), reverted before T5.
- [x] **T5** Switch the four publishers to `port/publishing`, fix the related comments, and remove
  the four publisher entries from `internal/cmd/archcheck/baseline.go` (keep `migration`).
  Check: archcheck + tests; `scripts/ci/verify-module.sh` for each publisher; `go list -deps`.
- [x] **T6** Update `design.md`, `docs/ci.md`, `CHANGELOG.md` and this document.
  Check: structural readback.

**Evidence (local, Go 1.27.1 linux/amd64 — CI uses 1.27.0; golangci-lint v2.13.1 built with
Go 1.27.1, no `-race`).**

- RED: with Kafka's `EventsPublisher.Publish` returning `errors.New(...)`, the test failed on both
  `errors.Is` checks; the other three modules passed. Reverted.
- `go run ./internal/cmd/archcheck`: 15 packages, 70 edges, 1 baselined, 0 violations, 0 stale.
  `go test ./internal/cmd/archcheck/...`: ok.
- `scripts/ci/verify-module.sh` exit 0 (build, vet, lint 0 issues, test ok) for the four
  publishers, `benchmark` and `example/cluster`; `go build`/`go vet ./mocks/ego/`: ok.
- `go list -deps ./...` (GOWORK=off): kafka 299, nats 256, pulsar 577, websocket 239 packages;
  0 under `github.com/tochemey/goakt/v4` and none is the root package `ego`.
- `go mod tidy -diff`: no change in any publisher.
- Root: `go test -run TestPublisherContractsAliasPortPublishing .` ok. No root-package file
  changed, so the S1a `apidiff` result for package `ego` stands.
- `ciselect`: `publisher/kafka/kafka.go` alone → root mode `none`, `modules.json`
  `["publisher/kafka"]`; the full change set → root mode `affected` (`internal/cmd/archcheck`),
  modules = the four publishers.
- Not proven here: a build against a root tag on the Go proxy. No tag exists yet; `release.yml`
  runs `verify-published.sh` against the just-published root version before tagging a publisher.
