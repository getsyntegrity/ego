# Feature: spawn-site bridge for neutral behaviors (#123, slice S3-2)

Branch: `feat/123-s3-2-spawn-bridge` · Base: `origin/main` `27848da` · Epic: #10 · Issue: #123 ·
Design: `openspec/changes/ego-arch-002-s3/design.md` (§2.3, §5.3, §5.4, §5.6, §6, §8, §9 row S3-2)

## Problem

S3-1 added the runtime-neutral contracts in `port/behavior`, but the engine still handed every behavior
straight to GoAkt: `Engine.Entity`, `DurableStateEntity` and `Saga` called `ActorSystem.Inject(behavior)`
and put the behavior itself in `goakt.WithDependencies`, and the three actors type-asserted the old
`ego.*Behavior` interfaces, which embed GoAkt's `extension.Dependency`. Two consequences:

1. A behavior without `MarshalBinary`/`UnmarshalBinary` cannot reach an actor at all.
2. A behavior with value receivers makes `Inject` panic inside GoAkt's type registry
   (`reflect.Type.Elem` on a non-pointer), even on a single node, while GoAkt holds the actor-system
   lock, so the actor system can no longer be stopped (design §2.3).

## What changes in this slice

- `behavior_dependency.go` (new): `spawnDependency(sys, b)` decides what carries a behavior to GoAkt.
  A non-nil pointer that implements `extension.Dependency` passes through unchanged and is registered
  with `Inject`, exactly as before. Anything else is wrapped in `extensions.LocalBehavior` outside
  cluster mode, and rejected in cluster mode with `*BehaviorPlacementError` wrapping
  `ErrBehaviorNotSerializable` or `ErrBehaviorNotPointer`, before any spawn. `behaviorFrom[T]` reads a
  behavior back from a spawn dependency, unwrapping a `LocalBehavior`.
- `internal/extensions/extensions.go`: `LocalBehavior`, a spawn dependency that is never registered and
  never serialized; its `MarshalBinary`/`UnmarshalBinary` return an error.
- `engine.go`: `ErrBehaviorNotSerializable` and `ErrBehaviorNotPointer` in the error `var` block,
  `BehaviorPlacementError` next to it. The bodies of `Entity`, `DurableStateEntity` and `Saga` are
  renamed in place to the unexported `spawnEventSourced`, `spawnDurableState` and `spawnSaga`, which take
  the neutral contracts; the public methods keep their signatures and delegate. Each body calls
  `spawnDependency` where it used to call `Inject(behavior)`.
- `event_sourced_actor.go`, `durable_state_actor.go`, `saga_actor.go`: the unexported `behavior` fields
  and the envelope assertions use the `port/behavior` types; `PreStart` reads the behavior with
  `behaviorFrom`.

Out of scope (later slices): public `Spawn*` methods (S3-3), `BehaviorKind`/`WithBehaviorKinds` and the
`NewEngine` pointer check (S3-4), `Deprecated:` markers and examples (S3-5). `option.go` is untouched.

## Why this shape

Design §4 option C: the serializable path keeps the same Go value, GoAkt type name and wire bytes, so
rolling upgrades keep working; the rejected alternatives (kind registry with factories, engine-built
wrapper) change the wire format. The unexported spawn functions are the single place #105 IMPL-4's
family guard goes (design §5.4, ego-arch-003).

## Constraints

- No incompatible change to package `ego` (human decision: no break inside v4). apidiff must show only
  additions.
- No archcheck baseline change or exception.
- TDD: strict (user global configuration); runner `go test` (no `-race`, no workbench locally).
- Route: direct inline (one writer; the design already names every file and function).

## Tasks

- [x] **T1** RED: the single-node value-type test (`behavior_value_type_test.go`) against `main`'s code,
  unrecovered, `-timeout 90s`; then the build-failure RED of `behavior_dependency_test.go` and the
  `LocalBehavior` test. Check: panic/timeout recorded; `undefined: spawnDependency`,
  `undefined: NewLocalBehavior`.
- [x] **T2** GREEN: `LocalBehavior`, `spawnDependency`, `behaviorFrom`, the typed errors, the in-place
  rename with delegation, actors on the neutral types. Check: the new tests and the existing cluster
  tests pass.
- [x] **T3** Compatibility evidence: apidiff on package `ego`, the base-API consumer program against
  base and head, archcheck before/after. Check: additions only; identical output; unchanged baseline.
- [x] **T4** `CHANGELOG.md` (Unreleased, Features) and this document. Check: structural readback.
- [x] **T5** Full verification: `ciselect`, full root suite, nested modules, golangci-lint.

## Acceptance criteria (S3-2 row of design §9)

1. The single-node value-type test is RED on `main` (panic) and GREEN on the slice.
2. The `spawnDependency` pass-through identity and local-wrapper unit tests pass.
3. The existing root suite, including `TestEngineMultiNodeRemoteEntitySpawn`, passes unchanged.

## Progress and evidence

**RED (T1).** `go test -count=1 -run 'TestEngineEntityValueTypeBehaviorSingleNode$' -timeout 90s .` on
`main`'s code: `panic` in `reflect.(*rtype).Elem` ← `goakt/v4/internal/types.reflectType`
(`registry.go:109`) ← `(*actorSystem).Inject` (`actor_system.go:2263`) ← `(*Engine).Entity`
(`engine.go:662`). The test's cleanup then blocked in `actorSystem.shutdown` on the lock `Inject` left
held, and the 90 s timeout ended the binary (`FAIL github.com/pablogore/ego/v4 90.114s`). Build RED:
`vet: ./behavior_dependency_test.go:157:16: undefined: spawnDependency`;
`internal/extensions/extensions_test.go:208:11: undefined: NewLocalBehavior`.

**GREEN (T2).** `TestEngineEntityValueTypeBehaviorSingleNode` (same command, 0.01 s), `TestSpawnDependency`,
`TestBehaviorPlacementError`, `TestBehaviorFrom`, `TestEngineSpawnsDomainOnlyBehaviorsSingleNode` (three
families; the envelope-capable behavior receives `HandleEnvelope`),
`TestEngineRejectsUnplaceableBehaviorsInClusterMode` (single-node cluster; four rejections, nothing
spawned, a serializable behavior still spawns), `TestLocalBehavior`; unchanged
`TestEngineMultiNodeRemoteEntitySpawn`, `TestEngineClusterMode`, `TestEngineRemoteSpawnTenantBinding`
pass.

**apidiff (T3)**, package `ego`, `origin/main` `27848da` vs head:

```
Compatible changes:
- BehaviorPlacementError: added
- ErrBehaviorNotPointer: added
- ErrBehaviorNotSerializable: added
```

No incompatible change. **Consumer program**: the S3-1 base-API program (`replace` to base, then to
head): `go vet` and `go run` succeed on both, output identical. **archcheck**: before and after
`36 packages checked, 156 edges checked, 1 baselined, 0 violation(s), 0 stale entries`; no change under
`internal/cmd/archcheck`.

**Verification (T5).** `ciselect -base origin/main`: mode `full` (shared root package), 25 of 25
packages, all six nested modules selected. `go vet ./...` clean. Full root suite
(`go test -count=1 ./...`, Go 1.27.1, no `-race`): every package passes, the root package in 370 s.
Nested modules (`scripts/ci/verify-module.sh`, `GOWORK=off`, `GOFLAGS` cleared): with Go 1.27.1 the
lint step panics in golangci-lint's type loader ("file requires newer Go version go1.27"), the known
local toolchain mismatch; rerun with Go 1.26.6 (`GOTOOLCHAIN=local`): `benchmark`, `example/cluster`,
`publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket` all exit 0 (build, vet,
lint `0 issues`, tests pass, including the `compat` lane where present). Root golangci-lint
(`--new-from-rev=origin/main ./...`, Go 1.26.6, local `go mod vendor`, not committed): `0 issues`.
CI is authoritative for lint and the race lane.

## Review follow-up (PR #139, approve with nits)

- [x] **T6** Reject nil and typed-nil behaviors up front in `spawnDependency`, in every mode, with
  `*BehaviorPlacementError` wrapping `ErrBehaviorNotPointer` (no new exported error). Before, outside
  cluster mode a nil behavior was wrapped in `LocalBehavior` and the spawn panicked at `behavior.ID()`,
  and in cluster mode a nil or typed-nil domain-only behavior got `ErrBehaviorNotSerializable` (wrong
  cause). Reworded the "never registered" docs on `LocalBehavior` and `spawnDependency` (GoAkt's
  child-spawn path does `Inject` its dependencies, `actor/pid.go:3648`), and reflowed the
  `dispatchToBehavior` comments. `BehaviorPlacementError.Error()` with an empty `EntityID` now says
  "cannot register or place".
  - RED: new `TestSpawnDependency/nil_and_typed-nil_behaviors_are_rejected_in_every_mode` (7 behaviors
    × 2 modes) failed (`Expected nil, but got: &extensions.LocalBehavior{behavior:… (nil)}`); new
    `TestEngineRejectsNilBehaviorsSingleNode` failed in all 9 cases (nil and typed-nil, three
    families, old API and unexported functions) with `should not panic … nil pointer dereference`;
    the new cluster-mode subtest failed in 6 of 9 cases with the wrong cause (the 3 typed-nil
    serializable ones already got `ErrBehaviorNotPointer`).
  - GREEN: all pass, with no panic and no change in `NumActors`. The existing cluster tests pass, as
    do archcheck (unchanged) and apidiff (the same three additions only). golangci-lint on
    `. ./internal/extensions/` with Go 1.26.6: `0 issues`.

## Next step

S3-3 (public `SpawnEventSourced`/`SpawnDurableState`/`SpawnSaga`, two-node test helper, remote-spawn
test), then S3-4, which must land before #105 IMPL-4.
