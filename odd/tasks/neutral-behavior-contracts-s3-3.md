# Feature: public Spawn* methods and the remote-spawn test (#123, slice S3-3)

Branch: `feat/123-s3-3-spawn-methods` · Base: `origin/main` `9084b80` · Epic: #10 · Issue: #123 ·
Design: `openspec/changes/ego-arch-002-s3/design.md` (§5.4, §6, §8, §9 row S3-3)

## Problem

S3-1 added the runtime-neutral behavior contracts in `port/behavior`, and S3-2 made the engine's three
spawn paths (`spawnEventSourced`, `spawnDurableState`, `spawnSaga` in `engine.go`) accept them. But
the only public entry points were still `Engine.Entity`, `DurableStateEntity` and `Saga`, which take the
old `ego.*Behavior` interfaces. Those embed GoAkt's `extension.Dependency`, so a behavior with only
`ID()` and its domain methods still could not be spawned by a caller outside package `ego`. Nothing
tested the neutral behaviors on a real two-node cluster either.

## What changes in this slice

- `engine_spawn.go` (new): `Engine.SpawnEventSourced`, `SpawnDurableState` and `SpawnSaga`, each a
  one-line delegation to the unexported spawn function S3-2 introduced (design §5.4). They take the
  `port/behavior` contracts and the same options as the old methods.
- `engine_test.go`: the node setup of `TestEngineMultiNodeRemoteEntitySpawn` moves into a helper,
  `newTestCluster(t, nodeOpts...)`, one `[]Option` per node. The test's spawns and assertions are
  unchanged.
- `engine_neutral_cluster_test.go` (new): `TestEngineMultiNodeNeutralBehaviors`, one two-node cluster
  shared by three subtests: a serializable behavior spawned eight times from node 1 through
  `SpawnEventSourced` lands on node 2 at least once and answers commands (kinds registered with the
  existing `WithEntityKinds`); domain-only behaviors of all three families are rejected with
  `ErrBehaviorNotSerializable`, the right `Kind` and `EntityID`, and no actor on either node; a
  value-type behavior spawned through the old `Entity` gets `ErrBehaviorNotPointer` and no actor.
- `engine_spawn_test.go` (new): `TestEngineSpawnMethodsDomainOnlySingleNode`, the single-node
  domain-only round trips through the public methods (the envelope-capable behavior still receives
  `HandleEnvelope`), plus `ErrEngineNotStarted` before `Start`.
- `CHANGELOG.md`: one Features entry.

Out of scope: `BehaviorKind`/`WithBehaviorKinds` and the "old and new registration interoperate"
subtest (S3-4); `Deprecated:` markers and examples (S3-5). `engine.go` and `option.go` are untouched.

## Why this shape

The new methods only delegate, so the old and new entry points share one spawn path, and #105 IMPL-4's
family guard needs one copy (design §5.4). The two-node test reuses the existing in-process cluster
setup instead of `example/cluster`, which needs Kubernetes. The helper takes one option slice per node
so S3-4 can register kinds differently on each node without another refactor.

## Constraints

- No incompatible change to package `ego`; apidiff must show only the three methods.
- No archcheck baseline change or exception. No release tag between S3-2 and S3-4.
- TDD: strict (user global configuration); runner `go test` (no `-race`, no workbench, always
  `-timeout`).
- Route: direct inline. One writer; the design names every file and function, and the change is
  three one-line methods plus tests.

## Tasks

- [x] **T1** RED: build failure of the new tests on `origin/main`; the guarded value-type cluster run on
  pre-S3-2 `main`. Check: `undefined: SpawnEventSourced`; unrecovered panic, then timeout.
- [x] **T2** GREEN: `engine_spawn.go`; helper extraction. Check: new tests and the existing cluster
  tests pass.
- [x] **T3** Compatibility evidence: apidiff and archcheck. Check: three additions only; unchanged
  archcheck.
- [x] **T4** `CHANGELOG.md` and this document. Check: structural readback.
- [x] **T5** Full verification: `ciselect`, full root suite, nested modules, golangci-lint.

## Acceptance criteria (S3-3 row of design §9)

1. The value-type cluster subtest's guarded run on `main` panics (output recorded in the PR).
2. All three S3-3 subtests and the single-node domain-only tests pass on the slice.
3. `TestEngineMultiNodeRemoteEntitySpawn` passes after the helper extraction with unchanged assertions.

## Progress and evidence

**RED (T1).** Build RED on `origin/main` `9084b80` with the new test files:
`./engine_neutral_cluster_test.go:81:31: engine1.SpawnEventSourced undefined (type *Engine has no field
or method SpawnEventSourced, but does have method spawnEventSourced)`, and the same for
`SpawnDurableState`, `SpawnSaga` and `engine_spawn_test.go`; `FAIL github.com/pablogore/ego/v4 [build
failed]`.

Behavioral RED of `value-type behavior in cluster mode`. `origin/main` already contains S3-2, so the
old `Entity` no longer reaches `Inject` for a value type there. The guarded run therefore used the
`main` commit just before S3-2, `23bc7f4`, exported with `git archive` into a scratch directory, with
this slice's `engine_test.go` (for `newTestCluster`) and a throwaway copy of the subtest reduced to the
old-API call, not committed. Command:
`go test -count=1 -run 'TestEngineMultiNodeNeutralBehaviors/value-type' -timeout 90s .`. Result: the
subtest goroutine's stack shows a `panic` in `reflect.(*rtype).Elem` ← `goakt/v4/internal/types.reflectType`
(`registry.go:109`) ← `(*registry).Register` (`registry.go:87`) ← `(*actorSystem).Inject`
(`actor_system.go:2263`) ← `(*Engine).Entity` (`engine.go:662`). The unrecovered panic ran the test
cleanups, and `newTestCluster`'s actor-system stop blocked in `(*actorSystem).localActors`
(`actor_system.go:1964`) on the lock `Inject` left held, until `panic: test timed out after 1m30s`,
`FAIL github.com/pablogore/ego/v4 90.019s`. (The timeout report supersedes the original panic
message, so only the stack records it.)

**GREEN (T2).** `go test -count=1 -run 'TestEngineMultiNodeNeutralBehaviors|TestEngineSpawnMethodsDomainOnlySingleNode' -timeout 90s -v .`:
all three cluster subtests and all four single-node subtests pass (0.87 s); `-count=5` passes (4.3 s).
Unchanged `TestEngineMultiNodeRemoteEntitySpawn`, `TestEngineClusterMode`,
`TestEngineRemoteSpawnTenantBinding` pass.

**apidiff (T3)**, package `ego`, `origin/main` `9084b80` vs head:

```
Compatible changes:
- (*Engine).SpawnDurableState: added
- (*Engine).SpawnEventSourced: added
- (*Engine).SpawnSaga: added
```

**archcheck**, base and head: `37 packages checked, 157 edges checked, 1 baselined, 0 violation(s),
0 stale entries`; nothing under `internal/cmd/archcheck` changed.

**Verification (T5).** `ciselect -base origin/main`: mode `full` (shared root package), 26 of 26
packages, all six nested modules selected. `go vet ./...` clean. Full root suite
(`go test -count=1 -timeout 1200s ./...`, Go 1.27.1 with `GOROOT` unset, no `-race`): every package
passes, the root package in 370 s. Nested modules (`scripts/ci/verify-module.sh`, Go 1.26.6 with
`GOTOOLCHAIN=local`, the known local golangci-lint/Go 1.27 mismatch): `benchmark`, `example/cluster`,
`publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket` all exit 0 (tidy, build,
vet, lint `0 issues`, tests, and the `compat` lane where present). Root golangci-lint
(`--new-from-rev=origin/main --modules-download-mode=readonly .`, Go 1.26.6; the readonly flag
overrides `.golangci.yml`'s `vendor` mode instead of vendoring locally): `0 issues`. CI is
authoritative for lint and the race lane.

## Next step

S3-4 (`BehaviorKind`, `WithBehaviorKinds`, `NewEngine` pointer check, the interop subtest), which must
land before #105 IMPL-4.
