# tenancy tests on go-specs v0.3.3 (#223)

## Problem

PR #223 (branch `test/205-migrate-tenancy`) moved the unit tests of the `tenancy` package to go-specs
v0.3.1. They already use `specs.Describe`, but a few things the #219 conventions now ask for are still
missing:

- `resolveCountingResolver` in `resolver_test.go` is a hand-written recording fake. It counts `Resolve`
  calls in a field, and the test asserts `r.calls == 0` by hand.
- `mustTenantID`, `mustResolve` and `simulateSagaStep` call `t.Fatalf`, which bypasses the spec.
- `metadata_test.go` reads the map by hand (`md[key]` plus `_, ok := md[key]` and `BeFalse`).
- Four case loops (`s.It` inside `for`) are written by hand instead of `specs.Table`.
- Many `ctx.Expect(err).To(specs.Not(specs.BeNil()))` lines sit right before `MatchError`, which already
  fails for a nil error.

## What changes

Only `*_test.go` files under `tenancy/` change.

- `resolveCountingResolver` becomes `resolverMock`, a `mock.Controller` adapter. The case declares
  `Expect(mock.Any()).Never()` for `Resolve` and reads `Calls()` with `BeEmpty()`.
- The helpers take `*specs.Context` and assert with `ctx.Expect(err).To(specs.BeNil())`. No `t.Fatalf` is left.
  The accessor tables build their resolver inside the case through a small function, so a failure while
  building it is also reported by the spec.
- Metadata checks use `HavePair` and `Not(HaveKey(...))`.
- The redundant `Not(BeNil())` lines before `MatchError` are removed. Test: a nil error produces
  `expected an error matching ... got a nil error`, so nothing is lost.
- Loops become `specs.Table`: the sentinel-by-reason cases, the invalid tenant-id and the
  actor/reason cases of `tenant_context_test.go`, the non-UUID identifiers, and the two fixed-tenant accessor tables.

## What does not change, and why

- **Production code.** Nothing under `tenancy/` other than `*_test.go` is touched.
- **`fixedResolver` and `advertisingResolver`.** They are real resolvers that are inputs of the test: the
  tests ask what the accessors say about each shape of resolver. They do not stand in for an external dependency.
- **`simulateSagaCommand`, `simulateBehavior` and `simulateEntrypoint`.** They model the code around the
  tenancy package. They have real behavior and no calls to verify.
- **Single-`It` `Test` functions** such as the `NewTenantID_Rejects*` family. Each has its own `Test` name
  and `Describe`, which must stay, so a one-row table would add nothing.
- **Time, network and databases.** The package has none: there are no sleeps and no external resources.

## Constraints

- Every case and invariant stays. Subtest names stay. The case count does not drop: 153 `--- PASS` before
  and after, with the sorted list of names identical.
- No force-push (`develop` is merged into the branch), no `-race`, no workbench.
- TDD is strict. The test runner is `go test ./tenancy/`. RED is shown per task by a deliberate production
  mutation that the rewritten cases catch, and the mutation is reverted.
- Delivery: plain push to `test/205-migrate-tenancy` and update the #223 description.

## Tasks

- [x] T1 Merge `origin/develop` (go-specs v0.3.3). Route: inline. Evidence: merge commit `b036039`, clean
      build, no change to any `go.mod` or `go.sum`, so `go mod tidy` is a no-op.
- [x] T2 Matchers and tables in the unit tests. Route: delegated writer (one bounded writer). Evidence:
      `94d4a1b`. RED mutations, both caught: `MarshalMetadata` always writing the correlation id key failed
      with `expected map[...] not to be having key ego.tenant.admin_correlation_id`; `NewTenantID` ignoring
      control runes failed with `expected an error matching tenancy: tenant identity invalid ..., got a nil
      error` on the control-rune cases.
- [x] T3 `mock.Controller` for the Resolve count and spec-based helpers. Route: the same writer. Evidence:
      `156504a`. RED mutations, both caught: `FixedTenantOf` calling `Resolve` failed with `mock: forbidden
      call Resolve(context.Background): expectation Resolve(any value) declared at resolver_test.go:272 says
      never`; `UnmarshalMetadata` appending a space to the id failed the saga boundary test with `expected nil,
      got tenancy: tenant id must not have leading or trailing whitespace`, reported through the spec and no
      longer through `t.Fatalf`.
- [x] T4 Verify and deliver. Route: inline. Evidence: build, `go vet`, `golangci-lint` (0 issues) and `gofmt`
      are clean; `go test -count=5 ./tenancy/` passes; coverage `tenancy` 91.5% before and after; push and
      PR description update recorded below.

## Follow-up

None needed. The package has no real-time waits and no external dependencies to replace.

## Progress

- 2026-09-30: T1 to T4 done. Engram mirror: not written by this worker.
