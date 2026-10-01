# Tenant adoption fault stores on mock.Controller (follow-up of #228)

Depends on #228 (`test/205-migrate-tenant-adoption`).

## Problem

`migration/tenant_adoption_test.go` defines about twenty hand-written store wrappers. The #228 rework left them
alone and promised a follow-up: replace a wrapper with a go-specs `mock.Controller` only where it adds no
behavior beyond injecting a canned failure or recording calls.

## What changes

Only `migration/tenant_adoption_test.go`. The three `untouchable*Store` types (a nil embedded interface that
panics on any call) become `eventsStoreMock`, `snapshotStoreMock` and `stateStoreMock`, thin adapters that
forward every method to a `mock.Controller`. `TestNewTenantAdopterRejectsAnInvalidSourceScope` declares no
expectation, so any store call fails the case immediately and names the method and arguments, instead of a nil
pointer panic.

## What does not change, and why

I went through every wrapper. The test is: does it only fail or record, or does it also run real store logic?

- **Kept, real behavior:** each of these embeds a real `testkit` store and delegates to it, and the adopter's
  later reads depend on the data the real store holds. A mock would have to re-implement the store to keep the
  tests meaningful.
  - `corruptingEventsStore`, `corruptingSnapshotStore`, `corruptingStateStore`: rewrite the written record, then
    persist it.
  - `duplicatingEventsStore`, `hidingEventsStore`, `reorderingEventsStore`: transform what a real replay returns
    (reorder state is kept per call).
  - `racingEventsStore`, `racingSnapshotStore`, `replacingEventsStore`, `replacingSnapshotStore`,
    `recreatingEventsStore`, `recreatingSnapshotStore`, `raceTargetStateStore`: perform a second real write at
    a chosen moment of the adopter's calls.
  - `fencedSnapshotWriter`, `fencedSourceWriter`, `testFence`: real lock and goroutine behavior.
  - `panickingEventsStore`: panics only on target writes and delegates the source reads and writes to the real
    store, so it is a scoped fault, not a canned one.
- **Production code.** Untouched.

## Constraints

Every test and subtest name stays. No `-race`, no workbench. Runner: `go test ./migration/...`.

## Tasks

- [x] T1 Replace `untouchable*Store` with controller-backed adapters. Route: inline (one file). Evidence:
      RED by production mutation: calling `a.eventsStore.Ping` before the scope check in `NewTenantAdopter`
      failed the case with `mock: unexpected call Ping(context.Background): no expectations declared for this
      method`; reverted. GREEN with the mutation removed.
- [x] T2 Verify and deliver. Evidence: 153 `--- PASS` before and after with an identical name list; coverage
      of `migration` 82.8% before and after; `-count=5`, `go vet`, `golangci-lint` (0 issues), `gofmt` clean.

## Note on PR #227

`buildLegacyEventBytes` on develop still takes `*testing.T` and builds raw protobuf bytes, while
`newLegacyEvent` here builds events through `anypb`. Once #227 makes it `testing.TB`, the two file-level
legacy-event builders could share it. Not done here and nothing depends on it.

## Follow-up

None.

## Progress

- 2026-09-30: T1 and T2 done. Engram mirror: pending.
