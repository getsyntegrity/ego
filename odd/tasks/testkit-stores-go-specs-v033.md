# testkit store tests on go-specs v0.3.3 (#226)

## Problem

PR #226 (branch `test/205-migrate-testkit-stores`) moved the unit tests of the in-memory stores in `testkit/`
to go-specs. They still use habits the v0.3.3 conventions replace:

- Fixture helpers call `t.Fatalf` (`mustSetUpStore`, `newAccountEvent`, `newAccountState`), so a setup failure
  bypasses the spec. `disconnectAtEnd` reports through `t.Errorf` in a `t.Cleanup`.
- The `Connect`, `Disconnect` and `Ping` cases are copied four times, once per store, with identical bodies.
- Checks use `len(x)` with `ToEqual`, `len(x) <= 2` with `BeTrue`, `_, ok := m[k]` with `BeTrue`, and a
  `sort.Strings` + `Equal` pair instead of the matchers that say what is meant.

## What changes

Only `testkit/stores_test.go`, `eventstore_test.go`, `durablestore_test.go` and `replay_bounds_test.go`.

- `withConnectedStore` (in `stores_test.go`) gives each case its own connected store in `BeforeEach` and
  asserts a clean `Disconnect` in `AfterEach`. It replaces `mustSetUpStore`, `disconnectAtEnd` and the
  per-case `Connect` lines.
- `connectCases`, `disconnectCases` and `pingCases` register the lifecycle cases once as `specs.Table`
  rows. The four stores share them.
- Fixture builders take the `*specs.Context` and assert through `ctx.Expect` instead of `t.Fatalf`.
- Matchers: `HaveLen`, `BeEmpty`, `HavePair`, `HaveKey`, `BeGreaterThan`, `BeLessThanOrEqual`,
  `ContainTheSameElementsAs`, `HaveElementsInOrder` and `Project` (through the small `sequenceNumber` and
  `versionNumber` helpers, so a failure names the field). The "not connected" check projects the error text.
- One check is stricter on purpose: "replay with limit" asked for `len <= 2`. With three events and limit 2
  the store must return exactly two, so it now expects `HaveLen(2)`.

## What does not change, and why

- **Production code.** Nothing outside `*_test.go` is touched.
- **No mocks.** The stores are in-memory implementations that live in the repository, and these tests check
  their own behavior. The conventions say a test may use them directly, so there is nothing to replace with
  `mock.Controller`.
- **No `Eventually`/`Consistently`.** No test waits on time.
- **`KeyStore` tests** keep their shared store per `Describe` (they use distinct entity ids, and the store
  needs no connect/disconnect).
- **The `New...Store` tests** stay one case each; a one-row table adds nothing.
- **The conditional-write and duplicate-sequence tests** stay separate `Test` functions with one case each.
- Shared state moved from one store per `Describe` to one store per case. Every case still passes, and cases
  no longer depend on the order they run in.

## Constraints

- Every case and invariant stays. Subtest names stay. The case count per `Test` does not drop.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. The test runner is `go test ./testkit/`. RED is shown per task by a deliberate production
  mutation that the rewritten cases catch, and the mutation is then reverted.
- Delivery: push to `test/205-migrate-testkit-stores` and update the #226 description.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge commit `c0a08c8`, clean merge,
      `go build ./...` clean, `go mod tidy` leaves no change.
- [x] T2 `stores_test.go`: shared fixture, lifecycle tables, specific matchers. Route: inline (one file,
      already understood). Evidence: `c9d8b8c`. RED mutation: `ReplayEvents` returned `limit+1` events, and
      the test failed with `expected [...] to have length 2, got length 3`.
- [x] T3 `eventstore_test.go`, `durablestore_test.go`, `replay_bounds_test.go`: no `t.Fatalf` helpers,
      `withConnectedStore`, `HaveLen`/`HaveKey`/`Project`. Route: inline. Evidence: `8342cf7`. RED mutations:
      the duplicate-sequence overwrite disabled (`expected [...] to have length 1, got length 2`), and
      `DurableStore` reporting the wrong actual revision (`expected 2 to equal 1`).
- [x] T4 Verify and deliver: vet, lint, gofmt, `-count=5`, coverage, push, PR description. Route: inline.
      Evidence: 212 `--- PASS` before and after, names identical. Coverage 94.3-94.7% after, against
      93.8-94.3% before. The spread comes from one compare-and-swap failure branch in
      `testkit/eventstore.go:180` that the concurrency tests only reach sometimes.

## Follow-up spec (not in this document)

`testkit-concurrency-go-specs`: `concurrency_test.go`, `scenario_test.go`, `scope_test.go` and
`conformance_test.go` are not part of this PR. They can adopt the same fixture and matchers in their own PR.

## Progress

- 2026-09-30: T1-T4 done. Engram mirror: pending.
