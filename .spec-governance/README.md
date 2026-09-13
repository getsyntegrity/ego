# EGO SDD Governance (canonical source)

This directory is the **canonical, versioned source** of the Spec-Driven
Development (SDD) governance used by this project's workflow: the phase
lifecycle skills, their shared conventions, and the portable spec-governance
pack that enforces atomicity/evidence rules across phases.

## Ownership direction

> **`.spec-governance/skills/` in this repo is the single source of truth
> for EGO's SDD governance. `~/.claude/skills/` is nothing more than an
> installation of it — a consumer, never an authority.**

```text
EGO .spec-governance/skills/  = source of truth   (versioned, reviewed, this repo)
~/.claude/skills/              = installed copy    (unversioned, per-machine, disposable)
```

Claude Code loads skills from `~/.claude/skills/` at runtime — that copy has
to exist for any agent/session to actually use these skills. But it is a
**global, unversioned, per-machine install**, not protected by this repo. It
is never the authority. If the installed copy were deleted right now,
nothing would be lost — it would just need re-syncing from here. If this
repo's copy were lost without a backup, the installed copy is the only
place the governance would survive; that is the failure mode this
canonicalization exists to prevent.

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

## Self-check

`scripts/check-self-contained.sh` verifies the package doesn't quietly
depend on anything outside itself:

```bash
.spec-governance/scripts/check-self-contained.sh
```

It checks two things, both by inspecting only this package's own tree:

1. No file under `skills/` hardcodes `~/.claude` or an absolute `/Users/...`
   path — the exact failure mode fixed in `sdd-phase-common.md` §G before
   this package was first published (it used to say `~/.claude/skills/spec-*`
   instead of describing the spec-* skills as siblings in the package).
2. Every `_shared/<file>.md` a skill in this package references actually
   exists under `skills/_shared/` here — so a skill can never silently lean
   on a `_shared` file this package chose not to include.

Run it after any change that adds a skill, adds a cross-reference, or adds
a new `_shared` file — it does not know what *should* be included (see
"Deliberately NOT included" below for that judgment call), only whether
what's already wired together is actually self-contained.

## Version history

- **2.0.0** (this change) — first canonical, portable release of this
  package. Replaces an incomplete pre-porting bundle that held only 4
  `spec-*` skills with stale placeholder text ("not yet created", unchecked
  boxes) and no `sdd-*` lifecycle skills or `_shared` conventions at all.
  Bumped as a major version because the package's scope and contract
  changed completely — a consumer of the old `1.0.0` bundle (4 skills, no
  lifecycle, no ownership rule) cannot assume anything about this one
  without re-reading it.
- **1.0.0** — original ad-hoc copy of `spec-governance`/`spec-splitting`/
  `spec-authoring`/`spec-evidence`, predating this README and the
  source-of-truth rule above.

## What's in this package, and why

```text
.spec-governance/
├── README.md
├── VERSION
├── scripts/
│   └── check-self-contained.sh  # self-check: no leaks to global paths or missing _shared deps
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
