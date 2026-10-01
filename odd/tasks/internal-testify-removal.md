# Remove testify from the internal/ tests (#235, #236)

## Problem

The rule for Ego unit tests is absolute: every test uses go-specs `specs.Describe`/`It`, go-specs mocks and
the v0.3.3 matchers, and nothing imports testify. Three files under `internal/` already used `Describe` for
their behavior tests but still imported `github.com/stretchr/testify/assert` and `require`:

- `internal/logging/logging_test.go`
- `internal/instrumentation/instrumentation_test.go` (#235 left testify in its architecture test on purpose)
- `internal/projectionrunner/runner_test.go`

In all three the only testify use was one plain-`t` test that runs `go list -deps` and checks that the
package does not reach GoAkt, the engine or the GoAkt adapter internals. The wait-heavy parts of
`runner_test.go` were already moved to `ctx.Eventually` and a manual clock by #236
(see `projectionrunner-clock-seam.md` and `projectionrunner-go-specs-v033.md`), so no further change was
needed there.

## What changes

Each of the three guards becomes a `specs.Describe` with one `It`, keeping its top-level name
(`TestLoggingStaysRuntimeNeutral`, `TestInstrumentationStaysRuntimeNeutral`,
`TestProjectionRunnerStaysRuntimeNeutral`). The style follows `port/adapter/adapter_architecture_test.go`
(#254): a guard first (`ContainAllOf`/`Contain` of the package itself, so an empty graph cannot pass), then one
`specs.NoElement(specs.Satisfy(...))` per forbidden dependency, whose failure lists the offending packages by
index. A failing `go list` becomes an error checked with `BeNil`; a missing `go` tool still skips the case.

## What does not change, and why

- Production code. Only `*_test.go` files change.
- `go.mod`. Other tests in the root module still import testify (listed in the PR), so the dependency stays.
- The three guards still run a real `go list` subprocess. They read the build graph, not an external service,
  and `adapter_architecture_test.go` does the same.

## Constraints

Strict TDD, runner `go test`, no `-race`, no workbench. RED is a deliberate production mutation per file: a
temporary `zz_mut.go` blank-importing a forbidden package, removed afterwards.

## Tasks

- [x] T1 logging guard. Route: inline (one mechanical file). RED: blank import of `goakt/v4/actor` made
  `TestLoggingStaysRuntimeNeutral` fail with `NoElement: 45 of 513 elements matched`. GREEN: pass. Commit .
- [x] T2 instrumentation guard. Route: inline. RED: blank import of `goakt/v4/actor` and of
  `internal/extensions` both failed the test. GREEN: pass. Commit 70085e8.
- [x] T3 projectionrunner guard. Route: inline. RED: blank import of `goakt/v4/actor` failed the test.
  GREEN: pass. Commit e6ac8d7.

The engine and `internal/goaktlog` rules could not be driven red by a mutation: importing either from these
packages is an import cycle, which the compiler already rejects.

## Evidence

- `go build ./...`, `go vet`, `golangci-lint run` (0 issues), `gofmt -l` clean.
- `go test -count=5` on the three packages passes. The 30 top-level PASS names are identical before and after.
- Coverage before -> after: instrumentation 96.6% -> 96.6%, logging 100.0% -> 100.0%,
  projectionrunner 93.2% -> 93.5%.
- `rg -l 'stretchr/testify'` in the root module still finds testify in `engine/`, `internal/engine/`,
  `persistence/conformance/`, `testkit/` and `mocks/`, so `go.mod` keeps it.

## Follow-up

Migrate the remaining testify users listed above (`engine/`, `internal/engine/`, `testkit/`,
`persistence/conformance/`, the generated `mocks/`) in their own specs; then drop testify from `go.mod`.

## Progress

All three tasks done. Next step: PR against `develop`.
