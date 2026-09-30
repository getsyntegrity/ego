# S2: durablestate actor tests on go-specs v0.3.3

Spec 2 of `engine-actors-go-specs-chain.md`. Stacked PR on `refactor/engine-test-seams` (S1, #244).

## Problem

`internal/engine/durablestate` still holds `testing.T` tests next to the go-specs unit tests migrated by #238.
They have three problems.

- They start a real goakt actor system inline: 11 times in `durable_state_actor_test.go` and 8 times in
  `durable_state_actor_tenant_persist_test.go`.
- They mock `persistence.StateStore` with the generated testify `mocks/persistence.StateStore`
  (`durable_state_actor_test.go:393` and `:437`, `..._tenant_persist_test.go:409` and `:555`).
- They wait with `pause.For` 22 times, all in `..._tenant_persist_test.go`.

The package takes 22.2 s.

## What changes

Only the `*_test.go` files in `internal/engine/durablestate` change.

- **Store-failure cases that do not need the actor system become unit tests.** An example is the
  GetLatestState failure in `recoverFromStore`. They call the actor method directly with
  `enginetest.NewStateStoreMock(mock.NewController(ctx))`, the adapter from S1, and do not start goakt.
- **Every other use of `mocks/persistence.StateStore` moves to the S1 adapter.** This includes the cases
  that stay component tests. Afterwards the package no longer imports `mocks/`.
- **The component cases keep the actor system.** Their `pause.For` waits become `ctx.Eventually` on an
  observable condition: the actor is running, the state is persisted, or the actor has stopped. Where an
  `Ask` already synchronizes, the wait is removed. These cases are listed below for the final
  reclassification in S3b.
- **Old `testing.T` tests with a real system move to go-specs `specs.Describe`/`It`** when that is
  mechanical. Their Test function names and case names are kept.

## Constraints

- Every case and invariant is kept, along with every subtest name. The `--- PASS` count does not drop.
- No production change.
- No `-race`, no workbench.
- TDD is strict. RED is a deliberate production mutation per task, caught and then reverted.

## Tasks

- [ ] T1 Move the store-failure cases that can run without goakt (the `durable_state_actor_test.go`
      `:393`/`:437` cases and any others) to unit tests on `StateStoreMock`. Route: delegated writer.
      Check: green, and a mutation in `recoverFromStore` error handling is caught.
- [ ] T2 Move the remaining `mocks/persistence.StateStore` uses (`tenant_persist:409`, `:555`) to
      `StateStoreMock`. Route: the same writer. Check: no `mocks/` import left in the package; green.
- [ ] T3 Replace the 22 `pause.For` waits with `ctx.Eventually` or with an `Ask` that already
      synchronizes. Route: the same writer. Check: green with `-count=5`, the package time drops, and a
      mutation that skips persisting on the tenant path is caught.
- [ ] T4 Record the list of component cases (the tests that still need goakt) in this document. Then verify
      and deliver: run vet, lint, coverage, and the assessment (with an independent verifier if the result
      is `high`), push, and open the stacked PR. Route: inline.

## Component cases (filled in by T4)

_pending_

## Progress

- 2026-09-30: spec created on `test/durablestate-actor-go-specs`, on top of S1.
