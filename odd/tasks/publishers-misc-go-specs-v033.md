# publisher, egopb, eventstream, compat and testpb tests on go-specs v0.3.3 (#239)

## Problem

PR #239 (branch `test/205-migrate-publishers-misc`) moved the unit tests of `publisher/*`, `egopb`,
`eventstream`, `test/compat` and `test/data/testpb` to go-specs v0.3.1. Three things were left over:

- The four nested publisher modules and `test/compat` still pin go-specs v0.3.1. The root module is on
  v0.3.3 since #242.
- `eventstream/stream_test.go` waits for the asynchronous fan-out with a hand-written `waitUntil` that calls
  `time.Sleep` in a loop and `t.Fatalf` on timeout. Both are forbidden by the conventions.
- Several assertions hide the failing value: `len(x) ToEqual`, `x == nil` with `BeTrue/BeFalse`,
  `Messages().Len() > 0`, and a loop over messages that asserts field by field.
- The four `closure_test.go` files repeat two loops of near-identical cases.

## What changes

Only `*_test.go` files and the go.mod/go.sum files of the nested modules change.

- Nested modules move to go-specs v0.3.3 and are tidied (`publisher/kafka`, `nats`, `pulsar`, `websocket`,
  `test/compat`).
- `eventstream/stream_test.go`: `waitUntil` is gone. `awaitQueued` and the `Ready` waits use
  `ctx.Eventually` with `specs.WithTimeout`/`specs.WithInterval`. Non-blocking "did it fire" checks use a
  small `fired` helper. Length, nil and per-message checks use `HaveLen`, `BeEmpty`, `BeNil`,
  `EveryElement` and `Project`.
- `egopb`, `test/data/testpb` and `publisher/websocket` descriptor tests use the specific matchers.
- The `closure_test.go` rejected/allowed loops become `specs.Table`, and each rejected row now also checks the
  message kind with `StartWith`.

## What does not change, and why

- **Production code.** Not touched.
- **`TestUnitTestClosureExcludesRuntimeAndRoot`** runs `go list` as a subprocess. It is an architecture
  guard, not a unit test, so it stays out of this phase (as in #229).
- **The websocket and compat conformance tests** run the `adaptertest` and `publishingtest` harnesses, which
  take `*testing.T` by design, against an in-process `httptest` server. The harness API is the follow-up of
  #229, not this PR.
- **The publisher contract tests** (`publisher_contract_test.go`) already use `MatchError` on hand-built
  values; nothing to replace.
- **Real waiting in `eventstream`.** The fan-out runs in goroutines inside production code, so the test
  polls for an observable condition. There is no clock in the code to replace with a manual clock.

## Constraints

- Every case and invariant stays. Subtest names stay. The case count per `Test` does not drop.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. Runner: `go test` per package and per nested module. RED is shown per task by a
  deliberate production mutation that the rewritten cases must catch; it is then reverted.
- Delivery: push to `test/205-migrate-publishers-misc` and update the #239 description.

## Tasks

- [x] T1 Merge `origin/develop` and move the nested modules to v0.3.3. Route: inline. Evidence: merge commit
      MERGE_HASH, `go mod tidy` in the five nested modules, clean build and vet.
- [ ] T2 `eventstream/stream_test.go`: no `time.Sleep`, `Eventually`, specific matchers. Route: inline.
      Evidence: T2_HASH.
- [ ] T3 Descriptor tests (`egopb`, `testpb`, `publisher/websocket`): specific matchers. Route: inline.
      Evidence: T3_HASH.
- [ ] T4 `closure_test.go` in the four publishers: `specs.Table` and `StartWith`. Route: inline. Evidence:
      T4_HASH.
- [ ] T5 Verify and deliver. Route: inline. Evidence: below.

## Follow-up spec (not in this document)

`adaptertest-clock` (already named in #229): once the conformance harness has a clock seam, the websocket
conformance tests can drop their dependence on real time.

## Progress

- 2026-09-30: T1 done. Engram mirror: pending.
