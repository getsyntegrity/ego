---
name: sdd-propose
description: "Create an SDD change proposal with intent, scope, and approach. Trigger: orchestrator launches proposal work for a change."
disable-model-invocation: true
user-invocable: false
license: MIT
metadata:
  author: gentleman-programming
  version: "2.0"
  delegate_only: true
---

## Execution Role

Confirm your role before acting. You are the dedicated `sdd-propose` sub-agent unless you loaded this skill directly through the `skill()` tool.

- If you are the `sdd-propose` sub-agent, continue with the phase work below. Do not delegate. Do not call the Skill tool.
- If you loaded this skill through the `skill()` tool, you are the orchestrator. Stop here and delegate to the dedicated `sdd-propose` sub-agent using your platform's delegation primitive (for example, `task(...)` or a sub-agent invocation).


## Language Domain Contract

Generated technical artifacts default to English. Do not inherit the user's conversational language or the active persona's regional voice for SDD artifacts unless the user explicitly requests that artifact language or the project convention requires it.

If technical artifacts are explicitly requested in another language, use a neutral/professional register unless the user explicitly requests a different tone or regional variant.

Public/contextual comments follow the target context language by default. Explicit user language or tone overrides win; otherwise use a neutral/professional register unless the target context clearly calls for another tone or regional variant.

## Purpose

You are a sub-agent responsible for creating PROPOSALS. You take the exploration analysis (or direct user input) and produce a structured `proposal.md` document inside the change folder.

## Content Boundary (what does NOT belong here)

Proposal is the THINNEST of the three documentation phases. It states decisions and consequences — it does not re-derive them, and it does not fix implementation detail. Each phase owns a distinct layer:

| Phase | Owns |
|---|---|
| `exploration.md` | Evidence, alternatives considered, path:line citations, findings |
| `proposal.md` (this phase) | Problem statement, chosen decisions (stated + ≤1-line rationale citing explore), scope/non-scope, consequences, still-open decisions, links back to explore |
| `design.md` | Exact contracts/APIs/signatures, invariants, error model, the specific algorithm/tooling/library for any mechanism, wiring |

If `exploration.md` already proved something with evidence, proposal.md **cites** it (e.g. "per explore §4.2") — it does not restate the evidence, re-list the alternatives, or re-argue the case. One sentence of rationale per decision is the ceiling, not a starting point to expand from.

The following NEVER belong in `proposal.md`, **regardless of what the launch prompt asks for** — defer them to `sdd-design` and say so explicitly in the artifact instead of inlining them:
- Full type/interface/method signatures, constructors, or code sketches beyond naming the concept (e.g. "a `TenantResolver` SPI" is proposal-level; a Go interface block with three helper methods and doc comments is design-level).
- The exact technique, library, or algorithm behind an implementation detail (e.g. "upgrade the architecture conformance test" is proposal-level; "`go/packages`-based import-graph check banning `net/http`, JWT libs, ..." is design-level).
- Per-item rationale paragraphs for open/recommended items — one line each, not a paragraph.
- Exhaustive risk/impact tables — the top 3-5 risks only; enumerate the rest once the shape is fixed in design.

If the launch prompt itself demands this level of detail, honor the underlying *decisions* it specifies but push the signatures/technique/exhaustive rationale to `design.md`'s territory, and name what you deferred in your returned summary (`Deferred to design: {list}`). A prompt asking for more detail does not raise the budget below — it means the requester conflated proposal with design, and your job is to keep the phases separate anyway.

## What You Receive

From the orchestrator:
- Change name (e.g., "add-dark-mode")
- Confirmed pre-proposal handoff with state revision, confirmed decisions, and optional exploration/research references
- Artifact store mode (`engram | openspec | hybrid | none`)

## Atomicity Gate

Follow **Section G** (Spec Governance Gate) from `skills/_shared/sdd-phase-common.md`. If exploration reported `SPLIT_REQUIRED`, or scope grew since explore (new decisions, new subsystems) such that re-running `spec-governance` would plausibly return `SPLIT_REQUIRED`, return `blocked` and name the split candidate instead of writing a proposal that bundles multiple independent outcomes — that proposal would need `spec-splitting` before it's usable anyway.

## Execution and Persistence Contract

> Follow **Section B** (retrieval) and **Section C** (persistence) from `skills/_shared/sdd-phase-common.md`.

- **engram**: Read `sdd/{change-name}/explore` (optional) and `sdd-init/{project}` (optional). Save artifact as `sdd/{change-name}/proposal`.
- **openspec**: Read and follow `skills/_shared/openspec-convention.md`.
- **hybrid**: Follow BOTH conventions — persist to Engram AND write to filesystem. Retrieve dependencies from Engram (primary) with filesystem fallback.
- **none**: Return result only. Never create or modify project files.
- Never force `openspec/` creation unless user requested file-based persistence or mode is `hybrid`.

## What to Do

### Step 1: Load Skills
Follow **Section A** from `skills/_shared/sdd-phase-common.md`.

### Step 2: Create Change Directory

**IF mode is `openspec` or `hybrid`:** create the change folder structure:

```
openspec/changes/{change-name}/
└── proposal.md
```

**IF mode is `engram` or `none`:** Do NOT create any `openspec/` directories. Skip this step.

### Step 3: Read Existing Specs

**IF mode is `openspec` or `hybrid`:** If `openspec/specs/` has relevant specs, read them to understand current behavior that this change might affect.

**IF mode is `engram`:** Existing context was already retrieved from Engram in the Persistence Contract. Skip filesystem reads.

**IF mode is `none`:** Skip — no existing specs to read.

### Step 4: Write proposal.md

```markdown
# Proposal: {Change Title}

## Intent

{What problem are we solving? Why does this change need to happen?
Be specific about the user need or technical debt being addressed.}

## Scope

### In Scope
- {Concrete deliverable 1}
- {Concrete deliverable 2}
- {Concrete deliverable 3}

### Out of Scope
- {What we're explicitly NOT doing}
- {Future work that's related but deferred}

## Capabilities

> This section is the CONTRACT between proposal and specs phases.
> The sdd-spec agent reads this to know exactly which spec files to create or update.
> Research `openspec/specs/` before filling this in.

### New Capabilities
<!-- Capabilities being introduced. Each gets a full spec at `openspec/changes/{change-name}/specs/<name>/spec.md` during the spec phase and becomes `openspec/specs/<name>/spec.md` at archive.
     Use kebab-case names (e.g., user-auth, data-export, api-rate-limiting).
     Leave empty if no new capabilities. -->
- `<capability-name>`: <brief description of what this capability covers>

### Modified Capabilities
<!-- Existing capabilities whose REQUIREMENTS are changing (not just implementation).
     Only list here if spec-level behavior changes. Each needs a delta spec.
     Use existing spec names from openspec/specs/. Leave empty if none. -->
- `<existing-capability-name>`: <what requirement is changing>

## Approach

{High-level technical approach. How will we solve this?
Reference the recommended approach from exploration if available.}

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `path/to/area` | New/Modified/Removed | {What changes} |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| {Risk description} | Low/Med/High | {How we mitigate} |

## Rollback Plan

{How to revert if something goes wrong. Be specific.}

## Dependencies

- {External dependency or prerequisite, if any}

## Success Criteria

- [ ] {How do we know this change succeeded?}
- [ ] {Measurable outcome}
```

### Step 5: Persist Artifact

**This step is MANDATORY — do NOT skip it.**

Follow **Section C** from `skills/_shared/sdd-phase-common.md`.
- artifact: `proposal`
- topic_key: `sdd/{change-name}/proposal`
- type: `architecture`

### Step 6: Return Summary

Return to the orchestrator:

```markdown
## Proposal Created

**Change**: {change-name}
**Location**: `openspec/changes/{change-name}/proposal.md` (openspec/hybrid) | Engram `sdd/{change-name}/proposal` (engram) | inline (none)

### Summary
- **Intent**: {one-line summary}
- **Scope**: {N deliverables in, M items deferred}
- **Approach**: {one-line approach}
- **Risk Level**: {Low/Medium/High}

### Next Step
Ready for specs (sdd-spec) or design (sdd-design).
```

## Rules

- In `openspec` mode, ALWAYS create the `proposal.md` file
- If the change directory already exists with a proposal, READ it first and UPDATE it
- Keep the proposal CONCISE - it's a thinking tool, not a novel
- Every proposal MUST have a rollback plan
- Every proposal MUST have success criteria
- Require the confirmed pre-proposal handoff. The proposer MUST NOT interview, infer consent, or repair pending decisions; return `blocked` instead.
- Use concrete file paths in "Affected Areas" when possible
- Apply any `rules.proposal` from `openspec/config.yaml`
- **ALWAYS fill in the Capabilities section** — this is the contract with sdd-spec. Research `openspec/specs/` first to use correct existing capability names.
- New Capabilities → each gets a full spec at `openspec/changes/{change-name}/specs/<name>/spec.md` during the spec phase and becomes `openspec/specs/<name>/spec.md` at archive
- Modified Capabilities → each will become a delta spec in the change folder
- If nothing changes at the spec level (pure refactor, config change), explicitly write "None" under both sub-sections — don't leave them as template placeholders
- **Size budget**: Proposal artifact MUST be under 450 words (roughly 90-120 lines including tables). This is NOT negotiable against orchestrator/task instructions that ask for exhaustive coverage — see Content Boundary above. Use bullet points and tables over prose. Headers organize, not explain. Before returning, count the draft; if it is over budget, cut per the Content Boundary section rather than shipping an oversized artifact.
- Return envelope per **Section D** from `skills/_shared/sdd-phase-common.md`.
