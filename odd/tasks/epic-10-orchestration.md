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

## Task board

| ID | Issue | Branch | Agent | PR | State | Blocking reason |
| --- | --- | --- | --- | --- | --- | --- |
| W0a | #105 design review | — (read-only) | reviewer | #125 | in progress | — |
| W0b | #99 | fix/99-prestart-assertion-audit | writer | — | in progress | — |
| W0c | #101 | — | orchestrator | — | blocked | closing comment needs human approval |
| W0d | #112 slice 2 | test/112-async-waits-slice2 | writer | — | in progress | — |
| W0e | #10 body | — | orchestrator | — | blocked | epic body edit needs human approval |
| W1a | #123 (S3) | — | — | — | planned | protobuf policy decision (design.md §10) |
| W1b | #122 | — | — | — | planned | none (can start after Wave 0 frees a writer slot) |
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
