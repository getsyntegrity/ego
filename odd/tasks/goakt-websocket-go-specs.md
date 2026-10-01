# goakt and websocket tests on go-specs (#205)

## Problem

Three top-level tests on `develop` still do not use go-specs, which the epic (#205) requires for every test:

- `compose/goakt/cluster_test.go` (`TestApp_TwoNodeClusterPlacesAndStopsCleanly`) used `t.Run`, `t.Fatalf`, a
  hand-written `time.Sleep` poll loop and `select` with `time.After`.
- `compose/goakt/runtime_e2e_test.go` (`TestRuntime_ConsumerDrivesTheAppEndToEnd`) used `t.Fatalf` checks.
- `publisher/websocket/close_test.go` (`TestCloseIsIdempotent`, nested module with its own `go.mod`) looped over a
  map of closers and used `t.Errorf`.

## What changes

Only `_test.go` files. Each test keeps its top-level name and now holds one `specs.Describe`.

- The cluster test builds and starts the two nodes once in `s.BeforeAll` and stops them in `s.AfterAll` (Stop is
  idempotent). The three cases keep their names and order. The sleep poll that waited for the cluster to form
  became `ctx.Eventually` (30 s timeout, 100 ms interval, `BeEmpty` on the list of nodes that still lack peers).
  The two `select`/`time.After` waits for a published event or state became a small `receive` helper built on
  `ctx.Eventually`. The helpers `newClusterNodes`, `startCluster`, `spawnOnPeer` and `hosts` now take
  `*specs.Context` and report through `ctx.Expect`, so no `t.Fatalf` is left in them.
- The runtime end-to-end test is one `It`. Teardown uses `ctx.Cleanup`; the explicit `Stop` stays and is checked.
- The Close test is a `specs.Table` with an events row and a state row.
- `publisher/websocket/server_test.go`: `defer conn.Close()` became `defer func() { _ = conn.Close() }()` because
  `golangci-lint` in the nested module reported errcheck on it.

## What does not change, and why

- **Lanes and behavior.** The cluster still runs a real two-node GoAkt cluster on loopback and the e2e test a real
  actor system; the spec says to keep them where they are. None of the three tests had a build tag, env skip or
  `testing.Short` gate, so there is no gate to keep.
- **`connected` and `mustNew` in `compose/goakt`** still take `*testing.T` (the specs pass `ctx.T`). They are shared
  with many tests that are not specs yet (`app_test.go`), and `connected`'s `t.Fatalf` only fires when an in-memory
  testkit store fails to connect.
- **`newTestServer(t)` and `Stall(*testing.T)`** keep their signatures: `Stall` is part of the shape the
  `adaptertest` harness takes (`*testing.T`), and neither has a `t.Fatalf`.
- **`publisher_contract_test.go`** has only compile-time interface assertions.
- The cluster case is one spec group with shared state, so the old sequential `t.Run` behavior is preserved
  instead of building three clusters.

## Constraints

Strict TDD; runner is `go test` (root module and, inside `publisher/websocket`, its own module). No `-race`, no
workbench. Release note: NONE. Each cluster run takes about 1 s, so `-count=5` was used.

## Tasks

- [x] T1 Cluster test on go-specs. Route: inline (one file). Evidence: commit "test(goakt): move the two-node
      cluster test to go-specs". RED: `toSpawnPlacement` returning `goakt.Local` made both placement cases fail
      with `expected true, got false`; `engine.go` skipping `publisher.Close` made the stop case fail with
      `expected 0 to equal 1`. Both reverted.
- [x] T2 Runtime e2e test on go-specs. Route: inline. Evidence: its commit. RED: `runtimeconsumer.Run` crediting
      `Credit + 1` failed with `expected 151 to equal 150`. Reverted.
- [x] T3 Close test on go-specs plus the errcheck fix. Route: inline. Evidence: its commit. RED: the second-call
      branch of `Close` returning `context.Canceled` failed both rows with `expected nil, got context canceled`.
      Reverted.

## Follow-up

None.

## Progress

All tasks done. `go build ./...`, `go vet`, `golangci-lint run` on both modules and `gofmt -l` are clean.
`go test -count=5` passes for `./compose/goakt/` (4.3 s) and `publisher/websocket`. Coverage is unchanged:
`compose/goakt` 87.1% and `publisher/websocket` 87.9%, before and after.
