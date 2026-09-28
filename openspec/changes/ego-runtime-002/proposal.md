# Proposal — Deterministic in-memory runtime and `compose/inmem` (EGO-RUNTIME-005, #105 IMPL-6)

| Field | Value |
|---|---|
| Change | `ego-runtime-002` |
| Date | 2026-09-27 |
| Phase | `sdd-propose`: proposed, design only |
| Tracker | [`#148`](https://github.com/getsyntegrity/ego/issues/148), parent [`#11`](https://github.com/getsyntegrity/ego/issues/11), foundation [`#10`](https://github.com/getsyntegrity/ego/issues/10); closes the last criterion of [`#105`](https://github.com/getsyntegrity/ego/issues/105) when implemented |
| Baseline | `main` at `57c4b11` |
| Evidence | [`design.md`](./design.md) §2 (what the GoAkt runtime does today, with `file:line`), §9 (reproduction) |
| Builds on | ego-runtime-001 D1–D10 and maintainer decisions 1–9; ego-arch-003 §D1–§D8, §5.2, §6 IMPL-6; ego-arch-004 §D4, §D6 (V8), F-E; ego-arch-002-s3; ego-arch-006 D1–D8, F1; ego-arch-001 §3, §10 |
| Human gate | Maintainer answers to the open questions in design §5, then approval of this design before spec 1 |

## Problem

Issue #105 says Ego must have "at least one GoAkt composition and one in-memory composition without changing the domain". Only the GoAkt half exists. Ego's one runtime is `*ego.Engine`, which is built on the GoAkt actor system, so the only way to run a behavior today is to start GoAkt.

ego-arch-003 §5.2 listed three blockers for the in-memory half. Two are gone: behaviors no longer need GoAkt (`port/behavior`, #123), and consumer code can be written against a runtime-neutral interface (`port/runtime`, #147, reached through `compose/goakt.App.Runtime()`). The third blocker is the one this change designs: there is no second runtime that implements `port/runtime`, and no composition root that builds it.

## What changes

This change is documentation only. It proposes:

1. **An in-memory runtime** in a new root-module package, `internal/inmemruntime` (design §D1). It implements `port/runtime.Runtime` without any actor system. Each entity is a small serialized mailbox (a queue processed by one goroutine at a time), so commands to one entity run one after another, in the order they arrive. It persists through the same stores the `compose.Spec` carries, publishes on the same two event-stream topics, and feeds the same publishers.
2. **Its semantics, stated capability by capability** against what `*ego.Engine` does today (design §D3–§D7): event-sourced and durable-state entities, `SendCommand` and `Dispatch`, sagas including the `SagaStatus` lifecycle status fixed by #153, and the event stream. Projections return `runtime.ErrUnsupported` in this change; that is a recommendation, open question Q3.
3. **A composition root, `compose/inmem`**, with the same `compose.Spec`, the same `Spec.Validate` (V1–V8), and the same sequencer (`compose/internal/lifecycle`) and publisher start-and-probe helper (`compose/internal/adapters`) that `compose/goakt` uses. It exposes `App.Runtime() runtime.Runtime`, like `compose/goakt` (design §D8).
4. **An archcheck rule, `inmem-no-runtime`**, that keeps both new packages away from package `ego`, `internal/extensions` and GoAkt, plus closure tests that check the same thing transitively (design §D2). No baseline entry, no exception, no relaxed rule.
5. **The neutrality proof #105 asks for**: one behavior value and one consumer function, written against `port/runtime`, run on both compositions with the same `Spec`, producing the same observable state, journal and stream messages (design §D9).

## Why this shape

- **No change to package `ego`.** Nothing in the chain edits `engine.go`, `option.go` or any other root-package file, so it cannot conflict with the other writers of those hot files and cannot change GoAkt behavior. The price is that the in-memory runtime re-implements about a dozen small pure rules (deadline gates, result classification, event envelopes). A shared test table runs the same scenarios on both runtimes to catch drift (design §D10). Extracting the shared logic instead is open question Q4.
- **Internal runtime, public composition root.** Consumers reach the in-memory runtime only through `compose/inmem.App.Runtime()`, which returns the neutral interface. That keeps the new public v4 API to one small package. Promoting the runtime later is additive, and removing a public package inside v4 is not possible. This is open question Q1.
- **Determinism by construction.** There are no timers in the command path, no passivation, and no background polling. Every guarantee in design §D7 is checkable with condition-based waits instead of sleeps.

## Compatibility

Additive in v4 (ego-arch-001 §10). The new public package is `compose/inmem`, and apidiff must report additions only for it. `internal/inmemruntime` and `internal/runtimeconsumer` are internal. No existing exported symbol changes in `ego`, `port/runtime`, `compose` or `compose/goakt` (design §6).

## Chain of specs

| # | Spec | Tasks | Depends on |
|---|---|---|---|
| 1 | [`specs/inmem-runtime-core`](./specs/inmem-runtime-core/spec.md): package, archcheck rule, lifecycle, event-sourced entities, commands, event stream | 5 | this design approved |
| 2 | [`specs/inmem-runtime-state`](./specs/inmem-runtime-state/spec.md): durable state, publishers, tenancy, erasure | 5 | spec 1 |
| 3 | [`specs/inmem-runtime-sagas`](./specs/inmem-runtime-sagas/spec.md): sagas, compensation, timeout, `SagaStatus` | 4 | spec 1 (spec 2 for tenant-aware sagas) |
| 4 | [`specs/compose-inmem`](./specs/compose-inmem/spec.md): `compose/inmem` composition root | 5 | specs 1 and 2 |
| 5 | [`specs/runtime-neutrality`](./specs/runtime-neutrality/spec.md): neutrality proof, shared table, docs, #105 closure | 4 | specs 3 and 4 |

## Out of scope (MUST NOT in this change)

- Any production code; each spec is implemented in its own pull request.
- Placement, supervision and passivation contracts (RUNTIME-003), capability negotiation and a runtime `Descriptor` (RUNTIME-006, ego-arch-004 F-E), the public conformance suite (RUNTIME-007), drain and shutdown policy (#24), the physical move of the engine (#124), the write-side option redesign (#12), an `EntityRef` handle (#29).
- A projection runner for the in-memory runtime (follow-up FU-A, design §5 Q3).
- Changing any doc comment or behavior of the GoAkt adapter, including the two mismatches this design found (design §2.6, Q7).

## Rollback

Every spec is additive. Until a release contains `compose/inmem`, any spec can be reverted. After a release, `compose/inmem` is public v4 API and must not be deleted inside v4; the internal packages can still change freely.
