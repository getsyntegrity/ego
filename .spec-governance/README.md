# EGO SDD Governance (canonical source)

This directory is the **canonical, versioned source** of the Spec-Driven
Development (SDD) governance used by this project's workflow: the phase
lifecycle skills, their shared conventions, and the portable spec-governance
pack that enforces atomicity/evidence rules across phases.

## Ownership direction

```text
EGO .spec-governance/  = source of truth
~/.claude/skills/       = installed/runtime copy
```

Claude Code loads skills from `~/.claude/skills/` at runtime — that copy has
to exist for any agent/session to actually use these skills. But it is a
**global, unversioned, per-machine install**, not protected by this repo. It
is never the authority.

**Never edit the installed copy (`~/.claude/skills/...`) as the canonical
source.** A change made only there is invisible to every other clone/agent
and disappears the moment the machine's home directory changes. Changes to
SDD governance MUST originate in `.spec-governance/skills/` here, then be
synchronized outward to `~/.claude/skills/`.

## Syncing outward (until an installer exists)

There is no automated installer yet. Until one exists, sync manually after
any change to a file in `.spec-governance/skills/`:

```bash
rsync -a --exclude='.DS_Store' \
  .spec-governance/skills/ \
  ~/.claude/skills/
```

Before syncing, diff the two trees to make sure nothing drifted the other
way first (an edit made directly on the installed copy, out of band):

```bash
diff -rq --exclude='.DS_Store' .spec-governance/skills ~/.claude/skills
```

Any difference other than files intentionally excluded from this package
(see below) means one side changed without the other — treat that as a bug,
not a merge to resolve casually: figure out which side is correct, apply
that content to `.spec-governance/skills/` first, then re-sync outward.

## What's in this package, and why

```text
.spec-governance/
├── README.md
├── VERSION
└── skills/
    ├── _shared/
    │   ├── openspec-convention.md   # OpenSpec file conventions + state.yaml Discipline + State Hygiene Gate
    │   ├── sdd-phase-common.md      # shared phase protocol (skill loading, retrieval, persistence, gates)
    │   └── sdd-status-contract.md   # status schema consumed by sdd-apply/sdd-verify/sdd-archive
    ├── spec-governance/             # atomicity decision process (ATOMIC / REVIEW_REQUIRED / SPLIT_REQUIRED)
    ├── spec-splitting/              # mechanics for turning SPLIT_REQUIRED into child specs
    ├── spec-authoring/              # per-section authoring rules a spec-shaped artifact must follow
    ├── spec-evidence/               # PROVEN / NOT_PROVEN / BLOCKED / NOT_APPLICABLE evidence vocabulary
    ├── sdd-explore/
    ├── sdd-propose/
    ├── sdd-spec/
    ├── sdd-design/
    ├── sdd-tasks/
    ├── sdd-apply/
    ├── sdd-verify/
    └── sdd-archive/
```

Every skill and `_shared` file here is included because something in this
package actually depends on it (checked by grepping each `SKILL.md` for
`_shared/*.md` references, and each `_shared` file for references to the
others). Nothing was copied just because it existed in `~/.claude/skills/`.

**Deliberately NOT included**, and why:

- `sdd-init`, `sdd-onboard`, `sdd-research` — bootstrap, teaching-walkthrough,
  and optional-evidence-gathering skills. None of the lifecycle skills above
  depend on them, and they sit outside the explore→archive loop this package
  governs.
- `_shared/persistence-contract.md`, `_shared/engram-convention.md`,
  `_shared/sdd-orchestrator-workflow.md`, `_shared/sdd-orchestrator-sections.md`,
  `_shared/review-ledger-contract*.md`, `_shared/research-lifecycle.md`,
  `_shared/skill-resolver.md`, `_shared/README.md` — part of the native
  `gentle-ai` orchestrator/dispatcher layer or other skill families
  (`sdd-init`, `sdd-research`, `judgment-day`, `skill-registry`), not
  referenced by any file in this package.

If a future change to a lifecycle skill adds a real dependency on one of
these, bring that file into `.spec-governance/skills/_shared/` at the same
time — don't let a reference point outside this package.

## state.yaml governance (summary)

The rules that matter most for keeping `openspec/changes/*/state.yaml` from
absorbing narrative live in `skills/_shared/openspec-convention.md`:

- `state.yaml` is a **constrained workflow manifest** — a field belongs only
  if it's needed to resume, route, gate, locate, or close the workflow.
- A **Semantic Allowlist** (identity/tracking/execution/phases/decisions/
  constraints) and a **Denylist** (`revision_note`, `rationale`, `findings`,
  `implementation_notes`, ... and their semantic equivalents under any other
  field name) define what is and isn't a valid field.
- The **State Hygiene Gate** is mandatory: a phase MUST NOT be marked
  complete if its `state.yaml` write fails the gate's 10 checks.

## Do not confuse with product artifacts

This package governs *how the SDD skills behave*. It has no opinion on any
specific change's content — `openspec/changes/*/proposal.md`, `design.md`,
`state.yaml`, etc. are product artifacts governed BY these skills, not part
of this package.
