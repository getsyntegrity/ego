# Feature: deprecation markers, CHANGELOG and example migration (#123, slice S3-5)

Branch: `feat/123-s3-5-deprecations` · Base: `origin/main` `2a6d5a8` · Epic: #10 · Issue: #123 ·
Design: `openspec/changes/ego-arch-002-s3/design.md` (§5.2, §6, §9 row S3-5)

## Problem

S3-1 through S3-4 added the runtime-neutral contracts (`port/behavior`), the spawn-site bridge, the
public `Spawn*` methods and `BehaviorKind`/`WithBehaviorKinds`, all additive: the old GoAkt-bound names
(`ego.EventSourcedBehavior`, `DurableStateBehavior`, `SagaBehavior` and their envelope variants,
`Engine.Entity`/`DurableStateEntity`/`Saga`, `EntityKind`/`WithEntityKinds`) still exist, unmarked, and
the three non-cluster examples still implement `MarshalBinary`/`UnmarshalBinary` they no longer need.
Nothing yet tells a reader which name to use going forward, or removes the serialization boilerplate
the neutral contracts made optional.

## What changes in this slice

- `behavior.go`, `saga.go`: a `// Deprecated:` paragraph (Go convention) on `EventSourcedBehavior`,
  `EventSourcedEnvelopeBehavior`, `DurableStateBehavior`, `DurableStateEnvelopeBehavior` and
  `SagaBehavior`, each naming its `port/behavior` replacement and spawn method and "Removed in the next
  major release (#124)" (design §6's exact sentence). `SagaAction`/`SagaCommand` are **not** touched —
  design §6 marks them "None: aliases stay valid names until #124".
- `option.go`: the same paragraph on `EntityKind` (→ `BehaviorKind`) and `WithEntityKinds` (→
  `WithBehaviorKinds`).
- `engine.go` (doc comments only): the same paragraph on `Entity`, `DurableStateEntity` and `Saga` (→
  the matching `Spawn*` method). No signature or body changes.
- `testkit/scenario.go`: one added sentence on `EventSourcedBehavior`/`DurableStateBehavior` pointing to
  the `port/behavior` contract they structurally subset (design's S3-5 content line; doc comment only,
  not in the design's own file-ownership shorthand, but no other file carries it — noted for the
  reviewer as a deliberate small inclusion, not scope growth).
- `example/eventssourced/main.go`, `example/durablestate/main.go`, `example/saga/main.go`: the
  `AccountBehavior`/`FundTransferSaga` types now implement `behaviorport.EventSourced`/`DurableState`/
  `Saga` (port/behavior) instead of the old `ego.*Behavior` aliases, spawn through
  `Engine.SpawnEventSourced`/`SpawnDurableState`/`SpawnSaga`, and drop `MarshalBinary`/`UnmarshalBinary`
  (and the now-unused `encoding/json` import): none of these three examples runs in cluster mode, so
  they never needed GoAkt serialization.
- `example/cluster/behavior.go`, `example/cluster/main.go`: `AccountBehavior` keeps its
  `MarshalBinary`/`UnmarshalBinary` (cluster mode still needs them) and gains a compile-time assertion
  against `ego.BehaviorKind`; its interface assertion switches to `behaviorport.EventSourced`, and the
  one call site (`entityWithRetry`, `main.go`) switches from `ego.EventSourcedBehavior`/`engine.Entity`
  to `behaviorport.EventSourced`/`engine.SpawnEventSourced`. **Review fix (independent review of
  6585517):** this example had no `WithEntityKinds` call before this slice (checked:
  `rg -n "WithEntityKinds|ClusterKinds" example/cluster/main.go` found only `ego.ClusterKinds()`,
  GoAkt's own actor-kind registry, unrelated) — but design §9 S3-5 says "example/cluster switched to
  `WithBehaviorKinds`", and with `RoundRobin` placement through `SpawnOn` a pod that never spawned an
  `AccountBehavior` itself still needs to be able to decode one a peer places on it. Missing the
  registration was a real gap, not an out-of-scope addition: `cfg := ego.NewConfig(...)` now includes
  `ego.WithBehaviorKinds(new(AccountBehavior))`, and both files' comments now say so.
- `CHANGELOG.md`: one `### 🗑️ Deprecated` entry (Unreleased) with the old→new table, the removal
  milestone (#124), and the empty `apidiff` result.

Out of scope (per file ownership): `compose/`, `port/`, `internal/cmd/*`, `scripts/`, `.github/`, any
signature or behavior change.

## Why this shape

Every `Deprecated:` comment is added, not rewritten from scratch: the existing doc comment stays, and
the deprecation is a new trailing paragraph, because `go doc`/staticcheck's SA1019 only recognize a
paragraph that starts with the literal text `Deprecated: ` (Go's own deprecation convention). Verified
with `go doc . EventSourcedBehavior`, `go doc . WithEntityKinds`, `go doc . Engine.Entity` and
`go doc . SagaBehavior` (Progress section below) — each prints the new paragraph as its own block.

The design's own S3-5 row lists `testkit` doc as content but leaves it off the "files owned" shorthand
column; both can't be exactly right, and the doc-only sentence is low-risk and matches the row's stated
content, so it is included here rather than silently dropped or escalated as a blocking question.

## Constraints

- apidiff on package `ego` vs `origin/main`: no changes at all (a `Deprecated:` comment is not an API
  change; confirmed empty, see Progress).
- golangci-lint (`staticcheck`/SA1019 included) `--new-from-rev=origin/main`: 0 issues. Checked before
  writing any `//nolint` comment — see Progress; no test file needed one.
- No archcheck baseline change (comments and example imports only).
- TDD: strict (user global configuration) does not apply to a doc/example-only slice with no new
  behavior to RED/GREEN; the check here is the evidence in Progress (apidiff, lint, vet, build, `go
  doc`, full suite, nested modules), not a new failing test.
- Route: direct inline. One writer; the design names every file.

## Tasks

- [x] **T1** Deprecation markers on every symbol design §6 lists (`behavior.go`, `saga.go`,
  `option.go`, `engine.go`). Check: `go doc` on two symbols shows the `Deprecated:` paragraph; `go
  build`/`go vet` clean.
- [x] **T2** Migrate the three non-cluster examples to `port/behavior` + `Spawn*`, dropping
  serialization; migrate `example/cluster` to the new names, keeping serialization. Check: root module
  and the `example/cluster` nested module both build; `go vet` clean on both.
- [x] **T3** `CHANGELOG.md` Deprecated entry and this document. Check: structural readback.
- [x] **T4** Compatibility evidence: apidiff (`origin/main` vs head), consumer program (build/vet/run,
  base vs head, identical output), archcheck. Check: apidiff empty; consumer output identical; archcheck
  0 violations.
- [x] **T5** Full verification: `ciselect -base origin/main`, full root suite, `verify-module.sh` for
  all seven nested modules, golangci-lint (root, new-from-rev).

## Acceptance criteria (#123, checked after this slice — see gh issue view 123)

1. *Neutral contracts in `port/behavior`, not embedding/exposing GoAkt types; `ego.EventSourcedBehavior`
   /`DurableStateBehavior`/`SagaBehavior` redefined over them and deprecated until #124's major
   release.* **Met.** `port/behavior` (S3-1) has no GoAkt import; the three `ego.*Behavior` names are
   re-expressed on top of it (S3-1) and now carry `Deprecated:` (this slice).
2. *A behavior implementing only the domain methods and `ID()` compiles against the contracts and runs
   on GoAkt in local mode.* **Met** (S3-2/S3-3: `SpawnEventSourced`/`SpawnDurableState`/`SpawnSaga`;
   demonstrated live in the migrated `example/eventssourced`, `example/durablestate`, `example/saga`).
3. *In cluster mode, a behavior without serialization fails with a typed, descriptive error when
   registering or spawning, not a panic.* **Met** (S3-2/S3-3/S3-4: `*BehaviorPlacementError` wrapping
   `ErrBehaviorNotSerializable`/`ErrBehaviorNotPointer`).
4. *Examples and existing tests pass with no domain changes outside removing now-unneeded serialization
   code.* **Met by this slice**: only `MarshalBinary`/`UnmarshalBinary` and the type/spawn-call renames
   changed in the three non-cluster examples; no domain method body changed; the existing root and
   nested-module test suites pass unmodified (Progress).
5. *`apidiff` of package `ego` between `main` and the slice is recorded in the PR, with a SemVer/
   migration note.* **Met by this slice**: apidiff is empty (Progress); the CHANGELOG Deprecated entry
   is the migration note.
6. *The `migration` entry in archcheck's baseline is reviewed (stays or leaves, with a reason).*
   **Reviewed, unchanged, reason recorded** — design.md §7 (S3-1): `migration`'s only production use of
   package `ego` is `ego.ResolveLogger`, unrelated to behavior contracts, so S3 (including this slice)
   neither satisfies nor adds to the entry's removal criterion. Confirmed again here: archcheck reports
   the same "1 baselined" count as S3-4 (Progress), i.e. no new or removed baseline entry.

All six criteria are met after this slice. #123 can close on this PR.

## Progress and evidence

**T1 (deprecation markers).** `go build ./...` and `go vet ./...` clean after adding every paragraph.
`go doc . EventSourcedBehavior`, `go doc . WithEntityKinds`, `go doc . Engine.Entity` and
`go doc . SagaBehavior` each print the new `Deprecated: ...` paragraph as its own block (not merged into
the preceding paragraph). `go doc . EntityKind` also checked.

**T2 (examples).** Root module `go build ./...` and `go vet ./...` clean after migrating
`example/eventssourced`, `example/durablestate`, `example/saga`. `example/cluster` (nested module) `go
build ./...` and `go vet ./...` clean after its migration, with `replace github.com/pablogore/ego/v4 =>
../../` unchanged (already pointed at the local root). `gofmt -l .` at the root lists only pre-existing
drift in files this slice does not touch (`helper_test.go`, four `mocks/...` files) — none of the files
this slice edited.

**T3.** CHANGELOG `### 🗑️ Deprecated` entry added under Unreleased (old→new table, ten renamed names,
`SagaAction`/`SagaCommand` explicitly excluded, #124 as the removal milestone). This document.

**T4 (compatibility).**

- `apidiff` of package `ego`, `origin/main` `2a6d5a8` (checked out at `/tmp/ego-s3-5-base`, a plain
  `git worktree add --detach`) vs this branch's head, `golang.org/x/exp/cmd/apidiff@latest`: **empty
  output** — no compatible or incompatible changes at all.
- Consumer program (`/tmp/s3-5-consumer`, copied from the S3-1/S3-3/S3-4 slices' reusable
  `s3-1-consumer`, which implements all three old contracts with serialization, embeds them, calls
  `MarshalBinary` through `ego.EventSourcedBehavior`, type-asserts to/from `extension.Dependency`, binds
  `(*ego.Engine).Entity` to its old func type, builds `ego.SagaAction`/`SagaCommand` literals, spreads a
  `[]ego.EntityKind` into `WithEntityKinds`, and spawns/sends commands through the old API): builds,
  vets and runs cleanly with `replace` pointing at the base checkout, then again pointing at this
  branch's worktree. `diff` of the two runs' stdout: **identical** (9 lines, ends `OK`).
- `archcheck`: `8 modules checked, 44 packages checked, 186 edges checked, 1 baselined, 0 violation(s),
  0 stale entries`. Same package count and the same single baselined entry (`migration -> ego`) as
  S3-4's recorded run; the edge count moved (182 → 186) only because **all four** migrated examples —
  `eventssourced`, `durablestate`, `saga`, and `cluster` — now import `port/behavior` in addition to
  `ego` — expected, not a violation. (Corrected from an earlier draft of this document that said "the
  three migrated examples"; `example/cluster` also imports `port/behavior` now and counts as the
  fourth new edge — independent review of `6585517`.)

**T5 (full verification).**

- `ciselect -changed <this slice's changed files> -base origin/main -repo-root . -out-dir ...`: mode
  `full` (root package files changed: `behavior.go`, `engine.go`, `option.go`, `saga.go`), all 26
  included root-module packages selected, all seven nested modules selected (`benchmark`,
  `example/cluster`, `publisher/{kafka,nats,pulsar,websocket}`, `test/compat`).
- Root suite via `scripts/ci/go-test.sh`, `GO_TEST_RACE=0` (repository's local-testing rule; default
  go1.27.1 toolchain): `ok github.com/pablogore/ego/v4 381.125s` plus 22 further `ok`/`[no test
  files]` results, 23 `ok` lines total, no `FAIL` or `panic:` anywhere in the run's output (checked with
  `grep`). Matches the package count `ciselect` selected.
- `verify-module.sh` for all seven nested modules (GOROOT `/home/pablog/sdk/go1.26.6`,
  `GOTOOLCHAIN=local`): all seven exit 0 — `benchmark`, `example/cluster`, `publisher/kafka`,
  `publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `test/compat`. Each module's `go mod
  tidy -diff`, `go build`, `go vet`, golangci-lint (root `.golangci.yml`) and `go test` all passed; every
  module reported `0 issues` from golangci-lint.
- golangci-lint at the root, `--new-from-rev=origin/main --modules-download-mode=mod` (GOROOT
  `/home/pablog/sdk/go1.26.6`, `GOTOOLCHAIN=local`), over `.`, `./port/...`, `./testkit/...`,
  `./example/...` and again over the full `./...`: **0 issues** both times. No test file needed a
  `//nolint:staticcheck` comment. **Corrected reasoning (independent review of `6585517`):** an
  earlier draft of this document attributed that to `--new-from-rev` only reporting new/changed lines.
  That is not the actual mechanism. The two real reasons, confirmed by a full-tree (no
  `--new-from-rev`) run that also reports 0 issues: (1) `.golangci.yml`'s `run.tests: false` excludes
  every `_test.go` file from linting entirely, so the large majority of deprecated-API callers — the
  existing test suite, which the design says intentionally keeps exercising `Entity`/`WithEntityKinds`
  as regression coverage — is never linted, regardless of `--new-from-rev`; and (2) staticcheck's
  SA1019 does not flag a deprecated symbol's use from within the same package that declares it, which
  covers any remaining in-package (non-test) reference. The only internal non-test, non-same-package
  callers of the deprecated APIs were in `example/*` (each its own `package main`), which this slice
  migrates — that is the one case where `--new-from-rev` genuinely matters, since those calls did
  change.

**Follow-up note (for #124 or a lint-hardening change):** `migration/tenant_adoption_test.go` is in
package `migration`, a different package from the deprecated symbols' home (`ego`), so the
same-package SA1019 exemption above does not cover it; it currently escapes only because
`run.tests: false` excludes it, and lines 182, 481 and 2235 (`ego.EventSourcedBehavior`,
`engine.Entity` twice) would be flagged the day test linting is ever turned on.

## Next step

Open the PR against `main` (`Closes #123`, all six acceptance criteria met). Review and merge are the
maintainer's decisions. S3 is complete after this slice; #124 (the major release) removes every symbol
marked `Deprecated:` here.
