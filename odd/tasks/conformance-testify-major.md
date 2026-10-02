# Remove testify from persistence/conformance (breaking change)

Origin: #205. Follow-up of the stopped analysis `testkit-conformance-testify` (kept local only).

## Problem

`persistence/conformance` is a public package that adapter authors import in their own tests. It
imported `github.com/stretchr/testify/require`, so testify landed in every adapter's dependency graph.
Worse, testify leaked into the exported API: the field `Check[S].Run` had the type
`func(ctx context.Context, t require.TestingT, store S)`, and `EventsStoreChecks`, `StateStoreChecks`
and `SnapshotStoreChecks` inherit it. The earlier analysis showed `apidiff` flags even an identical method
set as incompatible, because `require.TestingT` is a named type. So removing testify is a deliberate
breaking change. The owner decided to accept it: nothing in the repo may keep testify.

## What changes

- `persistence/conformance/report.go` (new) defines the exported interface `TestingT` with `Errorf`,
  `FailNow` and `Helper`. That is the smallest set that `*testing.T` and the package's own recorder
  `captureTestingT` both satisfy. It also holds the stdlib-only assertions the checks use
  (`requireNoError`, `requireEqual`, ...). They report through `t.Errorf` and stop with `t.FailNow`, so
  failures stay fatal exactly as `require` did.
- `check.go`, `events.go`, `state.go`, `snapshot.go` and `helpers.go` use `TestingT` and the new helpers.
  Check names, check bodies and custom messages are unchanged.
- Failure text becomes `<rule>: <expected/actual>`, for example
  `tenant B must not see tenant A's record: expected nil, got: ...`. Testify's `Error Trace` blocks are gone.
  This is visible in `CheckResult.Errors`.
- `CHANGELOG.md` gets a Breaking changes entry with the old and new signature; `doc.go` gets a short
  "Writing a Check" section.

## What does not change, and why

- **Other testify users** (`testkit/scenario.go`, `internal/engine/enginetest`, generated `mocks/*`, and many
  test files) are out of scope for this spec: they have no exported-signature problem and are separate work.
- **go-specs stays out of non-test code.** Only the new `report_test.go` uses it.
- **Alias to `require.TestingT`** was rejected: it would keep testify in the dependency graph.

## Constraints

Strict TDD, test runner `go test` (no `-race`, no workbench). Existing test names stay. Delivery is a PR to
`develop`; `api` only warns on develop and blocks on main without `release:major`.

## Tasks

- [x] T1 RED then GREEN: reporting helpers and `TestingT` (`report_test.go`, `report.go`). Route: inline.
  Evidence: RED = `go vet` failed with `undefined: TestingT`; GREEN = tests pass; mutation (drop
  `t.FailNow()` in `fail`) makes 23 cases fail, then reverted.
- [x] T2 Replace every `require.*` in the five files; update `testkit/conformance_test.go` comment.
  Route: inline (mechanical). Evidence: `go build` in root and all nested modules, testkit conformance
  tests green.
- [x] T3 Release note, CHANGELOG entry, package doc, `kind/breaking` label. Evidence: PR body.
- Commit: see git log on this branch.

## Checks

`go build`, `go vet`, `golangci-lint run ./persistence/... ./testkit/...` (0 issues), `gofmt -l` clean,
`go test -count=5` green. Coverage of `persistence/conformance` (merged unit + testkit): 99.2% before,
about 99.8% after. `apidiff` reports exactly one incompatible change, `Check.Run`, plus compatible
`TestingT: added`.

## Follow-up (named)

`drop-testify-testkit-scenario-and-enginetest`: `testkit/scenario.go` and `internal/engine/enginetest`.
`drop-testify-tests-and-mocks`: the remaining test files and generated mocks.
