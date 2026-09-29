# Test lanes

Ego splits its tests into five lanes so that a feature or hotfix pull request runs only fast, hermetic suites, while tests that need real infrastructure run in their own workflows. This document is the contract: it defines each lane by what a test does, never by its name or its package. The inventory in [`inventory.md`](inventory.md) applies this contract to every test, and the freshness command described at the end keeps it honest. The plan behind it is epic [#201](https://github.com/getsyntegrity/ego/issues/201); this contract is issue #202.

## The rule in one paragraph

A test belongs to the lane its strongest resource signal points at. A test that needs infrastructure outside the test process (a database, a message broker, a multi-node cluster, another program) is `integration`. A test that reads source code or runs the Go toolchain to check project boundaries is `architecture`. A test that lives in a module whose purpose is to be an example is `example`. A test that runs real GoAkt actors or a loopback socket owned by the test process is `component`. Everything else is `unit`. A test called `TestIntegration...` or `TestE2E...` that uses none of those signals is not an integration test, whatever its name says.

## The lanes

| Lane | What the test may touch | Where it runs |
|---|---|---|
| `unit` | Injected fakes, stubs and in-memory data. Private temporary files (`t.TempDir`). Timers. Nothing real. | Every pull request, always for affected packages. |
| `component` | Real GoAkt actors and actor systems inside the test process, stores in memory, and loopback sockets that the test itself opens (for example `httptest.NewServer`). | Every pull request for affected packages. It is a merge gate, not an optional lane. |
| `integration` | PostgreSQL, Kafka, NATS, Pulsar, a GoAkt cluster (gossip and peer sockets), or any external program. | Its own workflow (#210 and the issues it points to) and the full run on `main`. |
| `architecture` | `go list`, `go build` or other subprocesses, and parsing or walking the source tree to verify boundaries. | Its own explicit lane (#208). |
| `example` | Tests of programs under `example/` and `benchmark/`. | A separate workflow (#214). A pull request that changes an example must at least compile it. |

The unit and component lanes stay in the pull request lane. The other three leave it, and `inventory.md` lists each test that leaves with the issue that will run it again.

## Signals

The generator (`internal/tools/testinventory`) reads the body of every `Test` function and records these signals. Each one is a concrete call, not a naming convention.

| Signal | Detected from | Effect |
|---|---|---|
| `db.sql` | `database/sql` `Open` or `OpenDB` | integration, destination #211 |
| `db.postgres` | any call into a `pgx` or `lib/pq` package | integration, destination #211 |
| `env.external-endpoint` | a test that reads an environment variable named like `*DSN*`, `*URL*`, `*ENDPOINT*`, `*BROKER*` or `*ADDR*` and can `t.Skip` because of it | integration |
| `broker.kafka`, `broker.nats`, `broker.pulsar` | a constructor or connect call (`New...`, `Connect`, `Dial`, `Open`) from a Kafka, NATS or Pulsar client package. Using only the client types does not count | integration, destination #213 |
| `cluster` | GoAkt `NewClusterConfig` or `WithCluster` | integration, destination #212. Even a one-node cluster opens gossip and peer sockets, so it counts |
| `process.exec` | `os/exec` `Command` or `CommandContext` that is not a Go toolchain call | integration |
| `process.go-toolchain` | the same call when an argument is `go`, `list`, `build`, `vet`, `env` or `test` | architecture, destination #208 |
| `source.inspect` | use of `go/parser`, `go/build`, `go/types` or `golang.org/x/tools/go/packages` | architecture, destination #208 |
| `actor.system` | GoAkt `NewActorSystem` or the GoAkt `testkit` constructor | component |
| `net.listen`, `net.dial` | `net.Listen*`, `net.Dial*`, `http.ListenAndServe*` | component (loopback assumed, see below) |
| `http.test-server` | `httptest.NewServer`, `NewTLSServer`, `NewUnstartedServer` | component |
| `net.port-alloc` | `dynaport.Get` | evidence only |
| `skip.env` | an environment variable read plus a `t.Skip` in the same test | evidence only, shown in the skip list |
| `wait.sleep`, `wait.pause` | `time.Sleep`, `pause.For`; the duration is evaluated when it is a constant | evidence only. Their sum is the test's `fixed_wait_ms` |
| `fs.tempdir`, `fs.io` | `t.TempDir`, `os.MkdirTemp`, `os.CreateTemp`; other `os` file calls | evidence only |
| `concurrency.parallel` | `t.Parallel()` | evidence only |
| `lifecycle.start` | a `Start`, `Spawn`, `SpawnOn`, `SpawnNamed` or `SpawnSingleton` call on any value | evidence only. It nominates a unit test for the review queue in `inventory.md`, because the scanner cannot tell a fake from a real actor system; a person confirms it with an override |

The example lane is the one place where location matters: modules listed under `example_modules` in [`inventory-overrides.json`](inventory-overrides.json) put their tests in `example`, unless a stronger signal makes them `integration` first. This is deliberate: the resource an example test touches is not the reason it leaves the pull request lane, its role is. The PostgreSQL tests of `example/cluster` therefore stay `integration`, not `example`.

Order of precedence: integration, architecture, example, component, unit.

## Two rules that could have gone either way

**Loopback sockets are component, not integration.** `httptest.NewServer` and a `net.Listen` on `127.0.0.1` use a real socket, so they cannot be `unit`. But the socket belongs to the test process, needs no other machine and no setup, and is as fast as the actors. Banning it would push tests such as the WebSocket publisher server test into a slow workflow for no isolation benefit. The rejected alternative was to call any socket integration. The limit of this rule is that the generator cannot tell a loopback address from a remote one; a test that dials a non-loopback host must get an override.

**Temporary files are unit-compatible.** `t.TempDir` is private to one test, removed automatically and shared with nobody. It is recorded as evidence and never moves a test out of `unit`. Reading the repository's own source files is different: it is an architecture concern, and is caught by `source.inspect` or an override.

## Limits of static detection

- Signals come from the `Test` function body and from local helper functions it calls, resolved one level deep. A helper defined in a `_test.go` file of the same package is scanned. A helper of a helper is not. Methods are not resolved.
- Subtests do not exist until run time, so signals belong to the top-level test. Subtests are listed from a real `go test -json` run, with their status and skip reason.
- Waits are counted once per occurrence. A sleep inside a loop is not multiplied, so `fixed_wait_ms` is a lower bound.
- Whatever static analysis cannot see is handled by an override in [`inventory-overrides.json`](inventory-overrides.json). Every override names an existing test and a written reason.

## Mixed files

Many files hold tests of different lanes. The generator classifies each `Test` function on its own and marks a file as mixed when its tests span more than one lane. Mixed files are listed in `inventory.md`. Splitting them is the job of the migration issues (#205, #206, #208, #211 to #213); this contract only makes them visible.

## Keeping the inventory fresh

```
go run ./internal/tools/testinventory -check     # static and fast: fails on drift
go run ./internal/tools/testinventory -update    # regenerate the static part, keep recorded run data
go run ./internal/tools/testinventory -run       # also run go test -json in every module (several minutes)
```

`-check` re-scans every module without running tests. It fails when a `Test` function is missing from `inventory.json`, when an entry no longer exists in the source, when a test's lane changed, when an entry has no valid lane, when an override points at nothing, or when `inventory.md` is stale. Wiring it into CI belongs to issue #209.
