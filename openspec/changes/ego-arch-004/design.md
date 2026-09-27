# Design — Adapter SPI, capabilities and lifecycle (EGO-ARCH-004)

| Field | Value |
|---|---|
| Change | `ego-arch-004` |
| Date | 2026-09-27 |
| Phase | `sdd-design` |
| Tracker | [`#106`](https://github.com/getsyntegrity/ego/issues/106), parent [`#10`](https://github.com/getsyntegrity/ego/issues/10) |
| Inputs | [`exploration.md`](./exploration.md), [`proposal.md`](./proposal.md); ego-arch-001 [`design.md`](../ego-arch-001/design.md) §2, §3, §10; ego-arch-003 [`design.md`](../ego-arch-003/design.md) §D2, §D5–§D8, §9; ego-arch-006 [`design.md`](../ego-arch-006/design.md) §3 D1, D7, D8, §6 |
| Baseline | `main` at `f2b5130148086d95a4d6362971b85e800b0ce155` |

## 1. Summary and vocabulary

Ego already has contracts for every adapter family (stores, publishers, encryptor, tenant resolver) and, since #105, one composition root that wires them. What it lacks is a common layer on top of those contracts: a way for any adapter to say what it is and what optional things it can do, a written contract for how it starts and stops, a shared test suite that proves both, and a rule that keeps adapters from depending on the composition root. This design adds that layer without changing any existing interface, so it is additive in v4.

In one paragraph: a new contract package, `port/adapter`, defines a small `Descriptor` (which port an adapter serves, and a name for its implementation) plus a list of declared capabilities. A **capability** is an optional behavior with a method behind it; the method lives in an optional interface in the contract package that owns the port, exactly as `tenancy.FixedTenantResolver` does today. An adapter declares the capability in its descriptor *and* implements the interface; the composition root checks, before anything starts, that the two agree; the conformance suite checks the same thing in the adapter's own tests. Core code asks one accessor per capability instead of asserting interfaces where it happens to need them. Lifecycle follows the ownership rules ego-arch-003 §D5 already fixed; this design adds what each adapter must guarantee (`Start` cleans up after itself, `Close` is idempotent and bounded, `Ping` is a readiness probe) so that #105's rollback actually releases everything.

Terms used below:

- **SPI (service provider interface)** — the contracts a third party implements so Ego can use its code.
- **Port** — one contract an adapter implements, such as `persistence.EventsStore` or `publishing.EventPublisher`.
- **Slot** — one field of `compose.Spec` that holds an adapter, such as `Spec.EventsStore` or one element of `Spec.EventPublishers`.
- **Borrowed adapter** — an adapter the consumer keeps owning (stores, encryptor, tenant resolver, telemetry, logger, per ego-arch-003 §D5). The composition root may probe it, never open or close it.
- **Owned adapter** — an adapter whose ownership moves to the composition root when `New` succeeds (publishers, per §D5). The composition root starts it, if it can be started, and closes it on every terminal path.
- **Capability** — an optional behavior of one port, identified by a stable string and backed by an optional Go interface.
- **Conformance suite** — a reusable test package an adapter author runs from their own tests to prove the adapter honors a contract.

## 2. Scope boundary with #105, #11, #24 and #37

| Concern | Owner | This design |
|---|---|---|
| Slots, static validation V1–V7, `StartError`, start/stop order, cleanup context | #105 (`compose`, `compose/goakt`, `compose/internal/lifecycle`) | Uses them; adds one rule, V8 (§D6), and one change inside step 4 (§D4) |
| Which adapter owns what | #105 §D5 | Restates it per adapter; changes nothing |
| Runtime SPI, runtime capability negotiation (RUNTIME-006) | #11 | Offers `port/adapter` as the shared vocabulary; does not design the runtime port |
| Drain and flush during shutdown (LIFE-003, LIFE-004), timeouts (LIFE-007), restart (LIFE-008), transferring store ownership (LIFE-006) | #24 | Reserves nothing with semantics; records every interaction in §7 |
| Adapter/SPI versioning and compatibility ranges (COMPAT-005) | #37 | Keeps `Descriptor` extensible so #37 can add a field additively |
| Observability contract (telemetry is still `*ego.Telemetry`, `telemetry.go:32-37`) | #31 | Out of scope; telemetry stays a borrowed value, not a port |

## 3. Decisions

These are the decisions this design proposes. They become accepted when the pull request that carries them is approved; nothing here is recorded as a maintainer decision yet. Genuinely open choices are listed separately in §9.

### D1 — Where the SPI lives

A new contract package `port/adapter` (`github.com/pablogore/ego/v4/port/adapter` until the ego-arch-006 D1 path migration). It follows ego-arch-001 §2: a new top-level contract is born under `port/`. It imports only the standard library, so `contract-allowlist` applies to it unchanged, and it gets a `go list -deps` architecture test like `port/publishing/publishing_architecture_test.go`, with an empty allowlist.

It holds only what is common to every port. Capabilities specific to a port are declared in that port's own package (§D3), so `port/adapter` never imports another contract and never grows a list of every capability in the framework.

Sketch (names are settled in slice SPI-1; the semantics are not):

```go
package adapter

// Port names the contract an adapter implements, e.g. "persistence.EventsStore".
type Port string

// Capability names one optional behavior of a port, e.g. "tenancy.fixed-tenant".
type Capability string

// Descriptor is what an adapter says about itself. It is a value, so it can be
// logged, compared and extended with new fields without breaking anyone.
type Descriptor struct {
	Port         Port         // which contract this value implements
	Name         string       // implementation name, e.g. "kafka", "testkit-memory"
	Capabilities []Capability // optional behaviors it declares; order irrelevant
}

// Describer is implemented by adapters that declare a Descriptor. Optional.
type Describer interface{ Describe() Descriptor }

// Starter is implemented by an owned adapter that has work to do between
// construction and first use (dial a broker, open a producer). Optional.
type Starter interface{ Start(ctx context.Context) error }

// Pinger is the readiness probe. Every store contract already has it.
type Pinger interface{ Ping(ctx context.Context) error }

// Describe returns v's Descriptor and true, or the zero Descriptor and false
// when v does not implement Describer. It is the one type assertion on
// Describer in the code base.
func Describe(v any) (Descriptor, bool)

// Declares reports whether d lists c.
func (d Descriptor) Declares(c Capability) bool
```

`Starter` and `Pinger` are lifecycle capabilities. They get constants `adapter.CapStart` and `adapter.CapReady` so a descriptor can declare them like any other capability.

### D2 — Identity

An adapter's identity has two parts, kept separate on purpose because today they are conflated (exploration §2):

- **Kind of adapter:** `Descriptor.Port` plus `Descriptor.Name`, for example `{Port: "publishing.EventPublisher", Name: "kafka"}`. It is what error messages, logs and capability validation use. `Port` must match the slot the adapter is placed in (§D6).
- **Instance:** the existing `ID()` for publishers, unchanged. V6 keeps requiring it to be unique per kind. Stores, encryptors and resolvers have no instance identity and do not need one: each sits in a single-value slot, and the slot's field name already identifies it.

An adapter that does not implement `Describer` is **undeclared**. It keeps working exactly as today. Inspection reports it as `(Descriptor{}, false)`, and the composition root names it by its slot, as `compose/goakt` already does (`compose/goakt/app.go:246-257`).

The fact that every publisher's `ID()` is a type-wide constant (`"ego-kafka"`, `kafka.go:84-86`) is a real limitation, but fixing it changes publisher configuration, not the SPI; it is open decision O3.

### D3 — Capabilities: declared, implemented, inspected in one place

#106 asks to tell apart a mandatory, an optional and an unsupported capability:

| Class | Meaning | How it is expressed |
|---|---|---|
| Mandatory | Every implementation of the port has it | The port interface's own method set; the compiler enforces it. Example: `Ping` on every store (`persistence/events_store.go:112`) |
| Optional, supported | This implementation has it | Declared in `Descriptor.Capabilities` **and** implemented through the optional interface the owning contract package declares |
| Unsupported | This implementation does not have it | Not declared and not implemented |

**Where a port and a capability are defined.** In the contract package that owns the port. Each contract package declares its port names (for example `publishing.PortEventPublisher`, `persistence.PortEventsStore`), and each optional capability as a constant next to its optional interface, plus one accessor. These are **untyped string constants**, which convert to `adapter.Port` and `adapter.Capability` where they are used, so no existing contract package has to import `port/adapter`. That matters twice: contracts stay independent of each other, and moving `port/publishing` into the ego-arch-006 contracts module cannot create a module cycle through `port/adapter` (which stays in the root module unless O2 says otherwise). For the one adapter capability core uses today:

```go
package tenancy

const CapFixedTenant = "tenancy.fixed-tenant" // untyped; converts to adapter.Capability

// FixedTenantOf returns r's fixed tenant when r implements FixedTenantResolver
// and reports one. It is the only place that asserts FixedTenantResolver.
func FixedTenantOf(r TenantResolver) (TenantID, bool)
```

`engine.go:883` then calls `tenancy.FixedTenantOf(engine.tenantResolver)` instead of asserting the interface itself. The behavior does not change; the assertion moves into the contract that defines it, where every future caller will find it. That is the concrete meaning of "no scattered type assertions": at most one assertion per capability, in its owning contract package, plus `adapter.Describe`.

**Why both a declaration and a method.** The method is what the code calls; the declaration is what can be inspected and validated before anything runs. Either one alone fails a criterion: a method alone is invisible until the first call (today's state), and a declaration alone can claim something the adapter cannot do. Keeping both means they can disagree, so two checks keep them honest: V8 at composition time (§D6) and the conformance suite in the adapter's own tests (§D8).

**Truth for undeclared adapters.** For an adapter without a descriptor, the accessor still works from the method set, exactly as today, so existing resolvers keep their fixed-tenant behavior without a code change. Declaring is how an adapter becomes inspectable; it is never required to keep working in v4.

**Initial vocabulary.** Only capabilities with a caller on `main` are defined in this change: `adapter.CapStart`, `adapter.CapReady` and `tenancy.CapFixedTenant`. `logger.go:125` and `:334` assert `kitlog.CallerSkipper` and `kitlog.ManagedLogger`, but those are optional interfaces of the separate `kit-logger` module and the logger is not an Ego port; they stay as they are until #31 defines an observability contract. The `port/behavior` envelope assertions (`event_sourced_actor.go:773`, `durable_state_actor.go:502`) are behaviors written by the domain author, not adapters, and are out of scope. A capability that #24 or #11 needs later (for example a publisher that can flush) is added by that issue with its semantics; this design reserves no name without semantics.

### D4 — Lifecycle and ownership contract

The composition-level rules stay those of ego-arch-003 §D5–§D7. This decision states the adapter-level half. Each rule names the conformance check that proves it (§D8).

| Rule | Applies to | Contract | Why | Check |
|---|---|---|---|---|
| L1 | adapters implementing `Starter` | `Start` either succeeds or releases whatever it acquired before returning its error | `compose/internal/lifecycle` never undoes the step that failed (`lifecycle.go:60-63`); an adapter that leaks on a failed `Start` leaks for good | AT-2 |
| L2 | owned adapters (`Close`) | `Close` is idempotent, safe on a value that was never started, and safe after a failed `Start` | `compose/goakt` closes unattached publishers from `releasePublishers` on every failure path (`app.go:406-425`) and attached ones through `Engine.Stop` (`engine.go:464-501`); a second close must not fail or panic | AT-3 |
| L3 | owned and borrowed (`Close`, `Disconnect`) | Return by the caller's context deadline | Cleanup runs under one context bounded by `ShutdownTimeout` for all steps together (`lifecycle.go:79-83`, `:272-293`); one adapter that ignores it consumes everyone's budget. `kafka.go:76` is the counter-example today | AT-4 |
| L4 | adapters implementing `Pinger` | `Ping` answers "ready to serve now". It may establish a connection, as the store contracts document (`persistence/events_store.go:109-112`); a connection it opens on a borrowed adapter stays the consumer's to close | Keeps ego-arch-003 §D5 ("App only pings them") literally true while admitting what `testkit/eventstore.go:273-276` does | AT-5 |
| L5 | owned adapters | After `Close`, operations fail with the port's documented error (`publishing.ErrPublisherNotStarted` for publishers) and never block | Today's publisher contract (`publisher/kafka/publisher_contract_test.go:56-68`), generalized | PT-1 |
| L6 | new adapters | A constructor should do no I/O; I/O belongs in `Start` | Lets `New` stay I/O-free end to end (ego-arch-003 §D4a) and makes rollback of a failed dial the composition root's job, not the consumer's. Existing publishers dial in their constructor (`kafka.go:58-70`); changing that is open decision O5 | review |

**Where Start and Ready happen in the composition root.** Inside the existing step 4, "attach publishers" (`compose/goakt/app.go:174`, `:355-369`). For each publisher, in `Spec` order, step 4 calls `Start` when the publisher implements `Starter`, then `Ping` when it implements `Pinger`, then attaches the whole kind as it does today. Borrowed stores keep being pinged in step 1. No new step is added, so `StartError.Step` values and the D6 table keep their meaning; the step's error names the publisher by `ID()` and, when declared, by `Descriptor.Name`.

**Partial failure.** If publisher *k* fails `Start` or `Ping`, step 4 fails, the lifecycle undoes steps 3 down to 1, and `releasePublishers` closes every publisher that was not attached, started or not. L1 guarantees publisher *k* cleaned up after itself, and L2 makes closing the others safe whether or not they were started. The ownership rule for the consumer stays the one ego-arch-003 §D5 states: once `New` succeeded, call `Stop` and never close a publisher yourself.

**Stop.** Unchanged from ego-arch-003 §D7: projections, then `Engine.Stop` (closes publishers and the event stream), then the actor system. Borrowed adapters are never closed. The known gap (anything an actor emits after publishers close is not published) stays #24's (§7).

**Restart.** An adapter is not required to support `Start` after `Close`. The `App` is single-use (`lifecycle.go:124-129`); restart semantics belong to #24 (LIFE-008).

### D5 — `compose/internal/lifecycle` stays internal (recommended; open decision O4)

ego-arch-003 §9 left this to #106. The recommendation is to keep it internal. Adapters implement `Start`, `Close` and `Ping`; they never sequence other components, so they need the contract in §D4, not the sequencer. Making the sequencer public would create an API with one kind of caller (composition roots, all under `compose/`), and #24's LIFE-001 state machine may still reshape it.

### D6 — Validation at composition: rule V8

`compose.Spec.Validate` gains one rule, reported as a `*compose.ValidationError` with `Rule: "V8"` like the others (`compose/errors.go:30-50`):

- **V8a** — an adapter that declares a descriptor must declare the port of the slot it sits in. A value placed in `Spec.StateStore` whose descriptor says `persistence.EventsStore` is a wiring mistake.
- **V8b** — for every capability an adapter declares, it implements the matching optional interface, and for every lifecycle interface it implements (`Starter`, `Pinger`), it declares the matching capability. A mismatch is reported with the capability name.
- **V8c** — a slot's required capabilities are met. **In v4 no slot requires an optional capability**, so V8c starts empty. It exists so #11 (runtime negotiation) and #24 (for example, a drain policy that needs publishers able to flush) can add a requirement as a table entry with a test, instead of a type assertion at the point of use.

V8 runs only on adapters that declare a descriptor, so every `Spec` that validates today still validates. It is static: it inspects values the consumer already placed in named fields, with no I/O and no reflection beyond the existing typed-nil check (ego-arch-003 §D2 allows exactly that one use).

`compose` imports `port/adapter` for V8. That is a contract import, which `composition-no-runtime` allows (`internal/cmd/archcheck/rules/rules.go:202-214`).

### D7 — Adapters must not depend on the composition root

**Recommendation (open decision O1): no.** A nested adapter module must not import `compose` or anything under `compose/`, in production code or in tests.

Why:

1. **Direction.** The composition root depends on adapters, because the consumer's `main` hands them to it; an adapter depending on the composition root inverts that. ego-arch-001 §3 already says an external adapter "MAY import only contract packages and `egopb`". `compose` is not a contract. The rule only makes enforceable what the ADR already states.
2. **Measured cost.** Importing `compose/goakt` from `publisher/kafka` brings 45 GoAkt packages back into its production build, and importing the neutral `compose` pulls six root-level contracts that stay in the root module until ego-arch-006 F1 (exploration §6). After ego-arch-006 S3 either import forces the publisher to require the root module again.
3. **No legitimate need.** The obvious use, a helper such as `kafka.Register(spec *compose.Spec)`, is a form of self-registration that ego-arch-003 §D2 forbids; the consumer writes `spec.EventPublishers = append(spec.EventPublishers, p)` instead. End-to-end tests that need a running `App` belong in an unreleased integration module such as `test/compat` (ego-arch-006 D5).

**The rule: `external-adapter-no-composition`.** Added to `DefaultRules` in `internal/cmd/archcheck/rules/rules.go`:

| Field | Value |
|---|---|
| ID | `external-adapter-no-composition` |
| Description | nested adapter modules must not import the composition root (`compose` or anything under it) |
| Source | `ego-arch-004/design.md §D7` |
| Layer | `ExternalAdapterLayer` (`internal/cmd/archcheck/rules/layers.go:121-131`), the same layer `external-adapter-no-runtime` uses: every package of a nested module under `publisher/` |
| Semantics | denylist |
| Forbids | `isCompositionImport(rootModulePath, importPath)` (`layers.go:157-159`), the predicate `composition-leaf` already uses: `<root>/compose` or any path under it, matched by whole path segment |
| Reason | names the composition package imported and says adapters may import only contracts and `egopb` (ego-arch-001 §3) |

**Fail-closed behavior.**

- The layer is shared with `external-adapter-no-runtime`. If the publisher modules move or the root module path is misread, the layer matches zero packages and `Evaluate` fails the run (`internal/cmd/archcheck/rules/evaluate.go:211-217`) instead of passing vacuously.
- The composition prefix is built from the root module path read from `go.mod` (`DefaultRules(rootModulePath)`), so the rule follows the ego-arch-006 D1 path migration without an edit.
- The nested-module loader parses every non-test file and ignores build tags (`internal/cmd/archcheck/loader.go:240-277`), so it can over-report an import but never miss one in a production file.
- **No `main` or example exemption inside an adapter module**, unlike `composition-leaf`. The harm is to the adapter module's own `go.mod`, which a `main` package or an example inside that module damages just as much. A demo program belongs in its own unreleased module.
- **Tests.** archcheck does not read `_test.go` files. The test side is covered by each publisher's closure test (`publisher/kafka/closure_test.go:59-75`), which slice SPI-2 extends to also reject `<root>/compose` and anything under it. `compose/goakt` is already caught today through the GoAkt check.

**Interaction with the other rules.**

- `composition-leaf` covers root-module production packages (`layers.go:189-206`); this rule covers nested adapter modules. The layers do not overlap, and both use the same predicate, so together they say: only packages under `compose/`, root-module `main` packages, examples, tests, and unreleased consumer modules (`benchmark`, `example/cluster`, `test/compat`) may import the composition root.
- `no-cross-module-internal` (`modules.go:86-100`) already rejects an adapter importing `compose/internal/...`, and Go's own `internal` rule rejects it too. For that path, both archcheck rules report the same edge. That duplication is accepted: each report names a different broken constraint, and no baseline entry is needed because no such import exists.
- `external-adapter-no-runtime` is not widened. Its ID and description say "runtime", and a violation must name the rule it actually broke.
- **New adapter families.** `ExternalAdapterLayer` matches only `publisher/`. When the first adapter module outside it appears (a store or telemetry adapter), its directory root is added to that one layer, and both adapter rules follow. Where such modules live is open decision O6.

### D8 — Conformance suite

Two new packages, both standard-library-only so they pass `contract-allowlist` and the publishers' closure tests (exploration §5):

| Package | Tests | Used by |
|---|---|---|
| `port/adapter/adaptertest` | Lifecycle and descriptor rules any adapter must meet: **AT-1** a declared descriptor is stable across calls and its `Port` is the one the caller expects; declared capabilities and implemented interfaces agree (V8b, in the adapter's own tests). **AT-2** `Start` failure releases resources (L1), driven by an optional failure hook the caller supplies. **AT-3** `Close` twice, `Close` without `Start`, `Close` after a failed `Start` (L2). **AT-4** `Close`/`Disconnect` return within a short deadline (L3). **AT-5** `Ping` after `Start` succeeds for a reachable adapter (L4) | any adapter: publishers, stores, future adapters |
| `port/publishing/publishingtest` | Publisher-specific rules: **PT-1** `Publish` after `Close` returns `publishing.ErrPublisherNotStarted` (L5, generalizing today's four `publisher_contract_test.go` copies); **PT-2** `ID()` is non-empty and stable; **PT-3** a published event reaches the caller-supplied observer | the four publishers |

Stores keep `persistence/conformance` for their data semantics. It is canonical (EGO-TENANT-003), and #106 says not to redesign existing persistence contracts without evidence. They add `adaptertest` for lifecycle only.

**How the suites are called.** Same shape as `persistence/conformance`: the adapter's own test passes a factory that returns a fresh value per check. When the factory reports the backing service is unreachable, the check is **skipped** with a message, never passed (the rule of `persistence/conformance/check.go:79-81`).

```go
// in publisher/websocket's own tests
func TestConformance(t *testing.T) {
	srv := httptest.NewServer(echoHandler) // stdlib; allowed in an adapter module's tests
	defer srv.Close()
	adaptertest.Run(t, adaptertest.Target{
		Port: publishing.PortEventPublisher,
		New:  func(t *testing.T) (any, error) { return websocket.NewEventsPublisher(&websocket.Config{URL: wsURL(srv)}) },
	})
	publishingtest.RunEvents(t, /* same factory, plus an observer on srv */)
}
```

**The two adopters #106 requires** (slice SPI-4):

- **A publisher:** `publisher/websocket`. It is the only publisher whose tests can build a real instance in CI with the standard library alone (an `httptest` server), since the others need a broker. Kafka, NATS and Pulsar adopt in follow-up F-A and skip where no broker is reachable.
- **A store:** the `testkit` in-memory `EventStore`, `DurableStore` and `OffsetStore`, which already run `persistence/conformance` (`testkit/conformance_test.go:47-59`) and add `adaptertest`.

Neither needs a special case in core. The composition root probes stores through `adapter.Pinger` instead of its private `pinger` (`compose/goakt/app.go:241`), starts publishers through `adapter.Starter`, and validates both through V8.

**How nested modules run the suites without the root runtime.** `adaptertest` imports only the standard library and `port/adapter`; `publishingtest` imports only the standard library, `port/publishing` and `egopb`, and deliberately not `port/adapter`, so it can move into the ego-arch-006 contracts module with `port/publishing` without creating a module cycle. A publisher's test closure therefore gains no GoAkt package and not the root package `ego`, so `TestUnitTestClosureExcludesRuntimeAndRoot` stays green. Today the publishers resolve these packages through their existing requirement on the root module (`publisher/kafka/go.mod:7`, `:50`). After ego-arch-006 S3 they require only the contracts module; `port/publishing/publishingtest` moves with `port/publishing` automatically, but `port/adapter` and `port/adapter/adaptertest` are not in that module under the approved D7 (i). That is open decision O2.

A nested *store* module (none exists yet) that wants `persistence/conformance` must still require the root module, because `persistence` and `testkit` stay there until ego-arch-006 F1/F2. That is already recorded in ego-arch-006 §2.2 and is not changed here.

### D9 — Compatibility and apidiff

Everything is additive in v4, per ego-arch-001 §10.

| Package | Change | apidiff expectation |
|---|---|---|
| `port/adapter`, `port/adapter/adaptertest`, `port/publishing/publishingtest` | new | additions only |
| every existing contract interface (`port/publishing`, `persistence`, `offsetstore`, `encryption`, `tenancy`) | no method added to any interface | no incompatible change |
| `tenancy`, `persistence`, `offsetstore`, `port/publishing` (port-name constants only) | adds untyped constants; `tenancy` also adds `FixedTenantOf` | additions only; no new import |
| `compose` | adds rule V8 inside `Validate` | no exported change; behavior changes only for adapters that declare a descriptor, which none do before this change |
| `compose/goakt` | step 4 calls `Start`/`Ping` when implemented | no exported change |
| `ego` | `engine.go:883` calls `tenancy.FixedTenantOf` | no exported change |
| `testkit`, `publisher/websocket` | add `Describe` methods (and `Start` only if O5 chooses it) | additions only |

**No deprecation is needed in v4.** Nothing is replaced; the optional interfaces keep working for undeclared adapters. Whether the next major (#124) makes `Describe` a required method on each port is a question for #124 and #37, recorded in §9 and not decided here.

**Required check per slice:** `apidiff` (`golang.org/x/exp/cmd/apidiff`) between the baseline and the slice head for every package the slice touches, recorded in the pull request, as ego-arch-001 §5 does for S1.

## 4. Extension guide outline

Slice SPI-5 writes `docs/adapters.md` from this outline. Its promise: a new adapter is added without editing any file outside its own module, except one line in archcheck when it opens a new adapter family.

1. **Pick the port.** Find the contract package (`persistence`, `offsetstore`, `port/publishing`, `encryption`, `tenancy`). If none fits, the adapter needs a new contract under `port/`, which is an ADR change, not an adapter.
2. **Create the module.** Put it in its own directory with its own `go.mod`, under the family's adapter root (`publisher/` today; see O6). Import only contract packages and `egopb`; never `ego`, GoAkt or `compose/...` (archcheck rules `external-adapter-no-runtime`, `external-adapter-no-composition`). Copy `closure_test.go` from an existing publisher.
3. **Implement the port,** plus a `var _ port.Interface = (*T)(nil)` assertion.
4. **Declare the descriptor.** Implement `Describe()` with the port, a name and every optional capability you implement. Declare exactly what you implement: V8 and `adaptertest` both fail on a mismatch.
5. **Lifecycle.** Owned adapter: do I/O in `Start`, clean up after a failed `Start`, make `Close` idempotent and deadline-bound. Borrowed adapter: `Connect`/`Disconnect`/`Ping` as the port documents; the composition root never connects or closes it.
6. **Run the conformance suites** from your tests: `adaptertest` always, plus the port's own suite (`publishingtest`, `persistence/conformance`). Skip, never pass, when the backing service is unreachable.
7. **Wire it.** Show the consumer's `compose.Spec` field and the ownership rule (publishers: never close after `New`; stores: connect before `New`, disconnect after `Stop`).
8. **CI.** Nothing to register: `ciselect` discovers the module from its `go.mod`. List it in `docs/ci.md` if it is released.
9. **Checklist** for the pull request: archcheck green, closure test green, conformance green or skipped with a reason, apidiff additions only.

## 5. Slices

Each row is one pull request with at most five tasks. The authored-line counts are not estimated here; ~400 changed lines per slice is a planning heuristic, not a cap.

### SPI-1 — `port/adapter` contract package

- **Owns:** `port/adapter/**` (new); one new file per contract package holding its untyped port-name constants (`port/publishing/port.go`, `persistence/port.go`, `offsetstore/port.go`).
- **Tasks:** 1. `Port`, `Capability`, `Descriptor` (with `Declares`), `Describer`, `Starter`, `Pinger`, `CapStart`, `CapReady`, `Describe`. 2. Unit tests (RED first): `Describe` on declared and undeclared values; `Declares`; typed-nil input. 3. `port_adapter_architecture_test.go` with an empty non-stdlib allowlist, modeled on `port/publishing/publishing_architecture_test.go`. 4. Untyped port-name constants in the three contract packages, with a test that none of them imports `port/adapter`. 5. Package doc naming this ADR and the one-assertion rule.
- **Checks:** `go test ./port/adapter/...`; `go run ./internal/cmd/archcheck`; apidiff (additions only).
- **Depends on:** nothing.

### SPI-2 — archcheck: `external-adapter-no-composition`

- **Owns:** `internal/cmd/archcheck/rules/rules.go`, `internal/cmd/archcheck/rules/*_test.go`, `docs/ci.md` (rule table), `publisher/*/closure_test.go`.
- **Tasks:** 1. RED: a graph where a `publisher/` package imports `compose`, one importing `compose/goakt`, one importing `compose/internal/lifecycle` (expect this rule and `no-cross-module-internal`), and a root `main` package importing `compose` that stays allowed. 2. Add the rule (§D7). 3. Extend the four closure tests to reject `<root>/compose` and its subpackages. 4. `docs/ci.md` rule table and the "Adding a layer" note on adapter roots.
- **Checks:** `go test ./internal/cmd/archcheck/...`; archcheck on `main` stays at 0 violations with no new baseline entry; the exploration §6 spike reproduced as a failing graph test.
- **Depends on:** nothing. It can land first.

### SPI-3 — Conformance packages

- **Owns:** `port/adapter/adaptertest/**`, `port/publishing/publishingtest/**` (new).
- **Tasks:** 1. `adaptertest` AT-1…AT-5 with a `Target` factory and skip-on-unreachable. 2. `publishingtest` PT-1…PT-3. 3. Self-checks in the style of `persistence/conformance`'s capture mode: a deliberately broken fake (non-idempotent `Close`, a `Close` that ignores the deadline, a descriptor that lies) must make each check fail. 4. Architecture tests keeping both packages standard-library-only.
- **Checks:** `go test ./port/...`; archcheck; apidiff additions only.
- **Depends on:** SPI-1.

### SPI-4 — Two adopters (a publisher and a store)

- **Owns:** `publisher/websocket/**`, `testkit/eventstore.go`, `testkit/durablestore.go`, `testkit/offsetstore.go` and their tests.
- **Tasks:** 1. `websocket.EventsPublisher`/`DurableStatePublisher` implement `Describe`. 2. Their tests run `adaptertest` and `publishingtest` against an `httptest` server; the existing `publisher_contract_test.go` check folds into PT-1. 3. `testkit` stores implement `Describe` (declaring `CapReady`) and run `adaptertest` next to `persistence/conformance`. 4. Record per publisher that `go list -deps -test ./...` still has no GoAkt and no root package.
- **Checks:** `scripts/ci/verify-module.sh publisher/websocket`; root lane for `testkit`; apidiff.
- **Depends on:** SPI-3. **If ego-arch-006 S3 has landed first,** the websocket half is blocked on O2 (placing `port/adapter` in the contracts module); the `testkit` half is not.

### SPI-5 — Composition uses the SPI, and the guide

- **Owns:** `compose/spec.go`, `compose/spec_test.go`, `compose/goakt/app.go`, `compose/goakt/app_test.go`, `tenancy/resolver.go` (accessor), `engine.go` (the one line at `:883`), `docs/adapters.md` (new).
- **Tasks:** 1. V8a/V8b in `Spec.Validate`, with V8c as an empty requirement table (RED first). 2. `compose/goakt` step 4 starts and probes `Starter`/`Pinger` publishers; injected failure at publisher *k* leaves every publisher closed and names the publisher. 3. `probeStores` uses `adapter.Pinger`. 4. `tenancy.CapFixedTenant`, `tenancy.FixedTenantOf`; `engine.go:883` calls it. 5. `docs/adapters.md` from §4.
- **Checks:** `go test ./compose/... ./tenancy/...`; the root lane for the `engine.go` change (the full root package suite, which the selector runs for any root-package file); archcheck; apidiff.
- **Depends on:** SPI-1; SPI-4 for the adopter used in the guide's example.

### Named follow-ups (outside this change)

- **F-A** Kafka, NATS and Pulsar adopt `Describe` and the suites (skipped without a broker; Pulsar through its existing testcontainers setup). Fixes `kafka.go:76` under L3.
- **F-B** `port/adapter` and `adaptertest` in the ego-arch-006 contracts module, if O2 chooses (a).
- **F-C** Publisher instance IDs, if O3 chooses (a).
- **F-D** `external-adapter-contracts-only` allowlist, if O1 chooses (c) later.
- **F-E** Runtime capabilities for #11 RUNTIME-006, using the same `Descriptor`.
- **F-F** Observability port for telemetry and logging (#31), after which the `kit-logger` assertions can move behind it.

## 6. Dependencies and sequencing

- **Nothing blocks SPI-1, SPI-2 or SPI-3.** They touch only new packages, archcheck and tests.
- **ego-arch-006 S2/S3 (#102) and F4.** At the baseline, publishers still require the root module, so SPI-4 works as designed. If S3 lands first, the publisher half of SPI-4 waits for O2. This design does not decide S2's contents; D7 (i) was approved on 2026-09-27 (ego-arch-006 §3) and changing it is the maintainers' call.
- **ego-arch-006 D1 (module path migration).** Direction approved 2026-09-27, execution pending confirmation (ego-arch-006 §3). The new packages have no special path handling; the single D1 pull request renames them with everything else. Avoid running a slice and the D1 pull request in parallel on the same files.
- **#123 and #105 IMPL-5/IMPL-6.** No dependency. SPI-5 edits `compose/goakt/app.go` step 4 and `probeStores`, which IMPL-5 (example migration) does not touch.
- **#24.** See §7. No slice waits for #24.

## 7. Interactions with #24 (lifecycle epic)

| #24 item | Interaction | Stays with #24 |
|---|---|---|
| LIFE-001 state machine and ownership | §D4 states adapter-level guarantees under the ownership ego-arch-003 §D5 fixed | The framework-wide state machine; whether `lifecycle` is reshaped (O4) |
| LIFE-002 dependency startup ordering | Owned adapters start inside step 4, in `Spec` order | Any ordering beyond D6's five steps |
| LIFE-003 stop admitting commands | None; admission still closes at `Engine.Stop` (ego-arch-003 §D7) | The engine-level admission switch |
| LIFE-004 flush pending work | None. A future "flush" capability is added by #24 with its semantics, through V8c | The drain policy and the dropped-emissions question of ego-arch-003 §D7 |
| LIFE-006 runtime/store ownership transfer | Borrowed stays borrowed | Whether stores can be handed over |
| LIFE-007 timeouts | L3 makes every adapter honor the shared cleanup deadline | The default `ShutdownTimeout` (ego-arch-003 §9) |
| LIFE-008 restart | Adapters need not restart after `Close` | Restart semantics |
| "Startup failure identifies the responsible component" | Step 4 errors name the publisher by `ID()` and descriptor name | — |

## 8. Acceptance mapping for #106

| #106 criterion | Where | Proof |
|---|---|---|
| Public SPI does not depend on concrete implementations | SPI-1 | `port/adapter` architecture test (stdlib only) and `contract-allowlist` |
| Capabilities are explicit and inspectable without scattered type assertions | SPI-1, SPI-5 | `adapter.Describe` and one accessor per capability in its owning contract; `engine.go:883` moved behind `tenancy.FixedTenantOf`; V8 |
| Lifecycle and ownership are documented | §D4, SPI-5 guide | L1–L6 and the ego-arch-003 §D5 table, linked from `docs/adapters.md` |
| Partial failures clean up started resources | SPI-3, SPI-5 | AT-2/AT-3; step-4 injected-failure test in `compose/goakt` |
| A minimal reusable conformance suite exists | SPI-3 | `adaptertest`, `publishingtest`, with self-checks |
| At least two adapter types use the model without special conditions in core | SPI-4, SPI-5 | `publisher/websocket` and `testkit` stores; `compose/goakt` uses only `port/adapter` interfaces |
| The guide explains how to add an adapter without modifying core | SPI-5 | `docs/adapters.md` |
| (comment) Adapters and the composition root | SPI-2 | `external-adapter-no-composition` plus the closure-test extension |

## 9. Open decisions for the maintainers

None of these is decided by this design. Each has a recommendation.

| # | Decision | Options | Recommendation |
|---|---|---|---|
| O1 | May nested adapter modules import `compose` or `compose/...`? | (a) No, enforced by the dedicated denylist rule `external-adapter-no-composition` (§D7). (b) Allow neutral `compose`, forbid `compose/goakt` and `compose/internal/...`. (c) No, enforced by widening the adapter rule into an allowlist, `external-adapter-contracts-only`, which accepts only contract packages, `egopb` and non-GoAkt third-party imports. That makes all of ego-arch-001 §3 enforceable (it also catches `testkit`, `migration`, `internal/...`) but amends that section's "enforced in review" note. (d) Allow and document | **(a)** now; (c) as follow-up F-D if the maintainers want the whole §3 statement enforced. (b) still breaks the publishers after ego-arch-006 S3 (exploration §6) |
| O2 | Where `port/adapter` and `adaptertest` live once publishers require only the contracts module (ego-arch-006 S3) | (a) Add them to the contracts module in S2. Both are stdlib-only, so no cycle is possible; this amends the approved D7 (i). (b) A separate SPI module; fails ego-arch-006 §6(2) on its own. (c) Publishers adopt the SPI only after F1 | **(a)** |
| O3 | Publisher instance identity: every `ID()` is a type-wide constant (`kafka.go:84-86`), so two publishers of one type collide under V6 | (a) Add an optional `ID` to each publisher `Config`, defaulting to today's constant (additive). (b) Leave it and document one publisher per type. (c) Derive the ID from the topic or URL | **(a)**, as follow-up F-C |
| O4 | Promote `compose/internal/lifecycle` to a public package (left to #106 by ego-arch-003 §9) | (a) Keep internal. (b) Promote now | **(a)** (§D5) |
| O5 | Constructors that do I/O (all four publishers dial in `New*`) | (a) Keep them; new adapters should defer I/O to `Start` (L6). (b) Add lazy constructors plus `Start` to the existing publishers, and deprecate the dialing ones until #124 | **(a)**; revisit with F-A |
| O6 | Directory convention for future adapter modules outside `publisher/` | (a) One root per family (`publisher/`, `store/`, …), each added to `ExternalAdapterLayer` when its first module appears. (b) A single `adapter/<family>/<name>` root. (c) Decide when the first non-publisher adapter arrives | **(c)**, with (a) as the default; any choice is a one-line layer change |
| O7 | Whether the next major (#124) makes `Describe` a required method of each port | (a) Yes, in the #124 major. (b) Keep it optional | Defer to #124 and #37 (COMPAT-005); no recommendation until adoption is measured |

## 10. Alternatives rejected

- **Add `Describe`, `Start` or `Ping` to the existing port interfaces.** It is the most direct, but it breaks every implementation outside the repository, and ego-arch-001 §10 decided "no break inside v4".
- **Capabilities only as optional interfaces, asserted where used** (today). Rejected: nothing can be inspected or validated before the first call, and the assertion sites multiply (exploration §3.1).
- **Capabilities only as a declared list, with no interface behind them.** Rejected: a flag can claim a behavior the adapter lacks, and the caller still needs a method to call, which means an assertion anyway.
- **One central capability registry in `port/adapter`** listing every capability of every port. Rejected: `port/adapter` would import or mirror every contract and change whenever any port does; the owning package already knows its capabilities.
- **Discovery by reflection** (scan an adapter's methods to build its descriptor). Rejected by ego-arch-003 §D2, which forbids reflection-based wiring, and it would make a descriptor impossible to review.
- **A plugin loader or a registry adapters add themselves to.** Out of scope for #106 ("dynamic loading", "remote registry"), and a registry is a service locator (ego-arch-003 §D2).
- **Widening `external-adapter-no-runtime` to also deny `compose`.** Rejected: its ID and description say "runtime", so a report would name the wrong constraint (§D7).
- **Widening `composition-leaf` to nested modules.** Rejected: `composition-leaf` exempts `main` packages and examples, which must not be exempt inside an adapter module, and it was scoped to the root module on purpose (ego-arch-003 §D8).
- **Putting the conformance suites in `testkit`.** Rejected: `testkit` is in the root module and imports persistence helpers; publishers would need the root module in their test builds, and after ego-arch-006 S3 they cannot have it.
- **Using `testify` in the new suites,** as `persistence/conformance` does. Rejected: everything under `port/` is a contract for `contract-allowlist`, and a carve-out would add a third-party requirement to the future contracts module for a convenience.

## 11. Evidence and reproduction

| Claim | How it was obtained |
|---|---|
| Every `file:line` in this document and in `exploration.md` | Read on `f2b5130` in a clean worktree |
| An adapter importing `compose`/`compose/goakt` passes archcheck, and the closure numbers in exploration §6 | `git archive f2b5130` into a scratch directory; add one file to `publisher/kafka` importing `compose` (and `compose/goakt`); `GOWORK=off go mod tidy`; `go run ./internal/cmd/archcheck`; `GOWORK=off go list -deps ./... \| wc -l` and `\| rg -c tochemey/goakt/v4`. Go 1.27.1 linux/amd64. Nothing from the spike is committed |
| Type-assertion inventory (exploration §3.1) | `rg -n '\.\((interface\|[a-z]+\.[A-Z]\w+\|\*?[A-Z]\w+)\)'` over production `.go` files, excluding tests, `mocks/`, generated code, examples and `benchmark`; then reading each hit |
