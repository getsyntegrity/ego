# Feature: releaseplan, a pure release planner with a dry run on main (#159 A2 / F4, PR-A)

Branch: `feat/159-f4a-releaseplan` · Base: `origin/main` `48dc0a4` · Issue: #159 (A2, F4) · Related: #39, #38, #134

## Problem

The release pipeline that design ego-arch-006 calls F4 does not exist, and S2 is gated on it (design §3, D8 option C). Today `.github/workflows/release.yml` decides the release by hand:

- it hard-codes the root module path (`release.yml:90`, `:146`; `scripts/ci/verify-published.sh:35`);
- it has no notion of release order between modules;
- its `major` branch (`release.yml:126-130`) would compute a `publisher/<name>/v2.0.0` tag, which Go rejects for a module path without a `/v2` suffix.

## What changes

A new command, `internal/cmd/releaseplan`, reads every `go.mod` in the repository and computes, without side effects:

1. **Order.** The release order of the released modules, topologically sorted by their in-repository `require` edges (D3: a module is released after everything it requires). A cycle is an error that names the modules involved.
2. **Paths.** Each module's path, read from its `go.mod`, never hard-coded. So the planner keeps working unchanged when D1 migrates the paths.
3. **Tags.** The next tag of each released module under the approved D2 (a) scheme: the root is tagged `vX.Y.Z` and must match its `/vN` suffix; a nested module is tagged `<dir>/vX.Y.Z` and, without a `/vN` suffix, may only carry v0 or v1. A bump that would produce v2+ on a suffix-less path, or a major that does not match the root's suffix, is refused.

`build.yml` runs it as a dry run on every `main` push and writes the plan to the job summary. It publishes nothing and creates no tag.

"Pure" means the output depends only on the `go.mod` files and the explicit inputs (existing tags, requested bump). It does not call git, the network or the GitHub API. `go mod edit -json` is used to read each `go.mod`, as `internal/cmd/ciselect` does (`main.go:379-391`); it only parses the file.

## Scope and constraints

- **In scope:** `internal/cmd/releaseplan/**` with tests, a dry-run step in `.github/workflows/build.yml`, `docs/ci.md`, this document.
- **Out of scope, PR-B:**
  - checking that `build.yml` passed on the commit before any tag is created — a workflow step, not the planner, because doing it inside the same build's dry run would be circular;
  - changes to `release.yml` and `scripts/ci/verify-published.sh`.
- **Not touched:** module paths (D1 is not confirmed), production code, contracts, `pull_request.yml`. This PR does not unblock S2 on its own: S2 still needs the D1 confirmation and the rest of F4.
- **Which modules are released** is an explicit, reviewable input, not inferred. Today `release.yml` releases the root and `publisher/*`. `benchmark`, `example/cluster` and `test/compat` are never released (D5). A released module that requires an unreleased in-repository module is an error.
- **TDD:** strict (user global configuration); runner `go test`. Never `-race`, never the workbench.
- **RDD:** off (global).
- **Route:** delegated direct (one writer; 2+ non-trivial files).
- **Delivery:** one work-unit commit per task on this branch; push and PR after verification.

## Tasks

- [x] T1 Module discovery and the release graph: read each `go.mod`, keep in-repository requires, topological order, cycle detection, released-requires-unreleased check (tests first).
- [x] T2 Tag scheme: parse existing tags per module prefix, next version for `patch|minor|major`, D2 (a) naming, refusal of v2+ without `/vN` and of a root major that does not match its suffix (tests first).
- [x] T3 CLI: flags for repository root, released-module list, existing tags file and bump; JSON plan plus a markdown summary; non-zero exit on any refusal. Tests on a synthetic repository in `testdata`, plus a run against this repository.
- [x] T4 Dry run in `build.yml` (no publish, no tags) and `docs/ci.md`.

## Checks

- `GOROOT= GOWORK=off GOFLAGS=-mod=mod go test -count=1 ./internal/cmd/releaseplan/...` (no `-race`)
- `go vet` and staticcheck on the new package
- actionlint on `build.yml`
- `go run ./internal/cmd/releaseplan ...` on this repository: order puts the root before the publishers; no refusal for a `patch` bump
- `rg 'pablogore/ego' internal/cmd/releaseplan -g '!*_test.go' -g '!**/testdata/**'` shows only the package's own import paths, never a hard-coded module path used as data

## Progress and evidence

- Created before the first source write.

### T1 — module discovery and the release graph (`fa064c7`)

- Files: `internal/cmd/releaseplan/discover.go`, `order.go`, `release_set.go`,
  their `_test.go` files, and `testdata/{linear-chain,diamond,cycle,
  released-requires-unreleased,missing-listed-dir,tagscheme}`.
- Discovery mirrors `internal/cmd/ciselect/main.go:379-397`'s `readGoMod`
  (`go mod edit -json`, no network/build); an in-repository require is any
  `Require.Path` that equals another discovered module's path — the design
  note "replace directives only to resolve local dirs" turned out
  unnecessary here: every `go.mod` in this repository already declares the
  canonical path in `require` (the `replace` only swaps the resolved
  version for the working tree, `publisher/kafka/go.mod` etc.), so edge
  detection never needs to consult `replace` at all. Recorded as a
  judgement call: simpler and stays exactly "what the module declares it
  needs," independent of a local dev override.
- Cycle detection (`detectCycle`) runs over every discovered module, not
  only the released set, per the feature scope ("a cycle among discovered
  modules is an error naming the cycle").
- `releasedSet` is the released-requires-unreleased and missing-listed-dir
  check; `releaseOrder` is Kahn's algorithm restricted to the released
  subgraph, lexicographic tie-break on directory.
- TDD: implementation files were moved aside, tests written, RED confirmed
  (`undefined: discoverGraph` etc.), restored, GREEN confirmed (9/9 tests).
- Verification: `GOROOT= GOWORK=off GOFLAGS=-mod=mod go test -count=1
  ./internal/cmd/releaseplan/...` → PASS (9 tests). `go vet
  ./internal/cmd/releaseplan/...` → clean.

### T2 — tag scheme (`2039d0b`)

- Files: `internal/cmd/releaseplan/tags.go`, `tags_test.go` (reuses the
  `testdata/tagscheme` fixture from T1: root `example.com/repo/v4`,
  nested `pub` at `example.com/repo/v4/pub` with no suffix).
- **Judgement call — the no-tag baseline.** With no existing tag,
  `noTagBaseline` starts a module at the *largest* major D2 (a) allows
  before a bump would need a path-suffix change: the root's own forced
  major (its path's `/vN`, here 4), or `1` for a suffix-less path (the
  top of the `{0,1}` range). Rejected alternative: starting every module
  at `0.0.0` regardless of path. That alternative is simpler but hides a
  suffix-less module's major-bump refusal for a whole cycle (`0 -> 1` is
  always legal), and it cannot reproduce the required behavior of a bare
  `-bump major` against a truly untagged repository refusing immediately
  — verified below against this repository's own `.` and `publisher/*`
  with zero tags. No override flag was added: seeding `-tags` with a
  starting tag is already a trivial override.
- `nextTag(dir, modPath, tags, bumpKind)` returns the current tag (empty
  if none) and the next version, or an error naming the module directory,
  its path and the refused version.
- TDD: `tags_test.go` written first against not-yet-existing `nextTag`/
  `parseSemver`; RED confirmed (`undefined: nextTag`, `undefined:
  parseSemver`); `tags.go` added; GREEN confirmed (15/15 tests total).
- Verification: `GOROOT= GOWORK=off GOFLAGS=-mod=mod go test -count=1
  ./internal/cmd/releaseplan/...` → PASS (15 tests). `go vet
  ./internal/cmd/releaseplan/...` → clean.

### T3 — CLI, plan.json/summary.md, release-modules.txt (`f184638`)

- Files: `internal/cmd/releaseplan/main.go`, `plan.go`,
  `main_test.go`, `plan_test.go`, `scripts/ci/release-modules.txt`.
- `scripts/ci/release-modules.txt` was read off `.github/workflows/
  release.yml` (not modified): the root (its tag triggers the workflow;
  release.yml never creates it) and every `publisher/*` directory with a
  `go.mod`, discovered there by `for d in publisher/*/; do if [ -f
  "$d/go.mod" ]; ...` — today `kafka`, `nats`, `pulsar`, `websocket`.
  `benchmark`, `example/cluster`, `test/compat` are excluded (D5).
- `buildPlan` order: `detectCycle` (whole graph) → `releasedSet`
  (validates the list, D5) → `releaseOrder` → `nextTag` per module, in
  order, stopping at the first refusal — so a failure is always
  unambiguous about which module to fix first.
- TDD: `plan_test.go`/`main_test.go` written first against not-yet-
  existing `buildPlan`/`run`; RED confirmed (`undefined: buildPlan`,
  `undefined: run`); `plan.go`/`main.go` added; GREEN after fixing one
  test assertion that itself mismatched `json.MarshalIndent`'s spacing
  (`"requires":[]` vs the actual `"requires": []`) — not an
  implementation bug. `gofmt -w` also aligned a struct field in
  `discover.go` (whitespace only, included in this commit).
- Verification:
  - `GOROOT= GOWORK=off GOFLAGS=-mod=mod go test -count=1
    ./internal/cmd/releaseplan/...` → PASS (28 tests, including
    `TestRun_RealRepository` against this actual repository).
  - `go vet ./internal/cmd/releaseplan/...` → clean.
  - `GOROOT= go run honnef.co/go/tools/cmd/staticcheck@latest
    ./internal/cmd/releaseplan/...` → clean (no findings).
  - `rg -n 'pablogore/ego' internal/cmd/releaseplan -g '!*_test.go' -g
    '!**/testdata/**'` → no matches (exit 1): no hard-coded module path
    used as data outside tests/fixtures.
  - Real run, `-bump patch -tags /dev/null -release scripts/ci/
    release-modules.txt`: exit 0, order `.` (→ `v4.0.1`),
    `publisher/kafka` (→ `publisher/kafka/v1.0.1`), `publisher/nats`,
    `publisher/pulsar`, `publisher/websocket` (root first, then every
    publisher, each `Requires: [github.com/pablogore/ego/v4]`).
  - Real run, `-bump major`, same inputs: exit 1,
    `releaseplan: module . (github.com/pablogore/ego/v4): refusing tag
    v5.0.0: major v5 does not match the /v4 suffix of module path
    github.com/pablogore/ego/v4` (root fails first, so `buildPlan`
    stops there per its documented fail-fast order). Isolating a
    publisher's own refusal (temporary debug test, removed after
    capturing the message, tree confirmed clean by `git status`) gives:
    `module publisher/kafka (github.com/pablogore/ego/v4/publisher/kafka):
    refusing tag publisher/kafka/v2.0.0: major v2 requires a /v2 suffix
    in module path github.com/pablogore/ego/v4/publisher/kafka (Go
    modules require v2+ to be suffixed)`.

### T4 — dry run in `build.yml` and `docs/ci.md` (`c7128ec`)

- Files: `.github/workflows/build.yml` (new `release-plan` job, `ci-gate`
  now `needs: [plan, modules, release-plan]`), `docs/ci.md` (new "Release
  plan dry run (releaseplan)" section, plus short cross-references from
  "`build.yml`" and "Version policy").
- `release-plan` has no `needs:` on `plan`/`modules` (runs in parallel,
  never holds back the test/build gate), checks out with `fetch-depth: 0`
  and `fetch-tags: true`, writes `git tag -l` to a temp file, runs
  `releaseplan -bump patch`, appends `summary.md` to
  `$GITHUB_STEP_SUMMARY`, and uploads `plan.json` as the `release-plan`
  artifact. Job-scoped `permissions: contents: read` (no broader token
  than checkout needs).
- **Judgement call — fail `ci-gate` on a broken plan.** Recommended and
  implemented: yes. A dry run nobody has to look at protects nobody;
  making `release-plan` required means a newly nested module missing from
  `scripts/ci/release-modules.txt`, a cycle, or a tag-scheme violation is
  visible on `main` immediately, the same way `plan`/`modules` failures
  already are. Rejected alternative: leave it informational only
  (`if: always()`, never gating) — rejected because that is exactly the
  failure mode `docs/ci.md` already calls out for `modules` before
  `ci-gate` existed (a job whose result nobody is required to look at).
- `internal/cmd/releaseplan` was **not** added to `ciselect`'s
  full-fallback lists (`internal/cmd/ciselect/`, `internal/cmd/
  vulngate/`, …): it is a root package, so `ciselect`'s own root-module
  selection already tests it on any change under
  `internal/cmd/releaseplan/`; no concrete false negative was found that
  would justify forcing full mode for it specifically (unlike `ciselect`/
  `vulngate`, `releaseplan` does not itself decide *what* CI runs, only
  what a release would look like, so a stale selection plan is not a risk
  here the way it would be for those two).
- Verification:
  - `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/
    build.yml'))"` → `YAML OK`.
  - `GOROOT= go run github.com/rhysd/actionlint/cmd/actionlint@latest
    .github/workflows/build.yml` → 0 findings.
  - `bash -n` on the `release-plan` step's run body and the updated
    `ci-gate` run body → both OK.
  - `rg -n '\$\{\{' .github/workflows/build.yml` → every match is inside
    `outputs:`, `with:` or `env:`; none inside a `run:` shell body.
  - Simulated the `release-plan` job's exact commands locally
    (`git tag -l` → 0 tags → `releaseplan -bump patch`) → exit 0, same
    summary as T3's real run (root then the four publishers).
  - `git diff --stat origin/main...HEAD` (`origin/main` = `48dc0a4`, this
    branch's base): only `internal/cmd/releaseplan/**`,
    `.github/workflows/build.yml`, `docs/ci.md`,
    `scripts/ci/release-modules.txt` and this feature document changed —
    `release.yml`, `scripts/ci/verify-published.sh` and every `go.mod`
    outside `testdata/` are untouched.

## Overall verification (all four tasks, final pass)

- `GOROOT= GOWORK=off GOFLAGS=-mod=mod go test -count=1
  ./internal/cmd/releaseplan/... ./internal/cmd/ciselect/...` → PASS (28
  releaseplan tests + ciselect's own suite, all green).
- `go vet ./internal/cmd/releaseplan/...` → clean.
- `GOROOT= go run honnef.co/go/tools/cmd/staticcheck@latest
  ./internal/cmd/releaseplan/...` → clean.
- `actionlint .github/workflows/build.yml` → 0 findings; YAML parses;
  `bash -n` on the two changed run bodies → OK; no `${{ }}` inside any
  `run:` body.
- Real run on this repository, `-bump patch -tags /dev/null -release
  scripts/ci/release-modules.txt`: exit 0, root (`v4.0.1`) ordered before
  all four publishers (each `v1.0.1`, `Requires:
  [github.com/pablogore/ego/v4]`). `-bump major`, same inputs: exit 1,
  refused (root: v4 path suffix vs. requested v5; isolating a publisher
  confirmed its own refusal too: v1 ceiling vs. requested v2, no `/v2`
  suffix).
- `rg -n 'pablogore/ego' internal/cmd/releaseplan -g '!*_test.go' -g
  '!**/testdata/**'` → no matches: no hard-coded module path used as data
  outside tests/fixtures (the package's own `import` lines are excluded
  by `-g '!*_test.go'`'s sibling non-test files, none of which import
  anything under `github.com/pablogore/ego`).
- `git diff --stat origin/main...HEAD` → `release.yml`,
  `scripts/ci/verify-published.sh` and every real `go.mod` untouched;
  only `internal/cmd/releaseplan/**`, `scripts/ci/release-modules.txt`,
  `.github/workflows/build.yml`, `docs/ci.md` and this document changed.
- Commits: T1 `fa064c7`, T2 `2039d0b`, T3 `f184638`, T4 `c7128ec`, all on
  `feat/159-f4a-releaseplan`. Not pushed; no PR opened (per scope — push
  and PR are the user's decision after this report).
