# Feature: design for the in-memory runtime and `compose/inmem` (#148, #105 IMPL-6)

Branch: `docs/propose-ego-runtime-002` · Base: `origin/main` `57c4b11` · Epic: #10 · Parent: #11 · Issue: #148 ·
Design: `openspec/changes/ego-runtime-002`

## Problem

#105's last criterion (one GoAkt composition and one in-memory composition, same domain) has no
runtime to run on: `*ego.Engine` is the only implementation of `port/runtime`. #148 owns the
in-memory runtime (RUNTIME-005) and `compose/inmem` (IMPL-6), but no design existed.

## What changes

Documentation only: `openspec/changes/ego-runtime-002/` with `proposal.md`, `design.md` (the GoAkt
behavior to match, decisions D1–D10, open questions Q1–Q7 with recommendations, compatibility,
risks, chain and file ownership) and eight chained specs (0–7) of at most five tasks each. No production
code, no `CHANGELOG.md` entry (design pull requests #149 and #151 added none).

## Constraints

- Design only; the maintainer answers Q1–Q7 and approves before spec 1.
- No v4 break, no baseline entry, no relaxed archcheck rule (none needed: one rule is added).
- Route: direct inline (documentation; one delegated read-only exploration of the GoAkt actors,
  whose findings were re-verified on `57c4b11` because it read an older checkout).

## Tasks

- [x] **T1** Read #148, #105, #11, #10 and the merged designs (ego-runtime-001, ego-arch-003/004/006/001).
- [x] **T2** Map the GoAkt runtime's observable behavior with `file:line` on `57c4b11` (design §2).
- [x] **T3** Write proposal, design and the specs.
- [x] **T4** Open the pull request against `main` with the open questions and the spec chain (#165, head `3785827`).
- [x] **T6** Apply re-review N1–N9 and the four structural items the maintainer approved on 2026-09-27: spec 0 characterization, Q8 wording, Q9 internal clock, and `EraseEntity` as its own spec 7.
- [x] **T5** Apply the independent review's factual corrections (review of #165) and record the maintainer decisions of 2026-09-27 (passivation implemented, saga delivery order not in the contract, `EraseEntity` crypto-shredding per #166).

## Review round 1 (2026-09-27)

- **What the reviewer found that changed the design:**
  - after a failed write or an out-of-sync conflict, GoAkt stops the entity, so the in-memory runtime removes it too;
  - spawn checks the family before the store;
  - the saga tenant rules SG4, SG5 and SG-DUR1, plus the not-running guard;
  - the `runtimeconsumer` allowlist grows by `persistence` and `egopb`;
  - stream messages are compared as a multiset, with a count-based stop;
  - `Stop` waits for the turn in progress;
  - an option-by-option table for Q2;
  - evidence for the contract mismatches;
  - Q8 added.
- **Maintainer decisions recorded:**
  - passivation is implemented (new spec 4, three tasks, with a manual clock for determinism);
  - saga delivery order is not part of the contract, and a reversed-order run checks that no test depends on it;
  - `EraseEntity` follows #166, which blocks spec 2 (historical: since round 2, #166 blocks only spec 7).
- **Chain:** now six specs (5, 5, 5, 3, 5, 5 tasks).

## Review round 2 (2026-09-27, head `4bccf2e`)

- **N1:** new spec 0 characterizes GoAkt before spec 1: liveness after failures, a command queued behind a failed write, panics under each directive, passivation activity, and the ignored options. Every promise of a later correction is removed.
- **N2/N3 (Q8):** saga delivery order is unspecified on every runtime and promised nowhere. There is no reversed-order hook, and only the comparison fixtures must be order-independent.
- **N4/N7 (Q9):** no public clock. The manual clock is internal to `internal/inmemruntime/clock.go` (ordered by deadline, then registration) and moves to spec 1; spec 5 no longer depends on spec 4.
- **N8:** `EraseEntity` is spec 7, blocked only by #166. Until then it returns `ErrUnsupported`, which is stated as not meeting the contract.
- **N5:** the passivation activity rule and the write-and-publish on passivation are recorded, with the goakt citations.
- **N6:** saga options are measured in spec 0.
- **N9:** the nits are fixed.
- **Chain:** 0 (4 tasks), 1 (5), 2 (5), 3 (5), 4 (3), 5 (5), 6 (4), 7 (3).

## Review round 3 (2026-09-27, head `03c0d35`)

- **M1:** D12 and spec 0 now say that a measurement contradicting a maintainer decision (or a recommendation the maintainer relied on) is recorded in the pull request and the affected rule is marked "blocked on maintainer"; spec 1 then waits.
- **M2:** the passivation measurement uses `d ≥ 1 s`, spaces messages more than 100 ms apart, and asserts elapsed time ≥ `d − 100 ms`. §2.5 and D11 record that GoAkt does not re-check activity at the deadline, while the in-memory runtime does (stricter, within the 100 ms tolerance).
- **m1–m4 and nits:**
  - the ordering risk is reworded;
  - spec 7 adds a CHANGELOG line and a shared-table scenario;
  - the rebase of `port/runtime/runtime.go` is coordinated with FU-E and #166;
  - the public docs point to #166;
  - spec 0 fixes the stale `closure_test.go` comment;
  - this file's historical #166 line is marked.

## Findings worth keeping

- `eventstream` delivers each message to each subscriber in its own goroutine
  (`eventstream/stream.go:155`), so `Subscribe` and publishers get no ordering guarantee, even
  for one entity, on either runtime.
- A command with no events returns the current state, not nil; `port/runtime`'s doc says nil.
- `EraseEntity` does not delete the encryption key, although the port contract and its comment say it does (#166). Keys are selected by persistence ID alone, so two tenants with the same ID would share a key.
- A failed write (and an out-of-sync conflict) stops a GoAkt event-sourced entity: `EntityExists` then reports false.
- `compose/inmem` is not covered by `composition-no-runtime` (its layer excludes runtime-specific
  roots), hence the new `inmem-no-runtime` rule.

## Next step

Maintainer answers to the remaining open questions (Q1, Q3–Q6, placement and relocation in Q2, no-event text in Q7); then spec 0.
