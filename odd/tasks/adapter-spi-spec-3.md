# Feature: composition uses the adapter SPI, and the extension guide (#106, ego-arch-004 spec 3)

Branch: `feat/106-spec3-composition` · Base: `origin/main` `beed644` (spec 2, #158, and #147 S4-4, #161,
merged) · Epic: #10 · Issue: #106

Implements [`openspec/changes/ego-arch-004/specs/adapter-composition/spec.md`](../../openspec/changes/ego-arch-004/specs/adapter-composition/spec.md)
(spec 3 of 3: slice SPI-5). Previous: spec 2 (`adapter-conformance`, #158). Next: none in this chain;
the follow-ups F-A…F-F are listed in design §5.

## Problem

After specs 1 and 2 the adapter SPI (`port/adapter`) exists and has conformance suites, but core code
does not use it. `compose.Spec.Validate` cannot tell that a declared adapter lies about itself,
`compose/goakt` never calls a publisher's `Start` or `Ping`, `probeStores` asserts a private `pinger`
interface, and `engine.go` asserts `tenancy.FixedTenantResolver` itself instead of through the package
that owns it. There is also no guide for writing a new adapter.

## What changes

1. **V8** in `compose/spec.go`: a declared adapter must serve its slot's port (V8a), its declared and
   implemented optional capabilities must agree in both directions (V8b), and a slot's required
   capabilities must be met (V8c, an empty table in v4). V8 runs on each value after V5/V6 pass it.
2. **Start-and-probe helper** `compose/internal/adapters`: runtime-free, reusable by `compose/inmem`
   (#148). `compose/goakt` step 4 uses it before attaching publishers.
3. **Store probe**: `probeStores` uses `adapter.PingerOf`.
4. **Tenancy accessor**: `tenancy.CapFixedTenant`, `AsFixedTenantResolver`, `FixedTenantOf`;
   `engine.go` calls `FixedTenantOf` at its one fixed-tenant call site.
5. **Guide** `docs/adapters.md` from design §4, with the websocket adopter as its example.

## Scope and constraints

- File ownership: the spec's list plus the coordinator's carry-overs: a source-scan test for the
  assertion sites, the `docs/ci.md` PT-1 line, the design §D2/§D6 sentences, `CHANGELOG.md`, this
  document. `engine.go` only at the fixed-tenant call site. Not touched: `saga_actor.go`,
  `Engine.SagaStatus`, `port/runtime`, publishers' production code, `.github/`.
- Additive in v4; apidiff additions only (`compose`, `tenancy`, `compose/goakt`); no exported change in
  `ego`.
- TDD: strict (user global configuration); runner `go test`. Never `-race`, never the workbench.
- Route: delegated direct (one writer; 2+ non-trivial files).
- Delivery strategy: `single-pr` (the coordinator asked for one pull request for the spec), one
  work-unit commit per task.

## Tasks

- [ ] T4 Tenancy accessor and the `engine.go` call site (done first: V8 needs it).
- [ ] T1 V8 in `compose/spec.go`.
- [ ] T2 Start-and-probe helper and `compose/goakt` step 4.
- [ ] T3 `probeStores` through `adapter.PingerOf`.
- [ ] T5 Guide, `docs/ci.md`, design text, CHANGELOG.

## Evidence

(filled per task)
