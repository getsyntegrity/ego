# Proposal — Canonical `TenantContext` and `TenantResolver` SPI (EGO-TENANT-001)

| Field | Value |
|---|---|
| Change | `ego-tenant-context` |
| Date | 2026-09-13 |
| Phase | `sdd-propose` |
| Status | proposed |
| Tracker | [`getsyntegrity/ego#45`](https://github.com/getsyntegrity/ego/issues/45) (EGO-TENANT-001) |
| Epic | [`getsyntegrity/ego#23`](https://github.com/getsyntegrity/ego/issues/23) (native multi-tenancy) |
| Dependency | [`getsyntegrity/ego#10`](https://github.com/getsyntegrity/ego/issues/10) (hexagonalization) |
| Evidence base | `openspec/changes/2026-09-13-ego-tenant-context/exploration.md` |
| Prior decisions | `state.yaml` → `phases.decision` (closed by user, 2026-09-13) |

Documentation only. This proposal defines a contract; it writes no Go code.

---

## 1. Intent

Ego has **no tenancy concept whatsoever**. Exploration §3 confirms a repo-wide scan for `tenant` returns zero matches, and every adjacent candidate (`org`, `account`, `workspace`, `namespace`, `realm`) is a false positive — `testpb.Account` is a bank-account test fixture, not a tenancy primitive. Nothing partially occupies this space.

That greenfield status is the reason to act now, and it cuts both ways:

- **No convention to reconcile.** Epic #23 (TENANT-002…008: envelopes, store isolation, projections, topic isolation, single-tenant mode, conformance tests, administrative semantics) has no foundation to build on. Every one of those stories needs a shared vocabulary for "which tenant is this execution acting for," and each would otherwise invent its own.
- **No accidental scoping to lean on.** Exploration §8 and §10 show `Engine.EraseEntity` (`engine.go:945-974`) and `migration.Migrator.Run` already iterate persistence unconditionally, cross-boundary, with zero awareness that a boundary could exist. Today that is harmless. The moment tenant data lands in shared stores it is a silent data-leak path.

**Success looks like**: one canonical, transport-neutral, GoAkt-independent contract — `TenantID`, `TenantContext`, `TenantResolver` — that TENANT-002…008 consume rather than re-derive, plus an enforceable architecture guarantee that the contract cannot acquire transport, auth-provider, or actor-runtime dependencies.

---

## 2. Settled decisions (closed — not reopened here)

These were closed by the user against exploration §13(b). They are inputs to this proposal, not choices under evaluation.

**S1 — `TenantID` is `type TenantID string`, a defined type (not an alias).**
Exploration §4.1 establishes that *every* identifier in this codebase (`persistenceID`, `entityID`, `sagaID`, `keyID`, `projection_name`) is a bare `string`, with no typed-wrapper precedent anywhere. A defined string type deviates from that idiom by exactly one degree while buying the thing the idiom cannot: the compiler refuses to let a `TenantID` be silently interchanged with an `entityID`. `= string` was rejected because it provides no isolation at all. A struct was rejected as unjustified friction against §4.2's serialization and map-key ergonomics. The ID is **opaque** — the framework mandates no UUID format (§4.2: no existing ID in this repo mandates one either), so a constructor/validator carries the invariants the type cannot.

**S2 — the `TenantResolver` core contract MUST NOT depend on GoAkt, in any form.**
Exploration §6.1 shows the framework's existing SPI idiom (`encryption.Encryptor`, `encryptor.go:29-36`) takes `context.Context` plus framework-native types only, and §10 verifies `behavior.go` / `saga.go` are already clean of transport and infrastructure. The tenancy contract joins that clean set. Separately, §0.4/§10 show `internal/extensions` + `ctx.Extension(ID)` is a proven, four-times-over plug-in mechanism the GoAkt runtime **may** use to transport a resolver instance to actors. That is a runtime-adapter wiring detail, deferred to TENANT-006 — it is explicitly *not* the #001 decision, and this proposal does not present it as one.

**S3 — resolution and propagation are distinct concepts. Rule: resolve once at the trust boundary; propagate thereafter.**
`TenantResolver.Resolve(ctx) (TenantContext, error)` runs **once**, at the trust boundary (inbound HTTP handler, gRPC entrypoint, message consumer), producing a `TenantContext`. Propagation is a separate mechanism, and exploration §5 splits it cleanly: *locally*, `context.Context` already survives the entire command path today — `Engine.SendCommand` → `SendSync` (`engine.go:757`) → `goCtx := ctx.Context()` (`event_sourced_actor.go:506`) → `behavior.HandleCommand` (`event_sourced_actor.go:527`) recovers the same context value. *Remotely*, §5.2 is emphatic that this is a dead end: `SpawnOn` may place an actor on another node, behaviors travel as serialized dependencies, and `context.Context` does not cross a wire. Cross-boundary propagation therefore requires a tenant-aware envelope, which is **out of scope here and owned by TENANT-002**.

**S4 — tenant and aggregate identity together form the aggregate's effective identity.**
This replaces exploration §12/§13(b)'s "per-entity vs per-command" fork; it is no longer a live question. "Tenant A / Order 123" and "Tenant B / Order 123" are two different aggregates and **must never share a persistence stream**. The command execution context carries the tenant; the aggregate identity and its persistence namespace are tenant-scoped. The exact physical key/namespace format is deferred to TENANT-002/003. **Explicitly rejected**: any design in which a single global actor's tenant may vary arbitrarily from command to command. Exploration §12 names the hazard — a buggy or hostile caller sending a tenant-A command against a tenant-B entity — and this is a confused-deputy vulnerability, not an open architectural possibility to leave for later.

**S5 — single-tenant and multi-tenant share one machinery.**
Single-tenant mode is not a second execution model and not a bypass. `WithSingleTenant(TenantID)` installs a built-in resolver/policy that produces an ordinary `TenantContext` flowing through the exact same path multi-tenant mode uses. Exploration §7 supports this structurally: `ResolveLogger`'s post-loop nil/typed-nil fallback (`logger.go:80-89`, applied at `option.go:85-88`) is the repo's own precedent for "a documented built-in default installed through the same seam as a user-supplied one." The payoff is that there is exactly one code path to audit, one path to test, and no "single-tenant deployments skip the checks" branch that can be reached accidentally in a multi-tenant deployment.

---

## 3. Proposed core contract

A new GoAkt-free package (recommended import path `github.com/pablogore/ego/v4/tenancy`; the name is a recommendation, the *boundary* is not). Signatures below are **sketches** to fix shape and vocabulary — exact names, doc comments, and implementations belong to `sdd-design`.

```go
// Identity
type TenantID string

func NewTenantID(s string) (TenantID, error) // validating constructor — the only blessed way in
func (t TenantID) Validate() error           // invariants, re-checkable at boundaries
func (t TenantID) String() string

// Execution scope — a closed discriminator, never a magic string
type Scope int
const (
    ScopeTenant Scope = iota + 1 // acting for exactly one tenant
    ScopeAdministrative          // deliberate cross-tenant operation
)

// Execution context
type TenantContext struct { /* unexported: scope + tenant + admin attribution */ }

func (c TenantContext) Scope() Scope
func (c TenantContext) TenantID() (TenantID, bool) // false when administrative
func (c TenantContext) IsAdministrative() bool

// SPI — no GoAkt, no net/http, no auth SDK
type TenantResolver interface {
    Resolve(ctx context.Context) (TenantContext, error)
}

// Built-in policy, same machinery (S5)
func WithSingleTenant(id TenantID) TenantResolver
```

Two properties are load-bearing:

1. **Administrative scope is a first-class, explicit concept.** Exploration §8 rules out both alternatives on the epic's own stated principles, not merely on taste: a sentinel `TenantID` (`"system"`, `"__system__"`) is indistinguishable from a real tenant unless every consumer special-cases it, and nothing today would stop a customer from being named `system`; nil/empty-means-admin is directly forbidden — empty must mean *missing, reject*. So administrative context is a distinct `Scope` value with no `TenantID` at all, never a nil, an empty string, or a magic tenant name.
2. **The zero value is never valid.** A zero `TenantContext` has no scope and MUST be rejected. Per §4.2, Go cannot make a comparable primitive un-zero-constructible, so this is enforced by constructor plus an explicit require-helper at boundaries, not by the type system alone.

### Normative invariant (carried forward to `sdd-design`)

> **Once an execution has a tenant identity, Ego MUST NOT permit that identity to change implicitly while crossing command, saga, event, persistence or projection boundaries.**

"Implicitly" is the operative word: a deliberate, explicit, auditable transition to administrative scope is permitted; silent drift, re-resolution, or defaulting at an interior boundary is not. Exploration §5.1 identifies the boundaries this invariant must survive, including the saga `SendSync` seam (`saga_actor.go:337,391`), which shares the same mechanism as direct engine calls.

---

## 4. Architecture conformance

Exploration §2 reads the repo's only conformance test, `logger_architecture_test.go:104-160`: a `filepath.WalkDir` + `strings.Contains` **substring scan** over first-party non-test, non-generated `.go` files, banning literal import strings and API calls outside one exempted seam file (`logger.go`). It works, but §2.1/§11 name its blind spot precisely: it cannot express "no package in this subtree may *transitively* import X," and a leak arriving through an intermediate or third-party package is invisible to it.

**Proposed**: upgrade the technique for this boundary to an **import-graph check** built on `go/packages` (or `go list -deps` output) rather than byte matching, asserting that the `tenancy` core package and its transitive first-party closure import **none** of:

`net/http` · JWT libraries (`github.com/golang-jwt/...`) · the Ory SDK (`github.com/ory/...`) · GoAkt (`github.com/tochemey/goakt/...`) · gRPC transport types (`google.golang.org/grpc`) · broker clients (`IBM/sarama`, NATS, Pulsar)

This satisfies #45's acceptance criterion ("architecture tests prevent the core depending on concrete resolvers") with a guarantee the substring scan structurally cannot give. It is also a **candidate foundational deliverable for epic #10** — the same harness generalizes to any hexagonal boundary the framework later declares, which is exactly what §2.2 shows #10 does not have today. The existing logging test stays as-is; this is an addition, not a replacement.

---

## 5. Scope

### In scope
- The `TenantID` defined type, its validating constructor, and its helpers.
- `TenantContext` = tenant identity **+** execution-scope discriminator (`Tenant` | `Administrative`), with administrative attribution carried explicitly.
- The `TenantResolver` SPI: `Resolve(ctx context.Context) (TenantContext, error)`, in a GoAkt-independent package.
- `WithSingleTenant(TenantID)` as the built-in resolver/policy constructor (S5).
- Local propagation helpers: attach/read/require a `TenantContext` on a `context.Context`.
- The tenant error model (sentinels + typed wrapper), per §6.3/§9's `projectionRunnerError` precedent.
- The import-graph architecture-conformance test for the tenancy core boundary (§4).
- The normative invariant, stated as a contract for `sdd-design` to specify against.

### Out of scope
| Deferred item | Owner | Basis |
|---|---|---|
| Remote / cross-node envelope propagation | TENANT-002 | exploration §5.2, §12 — `context.Context` cannot cross a wire; envelopes are load-bearing, not optional |
| Physical persistence namespace / key format | TENANT-002/003 | S4 defers the format; only the *effective identity* rule is fixed here |
| Any tenant parameter added to `Behavior` / `Saga` / other public method signatures | never | §10, §13(c).1 — `HandleCommand`/`HandleEvent`/`Compensate` are the most-implemented public contract; a positional tenant parameter breaks every implementer |
| Engine-side `Option` wiring (`ego.WithTenantResolver`, `Config.tenantResolver`, `GoaktOptions()` extension registration) | TENANT-006 | S2 — runtime-adapter concern, deliberately not a core-contract decision |
| `testkit/scenario.go` context-injection seam (`When()` hardcodes `context.Background()`, `scenario.go:112`) | TENANT-007 | §6.5, §13(c).2 — a real missing public seam, but only needed once tenant-aware behavior is testable |
| Tenant scoping of `Engine.EraseEntity` / `migration.Migrator.Run` | TENANT-008 | §8, §13(c).3 — administrative semantics, dependent on this contract but not part of it |
| Per-tenant broker topic strategy | TENANT-005 | §10, §13(c).4 — publisher topic fields are flat strings today |

---

## 6. Recommendations on still-open items

These are **not** settled. Each is a defensible default proposed to keep `sdd-design` unblocked, and each is explicitly open to human override. They are listed separately from §2 on purpose.

| # | Open item (`state.yaml → open_decisions_for_propose`) | Recommendation | Rationale |
|---|---|---|---|
| R1 | Normalization policy | **Validate, do not normalize.** `NewTenantID` rejects empty, leading/trailing whitespace, control characters, and non-UTF-8; applies a length cap; does **not** case-fold. Format policies (UUID-only, slug-only) are application-level, layered above the constructor. | §4.2: no identifier in this repo is normalized today, and `persistenceID`/`entityID`/`keyID` are all used verbatim and case-sensitively. Silent normalization means the ID the caller supplied is not the ID persisted — an unacceptable footgun for a value that will become part of a persistence namespace (S4). Rejecting is loud; normalizing is lossy. |
| R2 | Does the engine invoke `TenantResolver`, or is it caller-side only? | **Split the roles.** Resolution happens at most once, at the trust boundary (S3). Ego's entrypoints are the **enforcement** point — they require an already-present `TenantContext` and reject when absent. `WithSingleTenant` is what makes that zero-plumbing for single-tenant deployments, since the built-in policy supplies the context. Engine-invoked resolution stays a TENANT-006 wiring question. | Keeps #001 purely a contract (S2) while still delivering §7's "no additional plumbing" requirement. Enforcement-not-resolution at interior boundaries is also what makes the §3 invariant checkable rather than aspirational. |
| R3 | Administrative-context payload | **Minimum viable auditability**: a required subject/actor identity and a required reason, optional correlation ID. No `TenantID` field. Constructed only through an explicit constructor. | §8 requires administrative context be auditable and structurally distinct. Requiring *both* who and why makes an unattributed cross-tenant operation impossible to construct by accident. TENANT-008 owns the full semantics and may extend the payload. |
| R4 | Error-reason taxonomy | **Three core reasons**: `Missing`, `Invalid`, `Denied` — `Denied` subsumes unknown/forbidden, with the resolver's own error preserved via `Unwrap()`. `Ambiguous` is not modeled (see R5). Sentinels for `errors.Is` plus a typed wrapper carrying the offending `TenantID`. | §6.3/§9: the repo already combines sentinel and typed-wrapper idioms (`engine.go:57-80`; `projectionRunnerError`, `projection_runner.go:827-839`). Core cannot honestly distinguish "unknown" from "forbidden" without re-deriving resolver-internal state, so it should not pretend to. Adapters still get everything they need to map a status code without string matching. |
| R5 | Resolver composition (chain/priority) | **Exactly one configured resolver.** No chaining, no priority, no fallback ordering in #001. | §6.4: nothing in this codebase composes SPI implementations, so this would be a brand-new compositional pattern introduced speculatively. `TenantResolver` is a plain interface — an application that genuinely needs chaining can implement a composing resolver itself with zero framework support, which is the cheapest possible way to keep the option open. |

---

## 7. Capabilities

> Contract with `sdd-spec`. `openspec/specs/` is currently empty — this repo has no main specs yet.

### New capabilities
- `tenancy-core`: canonical tenant identity (`TenantID`), execution context (`TenantContext` with tenant/administrative scope), the `TenantResolver` SPI, the built-in single-tenant policy, local context propagation helpers, the tenant error model, and the core-boundary import-graph conformance guarantee.

### Modified capabilities
- None. No existing spec exists, and no existing public behavior changes.

---

## 8. Affected areas

| Area | Impact | Description |
|---|---|---|
| `tenancy/` (new package) | New | Entire deliverable: `TenantID`, `TenantContext`, `Scope`, `TenantResolver`, `WithSingleTenant`, propagation helpers, error model |
| `tenancy_architecture_test.go` (or `internal/archtest/`) | New | `go/packages`-based import-graph conformance check (§4) |
| `mocks/tenancy/` | New | Mockery-generated `TenantResolver` fake, matching `mocks/encryption/encryptor.go` convention (§6.5) |
| `logger_architecture_test.go` | Unchanged | Existing substring test remains; the new check is additive, not a rewrite |
| `option.go`, `engine.go` | Unchanged | Wiring deferred to TENANT-006 (S2) |
| `behavior.go`, `saga.go` | Unchanged — protected | Signatures MUST NOT gain a tenant parameter (§10, §13(c).1) |
| `testkit/scenario.go` | Unchanged — flagged | `When()` hardcodes `context.Background()`; seam needed by TENANT-007 (§6.5) |

**Public consumer surfaces introduced** (per `openspec/config.yaml` → `rules.proposal`): the exported API of the new `tenancy` package only. No existing exported symbol is added to, removed, renamed, or re-typed by this change.

---

## 9. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| The contract is designed against in-process `context.Context` semantics and silently implies cluster-wide propagation works | Medium | §5.2 is the exploration's most consequential finding. The proposal states the same-node limitation explicitly (S3), and `sdd-design` MUST document it on the propagation helpers themselves, not only in prose |
| S4's effective-identity rule is stated but not yet enforceable, since persistence namespacing lands in TENANT-002/003 | High | Accepted and deliberate. The invariant is normative *now* so TENANT-002/003 inherit a constraint rather than negotiate one. The confused-deputy design is rejected up front, not deferred |
| Import-graph conformance test is a new, previously-unused technique in this repo (`go/packages` vs. substring scan) | Medium | Additive; the existing logging test is untouched. If the harness proves costly, the §11 substring fallback still satisfies #45's criterion with documented weaker guarantees |
| A defined `TenantID` type adds conversion friction at proto/JSON boundaries | Low | §4.2: a string-kinded type marshals as a JSON string and maps to a proto `string` field; conversion cost at the proto boundary is identical across all three candidate shapes |
| Recommendations R1–R5 are adopted downstream as if settled | Medium | §6 is structurally separated from §2 and every row is labelled a recommendation. `sdd-design` MUST re-surface any R-item it depends on |
| Contract ships with zero consumers and drifts before TENANT-002 lands | Medium | Conformance test plus mocks keep it compiling and honest; #23's sequencing should schedule TENANT-002 next |

---

## 10. Rollback plan

Rollback is unusually clean because this change is **purely additive with zero consumers**:

1. No existing exported signature changes → nothing to un-break for downstream users.
2. No persistence schema, proto field, or wire-format change → no persisted data to migrate or reinterpret.
3. No behavioral change to `NewConfig`, `Engine`, actors, projections, or publishers → no runtime regression surface.

To revert: delete the `tenancy` package, its conformance test, and its generated mocks; run `go mod tidy` and `go mod vendor` together (per `rules.apply`). If `go/packages` was added as a dependency, its removal is part of the same tidy. The framework returns byte-for-byte to its pre-change behavior. If only the conformance harness proves problematic, it can be reverted independently of the contract.

---

## 11. How this sets up TENANT-002 … 008

Derived from exploration §12.

| Downstream | What #001 hands it |
|---|---|
| TENANT-002 (tenant-aware envelopes) | A string-kinded `TenantID` that maps to a proto `string` field with no glue; the explicit statement that `context.Context` is same-node-only, so envelopes are known load-bearing rather than discovered so; the exact stamping point already located (`buildEnvelopes`/`marshalEvent`, `event_sourced_actor.go:560,593`) |
| TENANT-003 (store isolation) | S4's effective-identity rule as a hard constraint: tenant + aggregate identity, never a shared stream. Comparable/hashable `TenantID` keeps tenant-scoped indexing simple |
| TENANT-004 (projections) | The same `TenantContext` vocabulary at `StartProjection`/`RebuildProjection` (`engine.go:376,496`) |
| TENANT-005 (topic isolation) | A `TenantID` that interpolates directly into topic templates, minimising how invasive the eventual `publisher/*.Config` change must be |
| TENANT-006 (single-tenant mode) | S5's unified machinery plus `WithSingleTenant`; only `Option` wiring remains, following the `WithLogger`/`WithEncryptor` pattern verbatim (§7) |
| TENANT-007 (conformance tests) | R4's error taxonomy, so cross-tenant tests can assert *which* failure occurred; plus the flagged `testkit` `WithContext` prerequisite |
| TENANT-008 (administrative semantics) | A dedicated administrative `Scope` — the sentinel/nil ambiguity the epic forbids is designed out from day one, so TENANT-008 never has to introduce it as a breaking follow-up |
| Epic #10 (hexagonalization) | A reusable import-graph conformance harness, generalisable to any declared architectural boundary (§4) |

---

## 12. Success criteria

- [ ] `TenantID`, `TenantContext` (with tenant/administrative scope), and `TenantResolver` exist in a single package, and that package's transitive first-party import closure contains no HTTP, JWT, Ory, GoAkt, gRPC, or broker dependency — proven by an automated import-graph test, not by inspection.
- [ ] A `TenantContext` cannot be constructed in an invalid state: no empty `TenantID`, no zero-value context accepted at a boundary, no administrative context without attribution.
- [ ] Administrative scope is distinguishable from tenant scope by type, never by a sentinel string, an empty value, or a nil.
- [ ] `WithSingleTenant(id)` yields a `TenantContext` indistinguishable in kind from a multi-tenant one — no second execution path exists.
- [ ] The normative invariant from §3 is carried into `design.md` verbatim and specified against.
- [ ] `Behavior`, `Saga`, `Engine`, and `Config` public signatures are byte-identical before and after.
- [ ] Every R1–R5 recommendation is either explicitly ratified or explicitly overridden before `sdd-design` completes.
