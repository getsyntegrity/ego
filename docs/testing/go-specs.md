# Writing unit tests with go-specs

Epic #201 moves every Ego unit test to [go-specs](https://github.com/getsyntegrity/go-specs). This page is
the short convention for doing that, plus the evidence that the first migrated test (the pilot from #215)
behaves like the `t.Run` table it replaced. The checklist of tests still to migrate is
[`unit-migration.md`](unit-migration.md) (#202).

## Version

The root module pins `github.com/getsyntegrity/go-specs v0.3.3` in `go.mod` (#242). Use that version; bump it
in its own PR. Nested modules that start using go-specs pin the same version.

v0.3.2 added most of what this page relies on: `specs.Table`, the `mock` package, `Eventually` and
`Consistently` with a manual clock, `Project`, the length, map, string, ordering and collection matchers,
`MatchJSON`, and structural diffs in `Equal` failures. v0.3.3 only publishes the v0.3.2 changelog. An empty
`Describe` name adds no subtest segment since v0.3.1, and the `AfterEach` ordering described below holds
since v0.3.0.

## No external resources in a unit test

This is the one hard rule: **a unit test never reaches anything outside the process.** That means no
database, broker, HTTP or gRPC API, socket, subprocess, file outside `ctx.T.TempDir()`, actor system or
cluster (#201). It also rules out real time used as synchronization: no `time.Sleep`, no `pause.For`.

When the code under test depends on one of those, replace the dependency with a go-specs mock (next section).
A test that genuinely needs the real resource is not a unit test. It belongs to the component or integration
lane, and `unit-migration.md` lists it as out of phase. Tests that need a real database or broker go in the
`inttest` module, which starts its own containers and never skips; see
[Integration tests](../ci.md#integration-tests).

In-memory implementations that already live in the repository, such as the `testkit` stores, are not
external resources. A test may use one when what it checks is the behavior on top of the store and not the
calls made to it.

## Mocks: `mock.Controller`

Mocks use go-specs' `mock` package. `testify/mock` and the generated mockery `mocks/*` packages no longer exist in the
repository, and the gate rejects both. They were dropped because they verify only when the test remembers to call `AssertExpectations`, and their
failures do not go through the spec. A `mock.Controller` is bound to the case. It checks every expectation
when the case ends, including after a failed assertion or a panic, and it reports an unexpected call
immediately, naming the method, its arguments and the line where the expectation was declared.

A mock is two parts. The first is a small typed adapter that implements the port and forwards each method to
the controller. Write it once per port, next to the tests that use it. The second is the expectations, which
each case declares:

```go
type eventStoreMock struct{ c *mock.Controller }

func (m eventStoreMock) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event,
	precondition persistence.WritePrecondition) error {
	return m.c.Method("WriteEvents").Call(ctx, scope, events, precondition).Err(0)
}

func (m eventStoreMock) GetLatestEvent(ctx context.Context, scope persistence.Scope, id string) (*egopb.Event, error) {
	r := m.c.Method("GetLatestEvent").Call(ctx, scope, id)
	return mock.Value[*egopb.Event](r, 0), r.Err(1)
}

s.It("reports the store failure to the caller", func(ctx *specs.Context) {
	ctrl := mock.NewController(ctx) // expectations are verified when the case ends
	store := eventStoreMock{ctrl}
	ctrl.Method("WriteEvents").
		Expect(mock.Any(), mock.Any(), mock.Any(), persistence.ExpectRevision(3)).
		Return(errStoreDown)

	err := persist(context.Background(), store, events)

	ctx.Expect(err).To(specs.MatchError(errStoreDown))
})
```

The adapter follows `persistence.EventsStore`, and it only needs the methods the code under test calls.
`persist`, `events` and `errStoreDown` stand for the code and data of the test. The API is:

- `Expect(args...)` declares one call shape. Its arguments are plain values, `mock.Any()`, `mock.Equal(v)`,
  `mock.MatchT("description", func(T) bool)` or a `mock.Captor`. By default the call happens exactly once;
  use `Times(n)`, `AtLeast(n)`, `AtMost(n)`, `AnyTimes()` or `Never()` to change that.
- `Return(values...)` answers the call. Several `Return`s answer calls in sequence, and the last one repeats.
  Use `Do(func(args []any) []any)` when the answer depends on the arguments. Copy any argument you keep,
  because the recorded arguments are shallow copies.
- `ctrl.InOrder(exps...)` requires calls in that order, across methods.
- `mock.NewSpy()` records calls when the test only needs to check that they happened.

Assert what the code produced first. Add call expectations only when the interaction itself is the contract:
that a call happened, in an order, or with these arguments. A call expectation never replaces a result
assertion.

## Time: `Eventually`, `Consistently` and a manual clock

A test that waits for something to happen polls for an observable condition instead of sleeping:

```go
ctx.Eventually(func() any { return runner.Offset() }, specs.Equal(int64(3)),
	specs.WithTimeout(time.Second), specs.WithInterval(10*time.Millisecond))
```

The callback runs on the calling goroutine and is never interrupted. The case fails only on the final
verdict, and the message includes the attempts, the elapsed time and the last value seen. `ctx.Consistently`
is the opposite check: the condition must keep holding for the whole interval.

When the code under test accepts a clock, pass it `specs.NewManualClock()` and advance the clock from the
test with `clock.Advance(d)`. Give the same clock to the poll with `specs.WithClock(clock)`. The test is then
deterministic and runs without real delays.

## Shape of a spec

Each `Test` function keeps its name and holds one `specs.Describe`. Give the `Describe` a real description of
the behavior under test.

When the cases differ only by their input and the expected result, register them with `specs.Table`. Each row
becomes an ordinary `It`, with its own subtest name, hooks and failure, so the subtest names do not change
compared with writing one `It` per case. This is the pilot, `internal/engine/protocol/expected_revision_test.go`:

```go
type revisionCase struct {
	name        string
	revision    uint64
	hasRevision bool
	want        persistence.WritePrecondition
}

func TestPreconditionFromRevisionMapsPerD4(t *testing.T) {
	specs.Describe(t, "PreconditionFromRevision maps an expected revision to a write precondition", func(s *specs.Spec) {
		specs.Table(s, []revisionCase{
			{name: "absent is unconditional", revision: 0, hasRevision: false, want: persistence.Unconditional()},
			{name: "zero is genesis, not absence", revision: 0, hasRevision: true, want: persistence.ExpectGenesis()},
			{name: "positive revision is an exact expectation", revision: 42, hasRevision: true, want: persistence.ExpectRevision(42)},
		}, func(c revisionCase) string { return c.name }, func(ctx *specs.Context, c revisionCase) {
			ctx.Expect(PreconditionFromRevision(c.revision, c.hasRevision)).ToEqual(c.want)
		})
	})
}
```

`Table` panics at registration on an empty or duplicate row name, so a copy-pasted row cannot hide a case.
Use `specs.TableParallel` only when the rows share no state. Cases that need their own setup or several steps
stay as separate `s.It` blocks. Keep every case and every invariant the old test asserted: a migration removes
the old test only once the new one proves the same cases.

## Assertions

- `ctx.Expect(got).ToEqual(want)` compares deeply (structs, slices, maps) and treats errors with `errors.Is`.
  A failure lists each difference by path (`Items[2].Price: expected 9.99, actual 12.5`), so an assertion on
  a whole struct is readable when it fails. Prefer it to comparing field by field.
- `specs.ExpectT(ctx, got).ToEqual(want)` is the typed, allocation-free form for comparable values.
- Use a matcher through `To(...)` whenever one says what you mean; `ctx.Expect(len(xs)).ToEqual(3)` hides the
  collection when it fails. The matchers are:
  - General: `Equal`, `NotEqual`, `BeTrue`, `BeFalse`, `BeNil`, `BeZero`, `BeOneOf`, `Satisfy`.
  - Errors: `MatchError` (`errors.Is`) and `MatchErrorAs` (`errors.As`).
  - Length: `HaveLen(n)` and `BeEmpty()`.
  - Collections: `Contain`, `ContainAllOf`, `ContainAnyOf`, `ContainTheSameElementsAs` (order-insensitive),
    `EveryElement(m)`, `AnyElement(m)`, `NoElement(m)`, `ExactlyNElements(n, m)`, `HaveElementsInOrder(ms...)`
    and `ContainElementsInOrder(ms...)`.
  - Maps: `HaveKey`, `HaveValue` and `HavePair`.
  - Strings and `[]byte`: `StartWith`, `EndWith` and `MatchRegex`.
  - Numbers, including `time.Duration`: `BeGreaterThan`, `BeGreaterThanOrEqual`, `BeLessThan`,
    `BeLessThanOrEqual`, `BeBetween` and `BeCloseTo`.
  - JSON: `MatchJSON(want)`, which ignores key order and whitespace.
  - Composition: `Not`, `All` and `Any`.
- `specs.Project("Status", func(e *Event) string { return e.Status }, specs.Equal("paid"))` checks one field or
  derived value, and its failure starts with the field name (`Status: expected open to equal paid`). Use it
  instead of `Satisfy` when a predicate would only compare a field.
- Each `ctx.Expect(...)` carries one assertion; write a new one per check.
- A failed expectation stops its case, like testify's `require`: the rest of that `It` does not run, and the
  other cases still do. Migrating testify's `assert` (which keeps going) therefore reports only the first
  failure of a case; that is expected, not a lost check.
- Expectations take no custom message. When an old loop identified the failing input in its testify message
  (`"must parse: %s"`), make each input a `Table` row named by that input, so a failure still says which one
  broke. The case count goes up; say so in the PR. When the inputs cannot become cases (a thousand random
  values), assert on the whole collection with a quantified matcher, for example
  `ctx.Expect(ids).To(specs.EveryElement(specs.MatchRegex(uuidPattern)))`. Its failure names the offending
  elements by index.

## Fuzzing an invariant

When a function has a rule that must hold for every input, back the table with a native fuzz target
(`testing.F`), as the pilot's `FuzzPreconditionFromRevision` does. Check it with a go-specs matcher so the
failure reads like the rest of the suite:

```go
f.Fuzz(func(t *testing.T, revision uint64, hasRevision bool) {
	want := persistence.ExpectRevision(revision) // the D4 rule, restated
	switch {
	case !hasRevision:
		want = persistence.Unconditional()
	case revision == 0:
		want = persistence.ExpectGenesis()
	}
	got := PreconditionFromRevision(revision, hasRevision)
	if m := assert.Equal(want); !m.Match(got) {
		t.Fatal(m.FailureMessage(got))
	}
})
```

`go test ./...` replays only the seeds (`f.Add`) and any committed `testdata/fuzz` corpus, so the unit lane
stays fast. Run a campaign on demand with `go test -run '^$' -fuzz FuzzName -fuzztime 30s ./pkg/`, and commit
the input file of any failure it finds as a regression seed. A fuzz target is subject to the same rule as any
unit test: no external resources. `github.com/getsyntegrity/go-specs/property`, which adds shrinking, is a
separate module that brings in `pgregory.net/rapid`. It is not adopted yet, and adding it is its own decision.

## Setup, teardown and concurrency

Setup and teardown: `s.BeforeEach` / `s.AfterEach` per case, declared before the first `It` of their scope.
Nested `AfterEach` hooks run innermost scope first, last registered first within a scope. `ctx.T` is the
case's `*testing.T`, so `ctx.T.TempDir()` and `ctx.T.Setenv(...)` work as usual. Register cleanup with
`ctx.Cleanup(fn)`. It runs after the case's `AfterEach` hooks and after every `ctx.Go` task has finished, on
every engine.

Concurrency: never call `ctx.T.Parallel()`; declare `s.ItParallel(...)` or `specs.TableParallel` instead. A
goroutine that asserts must be started with `ctx.Go(func(ctx *specs.Context) { ... })`, which the case waits
for. Do not keep `ctx` after the case ends.

## Writing a cluster test

A test that starts a clustered actor system (`goakt.WithCluster`, with one node or several) opens gossip, peer
and remoting ports on loopback, so it runs in its own CI lane (the `cluster` job, on every pull request with Go changes) and not in the unit
shards. The lane is chosen by name and covers the whole root module, so a new cluster test needs no tag and no CI
change:

1. Name the top-level test `TestCluster<Something>`, for example `TestClusterEngineRemoteEntitySpawn`. Keep it in
   the same package as the code it tests, so a white-box test stays white-box. A single-node test of a cluster
   helper is not a cluster test and must not use the prefix.
2. If you create a new file for it, name it `*_cluster_test.go`. An existing file is fine when the test belongs
   with its neighbors, and that is where the current ones live. Move a cluster case out of a mostly single-node
   top-level test into its own `TestCluster*` function, because the lane selects whole top-level tests.
3. Get free ports from `dynaport` (`github.com/travisjeffery/go-dynaport`) and never hard-code one. Reuse the
   helpers of the package: `newTestCluster` in `engine/engine_test.go` and `mockClusterProvider` in
   `engine/helper_test.go`, `newClusterNodes` and `startCluster` in `compose/goakt/cluster_test.go`.
4. Wait with `Eventually` (a cluster forms and rebalances asynchronously). Never use `time.Sleep`.
5. Run it with `go test -run '^TestCluster' ./engine/ ./compose/goakt/...`. It does not run in the unit shards
   (`go test -skip '^TestCluster' ./...`), and it is not run under `-race` yet.

The `unit-gate` rule `cluster-name` fails when a test file starts a cluster (`WithCluster`, `dynaport`) from a
top-level test whose name does not start with `TestCluster`. See [Test lanes](../ci.md#test-lanes) for what each
lane runs and when.

## Subtest names and `go test -run`

go-specs makes every `Describe` name one segment of the subtest name, so a migrated case moves one level down:

| | Subtest name |
|---|---|
| Table test | `TestPreconditionFromRevisionMapsPerD4/zero_is_genesis,_not_absence` |
| go-specs | `TestPreconditionFromRevisionMapsPerD4/PreconditionFromRevision_maps_an_expected_revision_to_a_write_precondition/zero_is_genesis,_not_absence` |

Every case stays selectable; a selector needs the `Describe` segment, for example
`-run '^TestPreconditionFromRevisionMapsPerD4$/^PreconditionFromRevision_maps/^zero_is_genesis'`. `specs.Table`
rows follow the same rule. List the old and new names of the cases a migration PR touches in its description.
The top-level `Test` name never changes, so CI's per-test sharding (`.github/scripts/test-matrix.sh`) is
unaffected.

## The unit-test gate

CI runs `go run ./.github/scripts/unitgate` in the `unit-gate` job (#204). It reads every `.go` file of every
module, nested ones included, and fails on:

1. an import of `github.com/stretchr/testify/...`, in a test or a non-test file;
2. an import of the generated `github.com/getsyntegrity/ego/mocks/...` packages from outside `mocks/`;
3. a `*_test.go` file that declares `func TestXxx(t *testing.T)` and never calls `specs.Describe`. Fuzz
   targets, benchmarks and `TestMain` do not count as tests here;
4. a test file that calls something outside the process: `sql.Open`, `net.Dial*`, `net.Listen*`,
   `exec.Command*`, `httptest.NewServer*`, a real goakt `actor.NewActorSystem`, `os.Create` in a file with no
   `TempDir`, or `os.Getenv("...DSN...")`. The check is static and per file. Files under `inttest/` are
   outside this rule, because that module exists to run against real infrastructure started with
   Testcontainers; rules 1 to 3 still apply there. The module has two kinds of packages and no others:
   `inttest/infra/<backend>` starts a container (today `infra/postgres`; `infra/kafka`, `infra/nats` and
   `infra/pulsar` will sit beside it), and `inttest/flows/<area>` checks a behavior against it (today
   `flows/eventstore`);
5. under `inttest/` only, any call to `Skip`, `Skipf` or `SkipNow` on any receiver, to the go-specs
   `SkipIt`, `PendingIt` or `FIt` (on a `Spec` or a `Builder`; `FIt` focuses one case and so skips all the
   others), or to `testing.Short`, in a test or a non-test file. A test there must fail when its dependency is
   missing, so it can never look green without running. No allowlist can excuse it. See [Integration tests](../ci.md#integration-tests).

Two plain-text lists hold the exceptions, one `path | note` per line, and the note is required:

- `.github/unit-test-gate-resources.txt` is permanent. It lists the test files that are legitimately outside
  the unit lane (architecture tests that run `go list`, the loopback websocket server, the real actor-system
  tests), each with its reason. Add a line only with a reason a reviewer
  can argue with.
- `.github/unit-test-gate-pending.txt` was the temporary list used while the migration PRs were in flight. It is
  now empty and only its header stays, because the gate reads the file. CI runs the gate with `-strict`, so a
  stale line in either list is an error, and a new violation always fails: there is no way left to park a
  testify import or a test file without `specs.Describe`.

The gate's own tests are in `.github/scripts/unitgate` and use an in-memory file tree, so they touch no disk.
Run it locally with `go test ./.github/scripts/unitgate && go run ./.github/scripts/unitgate` (CI adds
`-strict`, which turns stale list entries into errors).

## Pilot validation (#203)

Recorded on `develop` `0de4249` for `internal/engine/protocol.TestPreconditionFromRevisionMapsPerD4`, go1.26.6,
while the pilot still used one `s.It` per case on v0.3.1. #242 moved it to `specs.Table` without changing the
names of those three cases.

| Check | Result |
|---|---|
| Names (`go test -v`) | the three cases run as `…MapsPerD4/<Describe>/<case>` |
| `-run` one case | runs only that case |
| `-run` with no match | `no tests to run`; nothing executes |
| `go test -json` | one `pass` event per case plus the parent, with the full subtest name |
| Failure report | a failing `ctx.Expect(...).ToEqual(...)` reports `expected genesis to equal unconditional` at the test's own `file:line`; the failing case is `FAIL`, its siblings still run |
| Cleanup | `AfterEach` and `ctx.T.Cleanup` both run when a case fails, `AfterEach` first |
| Coverage | `internal/engine/protocol` 51.4% of statements, the same as the original table test on `main` `c41d025` |
| Race detector | not applicable: the current CI (`ci.yml`) does not run `-race`, and it is not run locally |

The failure and cleanup rows were checked with a throwaway spec that was not committed. In #242 a deliberate
mutation (`revision == 0` changed to `revision <= 1`) passed the table and was caught by
`FuzzPreconditionFromRevision` in 0.19 s, which is why the pilot keeps both.
