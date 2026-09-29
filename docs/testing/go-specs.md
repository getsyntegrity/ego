# Writing unit tests with go-specs

Epic #201 moves every Ego unit test to [go-specs](https://github.com/getsyntegrity/go-specs). This page is
the short convention for doing that, plus the evidence that the first migrated test (the pilot from #215)
behaves like the `t.Run` table it replaced. The checklist of tests still to migrate is
[`unit-migration.md`](unit-migration.md) (#202).

## Version

The root module pins `github.com/getsyntegrity/go-specs v0.3.1` in `go.mod`. Use that version; bump it in its
own PR. Nested modules that start using go-specs pin the same version. v0.3.1 is the hotfix release in which
an empty `Describe` name adds no subtest segment; the `AfterEach` ordering described below holds since v0.3.0.

## What a unit test may use

A unit test needs no real component or resource: no database, broker, socket, subprocess, actor system or
cluster (#201). Replace what the code depends on with something the test controls:

- **Fakes** first: in-memory implementations with real behavior. The `testkit` stores are fakes already.
- **Stubs** for fixed answers (return this value, fail with this error).
- **Mocks** (`testify/mock` or the generated `mocks/*` packages) only when the interaction itself is the
  contract — that a call happened, in an order, with these arguments. Never replace a result assertion with
  a call expectation: assert what the code produced, and add call expectations only on top of that.
- Fixed waits (`pause.For`, `time.Sleep`) are not a dependency to keep. Wait on an observable condition, or
  inject a clock or ticker the test drives.

Tests that need a real component stay as they are in this phase; `unit-migration.md` lists them as out of
phase.

## Shape of a spec

Each `Test` function keeps its name and holds one `specs.Describe`. Give the `Describe` a real description of
the behavior under test, and write one `It` per case:

```go
func TestPreconditionFromRevisionMapsPerD4(t *testing.T) {
	specs.Describe(t, "PreconditionFromRevision maps an expected revision to a write precondition", func(s *specs.Spec) {
		s.It("absent is unconditional", func(ctx *specs.Context) {
			ctx.Expect(PreconditionFromRevision(0, false)).ToEqual(persistence.Unconditional())
		})
		s.It("zero is genesis, not absence", func(ctx *specs.Context) {
			ctx.Expect(PreconditionFromRevision(0, true)).ToEqual(persistence.ExpectGenesis())
		})
	})
}
```

A table test can stay a table: loop over the cases and call `s.It(tc.name, ...)` inside the loop. Keep every
case and every invariant the old test asserted; a migration removes the old test only once the new one proves
the same cases.

Assertions:

- `ctx.Expect(got).ToEqual(want)` compares deeply (structs, slices, maps) and treats errors with `errors.Is`.
- `specs.ExpectT(ctx, got).ToEqual(want)` is the typed, allocation-free form for comparable values.
- Matchers through `To(...)`: `specs.Equal`, `specs.NotEqual`, `specs.BeTrue`, `specs.BeFalse`, `specs.BeNil`,
  `specs.Contain`, `specs.MatchError` (`errors.Is`), `specs.MatchErrorAs` (`errors.As`), composed with
  `specs.Not`, `specs.All`, `specs.Any`. There is no length matcher: use `ctx.Expect(len(xs)).ToEqual(3)`.
- Each `ctx.Expect(...)` carries one assertion; write a new one per check.
- A failed expectation stops its case, like testify's `require`: the rest of that `It` does not run, and the
  other cases still do. Migrating testify's `assert` (which keeps going) therefore reports only the first
  failure of a case; that is expected, not a lost check.
- Expectations take no custom message. When an old loop identified the failing input in its testify
  message (`"must parse: %s"`), make each input its own `It`, named by that input, so a failure still says
  which one broke. The case count goes up; say so in the PR.

Setup and teardown: `s.BeforeEach` / `s.AfterEach` per case. Nested `AfterEach` hooks run innermost scope
first, last registered first within a scope. `ctx.T` is the case's `*testing.T`, so `ctx.T.TempDir()`,
`ctx.T.Cleanup(...)` and `ctx.T.Setenv(...)` work as usual; an `AfterEach` runs before the case's
`ctx.T.Cleanup` functions.

Concurrency: never call `ctx.T.Parallel()`; declare `s.ItParallel(...)` instead. A goroutine that asserts must
be started with `ctx.Go(func(ctx *specs.Context) { ... })`, which the case waits for. Do not keep `ctx` after
the case ends.

## Subtest names and `go test -run`

go-specs makes every `Describe` name one segment of the subtest name, so a migrated case moves one level down:

| | Subtest name |
|---|---|
| Table test | `TestPreconditionFromRevisionMapsPerD4/zero_is_genesis,_not_absence` |
| go-specs | `TestPreconditionFromRevisionMapsPerD4/PreconditionFromRevision_maps_an_expected_revision_to_a_write_precondition/zero_is_genesis,_not_absence` |

Every case stays selectable; a selector needs the `Describe` segment, for example
`-run '^TestPreconditionFromRevisionMapsPerD4$/^PreconditionFromRevision_maps/^zero_is_genesis'`. List the old
and new names of the cases a migration PR touches in its description. The top-level `Test` name never changes,
so CI's per-test sharding (`.github/scripts/test-matrix.sh`) is unaffected.

## Pilot validation (#203)

Recorded on `develop` `0de4249` for `internal/engine/protocol.TestPreconditionFromRevisionMapsPerD4`, go1.26.6:

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

The failure and cleanup rows were checked with a throwaway spec that was not committed.
