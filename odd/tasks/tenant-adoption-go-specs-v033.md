# Tenant adoption tests on go-specs v0.3.3 (#228)

## Problem

PR #228 (branch `test/205-migrate-tenant-adoption`) moved `migration/tenant_adoption_test.go` to go-specs
v0.3.1. After v0.3.3 and the #219 conventions, the file still has these gaps:

- Two tests (`TestTenantAdopterEndToEndRecoveryThroughRealActor` and
  `TestScopedMigratorSnapshotRecoversThroughTenantAwareActor`) are not specs at all. They use testify
  `require`/`assert` and `t.Cleanup`.
- About 45 assertions are written as `ctx.Expect(len(x)).ToEqual(n)`, which hides the collection when they fail.
- Setup closures call `t.Fatalf` and bypass the spec.
- `testFence` keeps `acquired`, `released` and `order` fields only to record calls.
- A fixed 100 ms deadline is used to simulate a cancelled wait, and two `for ... s.It` loops stand in for tables.

## What changes

Only `migration/tenant_adoption_test.go` changes.

- `len` checks become `HaveLen`/`BeEmpty`, tenant metadata checks use `HavePair`/`HaveKey`, and
  `>= n` checks use `BeGreaterThanOrEqual`. The sort-and-nil loop in the pagination test walks an ordered id
  slice and asserts `BeEmpty`.
- The two testify tests become a `specs.Describe` with one `It` each, built on `ctx.Expect(...)` and
  `ctx.Cleanup`. They add one subtest segment each, so the PASS count goes from 151 to 153.
- `t.Fatalf` in the closures and corruption callbacks becomes `ctx.Expect(err).To(specs.BeNil())`.
- `testFence` records acquires and releases with two `mock.Spy` values. `counts()` and `order()` read them.
- The two `for ... s.It` loops become `specs.Table`. The 100 ms deadline becomes an already expired deadline.

## What does not change, and why

- **Production code.** Nothing under `migration/` other than the test file changes.
- **The fault-injecting stores** (`corruptingEventsStore`, `racingEventsStore`, `replacing*`, `hiding*` and
  similar). They wrap a real in-memory `testkit` store and change its behavior on purpose. They are inputs of
  the test, not recorders of a replaced dependency, so `mock.Controller` does not apply. `testFence` also keeps
  its real lock behavior; only its counters moved to spies.
- **Describe-level setup helpers** (`tenantScope(t, ...)`, `connectedEventsStore(t)`, ...). They run while
  the spec is declared, before any case context exists, so they keep `testing.TB` and `t.Fatalf`.
- **`MissingSourceClassification` and `SamePositionTargetClassification`.** Their cases build different
  stores and records, so a table would be less readable than separate `It` blocks.
- **`panics` helper.** go-specs has no panic matcher.

## Constraints

- Every case and invariant stays; subtest names stay. No force-push (`develop` is merged). No `-race`, no workbench.
- Strict TDD with the runner `go test ./migration/...`. RED is shown per task by a production mutation, then reverted.
- Delivery: push to `test/205-migrate-tenant-adoption` and update the #228 description.

## Tasks

- [x] T1 Merge `origin/develop`. Route: inline. Evidence: merge commit `7147369`, clean build.
- [x] T2 Length, map and order matchers. Route: inline. Evidence: `06ecd9e`. RED: dropping the last id of a full
      page failed with `expected 10 to equal 13`; skipping the receipt stamp failed with
      `expected map[...] to have length 3, got length 2`.
- [x] T3 Testify and `t.Fatalf` to spec matchers. Route: inline. Evidence: `3780a7d`. RED: making
      `setTenantMetadata` a no-op failed the real-actor test with `failed to unmarshal event tenant metadata`.
- [x] T4 `testFence` spies, tables, expired deadline. Route: inline. Evidence: `9ff3032`. RED: swapping the
      fence order failed with `expected [tenant:south|shared tenant:north|shared] to equal [...]`; not releasing
      fences failed with `expected 0 to equal 2`.
- [x] T5 Verify and deliver. Evidence: 151 `--- PASS` before, 153 after; the only difference is the two
      new Describe segments. Coverage `migration` 82.8% before and after. `-count=5`, vet, golangci-lint (0 issues)
      and gofmt are clean.

## Follow-up spec (not in this document)

`tenant-adoption-fault-stores`: replace the fault-injecting stores with `mock.Controller` stubs only if the
per-store wrappers can be shown to add no behavior.

## Progress

- 2026-09-30: T1 to T5 done. Engram mirror: pending.
