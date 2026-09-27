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
- `engine.go` (`NewEngine` only): before calling `Inject`, every registered kind must be a pointer.
  An untyped nil or a non-pointer makes `NewEngine` return `*BehaviorPlacementError{Kind: "<Go
  type>", Err: ErrBehaviorNotPointer}` with an empty `EntityID`, in single-node and cluster mode
  alike, and injects nothing. The design names the pointer check but not nil kinds. An untyped nil
  panics in GoAkt's registry on `main`, so it is rejected with the same error. A typed-nil pointer
  such as `(*T)(nil)` is accepted: GoAkt v4.5.4 registers it through `reflect.TypeOf(v).Elem()` and
  decodes into `reflect.New(type)`, so on `main` it works exactly like `new(T)`, and rejecting it
  would be a runtime break inside v4 (review finding on PR #143 at `0b35ef3`; the first version of
  this slice rejected it).
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

**GREEN (T2).** Commit `69723c1` (after the rebase onto `658bbae`; `e618a82` before it). `TestBehaviorKindAssignability`,
`TestWithEntityKindsAndWithBehaviorKindsShareRegistration`,
`TestNewEngineRejectsUnregistrableKindsSingleNode` (value type and untyped nil, each through both
options; a typed-nil case was here until the review fix below), `TestNewEngineRejectsValueTypeKindInClusterMode`, all of
`TestEngineMultiNodeNeutralBehaviors` (the three S3-3 subtests plus the interop subtest, both
directions) and `TestEngineMultiNodeRemoteEntitySpawn` pass. The interop subtest passed in 10 of 10
runs (`-count=5`, twice). Negative control (not committed): with `AccountEventSourcedBehavior` removed
from node 2's `WithBehaviorKinds`, the node-1-to-node-2 direction fails with `dependency type is not
registered`, so the subtest does detect a missing registration.

**Rebase.** #142 (the `test/compat` module) merged as `658bbae` after this branch was cut from
`b4aa140`; the branch was rebased onto it before publishing and every check below was re-run there.

**Compatibility (T3).** `apidiff` of package `ego`, `origin/main` `658bbae` vs branch: `Compatible
changes: BehaviorKind: added; WithBehaviorKinds: added`, nothing else (same result against `b4aa140`).
The S3-1 consumer program (uses `WithEntityKinds(kinds...)` with a `[]ego.EntityKind`) builds and
runs against both, output identical (9 lines, ends `OK`). archcheck identical on main and branch:
`8 modules checked, 44 packages checked, 182 edges checked, 1 baselined, 0 violation(s), 0 stale
entries`.

**T4.** `CHANGELOG.md` Features entry added.

**T5.** `ciselect -changed ... -base origin/main`: mode `full` (root package changed; all seven nested
modules selected, `test/compat` included). Root suite through `scripts/ci/go-test.sh` with
`GO_TEST_RACE=0` (default toolchain, go1.27.1 auto): exit 0, 23 packages `ok`. `verify-module.sh` for
`benchmark`, `example/cluster`, `publisher/{kafka,nats,pulsar,websocket}` and `test/compat`: all exit
0 with GOROOT go1.26.6 and `GOTOOLCHAIN=local` (with the go1.27.1 toolchain the script's lint step
fails on a typecheck error inside the toolchain's own `crypto/internal/randutil`, an environment
issue unrelated to this change). golangci-lint `--new-from-rev=origin/main` (go1.26.6,
`--modules-download-mode=mod` because no `vendor/` exists locally): `0 issues`.
`TestEngineMultiNodeNeutralBehaviors -count=5` after the rebase: `ok`.

**Review fix (typed-nil kinds).** Review of PR #143 at `0b35ef3` found that rejecting a typed-nil
kind breaks v4 at runtime. Confirmed on a `658bbae` export: `WithEntityKinds((*AccountEventSourcedBehavior)(nil))`
→ `NewEngine`, `Start`, `Stop` all succeed. RED: new `TestNewEngineAcceptsTypedNilPointerKind` (both
options) fails on `0b35ef3` with `eGo: cannot register or place behavior *ego.AccountEventSourcedBehavior:
... must be a non-nil pointer`. GREEN: `NewEngine` now rejects only an untyped nil or a non-pointer
kind; the new test, the rejection tests (value type and untyped nil, single node and cluster), the
interop subtest (`-count=5`), `go vet`, apidiff (still the two additions), `verify-module.sh
test/compat` and golangci-lint (0 issues) pass.

**Error message amended (maintainer decision 2026-09-27).** The old `ErrBehaviorNotPointer` text,
"eGo: a behavior kind registered with WithBehaviorKinds or WithEntityKinds, or spawned in cluster
mode, must be a non-nil pointer", was stricter than `NewEngine` after the typed-nil fix (the RED
output quoted above predates this change). New text: "eGo: a behavior must be a non-nil pointer to be
spawned in cluster mode, and a behavior kind registered with WithBehaviorKinds or WithEntityKinds must
be a pointer type (a typed nil is allowed)". Variable name and `errors.Is` behavior unchanged; design
§5.6 updated with "(message amended 2026-09-27, #143)". RED: new `TestErrBehaviorNotPointerMessage`
failed on the old text; GREEN after the change. No other test asserted the old text.

## Next step

Open the PR; review and merge are the maintainer's decisions. S3-5 (deprecation markers, examples)
follows.
