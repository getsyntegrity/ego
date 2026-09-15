# Tasks: Canonical command/result envelopes and metadata (EGO-WRITE-003)

Tracker #59 · `specs/command-envelope/spec.md` (read-only) · `design.md` (read-only).

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | ~850-950 (7 types + test siblings + root conformance test) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 identity/errors/principal -> PR2 metadata/envelope/result -> PR3 carrier/conformance/verification |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending (owner decision) |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

Rationale: one leaf package, zero runtime wiring (W1); size mirrors EGO-TENANT-001's ~900-line, 3-PR precedent.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 1 | Errors, identity, principal | PR 1 | `go test ./command/... -run 'Error\|Identity\|Principal'` | N/A — pure value types | Delete `command/{errors,identity,principal}*.go` |
| 2 | Metadata (+Derive), envelope, result | PR 2 | `go test ./command/... -run 'Metadata\|Envelope\|Result'` | N/A — pure value types | Delete `command/{metadata,envelope,result}*.go` |
| 3 | Carrier, conformance, migration confirm, verify | PR 3 | `go test -run TestCommandArchitecture ./...` | `go list -deps ./command/...` (real exec) | Delete `command/carrier*.go`, `command_architecture_test.go` |

## Phase 1: Foundation — Errors & Identity

- [x] 1.1 RED `command/errors_test.go` — sentinels; `Error.Unwrap/Is` vs `ErrRejected/ErrFailed/ErrTimedOut/ErrCanceled`
- [x] 1.2 GREEN `command/errors.go` implementing 1.1
- [x] 1.3 RED `command/identity_test.go` — `NewOperationID` reuses `tenancy.NewTenantID` (read-only) rules; `GenerateOperationID` uniqueness
- [x] 1.4 GREEN `command/identity.go` — `OperationID/CorrelationID/CausationID`, `NewOperationID`, `GenerateOperationID` (crypto/rand)

## Phase 2: Principal & Metadata

- [x] 2.1 RED `command/principal_test.go` — `NewPrincipal` id required, kind optional
- [x] 2.2 GREEN `command/principal.go` implementing 2.1
- [ ] 2.3 RED `command/metadata_test.go` — root: correlation defaults to `CorrelationID(op)`; causation absent
- [ ] 2.4 RED `command/metadata_test.go` — tenant/principal/timestamp/deadline options; `Custom()` defensive copy
- [ ] 2.5 RED `command/metadata_test.go` — `WithCustom` rejects `ego.`-prefixed/canonical keys (`ErrReservedKey`); accepts valid key/value (D6)
- [ ] 2.6 GREEN `command/metadata.go` — struct, `NewMetadata`, options, accessors
- [ ] 2.7 RED `command/metadata_test.go` — `Derive`: correlation inherited, new `operation_id` (else `ErrSameOperationID`), causation = parent op
- [ ] 2.8 RED `command/metadata_test.go` — `Derive`: tenant switch -> `tenancy.ErrDenied` (`tenancy.VerifyUnchanged`, read-only); deadline only shortened (`ErrDeadlineExtension`); custom NOT inherited
- [ ] 2.9 GREEN `command/metadata.go` — `Derive` implementing 2.7-2.8 (D7)

## Phase 3: Envelope & Result

- [ ] 3.1 RED `command/envelope_test.go` — `NewEnvelope` rejects nil payload; `PayloadAs[T]` typed extraction
- [ ] 3.2 RED `command/envelope_test.go` — `Envelope.Derive` delegates to `Metadata.Derive`
- [ ] 3.3 GREEN `command/envelope.go` implementing 3.1-3.2
- [ ] 3.4 RED `command/result_test.go` — `Outcome` zero-value invalid; `String()`; six kinds mutually exclusive
- [ ] 3.5 RED `command/result_test.go` — kind-specific constructors reject wrong shape; `NewTimedOut/NewCanceled` default cause
- [ ] 3.6 RED `command/result_test.go` — `Err()`/`errors.Is/As/Unwrap`; `StateAs[T]`
- [ ] 3.7 GREEN `command/result.go` — `Outcome`, `Failure`, `Result` implementing 3.4-3.6 (D5)

## Phase 4: Carrier (D9)

- [ ] 4.1 RED `command/carrier_test.go` — `Metadata -> Carrier -> Metadata` round-trip; exact `operation_id/correlation_id/causation_id` preservation
- [ ] 4.2 RED `command/carrier_test.go` — optional-field absence/presence (causation, tenant, principal, deadline)
- [ ] 4.3 RED `command/carrier_test.go` — `Unmarshal` rejects reserved-key/invalid values; unknown keys ignored
- [ ] 4.4 GREEN `command/carrier.go` — `Carrier`, `Marshal/UnmarshalMetadata`, `ego.cmd.*` keys; tenant slot delegates to `tenancy.MarshalMetadata`/`UnmarshalMetadata` (read-only)

## Phase 5: Conformance

- [ ] 5.1 RED `command_architecture_test.go` (root, pkg `ego`) — `go list -deps ./command/...` fails on non-allowlisted import; mirrors `tenancy_architecture_test.go` (read-only)
- [ ] 5.2 GREEN confirm `command/` has zero disallowed imports; 5.1 passes unmodified

## Phase 6: Migration Documentation (W7/AC12)

- [x] 6.1 Confirmed by repo owner (2026-09-14): `design.md` (read-only) §Migration/Rollout (M-1..M-4) satisfies AC12 as the shipped migration/compatibility document — names `SendCommand`/`SagaActor`, classifies breaking status, no adapter/shim in this change's diff. No separate file needed.
- [x] 6.2 Follow-up GitHub issue filed: [getsyntegrity/ego#60](https://github.com/getsyntegrity/ego/issues/60), naming `SendCommand`, dispatch, `SagaActor`, and migration execution as one unit, referencing #59.

## Phase 7: Verification

- [ ] 7.1 Run `go mod tidy && go mod vendor` (only if new deps), then `go test -mod=vendor -p 1 -timeout 0 -race ./command/... .`; record any pre-existing flake per EGO-TENANT-001/006 evidence convention
- [ ] 7.2 Run `go vet ./command/... .`; confirm `engine.go`, `saga.go`, `saga_actor.go`, `behavior.go`, `option.go`, `protos/`, `egopb/`, `tenancy/`, `Makefile` (all read-only) byte-identical
