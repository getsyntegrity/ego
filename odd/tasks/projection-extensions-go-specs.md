# projection actor and extension lookup tests on go-specs v0.3.3 (#205)

## Problem

Epic #205 requires every test to use go-specs. Two test files still did not:

- `internal/engine/projection/projection_actor_test.go` used testify, the generated mocks in
  `mocks/offsetstore` and `mocks/persistence`, and `pause.For(time.Second)` pauses between steps (up to
  about 6 seconds of fixed waiting per case).
- `internal/extensions/lookup_test.go` used testify and the generated `mocks/persistence`.

## What changes

Only `*_test.go` files change.

- Both files now wrap their cases in `specs.Describe` and `s.It` / `specs.Table`. Every top-level `Test`
  name and every case name is kept (11 cases in projection, 6 in extensions). Each case moves one segment
  down, under its `Describe` name, as `docs/testing/go-specs.md` describes.
- The generated and testify mocks become typed `mock.Controller` adapters, in new files
  `internal/engine/projection/specs_mocks_test.go` and `internal/extensions/specs_mocks_test.go`. They are
  copies of the adapters in `engine/specs_mocks_test.go`, kept test-local because that file is in another
  package's tests. `internal/engine/enginetest` on develop holds actors and fixtures but no port mock
  adapters, so there was nothing importable to reuse.
- `pause.For` is gone. The projection cases poll with `ctx.Eventually` for what the pause was waiting for:
  the stored offset reaching the last journal event, or the dead-letter count reaching 1. The four cases
  that used to wait two seconds and assert nothing (dead letter handler, event adapters, encryptor,
  telemetry) now check the offset too, so they prove the batch was consumed.
- The four near-identical "mistyped extension" cases in the projection test and the setup repeated in each
  case became a `specs.Table` and small helpers (`startSystem`, `newProjectionFixture`). The actor system
  is stopped through `ctx.Cleanup`, so it also stops when an expectation fails.
- Assertions use `specs.MatchError`, `specs.BeNil`, `specs.Contain` and `specs.Equal`. The `ResetOffset`
  expectation now names its arguments (`projectionName`, `resetAt.UnixMilli()`) and asserts the exact error.

## What does not change, and why

- **Production code.** Not touched.
- **The real in-process goakt actor system.** The spec asks to keep it and not move these cases to another
  lane. Moving them would be its own decision.
- **The generated `mocks/*` packages.** Still imported by seven other test files, all in
  `internal/engine/eventsource` and `internal/engine/durablestate` (listed in the PR). They are migrated by
  other specs of the epic.

## Constraints

Strict TDD (test runner `go test`), no `-race`, no workbench, no external resources. Release note: NONE.

## Tasks

- [x] T1 `internal/extensions/lookup_test.go`. Route: inline. Evidence: commit "test(extensions): migrate
      lookup tests to go-specs with a typed mock controller". RED mutation in `lookup.go` (the mismatch
      message changed and the missing-extension error replaced by `nil`) failed 3 of the 6 cases with
      `expected an error matching ... got a nil error` and a `to contain` failure; then reverted.
- [x] T2 `internal/engine/projection/projection_actor_test.go`. Route: inline. Evidence: commit "test(projection):
      migrate projection actor tests to go-specs with typed mock controllers". RED mutations, each reverted:
      the runner's `WriteOffset` skipped (the offset cases timed out at `expected -1 to equal <timestamp>`),
      the actor's `ctx.Unhandled()` removed (the dead-letter case timed out at `expected 0 to equal 1`), and
      `Start` errors swallowed in `PreStart` (the PreStart cases failed).
- [x] T3 Checks. Evidence: `go build ./...`, `go vet`, `golangci-lint run` (0 issues) and `gofmt -l` clean;
      `go test -count=5` passes on both packages; coverage 80.7% (projection) and 86.8% (extensions), the same
      before and after. The projection package now runs in about 5 seconds per pass instead of 19.

## Follow-up

Fold the port mock adapters in `engine`, `internal/engine/projection` and `internal/extensions` into one shared
package, once the remaining generated-mock importers above are migrated.

## Progress

- 2026-09-30: T1 to T3 done. Engram mirror: not written (bounded writer without that session).
