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
risks, chain and file ownership) and five chained specs of at most five tasks each. No production
code, no `CHANGELOG.md` entry (design pull requests #149 and #151 added none).

## Constraints

- Design only; the maintainer answers Q1–Q7 and approves before spec 1.
- No v4 break, no baseline entry, no relaxed archcheck rule (none needed: one rule is added).
- Route: direct inline (documentation; one delegated read-only exploration of the GoAkt actors,
  whose findings were re-verified on `57c4b11` because it read an older checkout).

## Tasks

- [x] **T1** Read #148, #105, #11, #10 and the merged designs (ego-runtime-001, ego-arch-003/004/006/001).
- [x] **T2** Map the GoAkt runtime's observable behavior with `file:line` on `57c4b11` (design §2).
- [x] **T3** Write proposal, design and the five specs.
- [x] **T4** Open the pull request against `main` with the open questions and the spec chain.

## Findings worth keeping

- `eventstream` delivers each message to each subscriber in its own goroutine
  (`eventstream/stream.go:155`), so `Subscribe` and publishers get no ordering guarantee, even
  for one entity, on either runtime.
- A command with no events returns the current state, not nil; `port/runtime`'s doc says nil.
- `EraseEntity` does not delete the encryption key, although its comment says it does.
- `compose/inmem` is not covered by `composition-no-runtime` (its layer excludes runtime-specific
  roots), hence the new `inmem-no-runtime` rule.

## Next step

Maintainer review of the open questions; then spec 1.
