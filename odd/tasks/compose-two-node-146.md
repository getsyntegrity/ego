# Feature: two-node cluster integration test through `compose/goakt` (#146)

Branch: `test/146-compose-two-node` · Base: `origin/main` `e2d89ec` · Epic: #10 · Issue: #146 ·
Follow-up of #105 IMPL-4 (#145). Design: `openspec/changes/ego-arch-003/design.md` §5.1.

## Problem

`compose/goakt` checks its cluster preconditions at `New` (rule G1), but no test ever started a real
cluster through it. So the wiring `WithCluster(cfg, kinds ...ego.BehaviorKind)` performs — register
`ego.ClusterKinds()` on the GoAkt cluster config and the behavior kinds with `ego.WithBehaviorKinds` —
was never exercised end to end. Design §5.1 also left open whether `example/cluster`'s remote spawns
only worked because they happened to stay on the calling node.

## What changes

One new test file, `compose/goakt/cluster_test.go`, with no production change. The test
`TestApp_TwoNodeClusterPlacesAndStopsCleanly` builds two nodes with `egoakt.New`, each with
`WithCluster` and remoting through `WithActorSystemOptions(actor.WithRemote(...))`, its own testkit
stores, one publisher of each kind and a counting event stream. It starts both with `App.Start`
concurrently, waits until each node sees the other, then:

1. node A places a `ledger` (event-sourced, serializable) on node B and sends it a command;
2. node B places a `wallet` (durable state, serializable) on node A and sends it a command;
3. both nodes stop through `App.Stop`.

Node A registers only `wallet` and node B only `ledger`. So each receiving node can rebuild the
behavior only from its own `WithCluster` registration; the lazy `Inject` the calling node does at spawn
time never reaches the peer.

## How remote placement is made deterministic

The Apps use GoAkt's default `RoundRobin` placement. In goakt v4.5.4 `SpawnOn` picks
`members[(n-1) % len(members)]` (`actor/spawn.go`, `actorsRoundRobinPlacementPeer`), where `n` is a
cluster-wide counter every `SpawnOn` increments once (an olric `DMap.Incr`), and `members` is the
membership sorted by birth date (olric `GetMembers`), the same order on every call. With two members,
consecutive spawns therefore alternate between the nodes. The counter's value at the start of a
direction is unknown, so the helper `spawnOnPeer` first spawns "aligner" entities of the same type until
one lands on the calling node — at most two spawns; if two in a row both leave, the test fails because
the alternation no longer holds. The very next spawn, the subject, must then go to the peer, and the test
asserts it did: `ActorOf(id).IsLocal()` is true on the receiving node and false on the caller. The subject
is never retried, so a spawn that stays on the caller fails the test. Nothing else calls `SpawnOn` while
the test runs (ego only calls it from `spawnEventSourced`/`spawnDurableState`, and this Spec has no
projections).

Rejected alternative: spawn N entities and assert "at least one landed on the peer", as the root test
`TestEngineMultiNodeNeutralBehaviors` does. That is probabilistic in form and does not let a single named
spawn be asserted remote, which #146's first criterion requires. A role-based placement (`WithRole`) would
be exact, but ego's `SpawnOption` does not expose GoAkt roles, and changing that is production code, out of
scope.

## Constraints

- Test code, this document only. No change to `engine.go`, `option.go`, `spawn_config.go`, `port/`,
  `compose/goakt/app.go` or any other production file.
- `CHANGELOG.md` not touched: it records user-visible changes, and test-only additions have no entry of
  their own there.
- No `-race` locally, no workbench. `unset GOROOT`; lint with `GOROOT=/home/pablog/sdk/go1.26.6
  GOTOOLCHAIN=local` and `--modules-download-mode=mod` (the root `.golangci.yml` says `vendor`, as the
  CI's `verify-module.sh` also overrides).
- TDD: enabled (user configuration). This slice is test-only, so RED is shown by negative controls against
  the real test, not by a failing production change.

## Tasks

Route: direct inline (one new test file, already understood).

- [x] **T1** Write `compose/goakt/cluster_test.go` and prove it can fail (negative controls below), then
  pass. Commit: see PR.
- [x] **T2** Five consecutive runs, archcheck, vet, gofmt, golangci-lint on new code.
- [x] **T3** Answer design §5.1's `example/cluster` question from the test's evidence (below).

## Acceptance criteria (issue #146) and where each is proven

| Criterion | Assertion |
|---|---|
| Remote placement asserted unambiguously per direction; a spawn staying on the caller fails | `spawnOnPeer`: subject `IsLocal()` true on the receiver and false on the caller, subject never retried. Negative control N3. |
| The command succeeds in both directions, each with a different behavior type, and the reply reflects the state change | Subtests "A places a ledger on B" (`SpawnEventSourced`, balance 100, revision 1) and "B places a wallet on A" (`SpawnDurableState`, balance 250, revision 1); the receiving node's publisher gets the event / state, proving the entity ran there. Negative controls N1, N2. |
| Both nodes stop cleanly through `App.Stop` | Subtest "both nodes stop cleanly": `Stop` returns nil, `ActorSystem().Running()` false, engine not started, event stream closed (≥1 `Close`; the second is `stopActorSystem`'s documented no-op), each publisher closed exactly once, a second `Stop` is a no-op. |
| Five consecutive runs, no `-race` | `go test -count=5` below. |
| §5.1 answered from this test | Section below. |

## Verification evidence

Negative controls (throwaway edits of the test, reverted by copying the saved file back, never
committed; `cmp` confirmed the file restored):

- **N1** node B registers `wallet` instead of `ledger`: RED — `node-A: spawn "ledger-1": dependency type
  is not registered` (in this run the aligner landed on A, and the subject went to B, which cannot decode
  it). The failing spawn depends on where the round-robin counter starts: when the first aligner is
  placed on B, the run fails there instead, with `node-A: spawn aligner "ledger-1-aligner-0": dependency
  type is not registered`. Both messages are RED for the same cause.
- **N2** node A registers `ledger` instead of `wallet`: RED — `node-B: spawn aligner
  "wallet-1-aligner-0": dependency type is not registered`. As with N1, the failing spawn can instead be
  the subject (`node-B: spawn "wallet-1": ...`), depending on where the counter starts.
- **N3** A's spawns use `ego.WithPlacement(ego.Local)`: RED — `"ledger-1": local on node-B = false, local
  on node-A = true; want the spawn placed remotely on node-B, not kept on the calling node node-A`.

Real test GREEN. `go test -count=5 -run TestApp_TwoNodeClusterPlacesAndStopsCleanly -timeout 300s
./compose/goakt/`: 5/5 PASS, per run 0.81s, 0.85s, 0.85s, 0.85s, 0.85s (package 4.226s).

`go test ./compose/...` ok; `go vet ./compose/...` clean; `gofmt -l compose` empty;
`go run ./internal/cmd/archcheck`: 0 violations, 0 stale entries (unchanged);
`golangci-lint run --modules-download-mode=mod --new-from-rev=origin/main ./compose/...`: 0 issues.

## §5.1 answer: did `example/cluster`'s remote spawns only work because they stayed local?

Yes, for the version design §5.1 described. At that time `example/cluster` registered no behavior kind
(neither `WithEntityKinds` nor `WithBehaviorKinds`; `git log -S WithEntityKinds -- example/cluster/main.go`
finds nothing). Control N1 shows what happens to such a spawn when RoundRobin places it on a node that did
not register the type: GoAkt rejects it with "dependency type is not registered". The calling node's lazy
`Inject` registers the type only on the caller. So a spawn in that example could succeed only when it stayed
on the calling pod, or landed on a pod that had itself spawned an `AccountBehavior` earlier and so had lazily
registered it. (Inference, not tested here: the example's `entityWithRetry` retries up to five times, and each
retry advances the RoundRobin counter, so a retry could eventually land locally and hide the failure.)

Since #144 (`5a5621d`, #123 S3-5) `example/cluster` passes `ego.WithBehaviorKinds(new(AccountBehavior))`, so
this no longer holds. This test proves that the registration shape `compose/goakt.WithCluster` applies is
enough for real remote placement in both directions. `example/cluster` itself is not run here (it needs
Kubernetes and Postgres).

## Next step

Review of the PR; merge is the maintainer's decision.
