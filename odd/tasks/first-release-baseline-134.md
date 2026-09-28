# Feature: v4.0.0 as the first root release, and the releaseplan bug that hid it (#134)

Branch: `fix/134-first-release-baseline` · Base: `origin/main` `3021f63` · Issue: #134

## Problem

No tag has ever existed for `github.com/getsyntegrity/ego/v4`. The four publishers
(`publisher/{kafka,nats,pulsar,websocket}`, suffix-less paths, independent v0/v1 per
ADR D2(a)) still pin the root at `v4.4.3` — a number inherited from upstream
`tochemey/ego`'s own real release — only so the release flow's
`prepare-publisher-bump` step has something to rewrite once the root tag exists.

Separately, `internal/cmd/releaseplan/tags.go` has a bug: `noTagBaseline` documents
that a `/vN` module path "starts at vN.0.0", but `nextTag` always applies the
requested bump on top of that synthetic baseline. With no tag at all, that makes
the root's next tag v4.0.1 (patch), v4.1.0 (minor), or a refusal (major, since v5
would need a `/v5` path). The one version that actually is legal and intended —
v4.0.0 itself — is unreachable. `build.yml`'s `release-plan` dry-run job (`-bump
patch`, `git tag -l`) therefore advertises v4.0.1 to anyone reading the job summary.

## What changes

- **Decision (recorded here for review):** the first root tag is **v4.0.0**, not
  v4.0.1 or v4.4.x/v4.5.x. Continuing the upstream v4.4.x/v4.5.x lineage would be
  SemVer-dishonest, because `CHANGELOG.md`'s `[Unreleased]` section already carries
  breaking changes against upstream v4.4.3 (module path, `migration.New` signature,
  the removed logger seam) — and a v5 bump is blocked by D1 (root path must stay
  `/v4`). Upstream tags do not constrain the choice: the module path distinguishes
  the artifacts, and Go, the proxy and the checksum database key every version by
  module path (maintainer decision, 2026-09-28; this first draft wrongly argued
  that upstream numbers were "taken"). v4.0.1 would imply a v4.0.0 release that never happened.
  So numbering restarts under the new module identity at v4.0.0; publishers start
  their own v0.x line the same way (first tag `publisher/<name>/v0.1.0`).
- **Code fix:** `nextTag` in `internal/cmd/releaseplan/tags.go` special-cases the
  untagged-and-suffixed case: for any bump kind that is itself valid (patch, minor
  or major), the result is exactly `vN.0.0`, never a bump on top of it. An invalid
  bump kind is still an error. Suffix-less modules (the publishers) and any module
  that already has a matching tag are unaffected.
- **Docs:** a `CHANGELOG.md` note under `[Unreleased]` recording the v4.0.0 decision,
  and a fix to whichever release docs currently describe (or would now misdescribe)
  releaseplan's first-release behavior.

## Why

- The bug is a real defect independent of the numbering decision: even if the first
  release had been chosen as v4.0.1, the current code would only reach it by luck,
  and reaching v4.1.0 or refusing major are both wrong for a module with no tag yet.
- **Rejected alternative:** keep "bump on top of the synthetic baseline" and only
  special-case `-bump major` (since that is the one that currently errors). Rejected
  because the patch/minor cases would keep silently advertising v4.0.1 / v4.1.0 on
  `main`'s dry run, i.e. a version nobody decided, which is the actual complaint;
  special-casing only the error path would leave the misleading summary in place.

## Scope and constraints

- **In scope:** `internal/cmd/releaseplan/tags.go` and its doc comments,
  `internal/cmd/releaseplan/tags_test.go` (and any other test/golden file in the
  package asserting the old v4.0.1 baseline), `CHANGELOG.md`, and release docs that
  describe this behavior.
- **Out of scope:** `go.mod` version pins (untouched, per instructions — the release
  flow rewrites the publisher pins after the root tag exists), actually creating or
  pushing any tag, the release workflow's own logic beyond what the dry-run docs
  describe, and any other releaseplan behavior once a tag already exists.
- **TDD:** strict (user global configuration); runner `go test`. Never `-race`,
  never the workbench.
- **RDD:** off (global).
- **Delivery:** two work-unit commits (fix, then docs) on the feature branch; well
  under the ~400-line heuristic.

## Tasks

- [x] T1 — RED then GREEN: `nextTag` computes `vN.0.0` for every valid bump kind on
  an untagged `/vN` module; suffix-less modules and tagged modules unchanged; an
  invalid bump kind still errors. Route: delegated direct writer (source + test
  together).
- [x] T2 — Docs: `CHANGELOG.md` entry recording v4.0.0 as the first root release,
  and any release doc correction. Route: delegated direct writer.

## Acceptance criteria

- `go test -count=1 ./internal/cmd/releaseplan/...` passes.
- `go vet ./internal/cmd/releaseplan/...` passes; `gofmt -l internal/cmd/releaseplan`
  is empty.
- The local reproduction of `build.yml`'s `release-plan` dry-run step, run with an
  empty tags file, reports the root's next tag as `v4.0.0` (not `v4.0.1`).
- `rg -n '4\.0\.1'` inside `internal/cmd/releaseplan` finds nothing left over from
  the old expectation.

## Progress

### T1 — done

- `internal/cmd/releaseplan/tags_test.go`: replaced
  `TestNextTag_NoExistingTags_PatchSucceeds` and
  `TestNextTag_NoExistingTags_MajorRefusesRootAllowsV1` with
  `TestNextTag_NoExistingTags_SuffixedModuleGetsBaselineForEveryBumpKind` (root,
  patch/minor/major all → `4.0.0`, current tag empty) and
  `TestNextTag_NoExistingTags_SuffixlessModuleBumpsFromZero` (pub, unchanged:
  `0.0.1`/`0.1.0`/`1.0.0`). The old major-refusal assertion for the untagged root no
  longer holds under the new rule (untagged + suffixed + major is now `4.0.0`, not a
  refusal), so that test could not just be patched — removed the now-unused
  `strings` import along with it. `TestNextTag_UnknownBumpKind` already covers "an
  invalid bump kind still errors" for exactly this untagged-root case, so no new
  test was needed for that.
- `rg -n '4\.0\.1' internal/cmd/releaseplan` found only `tags_test.go` before the
  fix, but three more tests encoded the *same* now-wrong rule (untagged + major ⇒
  refusal) without using the literal string, so the search alone would have missed
  them — found instead by running the full package suite after the `tags.go` fix
  and reading each new failure:
  - `TestBuildPlan_MajorRefusal` (`plan_test.go`) called `buildPlan(g, [".",
    "pub"], nil, "major")` — nil tags, so both root and pub were untagged; under
    the fix that now succeeds (root → `4.0.0`, pub → `1.0.0`), so the refusal it
    asserted no longer happens. Changed it to seed an existing tag `v4.0.0`, so
    `-bump major` still asks for the disallowed `v5.0.0`.
  - `TestRun_RefusalIsNonZero` (`main_test.go`) had the identical problem (empty
    tags file, `-bump major` against `testdata/tagscheme`); fixed the same way
    (seeded `v4.0.0`).
  - `TestRun_RealRepository`'s "major bump refuses root and every publisher"
    subtest ran against the real repo's own `go.mod` paths with an empty tags
    file — same issue. Renamed it to "major bump refuses an already-tagged root"
    and seeded `v4.0.0` (the real root path also ends in `/v4`), and added a new
    sibling subtest, "major bump from no tags gives the root its first release,
    v4.0.0", asserting `plan.json`'s root `nextTag` is exactly `v4.0.0` — this is
    the positive case the whole fix exists for, so it gets its own explicit
    integration-level assertion rather than only living in `tags_test.go`.
- RED (before the `tags.go` fix, full package run):
  ```
  --- FAIL: TestNextTag_NoExistingTags_SuffixedModuleGetsBaselineForEveryBumpKind (0.01s)
      --- FAIL: .../patch (0.00s)
          tags_test.go:55: next = 4.0.1, want 4.0.0
      --- FAIL: .../minor (0.00s)
          tags_test.go:55: next = 4.1.0, want 4.0.0
      --- FAIL: .../major (0.00s)
          tags_test.go:49: nextTag("major"): module . (example.com/repo/v4): refusing tag v5.0.0: major v5 does not match the /v4 suffix of module path example.com/repo/v4
  FAIL
  ```
  (captured before `plan_test.go`/`main_test.go` were touched, so it isolates the
  `tags.go` defect itself; `plan_test.go`/`main_test.go` were edited afterward,
  purely to stop encoding the old rule, not to test new behavior beyond the one
  new subtest above.)
- GREEN: `tags.go`'s `nextTag` now short-circuits to the `/vN` baseline itself
  (skipping the bump math, after still validating the bump kind through
  `bumpVersion`) when the module has no tag and its path carries a `/vN` suffix.
  Updated the `noTagBaseline`/`nextTag` doc comments with the new rule and a
  "Rejected:" note for the alternative considered (special-case only `-bump major`).
  `go test -count=1 -v ./internal/cmd/releaseplan/...`: all pass (verified full
  verbose output, no skips). `go vet ./internal/cmd/releaseplan/...`: clean.
  `gofmt -l internal/cmd/releaseplan`: empty. `go build ./...`: clean.
- Local dry-run reproduction of `build.yml`'s exact `release-plan` job invocation
  (`go run ./internal/cmd/releaseplan -repo-root . -release
  scripts/ci/release-modules.txt -tags <empty file> -bump patch -out-dir <tmp>`),
  run twice (`-bump patch` and `-bump minor`) against an empty tags file (real repo,
  real `scripts/ci/release-modules.txt`): root reports `v4.0.0` in both runs; each
  publisher reports `publisher/<name>/v0.0.1` (patch run) and
  `publisher/<name>/v0.1.0` (minor run) — matching the "first publisher tags
  `publisher/<name>/v0.1.0`" expectation for `bump=minor`.
- Commit: `fix(releaseplan): plan vN.0.0 as the first release of an untagged /vN module (#134)`.

### T2 — done

- `CHANGELOG.md`: added one new bullet under `[Unreleased]` → `💥 Breaking
  Changes`, directly after the existing #134 module-path-move bullet, recording
  that the first release will be v4.0.0, why (breaking changes already listed
  above it, D1 forbidding a `/v5` path; the "upstream numbers are taken" argument
  originally recorded here was withdrawn, see above), that `[v4.4.3]` and everything below it is upstream `tochemey/ego`
  history, and that each publisher starts its own line at
  `publisher/<name>/v0.1.0`. `git diff CHANGELOG.md` is a pure two-line addition
  (`1 file changed, 2 insertions(+)`) — no existing entry touched.
- Release docs: `rg -n 'v4\.0\.1|noTagBaseline|first release|releaseplan' docs
  openspec/changes/ego-arch-006 readme.md contributing.md` found no file
  asserting the old, now-wrong "untagged + major always refuses" rule or the
  v4.0.1 baseline. `docs/ci.md`'s "Release plan dry run (releaseplan)" section
  describes the job mechanically (what it runs, what it uploads) without
  naming a specific computed version, so it was not wrong and was left as is.
  The `ego-arch-006` design/proposal mentions of "the first release" are D1
  decision history (migrate the module path before the first release) and were
  left untouched, per scope. No file needed a correction beyond the CHANGELOG
  entry.
- Commit: `docs(release): record v4.0.0 as the first root release (#134)`.

## Next step

None outstanding: the numbering decision, the releaseplan fix, and the docs
recording it are all in place and verified. The actual root tag is still not
created (explicitly out of scope here).
