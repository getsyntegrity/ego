# Feature: runtime-neutral behavior contracts in `port/behavior` (#123, slice S3-1)

Branch: `feat/123-s3-1-port-behavior` · Base: `origin/main` `77beda6` · Epic: #10 · Issue: #123 ·
Design: `openspec/changes/ego-arch-002-s3/design.md` in PR #128 (§5.1, §5.2, §6, §8, §9 row S3-1)

## Problem

The three public behavior contracts in package `ego` (`EventSourcedBehavior`, `DurableStateBehavior`,
`SagaBehavior`) embed GoAkt's `extension.Dependency`, which adds `MarshalBinary`/`UnmarshalBinary`.
That requirement exists only so GoAkt can copy a behavior to another cluster node, yet every domain
author must implement it, even on a single node. It also blocks #105: a second, in-memory composition
cannot show a runtime-independent domain while the domain's own types are GoAkt types.

## What changes in this slice

A new contract package, `github.com/pablogore/ego/v4/port/behavior`, declares the neutral contracts
`EventSourced`, `EventSourcedEnvelope`, `DurableState`, `DurableStateEnvelope` and `Saga`, plus
`SagaAction`, `SagaCommand` and the `Command`/`Event`/`State` aliases. They carry `ID()` and the
domain methods only. The old `ego` names are re-declared as "neutral contract + `extension.Dependency`",
so their method sets do not change. `SagaAction`/`SagaCommand` become aliases in `ego`, and the
unexported method `(*SagaAction).isNoop` becomes the function `sagaActionIsNoop` (a method cannot be
declared on a type from another package); its one call site in `saga_actor.go` changes.

## Why this shape

Human decisions (2026-09-26/27, design.md §11): protobuf stays public in v4, and nothing breaks inside
v4. Removing the embed from the old names is apidiff-incompatible and a real source break for callers
that use a behavior as an `extension.Dependency`, so #123 criterion 1 is met in v4 by the new
`port/behavior` names. The old names keep the embed until #124. Rejected alternative: shrinking the
existing interfaces in place (design.md §4, option D).

## Constraints

- No incompatible change to package `ego` beyond the documented apidiff false positive for the two
  moved structs and the four `SagaBehavior` methods that mention them (design.md §6).
- `port/behavior` must pass archcheck's `contract-allowlist` with no baseline change.
- No `Deprecated:` markers in this slice (that is S3-5). No change to `engine.go`, `option.go`,
  `internal/extensions`, the other actors, `.github/` or `internal/cmd/*`.
- TDD: strict (user global configuration); runner `go test` (no `-race`, no workbench locally).
- Route: direct inline (small, already-specified files; one writer).

## Tasks

- [x] **T1** Write the RED tests: dependency allowlist and domain-only contract tests in
  `port/behavior`; compile-time method-set, baseline-shape and alias-identity assertions plus
  `sagaActionIsNoop` cases in `behavior_compat_test.go`; neutral contracts added to
  `testkit_compat_test.go`. Check: the build fails before the implementation.
- [x] **T2** Add `port/behavior` and re-express `behavior.go`/`saga.go` on top of it; replace the
  `isNoop` call site. Check: the T1 tests pass; `go build ./...`, `go vet`, gofmt clean.
- [x] **T3** Record compatibility evidence: apidiff on package `ego`, the base-API consumer program
  against base and head, archcheck before/after. Check: only the documented apidiff false positives;
  identical consumer output except the documented `reflect` name; archcheck 16 packages, 0 violations,
  1 baselined, 0 stale.
- [x] **T4** `CHANGELOG.md` (Unreleased, Features) and this document. Check: structural readback.
- [x] **T5** Full verification: `ciselect` (mode `full`), full root suite, every nested module
  (build, vet, `go mod tidy -diff`, test), golangci-lint on the changed packages.

## Acceptance criteria (S3-1 row of design.md §9)

1. `go test ./port/behavior/` (dependency allowlist test) passes.
2. archcheck passes with an unchanged baseline.
3. apidiff reports only the §6 alias false positives.
4. The base-API consumer program builds and prints identical output on base and head (the `reflect`
   name of the two moved structs is the one documented difference).

## Progress and evidence

**Commits.** T1–T5: `2eabd30` (`feat(port): add runtime-neutral behavior contracts in port/behavior`).
Review nits (comment and documentation only): the follow-up `docs` commit on the same branch.

**RED (T1).** Before `port/behavior` had production files:
`go test ./port/behavior/` → `github.com/pablogore/ego/v4/port/behavior: no non-test Go files ... FAIL [build failed]`;
`go vet .` → same error (the root test files import the missing package). After adding only the
contract files (before touching `ego`), `go vet .` failed with
`cannot use interface{behaviorport.Saga; extension.Dependency}(nil) ... as SagaBehavior value ...
(wrong type for method Compensate)`: the event-sourced and durable-state method-set assertions already
held, and the saga one failed until `SagaAction`/`SagaCommand` became aliases.

**GREEN (T2).** `TestBehaviorDependsOnlyOnContracts`, `TestDomainOnlyBehaviorsRunThroughTheContracts`,
`TestSagaActionAndSagaCommandAreAliases` and `TestSagaActionIsNoop` pass; `go build ./...`,
`go vet . ./port/...` and gofmt are clean.

**apidiff (T3)**, package `ego`, `origin/main` `77beda6` vs head, `golang.org/x/exp/cmd/apidiff@latest`:

```
Incompatible changes:
- SagaAction: changed from SagaAction to github.com/pablogore/ego/v4/port/behavior.SagaAction
- SagaBehavior.Compensate: changed from func(context.Context, State) ([]SagaCommand, error) to func(context.Context, github.com/pablogore/ego/v4/port/behavior.State) ([]github.com/pablogore/ego/v4/port/behavior.SagaCommand, error)
- SagaBehavior.HandleError: changed from func(context.Context, string, error, State) (*SagaAction, error) to func(context.Context, string, error, github.com/pablogore/ego/v4/port/behavior.State) (*github.com/pablogore/ego/v4/port/behavior.SagaAction, error)
- SagaBehavior.HandleEvent: changed from func(context.Context, Event, State) (*SagaAction, error) to func(context.Context, github.com/pablogore/ego/v4/port/behavior.Event, github.com/pablogore/ego/v4/port/behavior.State) (*github.com/pablogore/ego/v4/port/behavior.SagaAction, error)
- SagaBehavior.HandleResult: changed from func(context.Context, string, State, State) (*SagaAction, error) to func(context.Context, string, github.com/pablogore/ego/v4/port/behavior.State, github.com/pablogore/ego/v4/port/behavior.State) (*github.com/pablogore/ego/v4/port/behavior.SagaAction, error)
- SagaCommand: changed from SagaCommand to github.com/pablogore/ego/v4/port/behavior.SagaCommand
```

Exactly the six items design.md §6 predicts (two moved structs, four `SagaBehavior` methods). Nothing
else is reported, and there are no compatible additions to package `ego` in this slice.

**Consumer program (T3).** A program outside the repository module (job scratch directory, `replace`
to base and then to head) implements the three old contracts with serialization, embeds them in a
struct and an interface, calls `MarshalBinary` through an `ego.EventSourcedBehavior`, type-asserts
`extension.Dependency` back to the old names, builds `goakt.WithDependencies`, spreads a
`[]ego.EntityKind` into `WithEntityKinds` and `ActorSystem.Inject`, binds `(*ego.Engine).Entity`,
`DurableStateEntity` and `Saga` to variables of their old function types, builds `ego.SagaAction`/
`ego.SagaCommand` literals, spawns an entity with `Entity` and sends two commands. `go vet` and
`go run` succeed against both. Design.md §6 also asks the consumer program to type-assert between
the old and new interfaces; that is impossible against the base, where the `port/behavior` names do
not exist, so that direction is covered in-repo by the two-way assertions in
`behavior_compat_test.go`. The only output difference:

```
7c7
< reflect: ego.SagaAction ego.SagaCommand
---
> reflect: behavior.SagaAction behavior.SagaCommand
```

**archcheck (T3).** Before: `15 packages checked, 70 edges checked, 1 baselined, 0 violation(s), 0 stale entries`.
After: `16 packages checked, 72 edges checked, 1 baselined, 0 violation(s), 0 stale entries`.
No change under `internal/cmd/archcheck`.

**Verification (T5).** `ciselect` on the changed files: mode `full` (shared root package), 24 of 24
packages, all six nested modules affected. Nested modules (`GOWORK=off`, `GOFLAGS` cleared, no
`-race`): `benchmark`, `example/cluster`, `publisher/kafka`, `publisher/nats`, `publisher/pulsar`,
`publisher/websocket` — build ok, vet ok, `go mod tidy -diff` clean, tests pass. golangci-lint
(`--new-from-rev=origin/main`, `. ./port/...`, after a local `go mod vendor`): with Go 1.27.1 the
linter panics inside its type loader (the known local toolchain mismatch); with Go 1.26.6
(`GOTOOLCHAIN=local`) it reports `0 issues`. CI lint is authoritative. Full root suite (`go test -count=1 ./...`, Go 1.27.1, no `-race`): every
package passes, the root package in 423 s.

## Next step

S3-2 (spawn-site bridge in `engine.go`, `extensions.LocalBehavior`, typed errors, actors on neutral
types), which must land before #105 IMPL-4.
