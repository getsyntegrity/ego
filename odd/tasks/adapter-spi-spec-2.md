# Feature: adapter conformance suites and two adopters (#106, ego-arch-004 spec 2)

Branch: `feat/106-spec2-conformance` · Base: `origin/main` `fd1ae63` (spec 1, #157, merged) · Epic: #10 ·
Issue: #106

Implements [`openspec/changes/ego-arch-004/specs/adapter-conformance/spec.md`](../../openspec/changes/ego-arch-004/specs/adapter-conformance/spec.md)
(spec 2 of 3: slices SPI-3 and SPI-4). Previous: spec 1 (`adapter-spi-boundary`, #157). Next: spec 3
(`adapter-composition`).

## Problem

Spec 1 gave adapters a way to describe themselves (`port/adapter`), but nothing checks that a
descriptor tells the truth or that an adapter follows the lifecycle rules L1–L5 of design §D4. #106
asks for a small reusable conformance suite and for at least two adapter types to use it. The chosen
publisher, `publisher/websocket`, also breaks L2 today: a second `Close` returns the error of closing
an already-closed connection.

## What changes

1. **SPI-3.** Two standard-library-only test packages: `port/adapter/adaptertest` (AT-1…AT-5, driven by
   a `Target`) and `port/publishing/publishingtest` (PT-1…PT-3). A check is skipped only when the
   factory returns an error matching `adaptertest.ErrUnreachable`; a check the suite cannot run (no
   hook, or an adapter that acquires in its constructor) is reported as "not exercised", never as
   passed.
2. **SPI-4.** `publisher/websocket` makes `Close` idempotent, adds `Describe` to both publishers and runs
   both suites against an `httptest` server. The `testkit` `EventStore`, `DurableStore` and
   `OffsetStore` add `Describe` and run `adaptertest` as `Borrowed`.

## Scope and constraints

- File ownership: the spec's list (`port/adapter/adaptertest/**`, `port/publishing/publishingtest/**`,
  `publisher/websocket/**` except `closure_test.go`, the three `testkit` stores and their tests), plus
  `CHANGELOG.md`, this document and a doc-comment-only sentence in `port/adapter/adapter.go` (the #157
  review carry-over, authorized by the coordinator). Not touched: `port/runtime`, `engine.go`,
  `spawn_config.go`, `saga.go`, `supervisor.go`, `compose/`, other publishers, `.github/`.
- Additive in v4; apidiff additions only.
- TDD: strict (user global configuration); runner `go test` (plus `scripts/ci/verify-module.sh` for
  nested modules). Never `-race`, never the workbench.
- Route: delegated direct (one writer; 2+ non-trivial files).
- Delivery strategy: `single-pr` (the coordinator asked for one pull request for the spec).

## Tasks

- [ ] T1 `adaptertest` (SPI-3).
- [ ] T2 `publishingtest` (SPI-3).
- [ ] T3 Idempotent websocket `Close` (SPI-4).
- [ ] T4 Websocket adopts the model (SPI-4).
- [ ] T5 `testkit` stores adopt the model (SPI-4).

## Next step

T3 RED (double close against `httptest`).
