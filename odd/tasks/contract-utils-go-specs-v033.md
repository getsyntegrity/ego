# encryption, eventadapter and projection tests on go-specs v0.3.3 (#221)

## Problem

PR #221 (branch `test/205-migrate-contract-utils`) moved the unit tests of `encryption`, `eventadapter` and
`projection` to go-specs v0.3.1. After #219 the conventions ask for go-specs v0.3.3 features where they help.
Four spots still used the old style:

- `eventadapter/adapter_test.go` used a hand-written `errorAdapter` and a `addSecondsAdapter{extra: 100} // should never run`
  comment. Nothing checked that the chain really stops at the first error.
- `eventadapter` compared pointers with `result == event` plus `BeTrue`, and `proto.Equal(...)` plus `BeTrue`.
- `encryption/aes_encryptor_test.go` used `len(ciphertext) > 0` plus `BeTrue`, and only checked that decrypting
  with an unknown key ID returned some error.
- `projection/recovery_test.go` registered the option cases with a `for` loop.

## What changes

Only `*_test.go` files change.

- `eventadapter`: `errorAdapter` is replaced by `adapterMock`, a small adapter backed by `mock.Controller`.
  The failing position declares `Expect(...).Return(nil, err)` (called once, with revision 1) and the position
  after it declares `Never()`, so the "stops early" invariant is now verified, not just commented. Pointer and
  protobuf checks become named matchers (`sameEvent`, `equalProto`) built on `specs.Satisfy`, which give a
  readable failure.
- `encryption`: `Not(BeEmpty())` for the ciphertext, and `MatchError(encryption.ErrKeyNotFound)` for an unknown
  key ID (the error wraps the sentinel with `%w`).
- `projection`: `TestRecoveryOption` becomes a `specs.Table` with the same three row names.

## What does not change, and why

- **Production code.** `git diff` shows no non-test file touched.
- **`testKeyStore` (encryption).** It is an in-memory store with real behavior (it creates keys, and the AES code
  encrypts with them). It is the input of the test, not a recorder standing in for a dependency, and the
  conventions allow in-memory stores. It stays.
- **`noopAdapter`, `addSecondsAdapter`, `revisionGatedAdapter`, `timestampToDurationAdapter`.** They have real
  behavior (the chain result depends on it), so they stay as fakes.
- **Single-`It` tests with several steps** (nil and empty slice, three revisions). Splitting them into table rows
  would rename subtests, so they stay.
- **`projection/deadletter_test.go`.** Its cases are one-line checks on discard handlers, with nothing a v0.3.3
  feature improves.
- **No waits exist** in these packages, so there is nothing to move to `Eventually`.

## Constraints

- Every case and subtest name stays. `--- PASS` count is 8 / 16 / 20 (encryption / eventadapter / projection)
  before and after, with identical names.
- No force-push (`develop` is merged). No `-race`. No workbench.
- Strict TDD. Runner: `go test ./encryption ./eventadapter ./projection`. RED is a deliberate production
  mutation per task, caught, then reverted.

## Tasks

- [x] T1 Merge `origin/develop` (v0.3.3). Route: inline. Evidence: merge `24b1707`, clean, build OK.
- [x] T2 `encryption`: specific matchers. Route: inline (one small file). Evidence: `02660ae`. RED: `%w` changed
      to `%v` in `Decrypt` gives `expected error failed to get decryption key: encryption key not found ... to
      match encryption key not found - errors.Is(actual, expected) is false`. Second RED: `Encrypt` returning
      `ciphertext[:0]` gives `expected [] not to be empty`.
- [x] T3 `eventadapter`: `mock.Controller` adapter and matchers. Route: inline (one file). Evidence: `8256f31`.
      RED: `return nil, err` changed to `continue` in `Chain` gives `mock: forbidden call after(...) ... says
      never`. Second RED: passing `revision+1` gives `mock: unexpected call failing(..., 2)`.
- [x] T4 `projection`: `specs.Table` for `TestRecoveryOption`. Route: inline. Evidence: `76afdcc`. RED:
      `recovery.retryDelay = 0` in `WithRetryDelay` gives `expected 0s to equal 2s` (and the table row fails).
- [x] T5 Verify and deliver. Route: inline. Evidence: build, vet, golangci-lint and gofmt are clean;
      `go test -count=5` passes; coverage 78.1% / 100% / 100%, unchanged; `--- PASS` names identical.

## Follow-up spec (not in this document)

None needed. The remaining untouched items are intentional (see "What does not change").

## Progress

- 2026-09-30: T1-T5 done. Engram mirror: pending.
