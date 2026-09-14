# Design: Runtime tenant wiring and zero-plumbing single-tenant mode (EGO-TENANT-006)

Tracker `getsyntegrity/ego#55` · spec `specs/tenancy-runtime/spec.md` (DP1/DP2 ratified 2026-09-14).

## Technical Approach

One new `Option` feeds one `Config` field. That field's non-nil-ness *is* tenant-aware mode. `Engine.SendCommand` resolves and attaches once; the two actors enforce twice from context only. No actor ever holds a `TenantResolver`. `tenancy/` is consumed unchanged.

## Architecture Decisions

### D1 — "tenant-aware enabled" is the resolver's presence, not a flag

| Option | Tradeoff | Decision |
|---|---|---|
| `Config.tenantResolver != nil` is the signal | Matches all 8 existing conditional extensions; the forbidden state "tenant-aware with zero resolvers" is unrepresentable | **Chosen** |
| Separate `tenantAware bool` | Can desync from the resolver, materializing exactly the state the spec says must fail closed | Rejected |

`tenantResolverCount > 0 && tenantResolver == nil` is still checked in `NewEngine` as a defensive guard (unreachable today; protects future refactors).

### D2 — Duplicate detection via a registration counter, not last-call-wins

`Option.Apply(c *Config)` returns nothing and `NewConfig` has no error return, so an `Option` cannot reject anything itself. The config therefore records what it saw and `NewEngine` judges it:

```go
tenantResolver      tenancy.TenantResolver // effective; non-nil == tenant-aware mode
tenantResolverCount int                    // non-nil registrations; >1 == ambiguous

func WithTenantResolver(r tenancy.TenantResolver) Option {
    return OptionFunc(func(c *Config) {
        if isNilResolver(r) { return }              // inert: no register, no reset
        c.tenantResolverCount++
        if c.tenantResolver == nil { c.tenantResolver = r }  // first-wins, deterministic
    })
}
```

`isNilResolver` mirrors `isNilLogger` (logger.go:72) so a typed-nil never activates tenancy. A counter beats a temporary slice: the hot read stays a single pointer compare, and no element has to be "picked" — `count > 1` is rejected outright with `ErrAmbiguousTenantResolver` (new var beside `ErrActorSystemRequired`, engine.go:70). Last-call-wins (`WithLogger`/`WithTelemetry` convention) is deliberately **not** followed; DP2 rationale.

### D3 — T4-A: trust boundary, before the handler

| Site | Placement |
|---|---|
| `engine.go:724 SendCommand` | after the telemetry span, before `ref.noSender.SendSync` (:757): `Resolve` → `tenancy.Attach` → replace `ctx`. Any error returns immediately; the command never enters the actor system. Guarded by `engine.tenantResolver != nil` |
| `event_sourced_actor.go:505 processCommandAndReply` | `tenancy.Require(goCtx)` after the metrics block, **before** `HandleCommand` (:527) |
| `event_sourced_actor.go:752 processAndBatch` | same, before `HandleCommand` (:772) |
| `durable_state_actor.go:172 processCommand` | same, before `HandleCommand` (:194) |

Failure → `sendErrorReply`, no state mutation, no write. The actor-side check exists because `SagaActor` dispatches through `actorSystem.NoSender().SendSync` and bypasses `SendCommand` entirely (#54).

### D4 — T4-B: defensive invariant at the write, no re-resolution

Both gates call `tenancy.Require`/`VerifyUnchanged` — pure `context` reads. Neither touches a resolver.

| Site | Placement |
|---|---|
| `processCommandAndReply` | after `buildEnvelopes` (:538), before `persistEvents` (:544) |
| `processAndBatch` | before `batchBuffer` append (:814); record `entity.batchTenant` on the first buffered entry, `tenancy.VerifyUnchanged` every later one, cleared in `resetBatch` |
| `durable_state_actor.go` | before `persistStateAndPublish` (:212) |

`flushBatch` (:850–880) is **not** modified: its `goakt.Ask(context.Background(), …)` (:859) merges many commands and can carry no identity. Homogeneity is proven at buffer-append time instead, so every envelope in a flush was accepted under one verified `TenantContext`.

**Explicit exclusion**: `DurableStateActor.PostStop` (:142) flushes state on a lifecycle context. That state already passed T4-A; gating it would break shutdown.

### D5 — `Resolve` exactly once per command

One call site (`SendCommand`), no retry wrapping it. Actors receive a **marker** extension carrying no resolver, so they cannot call `Resolve` even by mistake. goakt's `SendSync`→`Ask` passes `ctx` through unchanged (`receive_context.go`, `async=false`), so nothing downstream needs to re-derive identity; `Attach` is idempotent and returns `ErrDenied` rather than overwriting.

### D6 — `WithSingleTenant` uses the same path

`tenancy.WithSingleTenant(id)` returns an ordinary `TenantResolver` (tenancy/resolver.go:71) and is passed to the same `WithTenantResolver`. Same field, same call site, same gates, zero `if singleTenant` branches anywhere in ego.

### D7 — Backward compatibility

No registration ⇒ no marker extension ⇒ `entity.tenantAware == false` (one bool field read, already-loaded, per command) ⇒ both gates skipped; `SendCommand`'s resolve block is one nil compare; `validateActorSystemExtensions` gets `{TenancyExtensionID, cfg.tenantResolver != nil}` which is false for legacy engines. The forwarded `ctx` is identical to today's. Proof is the existing engine suite passing unmodified.

## Data Flow

    SendCommand ──Resolve──→ Attach ──→ SendSync ──→ Receive
        (once, engine)                                  │
                                       T4-A Require ────┤ before HandleCommand
                                       T4-B Require ────┘ before persist / buffer append
                                                          (flushBatch untouched)

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/extensions/extensions.go` | Modify | `TenancyExtensionID` + marker struct (no resolver field) |
| `option.go` | Modify | two `Config` fields, `WithTenantResolver`, `isNilResolver`, conditional marker in `GoaktOptions` |
| `engine.go` | Modify | `tenantResolver` field, `ErrAmbiguousTenantResolver`, `NewEngine` validation, resolve+attach in `SendCommand`, extension check |
| `event_sourced_actor.go` | Modify | `tenantAware`/`batchTenant` fields, gates at the four sites above |
| `durable_state_actor.go` | Modify | `tenantAware` field, two gates |
| `saga_actor.go`, `tenancy/` | Unchanged | #54 / consumed as-is |

## Testing Strategy

| Layer | What | How |
|---|---|---|
| Unit | Option inertness (nil, typed-nil, nil-after-valid), counter, marker registration | `option_test.go`, mirrors `TestConfigGoaktOptionsEncryptor` |
| Unit | `NewEngine` rejects 2+ registrations; accepts exactly 1 | `engine_test.go`, `require.ErrorIs` |
| Integration | `Resolve` called exactly once; `tenancy.From` visible in `HandleCommand`; resolver error ⇒ zero store writes | `mocks/tenancy.TenantResolver` + mock stores |
| Integration | T4-A: missing tenant ⇒ `HandleCommand` never invoked (spy), zero writes — batched **and** non-batched | actor tests |
| Integration | T4-B: mixed-tenant batch rejected at append, `Resolve` never re-called | actor tests |
| Regression | Legacy suite unmodified with no resolver | existing `engine_test.go` |

## Threat Matrix

N/A — no routing, shell, subprocess, VCS/PR automation, executable-file classification, or process-integration boundary. The trust boundary here is in-process and covered by T4-A/T4-B negative-path tests.

## Migration / Rollout

No migration. Purely additive and opt-in; rollback removes the option, the extension and the gate call sites.

## Open Questions

- [ ] None blocking. Known limitation: in tenant-aware mode, saga-issued commands fail closed at the actor gate until #54 lands — accepted and documented on the option.
- [ ] Tracker hygiene (owner, not SDD): reconcile #55's AC6 wording before the cycle closes.
