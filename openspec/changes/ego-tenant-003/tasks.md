# Tasks — EventStore tenant isolation (EGO-TENANT-003)

Tracker `#92`, epic `#23`. This change carries exactly one atomic task,
capped by this repository's hard 4-5-task spec-sizing rule at "as many
tasks as this slice needs, no more" — this slice needs one. The remaining
isolation work does not fit in this change; it is named below as an
explicit follow-up chain of later, separately-authorized SDD changes.

## T1 — Introduce `persistence.Scope`

- [x] 1.1 Write `persistence/scope_test.go` first (strict TDD is in force
      for this repository). Covers: zero value invalid; `Unscoped()` and a
      tenant scope both valid; `NewTenantScope` rejects an empty
      `tenancy.TenantID` with `ErrInvalidScope`; `Unscoped()` is never
      `Equal()` to a tenant scope in either direction; two tenant scopes
      compare equal iff their `tenancy.TenantID` is equal; `IsUnscoped()`
      is true only for `Unscoped()`; `TenantID()` round-trips for a tenant
      scope and is the zero value when unscoped; `String()` distinguishes
      the two kinds; a tenant literally named `"unscoped"` still does not
      equal `Unscoped()` (the forging guard — see design.md D3).
      Req: `specs/persistence-tenant-isolation/spec.md` — "Explicit Tenant
      Scope At The Boundary", "Effective Identity Is The Pair (Scope,
      persistence_id)", "Unscoped Backward Compatibility And
      Non-Collision".
- [x] 1.2 Ran the test suite before `scope.go` existed and observed it
      fail to compile (RED): `go test -mod=vendor ./persistence/...` →
      `undefined: persistence.Scope`, `undefined: persistence.Unscoped`,
      `undefined: persistence.NewTenantScope`,
      `undefined: persistence.ErrInvalidScope` (10 errors, capped at "too
      many errors" by the compiler).
- [x] 1.3 Implement `persistence/scope.go`: `Scope` value type (unexported
      `kind`/`tenant` fields, invalid zero value), `Unscoped()`,
      `NewTenantScope(tenancy.TenantID) (Scope, error)`, `ErrInvalidScope`
      sentinel, `Valid()`, `IsUnscoped()`, `TenantID()`, `Equal()`,
      `String()` — mirroring `persistence/precondition.go`'s conventions
      exactly (unexported fields, named constructors, `Valid()`,
      `String()`), per design.md D1–D3.
      Req: same as 1.1, plus "tenant_metadata Stays Non-Authoritative"
      (this task does not touch `tenant_metadata` handling at all, which
      is itself the evidence for that requirement at this slice).
- [x] 1.4 Ran the test suite again and observed it pass (GREEN):
      `go test -mod=vendor ./persistence/... -run TestScope -v` → 10/10
      `PASS`, then the full package `go test -mod=vendor ./persistence/...`
      → `ok`.
- [x] 1.5 Confirmed `tenancy` gained no dependency:
      `go test -mod=vendor -run TestTenancyArchitecture .` → `PASS`
      (import is one-directional, `persistence` → `tenancy`, per
      design.md D5).
- [x] 1.6 `go build -mod=vendor ./...` and
      `go vet -mod=vendor ./persistence/...` both clean.
- [x] 1.7 Wrote this proposal, `design.md`,
      `specs/persistence-tenant-isolation/spec.md`, and this task file.

**Evidence**: `persistence/scope.go`, `persistence/scope_test.go`. All
four verification commands captured in the apply/commit record for this
change (`go build -mod=vendor ./...`, `go test -mod=vendor
./persistence/...`, `go test -mod=vendor -run TestTenancyArchitecture .`,
`go vet -mod=vendor ./persistence/...`) — all clean. No production
interface signature was changed; `Scope` exists as an unused-by-any-store
type at the end of this slice, exactly as scoped.

## Follow-up chain (not part of this change; named here per this
## repository's 4-5-task spec cap, each a separate, later SDD change)

- **T2 — Extend the tenant-aware SPI**: change
  `EventsStore.WriteEvents`/`ReplayEvents`/`GetLatestEvent`/etc.,
  `StateStore.WriteState`/`GetLatestState`, and `SnapshotStore`'s methods
  to take a `persistence.Scope` parameter (placement and exact signature
  shape is T2's own design decision — see design.md, "What this slice
  deliberately leaves open"); update the `testkit` stores' keying to the
  pair `(Scope, persistence_id)`; regenerate/update any mocks; update
  every production call site (actors, migration tooling, projection
  runner) to pass `Unscoped()` until T4 wires a real tenant through. This
  is expected to be a breaking-signature change of similar shape and size
  to `ego-write-004`'s PR2 (SPI break + testkit CAS + mock regen + call
  sites) and should be planned as its own chained-PR sequence.
- **T3 — Cross-tenant conformance suite**: a test suite proving read and
  write isolation directly against the `testkit` stores (independent of
  any actor mailbox, mirroring `ego-store-001`'s T3.3 finding that
  direct-against-store evidence is stronger than actor-mediated evidence)
  — two tenants presenting the same `persistence_id` must not observe or
  overwrite each other's records, under `-race`.
- **T4 — Engine/actor wiring**: resolve the actor's real
  `tenancy.TenantContext` at construction/recovery time and pass the
  corresponding `persistence.Scope` into every store call, replacing the
  `Unscoped()` placeholder T2 introduced; decide and document what
  `Scope` an actor running under `tenancy.ScopeAdministrative` receives.
- **T5 — Migration and compatibility documentation**: write the
  external-adapter migration guidance for this breaking change, in the
  style of the WRITE-004 breaking-change block already present at the top
  of `persistence/events_store.go` ("Breaking change: WriteEvents gained a
  required precondition parameter").
