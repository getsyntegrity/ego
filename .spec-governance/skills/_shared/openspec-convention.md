# OpenSpec File Convention (shared across all SDD skills)

## Directory Structure

```
openspec/
├── config.yaml              <- Project-specific SDD config
├── specs/                   <- Source of truth (main specs)
│   └── {domain}/
│       └── spec.md
└── changes/                 <- Active changes
    ├── archive/             <- Completed changes (YYYY-MM-DD-{change-name}/)
    └── {change-name}/       <- Active change folder
        ├── state.yaml       <- DAG state (survives compaction)
        ├── exploration.md   <- (optional) from sdd-explore
        ├── research.md      <- (optional until selected) source-backed evidence
        ├── proposal.md      <- from sdd-propose
        ├── specs/           <- from sdd-spec
        │   └── {domain}/
        │       └── spec.md  <- Delta spec
        ├── design.md        <- from sdd-design
        ├── tasks.md         <- from sdd-tasks (updated by sdd-apply)
        └── verify-report.md <- from sdd-verify
```

## state.yaml Discipline

**`state.yaml` is a constrained workflow manifest.** If information is not required to resume, route, gate, locate, or close the SDD workflow, it does not belong in `state.yaml`. It is workflow metadata, not an architectural artifact — a state machine (phase status/dates), an artifact index (paths/topic keys), and decision/gate IDs — never a second `proposal.md` or `design.md`. Every phase skill that writes to it (Section C in `sdd-phase-common.md`) MUST enforce this boundary at write time, the same way `sdd-propose`'s Content Boundary governs `proposal.md`.

Git history, PR discussion, and artifact contents ARE the revision history. `state.yaml` MUST NOT preserve editing history — no before/after line counts, no rewrite rationale, no changelog entries. Any field that does not map to a category in the Semantic Allowlist below requires a concrete workflow reason (which of resume/route/gate/locate/close it serves), not an assumption that it might be useful later.

### Semantic Allowlist

Every `state.yaml` field must map to one of these categories:

- **Identity** — change name, created/closed dates, status.
- **Tracking** — tracker issue/epic/dependency/related links.
- **Execution** — execution_mode, delivery_strategy, review_budget, chain_strategy.
- **Phases** — per-phase status/date/artifact path/source/blocked_by/engram_topics.
- **Decisions** — decision ID, compact outcome/topic slug, source pointer, required_by/consuming phase.
- **Constraints/Gates** — compact gate ID, required state, the phase that consumes it.

### Denylist

The following field names — or their semantic equivalent under any other name — MUST NOT appear in `state.yaml`: `revision_note`, `revision_history`, `history`, `change_log`, `rationale`, `reasoning`, `explanation`, `findings`, `key_findings`, `lessons_learned`, `implementation_notes`, `design_notes`, `proposal_summary`, `exploration_summary`, `design_summary`, `tasks_summary`.

This is a semantic denylist, not a lexical one. Renaming the field does not make it valid — `metadata.note: "..."` containing narrative prose is exactly as invalid as `revision_note:` containing the same prose. If a field's value could be pasted into a commit message or PR comment without losing meaning, it's revision history or rationale, not workflow metadata — remove it.

```yaml
# INVALID — a changelog entry, not workflow metadata, regardless of the field name
phases:
  proposal:
    revision_note: "230->93 lines, 2026-09-13; see PR #47 review for rationale"
```
```yaml
# VALID — the same fact belongs in git/PR history; state.yaml only tracks status
phases:
  proposal:
    status: done
    date: 2026-09-13
    artifact: openspec/changes/{change}/proposal.md
```

`state.yaml` MAY contain:
- Change metadata, tracker references, session/execution settings.
- Phase status and dates, per phase.
- Artifact paths and engram topic keys (dependency/status metadata).
- Compact decision IDs, compact open-decision IDs, and compact gate/constraint IDs — each an `id: slug` pair, optionally with a `source:` pointer into the phase file that carries the actual rationale (e.g. `source: exploration.md#13`).

`state.yaml` MUST NOT contain:
- Architectural rationale or research evidence.
- Copied sections from `exploration.md`, `proposal.md`, or `design.md`.
- Long decision explanations — a paragraph justifying a decision belongs in the phase artifact that made it, referenced by ID.
- Implementation design (contracts, signatures, algorithms, tooling choices) — that's `design.md`'s job, and `state.yaml` MUST NOT assert a specific technique/tool is chosen when the owning artifact only names the requirement (e.g. record `required: import-graph` / `exact_tooling: deferred-to-design`, never assert `go/packages` or `go list -deps` was picked, if the phase artifact hasn't picked one).
- Revision-history prose (why a draft was rewritten, the before/after line count) beyond a one-line pointer to where that discussion lives (a PR, a commit).
- Duplicated acceptance criteria, or any explanation that belongs in another artifact.
- A duplicate list of "open decisions" once the phase that resolves them is `done` — either the decisions are closed (move to `closed_decisions`) or they carry forward under a name that reflects the next consumer phase (e.g. `open_decisions_for_design`), never both an old and a new copy of the same list.

**Reference format**: `state.yaml` carries the ID; the artifact carries the rationale.

```yaml
# state.yaml
closed_decisions:
  S3: resolve-once-propagate-after
```
```markdown
# proposal.md
S3 adopts the propagation split established in exploration.md §5.2.
```

A `state.yaml` entry that restates "resolution and propagation are distinct because context.Context cannot cross a GoAkt cluster relocation..." is a violation even at four YAML lines with one long string — **brevity by reference, not by minification**. A short block that inlines a paragraph is still bloat; a short block that points at the artifact section is not.

**Size budget**: soft target ≤100 lines, warning above 120 — secondary to semantic ownership, not a target to game. Numeric size is a symptom check, not the rule itself: a `state.yaml` under 100 lines can still fail this discipline (a short `revision_note` is still a violation), and passing the Semantic Allowlist/Denylist above matters more than the line count. When checking size, also look for what line count alone misses — multiline (`|`/`>`) blocks that aren't config context, a single scalar value long enough to be a summary sentence, or narrative embedded inside an otherwise-compact structure. If `state.yaml` is over budget, find and move the narrative that doesn't belong there; do not respond to the budget by compressing prose into denser strings.

**Anti-duplication**: before writing rationale into any artifact (including `state.yaml`), check whether it already exists in an earlier phase file. If it does: cite it (`file#section`), optionally summarize in ≤1 sentence when needed for local comprehension, and never re-paste the original paragraphs. This applies phase-to-phase too (`design.md` citing `exploration.md`, `tasks.md` citing `design.md`), not only to `state.yaml`.

### Future decision shape (documented now, not migrated)

New changes SHOULD prefer one stable `open_decisions` key over a phase-specific name, with the consuming phase named explicitly:

```yaml
open_decisions:
  R1:
    topic: tenant-id-normalization-policy
    required_by: design
```

This replaces phase-specific key names (`open_decisions_for_propose`, `open_decisions_for_design`, ...) going forward. Do not retroactively migrate existing `state.yaml` files to this shape — that would touch a shipped OpenSpec artifact for a naming preference alone. Apply it the next time a change's `state.yaml` is written or substantially revised.

## State Hygiene Gate (MANDATORY)

Run this gate before any phase completes a write to `state.yaml` (Section C, `sdd-phase-common.md`). It is a completion gate, not a recommendation: **failure of the State Hygiene Gate blocks completion of the current SDD phase** — fix the `state.yaml` write and re-run the gate, do not complete the phase with a known violation.

For every field being added or changed in `state.yaml`, confirm ALL of:

1. It exists to resume, route, gate, locate, or close the workflow — name which one.
2. It is not revision history, rationale, findings, or implementation prose — checked against the Denylist above by meaning, not just by name.
3. It does not copy content that already exists verbatim (or near-verbatim) in an earlier phase artifact.
4. A closed decision is recorded as `ID: compact-outcome-slug` (+ optional `source:` pointer) — never as a paragraph.
5. An open decision is recorded as a neutral topic slug (never the recommended answer) plus the phase that will resolve it — never pre-judging the outcome.
6. No recommendation is recorded as a closed decision — status reflects what's actually decided, not what's proposed.
7. No artifact-editing history (line counts, rewrite reasons, "why this changed") is recorded — that belongs to git/PR history.
8. No field contains multiline narrative prose unless it is itself workflow metadata.
9. No scalar value is a summary sentence of an artifact — reference the artifact instead.
10. The current phase's status is coherent with the open/closed decision lists — e.g. an open decision `required_by` a phase already marked `done` is a contradiction; resolve it before completing.

**Limitation**: this gate is process discipline the executing agent applies at write time — no CLI/schema currently validates `state.yaml` against this contract mechanically. Treat repeated violations as a signal that mechanical enforcement (a validator/schema check) is due, not as a reason to relax the gate.

## Artifact Ownership

Each phase file owns a distinct layer; none re-derives what an earlier one already established (see Anti-duplication above). **One fact, one canonical home**: `state.yaml` never duplicates a fact that already lives in an artifact or in git/PR history — it points at it (`source: exploration.md#13`, "see PR #47") when a pointer is useful, or omits it entirely when the fact isn't needed to resume/route/gate/locate/close the workflow.

| Artifact | Owns | Does NOT own |
|---|---|---|
| `exploration.md` | Evidence, repo findings, alternatives considered, trade-offs, path:line citations, open questions | Committing to a decision |
| `proposal.md` | Problem statement, chosen/closed decisions (stated + ≤1-line rationale citing explore), scope/non-scope, consequences, risks, still-open decisions, links back to explore | Re-deriving explore's evidence; exact contracts/signatures |
| `design.md` | Exact API/contract shapes, invariants, error model, wiring, propagation mechanics, conformance mechanism, the specific algorithm/tooling/library for any mechanism | Re-copying explore's research or re-arguing proposal's decisions |
| `tasks.md` | Implementable work units, dependencies, ordering, the acceptance/gates needed to mark work done | Repeating design's rationale or contract detail — reference it, don't restate it |
| `state.yaml` | State, artifact references, IDs, gates, execution metadata only | Any of the above — see `state.yaml Discipline` |

## Artifact File Paths

| Skill | Creates / Reads | Path |
|-------|----------------|------|
| orchestrator | Creates/Updates | `openspec/changes/{change-name}/state.yaml` |
| sdd-init | Creates | `openspec/config.yaml`, `openspec/specs/`, `openspec/changes/`, `openspec/changes/archive/` |
| sdd-explore | Creates (optional) | `openspec/changes/{change-name}/exploration.md` |
| sdd-research | Creates (when selected) | `openspec/changes/{change-name}/research.md` |
| sdd-propose | Creates | `openspec/changes/{change-name}/proposal.md` |
| sdd-spec | Creates | `openspec/changes/{change-name}/specs/{domain}/spec.md` |
| sdd-design | Creates | `openspec/changes/{change-name}/design.md` |
| sdd-tasks | Creates | `openspec/changes/{change-name}/tasks.md` |
| sdd-apply | Updates | `openspec/changes/{change-name}/tasks.md` (marks `[x]`) |
| sdd-verify | Creates | `openspec/changes/{change-name}/verify-report.md` |
| sdd-archive | Moves | `openspec/changes/{change-name}/` → `openspec/changes/archive/YYYY-MM-DD-{change-name}/` |
| sdd-archive | Updates | `openspec/specs/{domain}/spec.md` (merges deltas into main specs) |

## Reading Artifacts

```
Proposal:   openspec/changes/{change-name}/proposal.md
Specs:      openspec/changes/{change-name}/specs/  (all domain subdirectories)
Design:     openspec/changes/{change-name}/design.md
Tasks:      openspec/changes/{change-name}/tasks.md
Verify:     openspec/changes/{change-name}/verify-report.md
Config:     openspec/config.yaml
Main specs: openspec/specs/{domain}/spec.md
```

`research.md` contains exact `gentle-ai.sdd-research/v1` bytes. Hybrid pre-proposal state uses `gentle-ai.sdd-preproposal/v1`; compare its revision and bytes with Engram before readiness and never prefer one store after mismatch.

## Writing Rules

- Always create the change directory before writing artifacts
- If a file already exists, READ it first and UPDATE it (don't overwrite blindly)
- If the change directory already exists with artifacts, the change is being CONTINUED
- Use `openspec/config.yaml` `rules` section for project-specific constraints per phase

## Delta Spec Sections

Delta specs MAY include these sections:

```markdown
## ADDED Requirements
## MODIFIED Requirements
## REMOVED Requirements
## RENAMED Requirements
```

- `ADDED` appends new requirements to the main spec.
- `MODIFIED` replaces the full matching requirement block in the main spec. The delta MUST contain the entire updated requirement, including unchanged scenarios that must be preserved.
- `REMOVED` deletes the matching requirement from the main spec. Each removed requirement MUST include `(Reason: ...)` and SHOULD include `(Migration: ...)` when consumers or persisted behavior are affected.
- `RENAMED` changes a requirement heading/name without changing behavior unless the delta also includes a `MODIFIED` block for the new requirement. Each rename MUST state old and new names explicitly.

## Config File Reference

```yaml
# openspec/config.yaml
schema: spec-driven

context: |
  Tech stack: {detected}
  Architecture: {detected}
  Testing: {detected}
  Style: {detected}

rules:
  proposal:
    - Include rollback plan for risky changes
  specs:
    - Use Given/When/Then for scenarios
    - Use RFC 2119 keywords (MUST, SHALL, SHOULD, MAY)
  design:
    - Include sequence diagrams for complex flows
    - Document architecture decisions with rationale
  tasks:
    - Group by phase, use hierarchical numbering
    - Keep tasks completable in one session
  apply:
    guidelines:
      - Follow existing code patterns
    tdd: false           # Set to true to enable RED-GREEN-REFACTOR
    test_command: ""
  verify:
    test_command: ""
    build_command: ""
    coverage_threshold: 0
  archive:
    - Warn before merging destructive deltas
```

## Archive Structure

When archiving, the change folder moves to:
```
openspec/changes/archive/YYYY-MM-DD-{change-name}/
```

Use today's date in ISO format. The archive is an AUDIT TRAIL — never delete or modify archived changes.
