# Report test failures through go-specs, not raw t.Fatalf (follow-up of #205)

## Problem

The rule for unit tests is that every failure is reported through go-specs (`ctx.Expect(...).To(matcher)`),
not through `t.Fatalf` or `t.Errorf`. A raw `t.Fatalf` bypasses the spec: the failure has no go-specs
formatting and is not attributed to the case. The task list for this follow-up named 24 files. Scanning them
on `develop` showed that most already comply (their `Errorf` hits are `fmt.Errorf` or `err.Error()`). The raw
reporting was concentrated in four places, plus a few call sites that must stay.

## What changes

Only `*_test.go` files change.

- `migration/tenant_adoption_test.go`: the 18 fixture helpers (`newLegacyEvent`, `connectedEventsStore`,
  `tenantScope`, `seedEvents`, `newAdopter`, `mustAdopt`, `latestEvent`, `replayEvents` and so on) took a
  `testing.TB` and called `t.Fatalf`. They now take a `*specs.Context` and assert with
  `ctx.Expect(err).To(specs.BeNil())`. About 370 call sites changed from `ctx.T` to `ctx`. Seven Describe-level
  lines that built a tenant scope or context with the Describe's `*testing.T` now assign variables in
  `s.BeforeEach`, which go-specs v0.3.3 provides (`Spec.BeforeEach`, `spec.go`). One table of cases read such a
  variable while the table was being built, before any hook ran, so its `metadata` field became a function
  evaluated inside the case.
- `migration/migration_test.go`: the unused `buildLegacyEventBytes(testing.TB, ...)` is removed. Its one caller
  now uses `legacyEventBytes(ctx, ...)`, which already existed.
- `port/adapter/assertion_sites_test.go`: `TestOptionalInterfacesAreAssertedOnlyInTheirAccessors` and
  `TestNoPrivateCopiesOfOptionalInterfaces` were plain tests. They now run under `specs.Describe`: the scan is
  asserted in `BeforeEach` and each rule is an `It` that checks `BeEmpty` or `Equal`, so a stray site is named
  in the failure.
- `publisher/{kafka,nats,pulsar,websocket}/closure_test.go`: same pattern. `go list` is asserted in
  `BeforeEach`, and the violation messages are checked with `BeEmpty`. `websocket` is not in the task list but
  has the identical file, so it is included.

## What does not change, and why (kept call sites)

- `migration/migration_test.go` lines 130 and 143, `ctx.T.Errorf("disconnect ...")`: inside `ctx.Cleanup`
  teardown, which runs after the case's assertions, so there is no spec step left to report through.
- `persistence/conflict_test.go`, `FuzzParseConflictErrorRoundTrip` (`t.Skip`, `t.Fatalf`, `t.Fatal`): a fuzz
  target receives a `*testing.T` from `f.Fuzz` by API and has no `*specs.Context`. `docs/testing/go-specs.md`
  documents exactly this form (`t.Fatal(m.FailureMessage(got))`).
- `t.Logf` in `byCheck` (`publishingtest_internal_test.go`) and `outcomes` (`adaptertest_test.go`): diagnostic
  logging only, not failure reporting.
- Every other listed file (command, compose/internal/lifecycle, internal/engine/protocol, internal/goaktlog,
  internal/runtimeconsumer, migration/closure, port/behavior, port/publishing, port/runtime, adaptertest
  architecture and unreachable tests, adapter architecture) has no raw reporting call; its `Errorf` hits are
  `fmt.Errorf` or `err.Error()`. Nothing to change.
- Production code, `engine/`, `internal/engine/{saga,eventsource,durablestate}`, `internal/instrumentation`,
  `internal/logging` and `internal/projectionrunner` are untouched (other writers own them).
- Rejected: converting the fuzz target to `specs.Describe` inside `f.Fuzz`. It would create a spec tree per
  fuzz input and break the documented fuzz convention.
- Lost detail: helper failures no longer carry the identifier in the old message (for example `latest event %q`).
  go-specs reports the error value and the helper line instead.

## Constraints

Every case stays. `--- PASS` names: none removed; added names are the new subtests of the converted plain tests
(`TestOptionalInterfaces...`, `TestNoPrivateCopies...`, `TestUnitTestClosureExcludesRuntimeAndRoot` in the four
publishers). Top-level `Test` names are unchanged. No `-race`, no workbench, no external resource.
TDD runner: `go test` on the touched packages (nested modules from their own directory).

## Tasks

- [x] T1 migration helpers to `*specs.Context`. Route: inline mechanical rewrite with `sd` plus a python pass.
  RED: `NewTenantScope("")` in `tenantScope` fails `TestTenantAdopterMissingSourceClassification` with
  `expected nil, got persistence: scope is not valid` at the helper line; `store.Connect` replaced by an error
  fails `TestTenantAdopterDryRunWritesNothing` with `expected nil, got boom`. GREEN: `go test -count=5`.
  Commit `8a4554f`.
- [x] T2 port/adapter assertion-site scans under specs. Route: inline. RED: removing `PingerOf` from
  `allowedAssertionSites` gives `expected [port/adapter/adapter.go:PingerOf] to be empty, got length 1`;
  changing the expected owner gives `expected [port/adapter/adapter.go] to equal [nowhere.go]`. Commit `0edd4ef`.
- [x] T3 publisher closure guards under specs. Route: inline (one file copied to four modules). RED: treating
  `fmt` as the engine package fails with `expected [unit-test closure regressed: ...] to be empty`; a bad
  `go list` target fails the `BeforeEach` with `expected nil, got go list -deps -test ...`. Commit `e85bcc4`.
- [x] T4 classify the rest and record this document. Route: inline. Commit: the one that adds this file.

## Follow-up

`spec-reporting-helpers-2`: files outside the original list that still use raw reporting:
`compose/goakt/app_test.go` (62), `compose/goakt/runtime_test.go` (10), `compose/goakt/fixtures_test.go` (1),
`publisher/websocket/conformance_test.go` (6), `benchmark/benchmark_test.go` (23, benchmarks: no go-specs
context by design, to be classified), `example/cluster/stores_postgres_test.go` (2, Postgres-gated, out of unit
phase), `internal/engine/protocol/expected_revision_test.go` (1).

## Progress

Coverage before and after (same statements): migration 82.8%, port/adapter 95.0%, publisher/kafka 7.4%,
nats 5.3%, pulsar 7.1%, websocket 87.9%. `go test -count=5` passes on all touched packages. `golangci-lint`
reports 0 issues except one existing `unused` finding on `publisher/pulsar/pulsar.go` (`config` field), present
on `develop`.
