# Persistence Tenant Isolation Specification

## Purpose

Close the gap `ego-store-001`'s ratified `persistence-store-contract` spec
already documented and deliberately left open: `persistence_id` alone,
without a companion tenant boundary, does not prevent two different
tenants from colliding on the same record. This specification introduces
`persistence.Scope` (`persistence/scope.go`) as the vocabulary for that
boundary and states the normative requirements the type, and the store
behavior later slices build on top of it, must satisfy. This slice
(`ego-tenant-003` T1) delivers only the `Scope` type itself; requirements
below that describe store read/write behavior state the contract those
later slices (T2–T4, see `tasks.md`) must meet — they are not yet
satisfied by any store implementation, since no store signature has
changed yet.

## Requirements

### Requirement: Explicit Tenant Scope At The Boundary

A caller MUST be able to state, as an explicit, typed value, which tenant
boundary a persistence operation belongs to. `persistence.Scope` MUST
provide exactly two ways to obtain a valid value: `Unscoped()`, for
non-tenant deployments and administrative execution, and
`NewTenantScope(tenancy.TenantID) (Scope, error)`, for a specific tenant.
The zero value of `Scope` MUST be invalid, so that a caller can never
silently fall through to a meaningful scope by leaving a `Scope` field or
variable unset.

#### Scenario: Zero value is invalid

- GIVEN a zero-value `persistence.Scope` (e.g. `var s persistence.Scope`)
- WHEN `s.Valid()` is called
- THEN it returns `false`

#### Scenario: Unscoped and a tenant scope are both valid

- GIVEN `persistence.Unscoped()` and a `Scope` built via
  `persistence.NewTenantScope` with a valid `tenancy.TenantID`
- WHEN `Valid()` is called on each
- THEN both return `true`

#### Scenario: NewTenantScope rejects an invalid tenant id

- GIVEN the zero value of `tenancy.TenantID` (an empty string)
- WHEN `persistence.NewTenantScope` is called with it
- THEN it returns a non-nil error matching `persistence.ErrInvalidScope`
  via `errors.Is`, and the returned `Scope` is not usable

### Requirement: Effective Identity Is The Pair (Scope, persistence_id)

The effective identity of a persisted aggregate MUST be understood as the
pair `(Scope, persistence_id)`, never `persistence_id` alone.
`persistence_id` keeps the exact meaning `ego-store-001` already ratified
— an opaque, caller-assigned string the store never parses, prefixes, or
reinterprets. `Scope` is a structurally independent value, never derived
from `persistence_id` by composition, parsing, or any other
transformation, and never derived by parsing `Scope.String()` back apart
either.

#### Scenario: Two tenant scopes with different ids are distinct scopes

- GIVEN two `Scope` values built from two different, valid
  `tenancy.TenantID`s
- WHEN they are compared with `Equal()`
- THEN it returns `false`

#### Scenario: Two tenant scopes with the same id are the same scope

- GIVEN two `Scope` values independently built via `NewTenantScope` from
  the same `tenancy.TenantID`
- WHEN they are compared with `Equal()`
- THEN it returns `true`

#### Scenario: String() is documented as unsafe for building a storage key

- GIVEN `Scope.String()`'s doc comment
- WHEN it is read
- THEN it states explicitly that `String()` is a diagnostic rendering
  only, and that a store MUST key on the `(Scope, persistence_id)` pair
  structurally rather than by concatenating or parsing `String()`'s output

### Requirement: Cross-Tenant Read Isolation

Once a store's method signatures carry `Scope` (delivered by the T2
follow-up slice named in `tasks.md`; not yet true of any store at the end
of this slice), a read performed under one `Scope` MUST NOT return a
record that was written under a different `Scope`, even when both records
share the same `persistence_id`.

#### Scenario: A tenant cannot read another tenant's record under the same persistence_id

- GIVEN a record written with `Scope` = tenant A and `persistence_id` =
  `"order-42"`, and no record written with `Scope` = tenant B and the same
  `persistence_id`
- WHEN a read is performed with `Scope` = tenant B and `persistence_id` =
  `"order-42"`
- THEN the store reports no record found for tenant B, never tenant A's
  record

### Requirement: Cross-Tenant Write Isolation

A write performed under one `Scope` MUST NOT modify, overwrite, or
otherwise affect a record that belongs to a different `Scope`, even when
both records share the same `persistence_id`. This holds independently of
`WritePrecondition`/CAS evaluation, which (per the next requirement)
continues to operate only within one `Scope`.

#### Scenario: A tenant's write does not affect another tenant's record under the same persistence_id

- GIVEN an existing record written with `Scope` = tenant A and
  `persistence_id` = `"order-42"`
- WHEN a write is performed with `Scope` = tenant B and the same
  `persistence_id`, regardless of the `WritePrecondition` used
- THEN tenant A's record is unchanged, and the write either creates a new,
  independent record scoped to tenant B or is evaluated purely against
  tenant B's own prior state — never tenant A's

### Requirement: WritePrecondition And CAS Semantics Are Preserved Within A Scope

`persistence.WritePrecondition` and `*persistence.ConflictError`, as
ratified by `ego-write-004` and cited by `ego-store-001`'s R2, are
unchanged by this specification. Their compare-and-swap evaluation MUST
continue to operate against the `StorageRevision` recorded for one
`(Scope, persistence_id)` pair — introducing `Scope` narrows what
`persistence_id` alone used to mean as a key, it does not alter how a
precondition is evaluated once the correct record is identified.

#### Scenario: A precondition evaluated within one tenant's scope behaves exactly as it did before Scope existed

- GIVEN a single `Scope` and a single `persistence_id`, with no other
  tenant's data involved
- WHEN a conditional write with any `WritePrecondition`
  (`Unconditional()`, `ExpectGenesis()`, or `ExpectRevision(N)`) is
  evaluated
- THEN the accept/reject outcome and any `*ConflictError` returned are
  identical to what `ego-write-004`'s existing contract already specifies
  for a single-`persistence_id` write, with no additional condition
  contributed by `Scope` beyond identifying the correct record

### Requirement: Unscoped Backward Compatibility And Non-Collision

`persistence.Unscoped()` MUST be byte-compatible with pre-TENANT-003
behavior: a store that always receives `Unscoped()` MUST key, read, and
write exactly as it did before `Scope` was introduced, so that an existing
non-tenant deployment requires no data migration. `Unscoped()` MUST NOT be
`Equal()` to any `Scope` returned by `NewTenantScope`, for any valid
`tenancy.TenantID` — including one whose literal text is `"unscoped"`.

#### Scenario: Unscoped is never equal to a tenant scope, in either direction

- GIVEN `persistence.Unscoped()` and a `Scope` built via
  `NewTenantScope` for any valid `tenancy.TenantID`
- WHEN they are compared with `Equal()` in both directions
- THEN both comparisons return `false`

#### Scenario: A tenant named "unscoped" does not forge the unscoped scope

- GIVEN a `Scope` built via `NewTenantScope` for the `tenancy.TenantID`
  whose literal value is `"unscoped"`
- WHEN it is compared with `persistence.Unscoped()` via `Equal()`
- THEN the comparison returns `false`, because `Equal()` compares the
  scope's structural kind before ever comparing tenant identity text

### Requirement: tenant_metadata Stays Non-Authoritative

Introducing `Scope` MUST NOT change what `ego-store-001`'s R7 already
ratified about `tenant_metadata`: it remains data the store persists and
returns unchanged, and it MUST NOT be read, parsed, or otherwise consulted
to derive, validate, or substitute for a `Scope` value at any store
boundary. Isolation is provided by `Scope` being passed and enforced as an
explicit parameter, never by inspecting payload content.

#### Scenario: Scope is never derived from tenant_metadata

- GIVEN this specification's deliverables (`persistence/scope.go` and its
  tests)
- WHEN reviewed for any code path that reads `tenant_metadata` to produce,
  validate, or compare against a `Scope` value
- THEN none exists — `Scope` is constructed only from a caller-supplied
  `tenancy.TenantID` or via `Unscoped()`, never from message payload
  content

## Out of Scope (cross-reference)

Changing any store interface signature to accept `Scope`: T2 (not yet
filed as its own tracker; see `tasks.md`'s follow-up chain). A
cross-tenant conformance test suite proving the isolation requirements
above end to end: T3. Engine/actor wiring that resolves a real
`tenancy.TenantContext` into a `Scope` instead of always passing
`Unscoped()`: T4. External-adapter migration documentation: T5. Any
redefinition of `WritePrecondition`, `ConflictError`, or CAS ownership:
`ego-write-004` (`#65`), unchanged and un-reopened. Any change to
`tenancy`'s own API or its stdlib-only constraint: out of scope entirely —
this specification only adds a new consumer of `tenancy.TenantID`.
