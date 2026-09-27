# Feature: BehaviorKind, WithBehaviorKinds and the NewEngine pointer check (#123, slice S3-4)

Branch: `feat/123-s3-4-behavior-kind` · Base: `origin/main` `b4aa140` · Epic: #10 · Issue: #123 ·
Design: `openspec/changes/ego-arch-002-s3/design.md` (§5.5, §5.6, §6, §8, §9 row S3-4)

## Problem

In cluster mode every node must register the behavior types it may host, so it can decode a spawn
placed on it by a peer. Today that registration goes through `WithEntityKinds(kinds ...EntityKind)`,
and `EntityKind` is an alias of GoAkt's `extension.Dependency`, so the public option names a runtime
type. `NewEngine` (`engine.go`) then hands every kind to GoAkt's `ActorSystem.Inject`. GoAkt's type
registry names a type through a pointer: a value-type kind (or a nil kind) panics inside `Inject` while
it holds the actor-system lock, and the actor system can no longer be stopped. This happens even on a
single node, because kind registration always goes through the registry.

## What changes in this slice

- `option.go`: a new interface `BehaviorKind` with the same method set as `extension.Dependency`
  (`ID() string` plus `encoding.BinaryMarshaler` and `encoding.BinaryUnmarshaler`), spelled with the
  standard library only. A new option `WithBehaviorKinds(kinds ...BehaviorKind)`. The `Config` field
  `entityKinds []EntityKind` becomes `behaviorKinds []BehaviorKind`, and `WithEntityKinds` appends to
  it element by element, so both options feed one registration list and can be mixed. `EntityKind` and
  `WithEntityKinds` keep their exact signatures and are not deprecated yet (that is S3-5).
- `engine.go` (`NewEngine` only): before calling `Inject`, every registered kind must be a non-nil
  pointer. Otherwise `NewEngine` returns `*BehaviorPlacementError{Kind: "<Go type>", Err:
  ErrBehaviorNotPointer}` with an empty `EntityID`, in single-node and cluster mode alike, and injects
  nothing. The design names the pointer check; it does not say what a nil or typed-nil kind does. This
  slice rejects both with the same error, because `ErrBehaviorNotPointer` already says "must be a
  non-nil pointer" and GoAkt's registry cannot name either.
- `engine_neutral_cluster_test.go`: the `old and new registration interoperate` subtest.
- `behavior_kind_test.go` (new): assignability, shared registration, `NewEngine` rejection.
- `CHANGELOG.md`: one Features entry.

Out of scope: `Deprecated:` markers, the examples and the migration table (S3-5); `compose/`,
`port/`, the actor files, `internal/cmd/*`, `scripts/`, `.github/`.

## Why this shape

`BehaviorKind` is a named interface, not an alias, so `ego`'s public option no longer spells a GoAkt
type, while any `EntityKind` value stays assignable to it and back (identical method sets). The
rejected alternative, `type BehaviorKind = extension.Dependency`, would keep GoAkt in the signature
and would not let the kind move with the adapter after #124 (design §5.5). One consequence is visible
at call sites: a `[]EntityKind` cannot be spread into `WithBehaviorKinds(...)`, because Go does not
convert slices of different element types; a caller either keeps `WithEntityKinds(kinds...)` or
renames the slice's element type. The interop subtest uses two different behavior types, one per
direction, so each receiving node can only know the type from its own registration option and never
from the lazy local `Inject` the calling node does at spawn time.

## Constraints

- apidiff on package `ego` vs `origin/main`: only the two additions.
- No archcheck baseline change. No release tag between S3-2 and S3-4.
- TDD: strict (user global configuration); runner `go test` (no `-race`, no workbench, always
  `-timeout`).
- Route: direct inline. One writer; the design names every file and function.

## Tasks

- [x] **T1** RED: new tests fail to build on `origin/main` (`WithBehaviorKinds` undefined). Check:
  build failure recorded.
- [x] **T2** GREEN: `BehaviorKind`, `WithBehaviorKinds`, shared field, `NewEngine` check. Check: new
  unit tests pass; interop subtest passes 5 runs; existing cluster tests pass.
- [x] **T3** Compatibility evidence: apidiff, S3-1 consumer program on main and branch, archcheck.
  Check: two additions only; identical consumer output; unchanged archcheck.
- [x] **T4** `CHANGELOG.md` and this document. Check: structural readback.
- [x] **T5** Full verification: `ciselect`, full root suite, nested modules, golangci-lint.

## Acceptance criteria (S3-4 row of design §9)

1. The interop subtest passes in both directions.
2. `NewEngine` with a value-type kind returns `ErrBehaviorNotPointer` in single-node mode and does not
   panic.
3. The S3-3 subtests still pass.

## Progress and evidence

**RED (T1).** With only the tests added on `b4aa140`: `./behavior_kind_test.go:47:4: undefined:
BehaviorKind`, `undefined: WithBehaviorKinds`, `cfg.behaviorKinds undefined`; `FAIL
github.com/pablogore/ego/v4 [build failed]`. Behavioral RED of the `NewEngine` check, with
`BehaviorKind`/`WithBehaviorKinds` added but not the check:
`TestNewEngineRejectsUnregistrableKindsSingleNode/WithBehaviorKinds/value_type` failed with `should not
panic ... reflect: Elem of invalid type ego.valueTypeEventSourcedBehavior` from
`goakt .../types.(*registry).Register` via `actorSystem.Inject` via `NewEngine`; the `nil` case panicked
with a nil pointer dereference.

**GREEN (T2).** Commit `e618a82`. `TestBehaviorKindAssignability`,
`TestWithEntityKindsAndWithBehaviorKindsShareRegistration`,
`TestNewEngineRejectsUnregistrableKindsSingleNode` (6 cases: value type, nil, typed-nil, each through
both options), `TestNewEngineRejectsValueTypeKindInClusterMode`, all of
`TestEngineMultiNodeNeutralBehaviors` (the three S3-3 subtests plus the interop subtest, both
directions) and `TestEngineMultiNodeRemoteEntitySpawn` pass. The interop subtest passed in 10 of 10
runs (`-count=5`, twice). Negative control (not committed): with `AccountEventSourcedBehavior` removed
from node 2's `WithBehaviorKinds`, the node-1-to-node-2 direction fails with `dependency type is not
registered`, so the subtest does detect a missing registration.

**Compatibility (T3).** `apidiff` of package `ego`, `origin/main` `b4aa140` vs branch: `Compatible
changes: BehaviorKind: added; WithBehaviorKinds: added`, nothing else. The S3-1 consumer program
(uses `WithEntityKinds(kinds...)` with a `[]ego.EntityKind`) builds and runs against both, output
identical (9 lines, ends `OK`). archcheck on both: `37 packages checked, 157 edges checked, 1
baselined, 0 violation(s), 0 stale entries`.

**T4.** `CHANGELOG.md` Features entry added.

**T5.** `ciselect -base origin/main`: mode `full` (root package changed; all six nested modules
selected). golangci-lint `--new-from-rev=origin/main` (GOROOT go1.26.6, `GOTOOLCHAIN=local`,
`--modules-download-mode=mod` because no `vendor/` exists locally): `0 issues`. Root suite and
nested modules: see PR body.

## Next step

Open the PR; review and merge are the maintainer's decisions. S3-5 (deprecation markers, examples)
follows.
