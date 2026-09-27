# Proposal — Physical Go modules and impact-aware CI (EGO-ARCH-006)

| Field | Value |
|---|---|
| Change | `ego-arch-006` |
| Date | 2026-09-27 |
| Phase | `sdd-propose`: proposed (architecture decision record; ADR) |
| Tracker | [`#102`](https://github.com/getsyntegrity/ego/issues/102), parent [`#10`](https://github.com/getsyntegrity/ego/issues/10), CI owner [`#38`](https://github.com/getsyntegrity/ego/issues/38) |
| Baseline | `main` at `77beda6b91646b0e031ce78401ee997fd919bd65` |
| Evidence | [`exploration.md`](./exploration.md) (graphs, proxy resolution, selector behavior, CI runs); [`design.md`](./design.md) (module map, decisions, selector, slices) |
| Builds on | ego-arch-001 (`openspec/changes/ego-arch-001/design.md` §3 rules, §6 module criterion, §8 versioning) |
| Human gates | D1 module identity, D2 nested-module path and layout, D3 first published version (design §3) |
| Related | [`#39`](https://github.com/getsyntegrity/ego/issues/39) release identity, [`#122`](https://github.com/getsyntegrity/ego/issues/122) / [#130](https://github.com/getsyntegrity/ego/pull/130) publisher test closures, [`#123`](https://github.com/getsyntegrity/ego/issues/123) / [#128](https://github.com/getsyntegrity/ego/pull/128) `port/behavior`, [`#105`](https://github.com/getsyntegrity/ego/issues/105) / [#125](https://github.com/getsyntegrity/ego/pull/125) composition root, [`#124`](https://github.com/getsyntegrity/ego/issues/124) final layout, [`#112`](https://github.com/getsyntegrity/ego/issues/112) root test latency |

## Why now

#102 wants Ego's architectural boundaries to become real Go modules, and CI to run only what a change can affect. The package work of ego-arch-001 is done for the publisher contract (S1a, S1b), and #111 made CI verify every nested module. The next step needs three answers first: which boundaries become modules, how CI decides what a change affects when modules depend on each other, and whether the modules can be consumed once they exist. The exploration measured all three on `main`:

1. **Package extraction has gone as far as it can for the publishers.** Their production build compiles no GoAkt. But each publisher still *requires* the root module, so its module graph keeps 173 GoAkt edges and 35 Olric edges (exploration §4.2). Only a module below the root removes them. That is the benefit ego-arch-001 §6 criterion 2 asks for.
2. **The selector cannot follow a module that depends on another module.** It models only nested-module-to-root imports. It treats `go.work` as a root package file instead of a global change. And it misses a new `go.mod` carved out of a root directory (exploration §6). Today every root Go change outside the tooling under `internal/cmd` and `migration` selects all six nested modules.
3. **Nested modules in this repository cannot be consumed at all.** A path like `github.com/pablogore/ego/v4/publisher/kafka` in directory `publisher/kafka` does not resolve from the module proxy, or even directly from the Git repository (exploration §5). Any new module built the same way would inherit that, and a contracts module the root requires would make the root itself unconsumable.

## Intent

Adopt a module topology and a selection rule that satisfy #102 without breaking ego-arch-001 §6:

- **Selector first.** `ciselect` reads every `go.mod`, builds the graph of in-repository requirements, and selects every module that transitively depends on a changed one. It gives a reason chain for each module, forces the full gate on global paths, and treats a new or removed `go.mod` as a boundary change. No list is maintained by hand.
- **Then a few modules, one per pull request, each passing §6:**
  1. an unreleased `test/compat` integration module;
  2. a schema module (`egopb`);
  3. a port module (`port/publishing`, `port/behavior`);
  4. the four publishers switched to require only schema and port.
- **Everything else stays in the root module until #124.** That includes the GoAkt runtime adapter, the root-level contracts and `testkit`. The major release #124 already plans is the natural point for a second import-path change.

This change is documentation only.

## In scope (decisions this proposal closes)

- **Target module map and order** (design §2), with each boundary checked against ego-arch-001 §6, plus the boundaries rejected and why.
- **Selector design** (design §5):
  - module edges from `go.mod` requirements through `go mod edit -json`;
  - the reverse-transitive closure;
  - root-lane seeding through a dependency;
  - global paths, the boundary-change rule, and `plan.json` as the portable plan for #38 and Shipwright;
  - fixtures for the five cases #102 names.
- **Verification rules around `go.work`**: CI never verifies in workspace mode; integrated verification uses `replace`; published verification uses neither (design §4).
- **Wave 3 slices S0–S4**, each with file ownership, checks and a before/after CI measurement plan (design §6), and follow-ups F1–F5.

## Out of scope (MUST NOT in this change)

- Creating, moving or deleting any Go module, package, `go.mod`, workflow or script. S0–S4 do that later, each in its own PR.
- Choosing the module identity, the nested-module path scheme or the first version. Those are D1–D3, left to the maintainers.
- Publishing or tagging anything, and changing `release.yml`. That belongs to #39 and to follow-up F4.
- Root test latency: #112. Design §1 explains why modules do not shorten contract-change feedback.

## Approach and rejected alternatives

Recommended: **selector first, then a few §6-qualified modules, gated on the path decisions** (exploration §10, option 3). Rejected:

- **One module forever, with better selection only.** It cannot prune the publishers' requirement lists, and it leaves #102's "boundaries are real modules" unmet.
- **Many modules now, at today's `…/v4/<dir>` paths.** Every one of them would be unresolvable (exploration §5), and multiplying modules before the selector follows nested-to-nested edges would leave changes unverified.
- **A tools or `migration` module as the first "leaf".** Both fail §6(2): they remove nothing from anyone's requirement list. The first leaf is `test/compat`, which the #122 lane needs anyway, and which exercises the first nested-to-nested edge.

## Decisions needed from the maintainers

Design §3 gives the options and tradeoffs. In short:

| # | Decision | Recommendation | Blocks |
|---|---|---|---|
| D1 | Module identity: keep `github.com/pablogore/ego/v4` or move to `getsyntegrity` | Decide before S2. If a move is planned, do it before the first release (zero tags exist today). | S2–S4 |
| D2 | Nested-module path and layout | Paths without the root's major suffix (`…/ego/port`, `…/ego/publisher/kafka`), tagged `<dir>/vX.Y.Z`. This also makes today's publishers consumable. | S2–S4 |
| D3 | First published version and release order | Follows D1 and #39. Releases become topological: schema and port, then root, then publishers. | S2 consumability, release |
| D4 | `go.work` policy | Generated on demand by a script, not committed. CI always runs with `GOWORK=off`. | F5 only |
| D5 | Allow new *unreleased* integration modules (like `benchmark`) | Yes, provided no released module requires them. | S1 |
| D6 | Confirm that CI speed alone never justifies a module (ego-arch-001 §6(2)) | Confirm. | — |

Without D1–D3, Wave 3 still delivers S0 (the selector) and S1 (the first leaf module and nested-to-nested edge). No module that the root requires is created before D1–D3.

## Affected public consumer surfaces

None in this change. When the slices land:

- **S2 and S3** may change the import paths of `egopb`, `port/publishing` and `port/behavior`, depending on D2. Before the first release that needs no alias. After it, old paths keep aliases until #124, per the window recorded in #128.
- **S4** changes the publishers' `go.mod` requirements and, under D2 (a), their module paths. They cannot be resolved today, so no consumer can depend on the old path.
- **S0 and S1** change only CI tooling and test code.

## Rollback

Revert `openspec/changes/ego-arch-006/`. For the later slices:

- **S0** reverts cleanly, because `modules.json` keeps its shape.
- **S1** reverts by restoring the four `compat_test.go` files.
- **S2–S4** revert cleanly only before a release exposes the new module paths. After that, the modules stay, and only in-repository callers can be reverted.

## Risks

Design §7 has the full list. The main ones:

- **Release and tag scheme.** The current publisher tag scheme cannot be resolved (exploration §5).
- **Version skew.** Integrated verification with `replace` can pass while consumers fail. Mitigation: published verification for every released module (F4).
- **Cross-module `internal/` imports.** archcheck covers only nested-to-root today; S1 generalizes the rule.
- **Import-path churn with #124.** Mitigation: change paths before the first release, or alias until #124.

## Success criteria (acceptance of this ADR, mapped to #102)

- [ ] A normative module map with ownership and allowed dependencies exists, with each module justified against ego-arch-001 §6 (design §2).
- [ ] The current module inventory, import graph, module graph and CI baseline are recorded with run IDs and reproducible commands (exploration §3–§8).
- [ ] The selector design derives selection from the real `go.mod` graph with no manual list, forces the full gate on global paths, detects new modules, and defines fixtures for the leaf, shared-contract, transitive-consumer, new-module and global-change cases (design §5).
- [ ] The `go.work` policy and the verification rules are stated, and every part that depends on an open decision is named (design §3, §4).
- [ ] Wave 3 is sliced one module per PR, selector first, each slice at most five tasks with checks and a CI measurement plan; overflow is named as follow-ups (design §6).
- [ ] No production code, CI file or `go.mod` changes in this change.
