# Exploration: EGO-ARCH-001 package and module topology

Date: 2026-09-23
Tracker: getsyntegrity/ego#104; parent #10; downstream runtime epic #11
Baseline: `main` at `a4edded`
Phase: read-only architecture spike; no production changes

## Current state

The root module is `github.com/pablogore/ego/v4` (`go.mod:1`). Six nested
modules already exist: four `publisher/{kafka,nats,pulsar,websocket}` modules,
`benchmark`, and `example/cluster`. Each nested publisher requires root
`v4.4.3` and replaces it with `../../` in this checkout. A fresh nested-module
build and a published-module build therefore have distinct dependency inputs.

The root `ego` package contains 25 production and 41 test Go files. It mixes
behavior definitions (`behavior.go`), `Engine` (`engine.go`), GoAkt actor
implementations, options (`option.go`), projection and saga execution, and
telemetry. A static scan of production imports in the root module found 32
packages and 52 internal import edges. This is a source scan, **not** the
compiler-resolved graph; confirm it with `go list -deps -test -json` before
making dependency or timing claims. The local environment has no Go executable,
so build, tests, benchmarks and `go list` could not run in this spike.

Reproduce the module list with `find . -name go.mod -not -path './vendor/*'`.
In a Go-enabled checkout, enumerate each module separately with
`(cd MODULE_DIR && go list -deps -test -json ./...)`; use its `Imports`,
`TestImports`, `XTestImports`, `Module` and load errors to build the authoritative
graph. Time both `go test ./...` and targeted package lists with a fresh and a
warm Go cache. The source-scan counts above are provisional until that run.

| Package/group | Current role and imports | Initial destination |
| --- | --- | --- |
| Root `ego` | Public behavior contracts plus GoAkt actors, engine, options and OTel; imports 13 first-party packages | Split neutral definitions from GoAkt adapter; retain a compatibility facade at the old path |
| `command`, `tenancy` | Command model and tenant identity/resolution; `command` imports `tenancy` | Neutral application/domain contracts, subject to protobuf policy |
| `persistence`, `offsetstore` | Public store interfaces and scopes; both import `egopb`, and `persistence` imports `tenancy` | Ports, with explicit wire-format decision |
| `projection`, `eventstream` | Handler and stream interfaces; `eventstream` imports two internal utilities | Application/ports; isolate concrete runners |
| `encryption`, `eventadapter` | Public interfaces; adapter uses protobuf | Ports/codec boundary |
| `egopb` | Generated shared message types, imported by root, stores, migration, testkit and mocks | Explicit shared schema/codec boundary; do not assume runtime-neutral means protobuf-free |
| `internal/extensions` | GoAkt registration and concrete wiring; imports six contract packages | GoAkt adapter implementation |
| `migration` | Imports root `ego`, stores, tenancy and `egopb` | Application service or separately versioned tool after dependency check |
| `testkit`, `persistence/conformance`, `mocks`, `test/data` | Fixtures and conformance support | Test support; keep reverse test-import edges visible to CI |
| `publisher/*` | Already separate modules with broker/client implementations | Existing adapters; add independent CI coverage |
| `example/*`, `benchmark` | Consumer examples and benchmarks; cluster and benchmark have own modules | Consumer verification, outside core contract |
| `internal/{queue,runner,syncmap,ticker,pause}` | Local implementation utilities | Keep private; relocate only when an extracted module needs one |
| `internal/cmd/ciselect` | Root-module package selection | Extend to repository-wide module graph before another split |

The production graph is acyclic at package level, but neutral contracts are
not independent of implementation yet. `EventSourcedBehavior` and
`DurableStateBehavior` embed `extension.Dependency` (`behavior.go:47,99`),
and `SagaBehavior` does so in `saga.go:41`. The root API exposes
`NewEngine(goakt.ActorSystem, ...)` (`engine.go:221`),
`Engine.ActorSystem() goakt.ActorSystem` (`engine.go:415`),
`Config.GoaktOptions() []goakt.Option` (`option.go:122`) and
`ClusterKinds() []goakt.Actor` (`option.go:192`). Protobuf is also a
deliberate public API today: `Command`, `Event`, `State` alias `proto.Message`
(`behavior.go:34-44`), and command envelopes and results use it. The ADR must
separate the GoAkt coupling from the independent protobuf policy; treating all
protobuf as an accidental adapter detail would change the programming model.

## CI and test-selection evidence

`internal/cmd/ciselect/selector/graph.go` computes reverse production-import
closure and test-import consumers for the root module. The PR workflow runs
that selector and `scripts/ci/go-test.sh` under `-race`; the push workflow runs
its full mode. Both workflows execute `go mod tidy && go mod vendor` in the
root and use a root-only linter. This is a fast lane for the root module,
not repository-wide multi-module CI.

`selector/classify.go:164` marks any nested module change as `ClassSatellite`;
`selector/select.go:129,143` permits a change consisting only of such files
to return `ModeNone`. The root full suite (`go test` package list from root
`go list ./...`) also cannot include nested modules. No module-specific test
jobs appear in the two build workflows. Thus the publishers are **already
independently buildable in layout, but not covered by these CI jobs**. This
must be addressed before adding more `go.mod` files. In addition, all root
Go files force full fallback (`selector/classify.go`), so moving neutral code
out of root is necessary to realize a smaller affected set.

The current `-coverpkg` list remains the complete included root package set
even in affected mode (`scripts/ci/go-test.sh`). Benchmark whether this
instruments too much before claiming the selector's test subset provides
proportional compile savings. `go mod tidy && go mod vendor`, lint and the
race detector may dominate wall time independently of package selection.

## Approaches

1. **Create many modules now** — put every port and adapter behind its own
   `go.mod` immediately.
   - Pros: explicit compilation boundaries and isolated dependency lists.
   - Cons: breaks unprepared CI, complicates versions and `internal` imports,
     and retains GoAkt in the behavior API unless contracts change first.
   - Effort: high; not recommended.
2. **Extract neutral packages, then promote proven boundaries to modules** —
   move definitions behind stable imports, keep adapters outward, extend CI,
   and split module(s) after independent builds are demonstrated.
   - Pros: each move can preserve behavior and expose an actual boundary;
     reverse-dependency selection already works inside the root module.
   - Cons: temporary facade/compatibility code and two stages of CI work.
   - Effort: medium to high; recommended.
3. **Remain one root module permanently** — enforce package boundaries and
   keep the existing fast lane.
   - Pros: simplest versioning and releases.
   - Cons: cannot isolate GoAkt dependency graph/toolchain per module;
     package selection alone may not meet the compile-time objective.
   - Effort: medium; retain as a measured fallback, not an a priori choice.

## Recommendation and candidate boundaries

Define a neutral behavior/command/tenant/store-port dependency direction in
#104. #103 should remove GoAkt `extension.Dependency` from the behavior
contracts or introduce a separate neutral definition with a documented
compatibility bridge. #11 owns exact Runtime SPI, neutral references,
capabilities, GoAkt adapter and in-memory conformance; #104 should not lock
those signatures. #105 owns assembly, while #106 owns general adapter SPI.

First candidate module: **neutral core/ports**, once it demonstrably builds
without GoAkt and without importing the root `ego` package. The GoAkt runtime
adapter can become a second module after its imports point into that core;
existing publisher modules stay separate. `persistence`/`egopb` placement
depends on whether protobuf is part of the supported public contract. Do not
create a module for each small package or move code into `internal` if public
consumers must import its contracts. Avoid root ↔ nested-module cycles; local
`replace` directives must not be mistaken for release version policy.

For a change to component X, execute X's tests and the tests of **transitive
reverse consumers**, including packages that only import X in tests. Go
compiles forward dependencies automatically; rerunning all their tests is a
separate full-gate choice. Compute a repository-wide module/package graph,
include tests in every existing nested module, fail closed on unknown paths
and graph errors, and print the selection. A dynamic job matrix can supply
one workflow; a separate YAML per module is not required. Require full tests
for module/workspace metadata, generated schemas, selector/CI changes, and
the final main/merge gate. Measure cold/warm builds and unit/race timing for
root leaf, root contract, GoAkt adapter and one publisher before finalizing
the module layout or coverage denominator.

## Risks and open decisions

- Existing users implement `extension.Dependency` on behaviors and construct
  a GoAkt system before `NewEngine`; compatibility cannot be inferred from a
  pure package move. Name affected APIs and rollback in the proposal.
- Moving `egopb` or changing `proto.Message` changes serialization and public
  types. Decide this separately from removing GoAkt.
- Independent release/version policy for nested modules is undefined here;
  the checked-in `replace` directives can hide released-version failures.
- `internal/extensions` is rooted under today's module. Go's `internal`
  visibility rules and package imports need checking when a new module owns
  the adapter.
- CI's current satellite `ModeNone` is a safety gap. Correct it before any
  new module is introduced, and test already-existing satellite modules.

## Atomicity

SPLIT_REQUIRED: normative architecture/map (#104), runtime SPI and
conformance (#11), and multi-module CI (#38) are separate verifiable outcomes.
The proposal for #104 can remain atomic if it defines the topology and
compatibility decisions without implementing the other epics.

## Ready for proposal

Not yet for a final normative topology: run the compiler-resolved graph and
timing baseline in a Go-enabled checkout, then close protobuf and public
compatibility questions. This exploration is sufficient to draft the options
and the first extraction seam, with those decisions explicitly open.
