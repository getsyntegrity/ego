# internal/engine protocol and saga tests on go-specs v0.3.3 (#237)

## Problem

PR #237 (branch `test/205-migrate-engine-protocol-saga`) moved the unit tests of `internal/engine/protocol`
and `internal/engine/saga` to go-specs v0.3.1. They still carry habits the v0.3.3 conventions
(`docs/testing/go-specs.md`) replace:

- `saga_test.go` stands in for the events store with the generated testify mock `mocks.EventsStore`. Its
  expectations are only checked when a test remembers `AssertExpectations`, so most cases never verified
  them. One case (`PreStart: missing behavior fails to start`) declared `Ping` and `GetLatestEvent`
  expectations that never ran.
- `saga_actor_tenant_test.go` has three testify/`require` tests that wait with `time.Sleep(300ms)` after
  spawning an actor, plus helpers (`newAnyEvent`, `newBoundSagaActor`, the scope setup in
  `TestSagaActorEventContext`) that call `t.Fatalf` outside the spec.
- Small tests use hand loops (`noop_test.go`, `status_test.go`, `TestSagaStatus_String`), `== nil` with
  `BeTrue`, `len(...) ToEqual(0)`, and `Not(BeNil())` followed by `MatchError`.

## What changes

Only `*_test.go` files in those two packages change; production code does not.

- `protocol/reply_classification_test.go`: `newTestMetadata` reports through the spec instead of
  `t.Fatalf`; the sentinel-prefix check uses `BeEmpty`.
- `saga/noop_test.go`, `status_test.go`, `saga_test.go` (`TestSagaStatus_String`): the hand loops become
  `specs.Table` with the same row names.
- `saga/saga_actor_tenant_test.go`: the three testify tests become go-specs cases (same case names, one
  extra `Describe` segment); `time.Sleep` becomes `ctx.Eventually` on `ActorExists`; the failing store
  becomes a `mock.Controller` adapter (`writeOnlyStoreMock`, `Times(1)`); helpers take `*specs.Context`;
  the scope loop is a `specs.Table` whose rows build their context inside the case; nil, length and
  redundant `Not(BeNil())` checks use the specific matchers.
- `saga/saga_test.go`: every `mocks.EventsStore` becomes `sagaStoreMock`, a small adapter over
  `mock.Controller`. Expectations are now verified when each case ends. The dead expectations of the
  "missing behavior" case become `Never()`, which now asserts the store is not touched.

## What does not change, and why

- **The actor-system tests.** `TestSagaActor` (33 `t.Run` cases) and `TestSagaFailsClosed` start a real
  in-process goakt actor system and wait on real timeouts (the suite takes about 12 s). Removing that
  needs a production seam for the actor system and clock, so they stay testify/`require`; only their
  events-store mock changed. The three converted tenant tests also still start a goakt system, because
  `sendCommand` and `compensate` need a live actor to talk to.
- **Fakes with behavior.** `enginetest.CallbackSagaBehavior`, `ctxCapturingActor` and the in-memory
  `testkit` stores are inputs of the test, not recorders of a replaced dependency.
- **`command_context_test.go`, `tenant_binding_test.go`.** Already single-assertion specs; a table would
  not read better.

## Constraints

- Every case and invariant stays. 127 `--- PASS` lines before, 128 after (the one extra is the new
  `Describe` level of `TestSagaActorSendCommandThreadsTenantContext`). Leaf names are identical; the three
  converted tests gained one `Describe` segment in the path.
- No force-push, no `-race`, no workbench. TDD is strict, runner `go test ./internal/engine/protocol/...
  ./internal/engine/saga/...`; each task shows a RED production mutation, then reverts it.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3). Route: inline. Evidence: `5c348de`, clean build, no
      conflicts.
- [x] T2 protocol helper and matchers. Route: inline. Evidence: `138cf0f`. RED: deadline sentinel built as
      canceled, failed with `expected canceled to equal timed_out`; a prefix sentinel `ego: ` failed the
      registry check with `to be empty, got length 3`.
- [x] T3 tables for the no-op action, status round trip and `SagaStatus.String`. Route: inline. Evidence:
      `2a22467`. RED: `&&` changed to `||` in `actionIsNoop` (`expected true to equal false`);
      compensating mapped to running (`expected running to equal compensating`).
- [x] T4 tenant actor tests: Controller, polling, matchers. Route: inline (one file, already understood).
      Evidence: `087ddc1`. RED: dropping the `return` after a failed binding write failed
      `a failed first WriteEvents leaves zero residual appropriation`; compensation without the tenant
      context failed `compensate dispatches under boundTenant`.
- [x] T5 `saga_test.go` events-store mock to `mock.Controller`. Route: inline. Evidence: `f213b1b`. RED:
      `Ping` skipped in PreStart (`unexpected call GetLatestEvent ... unmet expectation Ping`); replay limit
      off by one (`unexpected call ReplayEvents(..., 1, 3, 4)`). The old mock would not have caught either.

## Follow-up spec (not in this document)

`saga-actor-component-lane`: move the goakt-based saga tests out of the unit lane or give `Actor` a seam
for the actor system and clock, so the 33 `TestSagaActor` cases and the 5 s default-timeout case stop
using real time.

## Progress

- 2026-09-30: T1-T5 done. Checks: build, vet, gofmt and golangci-lint clean; `-count=5` passes. Coverage
  protocol 51.4% before and after; saga 88.6-89.3% before, 88.3-89.3% after (actor timing makes the figure
  vary between runs on both sides). Engram mirror: pending.
