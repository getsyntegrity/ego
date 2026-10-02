# command tests on go-specs v0.3.3 (#224)

## Problem

PR #224 (branch `test/205-migrate-command`) moved the unit tests of the `command` package to go-specs
v0.3.1. They still carry habits from before the #219 conventions:

- The fixture helpers `mustOperationID`, `mustTenantContext` and `mustMetadata` call `t.Fatalf`, so a broken
  fixture bypasses the spec and its failure format.
- Loops that register one `It` per item (`canonical` reserved keys, the outcome kinds, the error
  classification cases) are written by hand instead of `specs.Table`.
- Several checks use `a.Equal(b) BeTrue`, `len(x) ToEqual 0`, `x == "" BeFalse`, a `carrierHas`
  helper, or `Not(BeNil())` where a matcher says more (`HaveKey`, `HavePair`, `BeEmpty`, `MatchError`).
- `proto.Equal(...) BeTrue` hides the two messages when it fails.

## What changes

Only the `*_test.go` files under `command/` change.

- The `must*` helpers take the `*specs.Context` and assert through `ctx.Expect(err).To(specs.BeNil())`.
  No `t.Fatalf` is left in the package.
- Small test-only matchers in `metadata_test.go` (`beTime`, `beTimeBetween`, `beTimeBefore`) and
  `result_test.go` (`equalProto`) are built on `specs.Satisfy`. They replace `Equal(...) BeTrue`, so a
  failure prints both values. go-specs does not order `time.Time`, which is why they are needed.
- `specs.Table` replaces hand-written `It` loops: reserved custom keys (`metadata_test.go`), outcome kinds
  (`result_test.go`), error classification (`errors_test.go`) and the rejected operation ids
  (`identity_test.go`).
- Carrier checks use `HavePair`, `HaveKey` and `Not(HaveKey)` instead of indexing the map and the removed
  `carrierHas` helper. Empty collections use `BeEmpty`.
- Three `Not(BeNil())` checks now say which sentinel they expect (`ErrInvalidMetadata`,
  `ErrInvalidPrincipal`), so a wrong error is no longer accepted.
- One input was added to the existing case "control rune rejected": `"order\x00123"`. The old input
  `"order-123\n"` is caught by the trailing-whitespace rule, so the control-rune rule was never exercised.
  Mutating that rule out left the old test green.

## What does not change, and why

- **Production code.** Nothing outside `*_test.go` changes.
- **Mocks and waits.** There is nothing to replace. The package has no recording fakes, no testify, no
  generated mocks, no `time.Sleep` and no external resource. `mock.Controller` and `Eventually` have no use
  here.
- **`Outcome.String` per kind and "validation sentinels are distinct".** They stay one case each. Turning
  them into tables would rename the subtests, and the nested sentinel loop reads better as a loop.
- **Single-case tests for malformed expected revisions** (`-1`, overflow, non-numeric). They are separate
  `Test` functions, whose names must stay, so there is nothing to merge into one table.
- **Outcome kinds being distinct** is also guaranteed by the compiler (duplicate `case` in `String`), so no
  production mutation can make that case fail. It stays as a regression check.

## Constraints

- Every case and invariant stays. Subtest names stay. The case count does not drop: 185 `--- PASS` before
  and after.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. The test runner is `go test ./command/`. RED is shown per task by a deliberate production
  mutation that the rewritten cases must catch, and the mutation is then reverted.
- Delivery: push to `test/205-migrate-command` and update the #224 description.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge commit `3f1f907`, a clean merge
      with no conflicts, `go.mod` unchanged against `develop`, a clean build.
- [x] T2 `metadata_test.go` helpers and time matchers, reserved keys as a table. Route: inline (one writer,
      one package). Evidence: `981f4a0`. RED mutations: dropping `expected_revision` from the reserved keys
      failed `.../WithCustom_rejects_every_canonical_bare_key/expected_revision`; shifting the
      `WithTimestamp` value failed with `expected 2020-01-01 00:00:01 +0000 UTC to satisfy "the instant
      2020-01-01T00:00:00Z"`; returning `ErrReservedKey` for a too-long value failed with `expected error
      ... to match command: invalid metadata`.
- [x] T3 `result_test.go`, `errors_test.go`, `identity_test.go`, `principal_test.go`: tables, proto matcher,
      sentinels. Route: inline. Evidence: `ef56ed5` and `1bbd0f2` (the extra control-rune input). RED
      mutations: `ErrTimedOut` also matching `ErrCanceled` failed `.../timed_out` with `expected boom not to
      be an error matching command: canceled`; a max length of 129 bytes failed `.../exceeds_max_length_rejected`;
      a wrong sentinel in `NewPrincipal` failed `.../id_required`; disabling the control-rune rule failed
      `.../control_rune_rejected`.
- [x] T4 `carrier_test.go`, `envelope_test.go`: `HavePair`/`HaveKey`/`BeEmpty` and `equalProto`. Route:
      inline. Evidence: `4143e27`. RED mutations: an empty timestamp in `MarshalMetadata` failed with
      `expected  not to be empty`; dropping the principal kind failed the optional-fields round trip; writing
      `revision+1` failed with `to have key ego.cmd.expected_revision with value 7 — key has value 8`.
- [x] T5 Verify and deliver. Route: inline. Evidence: `go build ./...`, `go vet ./command/`,
      `golangci-lint run ./command/...` (0 issues) and `gofmt -l command` are clean. `go test -count=5` passes.
      185 `--- PASS` before and after, with identical names. Coverage of `command` went from 92.3% to 92.7%
      because of the added control-rune input. Push and PR description update follow.

## Follow-up spec (not in this document)

None needed. All work found in this package fits the five tasks above.

## Progress

- 2026-09-30: T1 to T4 done and committed. Engram mirror: pending (not available to this writer).
