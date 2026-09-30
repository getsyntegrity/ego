# Engine actor tests on go-specs v0.3.3: the chain (#238)

## Problem

PR #238 (`test/205-migrate-engine-actors-unit`) migrated the pure unit tests of
`internal/engine/eventsource` and `internal/engine/durablestate` to go-specs. The same files still hold the
older `testing.T` tests, and those tests have three problems:

- **They start a real goakt actor system inline.** It is started about 70 times in
  `event_sourced_actor_test.go` and 27 times across the other files.
- **They mock with testify.** They use the generated `mocks/persistence`, `mocks/encryption` and
  `mocks/eventadapter` mocks: 58 `EXPECT()` calls in eventsource and 8 in durablestate.
- **They synchronize on real time.** There are about 160 `pause.For` waits, and `retryWithBackoff` sleeps
  with `time.After`.

The two packages currently take 225.6 s (eventsource) and 22.2 s (durablestate).

## What the chain does

The rule is that no unit test reaches an external resource. A test that needs a real actor system is a
component test. goakt's `ReceiveContext` is a concrete struct with unexported fields, and goakt's own
testkit also starts a real system, so an actor's `Receive` cannot be unit tested. Two consequences follow:

- Cases that only exercise logic reachable through a `context.Context` method (`recover`,
  `recoverFromStore`, `persistStateAndPublish`, and so on) move to pure unit tests. Their dependencies are
  go-specs `mock.Controller` adapters.
- The remaining actor-system cases stay as component tests. Their `pause.For` waits become `ctx.Eventually`,
  and they are listed as component-lane in `docs/testing/unit-migration.md` (from #218).

The generated `mocks/*` stay in the repository. Ten other test files still import them, so this chain only
stops these nine files from importing them.

## Specs, in order

The specs are stacked PRs on top of #238, each with at most 5 tasks.

1. **S1 `engine-test-seams`** (`odd/tasks/engine-test-seams.md`). It adds a clock seam for
   `retryWithBackoff` and moves `retry_test.go` off real time. It also adds typed `mock.Controller`
   adapters for `EventsStore`, `SnapshotStore`, `StateStore`, `Encryptor` and `EventAdapter` in
   `internal/engine/enginetest`.
2. **S2 `durablestate-actor-go-specs`.** The `StateStore` failure cases move to unit tests on the S1
   adapters. The 22 `pause.For` waits become `Eventually`, and the component cases are reclassified.
   Depends on S1 and #218.
3. **S3a `eventsource-actor-core-go-specs`.** Covers `TestEventSourcedActor` and `ErrorPaths`. About 11
   recovery failure cases move to unit tests on the S1 adapters, and their `pause.For` waits become
   `Eventually`. Depends on S1.
4. **S3b `eventsource-actor-batch-go-specs`.** Covers `Batch`, `BatchTenantHomogeneity*`,
   `GetStateDuringPersist`, tenancy and `tenant_persist`. It replaces their `pause.For` waits and does the
   final reclassification. Depends on S3a and #218.

## Why this cut

Each spec falls on a package or on a group of Test functions, so each PR is reviewable on its own. S1
comes first because S2 and S3 both need the adapters and the retry seam. The alternative was one spec per
file, which we rejected: `event_sourced_actor_test.go` alone has 5107 lines and 10 Test functions, so a
single spec would need far more than five tasks.

## Progress

- 2026-09-30: `develop` (go-specs v0.3.3) was merged into the #238 branch and pushed. `go mod tidy` is
  clean in every module. The two packages are green at 225.6 s and 22.2 s. S1 has started on
  `refactor/engine-test-seams`.
