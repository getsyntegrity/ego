# Isolate the multi-node cluster tests as TestCluster* (#206, spec 1 of 2)

Issue: https://github.com/getsyntegrity/ego/issues/206. Branch: `test/cluster-tests-206`, from `origin/develop`. Target: `develop` through a pull request.

Follow-up spec 2, `odd/tasks/test-lanes-206.md`, covers:
- `-skip '^TestCluster'` in the normal shards, compatible with `.github/scripts/test-matrix.sh`;
- the `cluster` and `race` jobs, with the `inttest` triggers and wired into `ci-ok`;
- a temporary commit plus its revert, to collect CI evidence;
- per-lane parity;
- `docs/ci.md` and `docs/testing/go-specs.md`;
- the comment on #206.

## Problem

The actor, engine and runtime suites were migrated to go-specs (#244–#273, #277). One part of #206 is still open: the multi-node cluster tests are not separated from the single-node in-process tests. Some live in their own files (`engine/engine_tenant_cluster_test.go`, `compose/goakt/cluster_test.go`), and others are cluster cases inside top-level tests that are otherwise single-node (`engine/engine_test.go`, `behavior_kind_test.go`, `behavior_dependency_test.go`, `publisher_test.go`). CI cannot run them in their own lane, because nothing in their names tells them apart.

## What changes

1. **Inventory.** Every test that starts more than one actor-system node or real cluster networking gets recorded below: file, top-level test and subtests. The search covers calls to `goakt.WithCluster`, discovery providers (`mockClusterProvider` in `engine/helper_test.go`, `staticDiscovery` in `compose/goakt/cluster_test.go`) and `dynaport`.
2. **Naming.**
   - Every multi-node top-level test is called `TestCluster<Something>`.
   - A cluster case that sits inside a mostly single-node top-level test moves to its own `TestCluster*` function, in a `*_cluster_test.go` file of the **same package**, so white-box tests stay white-box.
   - Shared helpers stay where they are.
   - This is a pure rename and move. No assertion or behavior changes, no new fixed waits, go-specs v0.3.3 only.
   - `.github/unit-test-gate-resources.txt` follows every moved or new file.
3. **A `unitgate` rule.** It fails when a test file calls `goakt.WithCluster` or `dynaport.Get` from a top-level test function whose name does not start with `TestCluster`. The call can be direct, or go through a helper in the same file. The rule is syntactic, and its doc says what it can and cannot see. It is covered by `unitgate` tests in both directions.
4. **Count parity.** For each package, the number of top-level tests and subtests before and after must be the same. The only allowed difference is that cases split out into `TestCluster*` functions leave their old top-level tests and appear in the new ones, and the totals still match. The counts come from `go test -json`, without `-race`.

CI does not change in this spec. The cluster tests keep running in the normal shards until spec 2.

## Why this shape

- **No new `go.mod`.** Modules isolate dependencies, not kinds of tests. These tests need no dependency the root module lacks. Also, 72 of the 73 test files in `engine/` and `internal/engine/*` are white-box and use unexported identifiers, so moving them would force exporting internals.
- **No build tags.** A tagged file is not compiled, vetted or seen by editors unless every tool passes the tag. That brings back the "green but nothing ran" problem that `inttest` removed. Every file keeps compiling in the normal lane, and only the choice of which tests execute changes.
- **Selection by name.** Using `TestCluster*` together with `-run`/`-skip` (Go 1.20 and later) needs no extra mechanism, and the `unitgate` rule keeps the convention honest.

## Scope and constraints

- Out of scope: CI changes (that is spec 2), moving tests to `inttest` or to another module, build tags, rolling out `t.Parallel()`, and fixing races.
- Never run with `-race` locally (user rule). No workbench.
- No direct push to `develop` or `main`.

## Execution

- TDD: strict (source: the user's CLAUDE.md). It applies to the `unitgate` rule: RED first, then GREEN. The renames are verified by the parity count. Runner: `go test ./.github/scripts/unitgate`, plus `go test -json` for the counts.
- RDD: off (global).

## Tasks

- [x] **T1 Inventory and baseline.** Write the full cluster inventory below. Record the per-package `go test -json` counts (top-level tests and subtests) on `origin/develop` for every package in scope: `engine`, `internal/engine/...`, `internal/projectionrunner`, `compose/goakt/...`, `internal/extensions`, `migration`. Route: delegated writer.
- [x] **T2 Isolate in `engine`.** Make every multi-node test a `TestCluster*`. Cluster cases split out of mixed tests go to `*_cluster_test.go` in the same package, and the allowlist is updated. Check: `go test ./engine/` is green. Route: delegated writer.
- [x] **T3 Isolate in the other packages.** The same treatment for `compose/goakt` and any other package the inventory finds. Check: their tests are green. Route: delegated writer.
- [ ] **T4 `unitgate` rule.** Add the rule with RED and GREEN tests in both directions, and make sure `unitgate -strict` is green on the repo. Route: delegated writer.
- [ ] **T5 Count parity.** Record the after counts per package, plus the reconciliation for every moved case. Check: `go test -skip '^TestCluster' ./...` and `go test -run '^TestCluster' <packages>` are both green, and their sum equals the baseline. Route: delegated writer.

## Inventory

The search covered `goakt.WithCluster`, `WithCluster(`, `mockClusterProvider`, `staticDiscovery`, `dynaport` and `discovery.` in every `*_test.go` of the repository (the nested modules `benchmark`, `example`, `inttest`, `test/compat` and the others have no hit). A test counts as multi-node when it starts a clustered actor system, either one node or more, because a clustered system opens gossip, peer and remoting ports on loopback. `TestStart_ActorSystemStepFailsForReal` in `compose/goakt/app_test.go` and the three `WithCluster` cases in the same file do not count: they pass a cluster config that GoAkt rejects before any network is opened (the test's own comment says "No real cluster is started").

**Finding that corrects the plan.** Every multi-node test is already a whole top-level test. No top-level test mixes single-node and cluster cases, so nothing has to be split out into a `*_cluster_test.go` file. T2 and T3 are therefore renames in place.

| File | Top-level test today | Subtests (go test names, after the top-level name) | Nodes | Helper that starts the cluster |
|---|---|---|---|---|
| `engine/engine_test.go` | `TestEngineClusterMode` | 1: `a single-node cluster ...serves entities/starts both projections and an entity` | 1 | `goakt.WithCluster` and `dynaport.Get` inline |
| `engine/engine_test.go` | `TestEngineMultiNodeRemoteEntitySpawn` | 1: `Engine Multi Node Remote Entity Spawn/holds` | 2 | `newTestCluster` (same file) |
| `engine/engine_test.go` | `TestEngineClusterModeStartProjectionAlreadyExists` | 1: `starting a projection twice in cluster mode is a no-op/takes the ErrSingletonAlreadyExists branch` | 1 | inline |
| `engine/behavior_dependency_test.go` | `TestEngineRejectsUnplaceableBehaviorsInClusterMode` | 14 (4 table cases, 9 in the nested `nil and typed-nil behaviors` group, 1 serializable case) | 1 | inline, in a `BeforeEach` |
| `engine/behavior_kind_test.go` | `TestNewEngineRejectsValueTypeKindInClusterMode` | 1: `New Engine Rejects Value Type Kind In Cluster Mode/holds` | 1 | inline |
| `engine/publisher_test.go` | `TestEventPublisherClusterHighPartitionCount` | 1: `a high partition count still delivers every entity event .../receives one event per entity, including shards beyond 271` | 1 | inline |
| `engine/engine_neutral_cluster_test.go` | `TestEngineMultiNodeNeutralBehaviors` | 7 | 2 | `newTestCluster` (in `engine_test.go`, another file) |
| `engine/engine_tenant_cluster_test.go` | `TestEngineRemoteSpawnTenantBinding` | 1 | 2 | inline |
| `compose/goakt/cluster_test.go` | `TestApp_TwoNodeClusterPlacesAndStopsCleanly` | 4 (the `New` group plus its 3 cases) | 2 | `newClusterNodes` and `startCluster` (same file) |

Nine tests in two packages, 31 subtests in all (1+1+1+14+1+1+7+1+4), counted as `go test -json` reports them: the table lists the names, and T5 compares the totals.

**Name clash found.** `engine/option_test.go` already has `TestClusterKindsExposesEgoActors`, a single-node test of `ClusterKinds()`. It would be picked up by `-run '^TestCluster'`. T2 renames it (it keeps its body).

**Where the unitgate rule is blind.** `TestEngineMultiNodeNeutralBehaviors` reaches the cluster through `newTestCluster`, which lives in another file of the package. A syntactic, per-file rule cannot follow that. The rule's doc says so (T4).

## Baseline (T1, before any change)

`go test -count=1 -json` per package, no `-race`, on `origin/develop` (`ee9af96`). Top-level tests have no `/` in the name, subtests do. Every test passed; no known flake failed, so no re-run was needed. The counting script is `/home/pablog/.claude/jobs/e070bb18/tmp/c206/count.py`, driven by `run.sh` in the same folder.

| package | top pass | top fail | top skip | sub pass | sub fail | sub skip | total |
|---|---|---|---|---|---|---|---|
| compose/goakt | 25 | 0 | 0 | 43 | 0 | 0 | 68 |
| engine | 205 | 0 | 0 | 434 | 0 | 0 | 639 |
| internal/engine/durablestate | 14 | 0 | 0 | 39 | 0 | 0 | 53 |
| internal/engine/enginetest | 7 | 0 | 0 | 19 | 0 | 0 | 26 |
| internal/engine/eventsource | 33 | 0 | 0 | 164 | 0 | 0 | 197 |
| internal/engine/projection | 2 | 0 | 0 | 11 | 0 | 0 | 13 |
| internal/engine/protocol | 14 | 0 | 0 | 22 | 0 | 0 | 36 |
| internal/engine/saga | 14 | 0 | 0 | 78 | 0 | 0 | 92 |
| internal/extensions | 15 | 0 | 0 | 22 | 0 | 0 | 37 |
| internal/projectionrunner | 18 | 0 | 0 | 66 | 0 | 0 | 84 |
| migration | 57 | 0 | 0 | 100 | 0 | 0 | 157 |
| **sum** | 404 | 0 | 0 | 998 | 0 | 0 | 1402 |

## Progress and evidence

- T1 done. Route: delegated writer (this session). Inventory and baseline above; the plan's "mixed top-level tests" case does not exist, so T2 and T3 shrink to renames (plus the one clashing name).
- T2 done. Eight cluster tests in `engine` renamed in place, plus `TestClusterKindsExposesEgoActors` renamed because it is single-node. Live docs that named them (`docs/engine.md`, `docs/testing/unit-migration.md`, the comment in `engine/cluster_kinds.go`) follow. No file was created or moved, so `.github/unit-test-gate-resources.txt` needs no change. Evidence: `go build ./...` and `go vet ./engine/` clean; `go test -count=1 -run '^(TestCluster|TestEngineClusterKinds)' ./engine`: 9 tests PASS (58.5 s); `go run ./.github/scripts/unitgate -strict`: ok (0 pending entries, 40 resource entries).
- T3 done. The inventory finds only one more package: `compose/goakt`, where `TestApp_TwoNodeClusterPlacesAndStopsCleanly` became `TestCluster_AppTwoNodePlacesAndStopsCleanly` (file `cluster_test.go` already holds only that test; `docs/testing/unit-migration.md` follows). Evidence: `go build ./...` and `go vet ./compose/...` clean; `go test -count=1 -run '^TestCluster' ./compose/goakt`: PASS; `go test -count=1 ./compose/...`: all ok; `unitgate -strict`: ok.

## Renames

| Old top-level name | New name |
|---|---|
| `TestEngineClusterMode` | `TestClusterEngineSingleNodeServesProjectionsAndEntities` |
| `TestEngineMultiNodeRemoteEntitySpawn` | `TestClusterEngineRemoteEntitySpawn` |
| `TestEngineClusterModeStartProjectionAlreadyExists` | `TestClusterEngineStartProjectionAlreadyExists` |
| `TestEngineRejectsUnplaceableBehaviorsInClusterMode` | `TestClusterEngineRejectsUnplaceableBehaviors` |
| `TestNewEngineRejectsValueTypeKindInClusterMode` | `TestClusterNewEngineRejectsValueTypeKind` |
| `TestEventPublisherClusterHighPartitionCount` | `TestClusterEventPublisherHighPartitionCount` |
| `TestEngineMultiNodeNeutralBehaviors` | `TestClusterEngineNeutralBehaviors` |
| `TestEngineRemoteSpawnTenantBinding` | `TestClusterEngineRemoteSpawnTenantBinding` |
| `TestClusterKindsExposesEgoActors` (single-node, name clash) | `TestEngineClusterKindsExposesEgoActors` |
| `TestApp_TwoNodeClusterPlacesAndStopsCleanly` (compose/goakt) | `TestCluster_AppTwoNodePlacesAndStopsCleanly` |

The tests stay in the files where they are: each one is already a whole cluster test, so moving it to a `*_cluster_test.go` file would only add churn. Describe and It texts are untouched, so subtest names do not change. A few of them break the user's naming rule (for example `Engine Multi Node Remote Entity Spawn` / `holds`); fixing them is outside this pure-rename spec and is left for a follow-up.

## Next step

T4.
