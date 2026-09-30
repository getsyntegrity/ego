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

- [x] T1 Move the store-failure cases that can run without goakt (the `durable_state_actor_test.go`
      `:393`/`:437` cases and any others) to unit tests on `StateStoreMock`. Route: delegated writer.
      Evidence: `9299781`. RED: `recoverFromStore` swallowing the store error failed with an `errors.Is`
      mismatch.
- [x] T2 Move the remaining `mocks/persistence.StateStore` uses (`tenant_persist:409`, `:555`) to
      `StateStoreMock`. Route: the same writer. Evidence: `689ff73`. No `ego/mocks` import is left.
      - This found a latent bug in the old test: `AssertNotCalled("WriteState", Anything, Anything)` named
        two arguments where the real call has four, so it always passed. It is now
        `Expect(...).Never()`.
      - RED: making `PostStop` always persist failed with a `forbidden call WriteState`.
- [x] T3 Replace the 22 `pause.For` waits with `ctx.Eventually` or with an `Ask` that already
      synchronizes. Route: the same writer. Evidence: `b31625e`. No `pause.For` is left.
      - RED: skipping the tenant-aware persist failed within 5 s. The message showed the last value
        observed: `expected <nil> to satisfy "is a persisted durable state"`.
      - The package time went from 22.2 s to 0.05 s. The parent confirmed this uncached (0.048 s).
- [x] T4 Record the component cases and deliver. Route: inline. Evidence:
      - Subtest names: 50 `--- PASS` before, 52 after. No old name was lost. Two single-case tests gained
        an `It` segment.
      - Coverage stayed at 85.8%.
      - vet, lint and gofmt are clean.
      - The assessment returned `medium` with RDD off, so the writer's self-verification with mutations
        stands.

## Component cases (they still start a real goakt actor system)

These go to the component lane in the final reclassification in S3b:

- `TestDurableStateActorPreStartExtensions` (1 case).
- `TestDurableStateBehavior`, 6 cases:
  - with state reply
  - with error reply
  - with state recovery from state store
  - with telemetry extension
  - with mismatched state types from HandleCommand
  - with invalid version increment from HandleCommand
- `TestDurableStateActorTenancyGate` (1).
- `TestDurableStateActorTenancyWritePath`.
- `TestDurableStateActorProcessCommandRejectsCrossTenant`.
- `TestDurableStateActorPostStopTenantPersist`, all 4 cases.
- `TestDurableStateActorGetStateCommandTenancyGate` (3).
- `TestDurableStateActorFailedFirstCommandDoesNotAppropriateActor`.
- `TestDurableStateActorRecoverFromStoreLegacyVersionZeroGenesis`, only its end-to-end case.

Left as is, because they have no fixed waits and no generated mocks: `TestDurableStateBehavior`,
`PreStartExtensions`, `TenancyGate` and `TenancyWritePath` are still `testing.T` with testify
`require`/`assert`. Moving them to go-specs is mechanical and is not needed for the lane rule.

No unit case covers `PreStart` propagating a `recoverFromStore` error, because `PreStart` takes a
`*goakt.Context`. The component case "an actor that fails recovery on invalid tenant metadata never
reaches PostStop's persist" covers that path.

## Progress

- 2026-09-30: spec created on `test/durablestate-actor-go-specs`, on top of S1.
