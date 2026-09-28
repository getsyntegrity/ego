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

- [x] T4 Tenancy accessor and the `engine.go` call site (done first: V8 needs it). Commit `7a8820c`.
- [x] T1 V8 in `compose/spec.go`. Commit `6492834`.
- [x] T2 Start-and-probe helper and `compose/goakt` step 4. Commit `f37224b`.
- [x] T3 `probeStores` through `adapter.PingerOf`. Commit `0ac0d06`.
- [x] T5 Guide, `docs/ci.md`, design text, CHANGELOG. Commit `e95c86c`.

## Judgement calls

- **V8 runs per value, not as a final pass.** Each value is checked right after V5/V6 accept it, so
  V8's problems keep `Validate`'s documented Spec field order and a V6-rejected duplicate is skipped
  naturally. Rejected: a separate pass after V7, which would break the field order.
- **The runtime port's one-directional rule is a table flag** (`capabilityCheck.declarationOnly`),
  tested with a synthetic entry, because no runtime slot exists in `Spec` yet (F-E). Rejected: leaving
  it undone until F-E, which would leave the spec's MUST untested.
- **V8c is a table keyed by port** (`requiredCapabilities`), empty in v4 and pinned empty by a test.
- **`FixedTenantOf` normalizes `(id, false)` to `("", false)`,** so its documented contract ("zero and
  false when it reports none") holds; the engine only ever used the ID when the flag was true, so its
  behavior is unchanged.
- **`AsFixedTenantResolver` is a plain assertion** (no nilness check): `tenancy` must not import
  `port/adapter`, and every caller already rejects typed nils first (V5; the engine's `isNilResolver`).
- **The source scan lives in `port/adapter`** (`assertion_sites_test.go`), next to the rule it
  enforces, and is AST-based rather than substring-based. It also flags inline or local interfaces
  made only of the optional methods, which is how the old private `pinger` in `probeStores` was caught
  (RED for T3).
- **The engine scenario test is a new root test file** (`engine_fixed_tenant_resolver_test.go`),
  because the only allowed `engine.go` edit is the call site and no existing root test implemented
  `FixedTenantResolver` with `(zero, false)`.
- **Not changed, recorded:** `compose/errors.go`'s `ValidationError.Rule` comment still lists V1–V7;
  it is outside this spec's file list.

## Evidence

RED/GREEN per task (strict TDD, `go test`):

- T4: RED `undefined: tenancy.CapFixedTenant / AsFixedTenantResolver / FixedTenantOf`; the "ID with
  false" subcase RED against a pass-through `FixedTenantOf`; the assertion-site scan RED against
  `origin/main`'s `engine.go` (`engine.go:spawnTenantScope type-asserts an optional adapter
  interface`). GREEN after. The engine scenario test passes before and after (characterization: no
  behavior change is the requirement).
- T1: RED with empty tables: V8a (2), V8b (6, incl. undeclared-but-implemented `FixedTenantResolver`),
  declaration-only, V8c and field-order tests failed. The skip/accept tests (typed nil, duplicate ID,
  unknown capability, implied capability, undeclared adapters) pass before and after as guards.
- T2: RED `no non-test Go files` for the helper; `compose/goakt` RED `Start/Ping calls = []` and
  `Start = <nil>` for the failure-at-k and ping-failure tests. GREEN after.
- T3: RED `TestNoPrivateCopiesOfOptionalInterfaces` naming `compose/goakt/app.go`; GREEN after.

Checks:

- `go test ./compose/... ./tenancy/... ./port/adapter/...`: ok (incl. #146 two-node test).
- archcheck: 8 modules, 52 packages, 213 edges, 0 baselined, 0 violations, 0 stale;
  `composition-no-runtime`'s layer matches `compose/internal/...`.
- apidiff (`beed644` → head): `tenancy` adds `AsFixedTenantResolver`, `CapFixedTenant`,
  `FixedTenantOf`; `ego`, `compose`, `compose/goakt`, `port/adapter`: no change.
- golangci-lint `--new-from-rev=origin/main`: 0 issues.
- ciselect `-base beed644`: mode `full` (root package files changed), all 7 nested modules selected.
- Full root suite once (`go test -count=1 ./...`, no `-race`): exit 0, 30 packages ok.
- `scripts/ci/verify-module.sh` (GO_TEST_RACE=0) for benchmark, example/cluster, publisher/kafka,
  publisher/nats, publisher/pulsar, publisher/websocket, test/compat: all exit 0.

## Next step

Open the pull request (`Closes #106`: with this spec every #106 acceptance criterion maps to merged
or included evidence, design §8). Push, review and merge stay the user's decisions.
