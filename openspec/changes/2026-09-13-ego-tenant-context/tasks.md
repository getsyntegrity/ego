# Tasks: Canonical TenantContext / TenantResolver SPI (EGO-TENANT-001)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~900 (12 tenancy files, root test, Makefile edit) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 foundation -> PR2 resolver/context -> PR3 conformance+mocks |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

Rationale: one leaf package, no engine wiring, but 12 files plus two acceptance scenarios and a conformance test exceed 400 lines; splits cleanly by file. Chain strategy is the orchestrator's call.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Errors, TenantID, TenantContext | PR 1 | `go test ./tenancy/... -run 'Error\|TenantID\|TenantContext'` | N/A — pure value types | Delete `errors*.go`, `tenant_id*.go`, `tenant_context*.go` |
| 2 | Resolver, context propagation, metadata | PR 2 | `go test ./tenancy/... -run 'Resolver\|Context\|Metadata'` | In-process saga/entrypoint simulation | Delete `resolver*.go`, `context*.go`, `metadata*.go` |
| 3 | Conformance test, mocks, Makefile | PR 3 | `go test -run TestTenancyArchitecture ./...` | `go list -deps ./tenancy/...` (real exec) | Delete `tenancy_architecture_test.go`, `mocks/tenancy/tenant_resolver.go`, revert `Makefile` |

## Phase 1: Foundation

- [ ] 1.1 RED: `tenancy/errors_test.go` — `Reason`, `Error.Unwrap/Is`, sentinels `ErrMissing/ErrInvalid/ErrDenied`
- [ ] 1.2 GREEN: `tenancy/errors.go` implementing 1.1
- [ ] 1.3 RED: `tenancy/tenant_id_test.go` — empty/whitespace/control-rune/invalid-UTF-8/>128-byte rejected; arbitrary non-UUID accepted
- [ ] 1.4 GREEN: `tenancy/tenant_id.go` — `TenantID`, `NewTenantID` (R1)
- [ ] 1.5 RED: `tenancy/tenant_context_test.go` — empty `TenantID` fails; admin scope type-distinct, attributed
- [ ] 1.6 GREEN: `tenancy/tenant_context.go` — `Scope`, `Administrative`, `TenantContext`, constructors, accessors

## Phase 2: Core Mechanics

- [ ] 2.1 RED: `tenancy/resolver_test.go` — `WithSingleTenant` produces a `TenantContext` indistinguishable from any resolver's
- [ ] 2.2 GREEN: `tenancy/resolver.go` — `TenantResolver` interface, `WithSingleTenant`
- [ ] 2.3 RED: `tenancy/context_test.go` — `Attach` idempotent/`ErrDenied` on change; `From`/`Require` `ErrMissing`; `VerifyUnchanged` -> `ErrDenied`
- [ ] 2.4 RED: `tenancy/context_test.go` — saga: `context.Background()` reset, reconstruct via metadata+`Attach`; skip -> `ErrMissing`
- [ ] 2.5 RED: `tenancy/context_test.go` — invocation: entrypoint `Resolve`+`Attach`, behavior only `Require`; skip -> fails
- [ ] 2.6 GREEN: `tenancy/context.go` — `Attach`, `From`, `Require`, `VerifyUnchanged` satisfying 2.3-2.5
- [ ] 2.7 RED: `tenancy/metadata_test.go` — `MarshalMetadata`/`UnmarshalMetadata` round-trip, tenant and administrative
- [ ] 2.8 GREEN: `tenancy/metadata.go` — `Metadata`, marshal/unmarshal (`ego.tenant.*` keys)

## Phase 3: Conformance & Tooling

- [ ] 3.1 RED: `tenancy_architecture_test.go` (root, pkg `ego`) — `go list -deps ./tenancy/...` fails on non-stdlib import
- [ ] 3.2 GREEN: confirm `tenancy/` has zero non-stdlib imports; 3.1 passes unmodified
- [ ] 3.3 Generate `mocks/tenancy/tenant_resolver.go` mock for `TenantResolver`
- [ ] 3.4 Add tenancy mock generation to `Makefile` `docker-mock` target

## Phase 4: Verification

- [ ] 4.1 Run `go mod tidy && go mod vendor`, then `go test -mod=vendor -p 1 -timeout 0 -race ./...` — confirm all spec scenarios pass
- [ ] 4.2 Run `go vet ./tenancy/...`; confirm `behavior.go`/`saga.go`/`engine.go`/`option.go` are byte-identical (proposal Success Criteria)
