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
5. Pace: two or three active agents; critical path #125 → #123 → S4 (#11)
   → #102 extraction. #122, #101 and #112 run alongside without touching the
   root package.

## Task board

| ID | Issue | Branch | Agent | PR | State | Blocking reason |
| --- | --- | --- | --- | --- | --- | --- |
| W0a | #105 design review | docs/propose-ego-arch-003 @cfa614c | reviewer | #125 | in review | @678c8cf: maintainer's a98eb37 (composition-no-runtime rule) + alignment with #128 per decision 6; cross-consistency review running. #128 review finding 2: #125 puts the family guard in public methods and assumes #123 removes the embed; #128 keeps the embed, guards in unexported spawn*, and §12 needs S3-4 before IMPL-4 — reconcile before merge (human decision). Earlier: approved after 3 rounds (landing order #123 S3-2 → IMPL-4; citations engine.go deps 692/925/1326, spawn 699/932/1332); CI build pass, modules skipped |
| W0b | #99 | fix/99-prestart-assertion-audit @2585811 | writer | #127 | in review | reviewer running; audit left 4 files with the same pattern (projection_actor.go, event_sourced_actor.go, events_janitor_actor.go, durable_state_actor.go) for a follow-up; root cause still unproven; local golangci-lint broken on clean main (Go 1.27 stdlib) |
| W0c | #101 | — | orchestrator | — | done | closed 2026-09-27 with evidence comment (issuecomment-5851642665) |
| W0d | #112 slice 2 | test/112-async-waits-slice2 @9d76483 | writer | #129 | in review | saga_test.go + durable_state_actor_test.go (88 waits); root pkg 423s → 360s; reviewer running |
| W0e | #10 body | — | orchestrator | — | done | applied 2026-09-27 01:32 UTC after updatedAt check; only "Issues hijos" (+#11, PR refs) and two criteria ticks changed, rest byte-identical |
| W1a | #123 (S3) design | docs/propose-ego-arch-002-s3 @c36e866 (S3-4 before IMPL-4 added) (reviewer approved at 20142c2; last 2-line nit delta checked by orchestrator) | design writer | #128 | in review | reviewer: approve with nits (C1–C8 pass; spike proves no v4 break; GoAkt Inject panic pre-existing on main). Fixes 1,3,4,5-text,6,8,9 sent to writer; findings 2 (S3-4 vs IMPL-4, #125 consistency) and 7 (package name) wait for human; then human review before implementation |
| W1b | #122 | ci/122-publisher-test-closures @5393faa | writer | #130 | in review | compat tag lane in verify-module.sh; GoAkt/root gone from unit-test closure in 4 publishers; reviewer running; also edits ego-arch-001 design.md (outside ownership, overlaps #128) |
| W1c | #105 impl | — | — | — | planned | #125 merge; serialize with W1a on engine.go/option.go |
| W1d | #102 explore/ADR | — | — | — | planned | none for exploration; module path + first version before extraction |
| W2a | S4 runtime SPI (#11) | — | — | — | planned | issue draft needs human approval |
| W2b | #106 | — | — | — | planned | #105 design + S3 merged |
| W3 | #102 modules | — | — | — | planned | #102 ADR approved; module path/version decisions |
| W4 | #124 | — | — | — | planned | all earlier items merged; breaking/versioning plan approved |

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
