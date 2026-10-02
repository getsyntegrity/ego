# engine tests, group G1: command and behavior tests without testify

Follows #252 (merged). The `engine` package still has 28 test files that import testify. They are split
across four writers; this document is group G1: `command_runtime_wiring_test.go`,
`behavior_dependency_test.go`, `behavior_kind_test.go`, `behavior_value_type_test.go` and
`command_architecture_test.go`. The other groups (expected-revision and precondition tests, tenant tests,
lifecycle/telemetry/misc) are separate PRs. `engine/helper_test.go` is shared by all groups and is not touched.

## Problem

`docs/testing/go-specs.md` asks for go-specs matchers instead of testify, `specs.Describe`/`It` for every
test, `mock.Controller` instead of testify mocks, and `ctx.Eventually` instead of fixed waits. The five G1
files still used testify `require`/`assert` in 36 plain tests and several helpers.

## What changes

Only `*_test.go` files in `engine/` change. No production code is touched.

- All five files: every `require`/`assert` call is a go-specs matcher (`BeNil`, `MatchError`, `MatchErrorAs`,
  `Equal`, `BeEmpty`, `Contain` and so on). Every plain test is wrapped in a `Describe`; subtests became
  `s.It`. `EqualValues` became `Equal` with the real type, because `specs.Equal` is type-strict.
- `command_architecture_test.go`: instead of asserting inside the loop, the test collects the dependencies
  outside the allowlist and expects the list to be empty, so a failure names every offender (the old testify
  message did that, a go-specs expectation takes no message).
- `behavior_dependency_test.go`: `nilBehaviorSpawns` no longer closes over an engine; the new
  `nilBehaviorRejectionSpecs` registers one `It` per case on a `Spec`, so the nine case names stay subtests.
  The two cluster/single-node tests build their shared engine once in a `BeforeEach` (`sync.Once`) instead of
  at registration, so every setup check goes through `sc.Expect`. The saga `require.EventuallyWithT` is one
  `ctx.Eventually`. The "serializable pointer behavior still spawns" check at the end of the cluster test is
  now its own `It`.
- `behavior_kind_test.go`: `requireKindRejected` takes the spec context instead of a `*testing.T`.
- `command_runtime_wiring_test.go`: the `newHarness` helper takes the spec context.
- New `engine/specs_helpers_g1_test.go`: `panicValueG1`, the go-specs counterpart of `NotPanics`. It has its
  own file and name so it cannot collide with helpers the other groups add.

## What does not change, and why

- **`engine/helper_test.go`** (`newTestEngine`, still testify). Shared by every group; its owner converts it.
- **The architecture tests' subprocess.** `TestCommandArchitecture` still runs `go list` (and uses
  `tenancyArchitectureGoList` from `tenancy_architecture_test.go`, which belongs to another group). It is not a
  unit test by the conventions page, but it keeps its behavior and only loses testify.
- Tests that start an actor system keep their bodies; they have no fixed wait left to replace.
- **The `RebuildProjection` flake** (`engine-rebuild-projection-race`) needs a production fix, so nothing was
  done for it here.

## Constraints

- Strict TDD, runner `go test ./engine`. For test-only work RED is a deliberate production mutation, reverted.
- Every top-level `Test` name stays (25 in G1, none lost). Every old subtest name is still a subtest, one level
  down under its `Describe`; the old `.../nil_and_typed-nil_behaviors` is now a `Describe` whose nine
  children are the old subtests.
- No `-race`, no workbench, no force-push.

## Tasks

- [x] T1 `behavior_value_type_test.go`, `command_architecture_test.go` and the `panicValueG1` helper. Route:
      inline (small files). Commit: see git log.
- [x] T2 `behavior_kind_test.go` and `behavior_dependency_test.go`. Route: scripted conversion (a one-off
      Python rewriter that turns `require.X(t, ...)` into `sc.Expect(...).To(...)`) plus manual restructuring
      of the helpers. RED: appending `x` to the kind in `NewEngine`'s kind check gave `expected
      engine.valueTypeEventSourcedBehaviorx to equal engine.valueTypeEventSourcedBehavior` in every kind case.
- [x] T3 `command_runtime_wiring_test.go`. Route: scripted conversion. Evidence: the file's 11 tests pass.
- [x] T4 Verify and deliver: see Progress.

## Follow-up (not in this document)

- The other groups listed above, and `engine/helper_test.go`.
- `engine-rebuild-projection-race`.

## Progress

- 2026-10-01: T1 to T4 done. Engram mirror: pending.
