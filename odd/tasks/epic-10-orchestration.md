# Feature: Epic #10 orchestration (hexagonal architecture, #102 modules, CI support)

Branch: worktree-epic-10-orchestration · Base: origin/main 77beda6 · Epic: #10 · Related: #11, #38, #102

This document is the orchestrator's status board. It records which child
work is done, what is running, who (which agent/branch/PR) owns it, and
what is blocked on a human decision. Merges, issue edits and open ADR
decisions are human gates: the orchestrator never performs them.

## Current state (2026-09-26, origin/main 77beda6)

Architecture check on main:

```
archcheck: 15 packages checked, 70 edges checked, 1 baselined, 0 violation(s), 0 stale entries
```

Remaining baseline entry (internal/cmd/archcheck/baseline.go):

| Importer | Import | Rule | Removal |
| --- | --- | --- | --- |
| `github.com/pablogore/ego/v4/migration` | `github.com/pablogore/ego/v4` | application-no-runtime | S4 runtime SPI (epic #11) |

The four publisher entries were removed by S1b (#121).

### Done

| Item | What | Evidence |
| --- | --- | --- |
| #104 | ADR ego-arch-001 (explore, proposal/design, amendment) | #110, #113, #118 merged |
| #103 S1a | `port/publishing` contracts + aliases in `ego` | #116 merged |
| #103 S1b | publishers import `port/publishing` | #121 merged (issue closed; criteria 1–3 moved to #123) |
| #107 (S2) | archcheck enforced in CI | #117 merged |
| #108 | affected-package PR fast lane, go-acc removed | #109 merged |
| #111 | every nested module verified in CI (integrated part) | #120 merged |
| #115 | example/cluster store implements scoped EventsStore | #119 merged |
| #112 slice 1 | fixed waits after synchronous setup removed | #114 merged |

### Open

#10 (epic), #11 (runtime epic), #38 (CI epic), #102, #105 (design PR #125
open), #106, #112 (slices 2+), #122, #123, #124, #99 (partial), #101
(implementation already on main, issue stale), #37, #39.

### Findings from the read-first step

- #101 is already implemented on main by #109: go-acc is gone, coverage is
  native `go test -coverpkg` with ciselect's denominator, and `docs/ci.md`
  records the explicit `testkit` decision. The issue only needs closing.
- #99: PR #100 fixed six PreStart paths, but `snapshots_writer_actor.go:94,97`
  still uses unchecked type assertions, and the root cause of the nil
  extension during spawn is unproven.
- #10's body already ticks #103/#104/#107/#108 and lists #102/#122/#123/#124;
  only the #11 link is missing.
- The PR workflow has jobs `build` and `modules`; there is no job named
  `nested-modules-result`.
- Local shell exports a stale `GOROOT=/home/pablog/sdk/go1.26.6`; every go
  command must run as `env -u GOROOT go ...`.

## Human decisions (2026-09-26)

1. Protobuf: `egopb` and `proto.Message` stay a supported public contract
   in v4; revisit at the next major. Recorded in ego-arch-001 §10 by the
   #123 design PR.
2. S3 compatibility: deprecate first, no break inside v4. Every temporary
   alias and deprecated API (EntityKind, S1 publisher aliases, anything S3
   adds) lives until the major release introduced by #124 and is removed
   there. The #123 design must solve cluster mode (behaviors are GoAkt spawn
   dependencies today), keep `WithEntityKinds` working, include a
   remote-spawn test, and go to the human before implementation.
3. #125: merge after the reviewer approves and CI is green; send the PR link
   and findings to the human first.
4. W0c: proceed with coverage semantics unchanged. Evidence at 77beda6:
   ciselect `coverpkg` is byte-identical in `full` (23 packages) and
   `affected` (leaf change, 3 packages) mode, sha256 `ce183755…`, testkit
   included. No code change is needed; closing #101 is an issue edit.
   W0e: send the draft; do not apply.
6. (2026-09-27) Order: #123 S3-2 and S3-4 both land before #105 IMPL-4;
   #125 is aligned with #128 before merge (guard in unexported spawn
   functions, #123 adds `port/behavior` instead of removing the embed,
   `WithCluster` takes `BehaviorKind`). Names still pending.
7. (2026-09-27) Names confirmed: `port/behavior`, `Spawn*`,
   `BehaviorKind`/`WithBehaviorKinds`, `BehaviorPlacementError`. #123
   criterion 1 is met in v4 by `port/behavior` (old names keep the embed,
   deprecated, until #124). S3-1 implementation dispatched.
8. (2026-09-27) Approved: (a) #123 criterion 1 reworded — applied
   07:07 UTC after updatedAt check, only that line changed; (b) add
   `go mod tidy -diff` as a nested-module CI gate in a separate PR after
   #130 merges; (c) keep #99 open, narrowed to the root cause, with a
   comment drafted for approval after W0b2 merges.
9. (2026-09-27) #133: narrow R1 (import filter when root manifest unchanged)
   and R2 (`-base` flag; go.mod edit ≠ add/remove) in #133 itself, after
   amending #132 §5.2 and the fixture; nits included; CI + review repeated
   before merge. #39: draft an issue for the unresolvable publisher module
   paths, show it before creating. D1–D8 approved to guide S2/S3; D1 needs
   an explicit target path + migration plan confirmed before execution; D8
   waits for F4.
10. (2026-09-27) Created #134 (publishers not installable: /v4 in the
   middle of nested module paths), label bug; linked from #39 "Issues hijos
   esperados" (one line added, rest byte-identical, updatedAt checked).
11. (2026-09-27) #137, #138, #139 merged. ctx.Err() check before each
   start step approved for #105 IMPL-4. No release tag between S3-2 and
   S3-4.
12. (2026-09-27) #142 → option (c), remove reflect/unsafe. #140 →
   ErrDuplicatePublisherID with whole-batch validation and no partial state.
13. (2026-09-27) #142 merged on the maintainer's own review (the
   orchestrator's reviewer never delivered a final verdict on option (c)).
   S3-5 and IMPL-4 wait until S3-4 is reviewed and CI-green. Still no release
   tag between S3-2 and S3-4.
14. (2026-09-27) #143 (S3-4) merged as 2a6d5a8 on the maintainer's
   standing authorization after the message fix and green CI. The no-tag
   restriction between S3-2 and S3-4 is lifted. S3-5 runs before IMPL-4
   (shared engine.go/option.go/example files).
15. (2026-09-27) #102 reopened (manual close was premature: S2/S3 stay in
   #102 until contracts and publishers extraction is implemented and
   verified); left unticked in #10. #123 ticked in #10 (only that line
   changed, updatedAt checked).
16. (2026-09-27) #145: maintainer edited the misleading PR comment
   personally. Approved: V7 added to ego-arch-003 §D4a; G1 sentinels, G2
   ValidationError, undeclared family sentinel; unknown family bits masked;
   one PR. Explicit follow-up: two-node cluster test through compose/goakt.
   Merge only after review + CI on the final head.
17. (2026-09-27) Created #146 (two-node test via compose/goakt; remote
   placement unambiguous, both directions, clean shutdown of both nodes, 5 runs).
   D1 reaffirmed: migrate to getsyntegrity before first publish; #102 S2/S3
   wait for F4 and that migration; #124 stays last with its planned major.
18. (2026-09-27) #105 reopened (premature manual close). S4 draft and its seven
   recommendations approved; created #147 (S4) and #148 (RUNTIME-005 +
   compose/inmem). S4-1 started. S4-D: small interfaces per capability, v4
   public compatibility; physical engine extraction stays in #124.
5. Pace: two or three active agents; critical path #125 → #123 → S4 (#11)
   → #102 extraction. #122, #101 and #112 run alongside without touching the
   root package.

## Task board

State as of 2026-09-27, origin/main 965293a. Merges are squash merges done
on the maintainer's instruction, each at the reviewed head
(`--match-head-commit`) with CI green; older per-round detail is in git
history of this file.

| ID | Issue | Branch / head | PR | State | Notes / blocking reason |
| --- | --- | --- | --- | --- | --- |
| W0a | #105 design | docs/propose-ego-arch-003 @a88282c | #125 | merged (69f78f6) | IMPL-2 carry-over (loader.go + rules.Package name field) is in the design |
| W0b | #99 fix | fix/99-prestart-assertion-audit @2585811 | #127 | merged (ffc5cd8) | root cause still unproven |
| W0b2 | #99 follow-up | fix/99-prestart-audit-followup @bf11ea8 | #136 | merged (0896c42) | approved; CI green 7/7 after rerun of example/cluster (golangci-lint download HTTP 500) | 4 PreStart files; must land before S3-2; draft #99 narrowing comment comes with it |
| W0b3 | #99 narrowing comment | — | — | done | posted as approved (issuecomment-5856624928) |
| W0c | #101 | — | — | done | closed 2026-09-27 with evidence comment |
| W0d | #112 slice 2 | test/112-async-waits-slice2 @9d76483 | #129 | merged (3b80ad3) | root pkg 423s → 360s |
| W0e | #10 body | — | — | done | applied after updatedAt check |
| W1a | #123 design | docs/propose-ego-arch-002-s3 @3f3b725 | #128 | merged (e729b1b) | names and criterion 1 decided; #123 criterion 1 reworded |
| W1a-1 | #123 S3-1 | feat/123-s3-1-port-behavior @adc416c | #131 | merged (543da2c) | merged tree vetted against main before merge |
| W1a-2 | #123 S3-2 | feat/123-s3-2-spawn-bridge @1dfa090 | #139 | merged (9084b80) | apidiff: 3 additions only; value-type RED hang reproduced; spawn* functions ready for IMPL-4 guard |
| W1a-3 | #123 S3-3 | feat/123-s3-3-spawn-methods @c478929 | #141 | merged (e86169a) | engine_spawn.go, new cluster test, engine_test.go helper only |
| W1a-4 | #123 S3-4 | feat/123-s3-4-behavior-kind @0b35ef3 | #143 @2e670f6 | merged (2a6d5a8) — typed-nil kinds accepted; ErrBehaviorNotPointer message amended (3 rules) | from main b4aa140; no release tag until S3-4 merges |
| W1a-5 | #123 S3-5 | feat/123-s3-5-deprecations @6585517 | #144 @98dcf19 | merged (5a5621d); #123 closed | from main 2a6d5a8; IMPL-4 waits for it (engine.go/option.go/example overlap) |
| W1b | #122 | ci/122-publisher-test-closures @24b652b | #130 | merged (965293a) | compat lane via build tag |
| W1b2 | #122 tidy -diff gate | ci/122-nested-tidy-gate @1f8bee9 | #138 | merged (23bc7f4) | all six modules tidy on main; no workflow change |
| W1c-1 | #105 IMPL-1 (#126) | fix/126-engine-lifecycle @74adde4 | #140 | merged (b4aa140); #126 closed | pre-existing Stop-lock and EraseEntity/ProjectionLag issues recorded on #24 (issuecomment-5857989413) |
| W1c-2 | #105 IMPL-2 | feat/105-impl-2-compose-spec @30d70cb | #135 @48e2ccb | merged (27848da) | nested adapters → follow-up on #106 (issuecomment-5856629314) | archcheck 36/156/1/0/0; V5 strict reading (typed nil rejected in optional fields too) and docs/ci.md rule rows (after #133) pending human |
| W1c-3 | #105 IMPL-3 | feat/105-impl-3-lifecycle @dddcea5 | #137 | merged (ab41d3c) | DefaultShutdownTimeout 30s only when unset (open question with #24) |
| W1c-4 | #105 IMPL-4 | feat/105-impl-4-compose-goakt @e4f0ce7 | #145 @dd14c52 | merged (f2b5130); follow-up: two-node cluster test through compose/goakt (issue draft pending approval); open for maintainer: misleading PR comment, V7 design line, G1/family shape, no split, cluster test follow-up | D7 observed: PostStop flush written to store, not delivered to publisher (for #24) | after S3-3, S3-4, IMPL-1. Must include: ctx.Err() check before each start step (approved 2026-09-27); family guard in the unexported spawn* functions; step 2 half-start self-cleanup test; compose.Spec negative ShutdownTimeout candidate |
| W1c-5 | #105 IMPL-5 | docs/105-impl-5-example | — | in progress (from f2b5130) | example/eventssourced via compose/goakt |
| W1c-6 | #148 RUNTIME-005 + #105 IMPL-6 | — | — | planned (issue created) | needs #147 |
| W1c-7 | #146 two-node test | — | — | planned | follow-up of #145 decision (e); issue created |
| W2a | #147 S4 runtime SPI | — | — | issue created | decisions 1–7 approved 2026-09-27 |
| W2a-1 | #147 S4-1 empty baseline | refactor/147-s4-1-logging | — | in progress | internal/logging; remove migration -> ego |
| W2a-D | #147 S4-D design | docs/propose-ego-runtime-001 | — | in progress (docs) | small per-capability interfaces, v4 compatible; aligns with #149 |
| W2b | #106 adapter SPI | docs/propose-ego-arch-004 @a5a67bd | #149 | in review (docs; O1–O7 open) | includes nested adapters -> compose dependency rule |
| W1d | #102 ADR | docs/propose-ego-arch-006 @c6c9463 | #132 | merged (47b2b34) | human said: wait for CI; §5.2 amendment done; D1 migration plan gated on confirmation |
| W3-S0 | #102 S0 selector | ci/102-s0-module-aware-selector @90ccf25 | #133 | merged (33fa052) | implementing R1/R2 (+ -base via merge-base); then CI + review before merge |
| W3-S1 | #102 S1 test/compat | ci/102-s1-test-compat @e6a669e | #142 | merged (658bbae) | approved with nits, CI 8/8; maintainer chose option (c): no reflect/unsafe, per-publisher pre-Start check + identity check in test/compat, ADR note, nits; then review + CI |
| W3-S2/S3 | #102 contracts module, publishers | — | — | planned | gated on F4 (D8) and D1 migration confirmation |
| W4 | #124 | — | — | planned | all earlier items merged; breaking/versioning plan approved |
| — | #134 | — | — | created | publisher module path bug under #39 |

## File ownership

| Task | Files/packages |
| --- | --- |
| W0b | snapshots_writer_actor.go, snapshots_writer_actor_test.go, extension_lookup*.go, CHANGELOG.md, odd/tasks/issue-99-* |
| W0d | root *_test.go with fixed waits (not snapshots/events writer tests), odd/tasks/remove-fixed-waits-112-slice2.md |
| W1a #123 | behavior.go, saga.go, option.go, engine.go, internal/extensions, example/eventssourced, archcheck baseline |
| W1b #122 | publisher/*/ tests and go.mod/go.sum, a CI workflow lane (serialize with other CI work) |
| W1c #105 | new compose/, archcheck rules, engine.go (serialize with W1a) |
| W1d #102 | openspec/changes (new ADR), docs only |

Hot spots: root package `ego` (S3, #105, S4, #124 serialized);
`.github/workflows/` and internal/cmd/ciselect (serialized);
internal/cmd/archcheck (one writer at a time). Maximum three writers.

## Checks for writers

TDD strict (RED before GREEN), runner `env -u GOROOT go test` without
`-race`; archcheck summary must not gain violations; ciselect decides
scope; full root suite once when the root package, contracts, go.mod or
CI change; golangci-lint 0 issues.

## Next step

Collect Wave 0 results, dispatch fresh reviewers for the #99 and #112 PRs,
then start W1b and W1d; W1a waits for the protobuf decision.
