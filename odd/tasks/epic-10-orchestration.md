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
| W0b2 | #99 follow-up | fix/99-prestart-audit-followup | — | in progress | 4 PreStart files; must land before S3-2; draft #99 narrowing comment comes with it |
| W0b3 | #99 narrowing comment | — | — | planned | after W0b2 merges; human approves text |
| W0c | #101 | — | — | done | closed 2026-09-27 with evidence comment |
| W0d | #112 slice 2 | test/112-async-waits-slice2 @9d76483 | #129 | merged (3b80ad3) | root pkg 423s → 360s |
| W0e | #10 body | — | — | done | applied after updatedAt check |
| W1a | #123 design | docs/propose-ego-arch-002-s3 @3f3b725 | #128 | merged (e729b1b) | names and criterion 1 decided; #123 criterion 1 reworded |
| W1a-1 | #123 S3-1 | feat/123-s3-1-port-behavior @adc416c | #131 | merged (543da2c) | merged tree vetted against main before merge |
| W1a-2 | #123 S3-2 | — | — | planned | after W0b2 merges (actor files) |
| W1a-3..5 | #123 S3-3..S3-5 | — | — | planned | S3-3 after S3-2; S3-4 after S3-3; S3-5 last |
| W1b | #122 | ci/122-publisher-test-closures @24b652b | #130 | merged (965293a) | compat lane via build tag |
| W1b2 | #122 tidy -diff gate | — | — | planned | after #133 merges (docs/ci.md, CI serialization) |
| W1c-1 | #105 IMPL-1 (#126) | — | — | planned | after S3-2 (Entity/Saga in engine.go) |
| W1c-2 | #105 IMPL-2 | feat/105-impl-2-compose-spec @30d70cb | #135 | in review | archcheck 36/156/1/0/0; V5 strict reading (typed nil rejected in optional fields too) and docs/ci.md rule rows (after #133) pending human |
| W1c-3 | #105 IMPL-3 | — | — | planned | after IMPL-2 |
| W1c-4 | #105 IMPL-4 | — | — | planned | after S3-2, S3-3, S3-4 and IMPL-2/3 |
| W1d | #102 ADR | docs/propose-ego-arch-006 @c6c9463 | #132 | ready after CI on c6c9463 | human said: wait for CI; §5.2 amendment done; D1 migration plan gated on confirmation |
| W3-S0 | #102 S0 selector | ci/102-s0-module-aware-selector @2e419d8 | #133 | approved with nits (fixing) | implementing R1/R2 (+ -base via merge-base); then CI + review before merge |
| W3-S1 | #102 S1 test/compat | — | — | planned | after #133 merges |
| W3-S2/S3 | #102 contracts module, publishers | — | — | planned | gated on F4 (D8) and D1 migration confirmation |
| W2a | S4 runtime SPI (#11) | — | — | planned | issue draft needs human approval |
| W2b | #106 | — | — | planned | after S3 and #105 IMPL-4 |
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
