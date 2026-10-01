# migration core tests on go-specs v0.3.3 (#227)

## Problem

PR #227 (branch `test/205-migrate-migration-core`) moved `migration/migration_test.go` to go-specs
v0.3.1. The file still has these problems:

- The helpers (`buildLegacyEventBytes`, `writeLegacyEvent`, `connectedStores`, `mustAny`, `mustNew`,
  `writeScopedLegacyEvent`) and the `TestMigratorScope` setup call `t.Fatalf`, which bypasses the spec.
- Every case ends with two hand-written `Disconnect` assertions instead of registering teardown.
- `TestMigratorUsesKitLogger` checks the injected logger through `kitlogtest.MockLogger` entries and a
  `messagesAt` filter. It never checks how many times a message was logged.
- `scopeSpy` is a hand-written, mutex-protected recorder of store calls.
- `len(calls) > 0` with `BeTrue`, a `strings.HasSuffix` loop and `proto.Equal` with `BeTrue` are used where
  a v0.3.3 matcher says more.
- `TestMigratorRunWithNilLogger` and the "returns nil" cases of `TestExtractLegacyResultingState` are
  near-identical single cases.

The #219 conventions now require go-specs `mock.Controller` for replaced dependencies and recommend
`specs.Table` and the specific matchers.

## What changes

Only `migration/migration_test.go` changes.

- The helpers assert through `ctx.Expect(...)`. The pure marshal logic moved to `marshalLegacyEvent`, which
  returns an error; `legacyEventBytes` checks it through the spec.
- `connectedEventStore` and `connectedSnapshotStore` register the `Disconnect` through `ctx.Cleanup`, so the
  stores close even when a case fails.
- `TestMigratorRunWithNilLogger` and the three "returns nil" cases of `TestExtractLegacyResultingState`
  become `specs.Table` rows with the same names.
- `loggerMock` is a typed adapter that forwards `InfoContext` and `DebugContext` to a `mock.Controller`.
  The log-message case now expects each migration message exactly once.
- `scopeSpy` becomes a `mock.Spy`. The decorators `spyEventsStore` and `spySnapshotStore` stay and call
  `recordScope`. The assertions use `Not(BeEmpty())`, `CalledWith`, and `EveryElement` with `Project`.
- `proto.Equal` with `BeTrue` becomes `Satisfy`, and `Not(Equal(""))` becomes `Not(BeEmpty())`.

## What does not change, and why

- **Production code.** Nothing under `migration/` other than the test file changed.
- **`buildLegacyEventBytes(t testing.TB, ...)`.** `migration/tenant_adoption_test.go` calls it with a
  `*testing.T`, and that file is not part of this PR. It stays as a thin wrapper over `marshalLegacyEvent`.
  Moving `tenant_adoption_test.go` is the follow-up below.
- **`testkit.EventStore` and `testkit.SnapshotStore`.** They are in-memory stores that live in the
  repository, not external resources. The tests check what the Migrator does on top of the store, so they
  stay as real inputs. The decorators around them only record calls.
- **Typed-nil `*kitlogtest.MockLogger`.** It is the input of the "typed nil" case (a typed nil must fall
  back to the default logger), not a recorder, so it stays.
- **`TestConsumeVarint` and `TestConsumeTag`.** Their cases assert different outputs (the "empty input"
  rows ignore the decoded value), so one table would need a flag per row and read worse. They stay as
  separate `It`s.
- **`panicValue`.** go-specs v0.3.3 has no panic matcher, so the recover helper stays.
- **No fixed waits.** The file has no `time.Sleep` or `pause.For`.

## Constraints

- Every case and invariant stays. Subtest names stay. The case count per `Test` does not drop: the package
  reports 120 `--- PASS` lines before and after, with identical names.
- No force-push: `develop` is merged into the branch. No `-race`. No workbench.
- TDD is strict. The test runner is `go test ./migration/...`. RED is shown per task by a deliberate
  production mutation that the rewritten cases must catch, and the mutation is then reverted.
- Delivery: push to `test/205-migrate-migration-core` and update the #227 description.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3). Route: inline. Evidence: merge commit `ef733bc`, no
      conflicts, `go build ./...` clean, and no `go mod tidy` change.
- [x] T2 Helpers, cleanup, tables and matchers. Route: delegated writer. Evidence: `c80314a`. RED mutation:
      `extractLegacyResultingState` returned a value for an event without unknown fields, and the failure was
      `expected nil, got type_url:"x" (*anypb.Any)` on two table rows.
- [x] T3 Logger mock and scope spy. Route: the same writer. Evidence: `e20022c`. RED mutations: the
      completion message was renamed, and the failure was `unexpected call InfoContext(... "migration:
      completed" ...)` plus `unmet expectation ... want 1, got 0`; `WriteSnapshot` was addressed to
      `Unscoped()` instead of the Migrator scope, and `TestMigratorScope` failed on the `WriteSnapshot` call.
- [x] T4 Verify and deliver. Route: inline. Evidence: `go build ./...`, `go vet ./migration`,
      `golangci-lint run ./migration/...` (0 issues), `gofmt -l` (clean), `go test -count=5 ./migration`
      (ok). 120 `--- PASS` before and after with identical names. Coverage of `migration` is 82.8% before and
      after.

## Follow-up spec (not in this document)

`migration-tenant-adoption-go-specs`: move `migration/tenant_adoption_test.go` (testify, 2629 lines) to
go-specs, and then delete the `buildLegacyEventBytes` wrapper.

## Progress

- 2026-09-30: T1 to T4 done. Engram mirror: not written from this worker.
