# CI: the PR fast lane, coverage, and race policy

This repository has two GitHub Actions workflows that run the Go test
suite: `.github/workflows/pull_request.yml`, which runs on every pull
request, and `.github/workflows/build.yml`, which runs on every push to
`main` (and can also be triggered by hand, via `workflow_dispatch`).

Until this change, both workflows ran the *entire* module under the race
detector, one package at a time (`-p 1`), through a third-party tool called
[`go-acc`](https://github.com/ory/go-acc). A one-line change to a leaf
package paid the same ~13 minutes as a change to `go.mod`, and `go-acc`'s
`--ignore` filter matched by substring, so `--ignore test` silently
excluded `./testkit` — a real, public package — from both test execution
and coverage, because `test` is a substring of `testkit`.

This document explains what changed: a small Go program,
`internal/cmd/ciselect`, decides which packages a pull request actually
needs to test; the push-to-`main` workflow keeps testing everything, with
native `go test` coverage instead of `go-acc`; and both lanes keep the
race detector.

## What each workflow runs

### `pull_request.yml` (the fast lane)

1. Checkout, Go setup, module cache, `go mod tidy && go mod vendor`,
   `golangci-lint` — unchanged.
2. **Determine changed files**: `git diff --name-only --no-renames
   "$BASE_SHA...$HEAD_SHA"` between the PR's base and head commits, written
   to `$RUNNER_TEMP/changed.txt`, and `git merge-base "$BASE_SHA"
   "$HEAD_SHA"` (the revision that three-dot diff starts from), written to
   `$RUNNER_TEMP/base.txt`.
3. **Select packages**: `go run ./internal/cmd/ciselect -changed
   "$RUNNER_TEMP/changed.txt" -base "$(cat "$RUNNER_TEMP/base.txt")"
   -out-dir "$RUNNER_TEMP/ci"` (`-base` is explained in "Module selection
   rules" below). If that command
   fails for any reason (a `go list` error, an unreadable file, a bug in
   the selector itself), the workflow logs a `::warning::` and re-runs with
   `-all -reason "selector failed; full-suite fallback"` instead — if
   *that* also fails, the job fails. Selection is never silently skipped.
   The selector's own `summary.md` is appended to the job's
   `$GITHUB_STEP_SUMMARY`, so the exact package list and the reason for it
   are visible on every PR run, not just inferred from logs.
4. **Run tests**: `scripts/ci/go-test.sh "$RUNNER_TEMP/ci" coverage.out`,
   with `GO_TEST_RACE=1` (the race detector stays on for pull requests).
5. **Coverage summary**: when `coverage.out` was produced, the workflow runs
   `go tool cover -func=coverage.out`, takes its final `total:` line, and
   appends it to the job's `$GITHUB_STEP_SUMMARY` together with the
   selection mode. In `affected` mode that total only reflects the
   packages that actually ran — the denominator (`-coverpkg`) is still
   every included package, so an `affected`-mode total is not directly
   comparable to a `full`-mode total. When the mode is `none`, no
   `coverage.out` exists and the summary says so in one line.

### `build.yml` (the full-suite gate)

Runs on every push to `main`, and can be triggered manually for any ref
via the Actions "Run workflow" button (`workflow_dispatch`). It runs
`go run ./internal/cmd/ciselect -all -out-dir "$RUNNER_TEMP/ci"` — always
the full suite, no change detection — appends the summary to the job
summary the same way, then `scripts/ci/go-test.sh` with the race detector
on, and appends the same coverage summary to the job summary. This is the
mandatory gate and always runs the complete suite.

### `scripts/ci/go-test.sh`

Both workflows run tests through the same script, so the only difference
between the fast lane and the full-suite gate is *which* packages
`ciselect` selected — not how they are tested. It reads `<out-dir>/mode`;
if `mode` is `none` it prints a notice and exits 0 (nothing to test). Else
it reads `<out-dir>/packages.txt` (the packages to test) and
`<out-dir>/coverpkg` (the coverage denominator — see below) and runs:

```bash
go test -timeout 30m -covermode=atomic -coverpkg="$(cat coverpkg)" \
  -coverprofile=coverage.out [-race] $(cat packages.txt)
```

`-race` is controlled by `GO_TEST_RACE` (default `1`); `GO_TEST_RACE=0`
lets the script run locally without the race detector, per this
repository's local-testing rule (`-race` is never run locally). `-mod`
comes from `GOFLAGS` (CI sets `GOFLAGS=-mod=vendor`); a local run without
`vendor/` can leave it unset. The script fails closed: if `mode` is not
`none` but `packages.txt` is empty, it exits non-zero rather than silently
testing nothing.

`make docker-test` runs the same two steps (`ciselect -all`, then
`go-test.sh`) inside the CI Docker image, replacing the old
`grep -v -E "(egopb|test|example|mocks)"` substring filter — which had the
same `testkit` bug as `go-acc`'s `--ignore`.

## How the selector decides (`internal/cmd/ciselect`)

The selector loads the module's package graph with `go list -e -json
./...`, then classifies every changed file into exactly one of five
buckets, checked in this order:

| Classification  | Matches                                                                                                                                                                                   | Effect |
|-----------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------|
| Full-fallback    | Exact files `go.mod`, `go.sum`, `Makefile`, `Dockerfile.ci`, `.golangci.yml`, `buf.yaml`, `buf.gen.yaml`; directories `.github/`, `protos/`, `internal/cmd/ciselect/`, `scripts/ci/`, `egopb/`; and any `.go` file directly in the module root (the shared root package) | Forces mode `full` |
| Satellite        | A directory that has its own `go.mod` on disk (`benchmark/`, `example/cluster/`, `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`)                          | Selects nothing in the root lane; selects that module in the nested module lane |
| No-test          | Any `*.md` file, `openspec/`, `.spec-governance/`, `assets/`, `LICENSE`, `renovate.json`                                                                                                    | Selects nothing for that file |
| Package          | A file whose directory is exactly a package's `Dir` (a file under a `testdata/` directory maps to the nearest ancestor package)                                                             | Adds that package to the changed set |
| Unknown          | Anything else                                                                                                                                                                                | Forces mode `full`, with the offending path in the reason |

Every path in `.github/` forces `full`, including `.github/CODEOWNERS`
(which has no file extension and would otherwise fall through to
"unknown" — the `.github/` directory rule catches it first).

### Reverse-dependency expansion

Once every changed file is classified, the selector has a set of changed
*packages* (`C`). It builds the reverse of the module's non-test import
graph and computes:

- `R` = `C`, plus every package whose non-test build (`go list`'s
  `Imports` field) transitively imports something in `C`.
- `affected` = `R`, plus every package `P` whose `TestImports` or
  `XTestImports` directly names a package in `R` — a test binary links its
  test imports' transitive dependencies, and `R` is already closed over
  those, so one extra pass over the direct test-import edges is enough.

This is why, for example, changing `internal/pause/pause.go` selects both
`internal/pause` and the root package `github.com/pablogore/ego/v4`: no
non-test file in the root package imports `internal/pause` — only the
root package's `_test.go` files do — so `internal/pause` never appears in
`R` via the build graph, but the root package is still correctly pulled in
through the `TestImports` step.

### Exclusions are whole path segments, not substrings

The selected-and-covered "included" package set is every module package
minus four excluded path segments: `egopb` (generated protobuf), `example`
(sample programs, and the `example/cluster` satellite module underneath
it), `mocks` (generated mockery output), and `test` (shared fixtures and
generated protobuf under `test/`). Matching is by whole path segment: a
package under `test/...` is excluded, but `testkit` is not, because
`testkit` is not the segment `test`. This is the deliberate fix for the
bug `go-acc --ignore test` had: it matched `test` as a *substring*, so it
silently dropped `./testkit` from both test execution and coverage. This
package list is always the coverage denominator (`coverpkg`), in every
selection mode, so coverage numbers stay comparable between a `full` run
on `main` and an `affected` run on a PR.

### Modes and fail-safe rules

- **`full`**: `-all` was passed, a full-fallback or unknown path changed,
  no changed files were detected at all, or the computed affected set
  happens to equal every included package.
- **`affected`**: a proper, non-empty, non-total subset of the included
  packages.
- **`none`**: every changed file was documentation/governance or belonged
  to a satellite module — nothing needs testing.
- **Fail-safe**: if package files did change but the affected set,
  intersected with the included packages, comes out empty (for example,
  only an excluded, unimported package changed), the selector does not
  report `none` — it falls back to `full`, with that reasoning recorded in
  `summary.md`. `none` is reserved for changes that are provably
  documentation/governance or satellite-only.

## Architecture boundary check

Both workflows run `go run ./internal/cmd/archcheck` right after dependencies are installed and before the linter, so a broken layer boundary fails the run in seconds instead of after the test suite. It enforces the dependency rules of the ego-arch-001 ADR (`openspec/changes/ego-arch-001/design.md` §3), tracked by [#107](https://github.com/getsyntegrity/ego/issues/107).

The tool reads the import graph in two ways:

- **Root module:** `go list -e -json ./...`, which gives each package's production imports with build constraints resolved. A package that fails to load fails the check (fail closed).
- **Nested modules** (`publisher/*`, `benchmark`, `example/cluster`): every non-test `.go` file is parsed in imports-only mode with `go/parser`. No module download or network is needed, which keeps the step at well under a second.

Each rule applies to one layer and checks the direct import edges of every package in it. Contract layers use a closed allowlist, and every allowed target is itself runtime-free, so a transitive path to GoAkt cannot open without adding a new direct edge that the check sees. `rules.Evaluate` counts how many packages each rule's layer actually matched, and fails the whole run — naming the empty rule and its layer — if any rule matched zero packages: that is almost always a sign the root module path or a layer definition is wrong, not that the layer is genuinely empty, since a check that matches nothing passes vacuously instead of catching anything.

**Build-constraint coverage.** The root module is loaded via `go list`, which resolves build constraints (`//go:build` tags, `_linux.go`-style suffixes) for the CI host's own `GOOS`/`GOARCH` only; a file restricted to another platform is not part of the graph `go list` reports, so an import that only exists on a platform the CI host does not build for is not checked. Nested modules are parsed with `go/parser` directly, ignoring build constraints entirely, so a file's imports are read regardless of which platform it is restricted to; this can only over-report a nested module's imports, never miss one, so it stays fail-safe in the direction that matters for this check.

| Rule | Applies to | Constraint |
|---|---|---|
| `contract-allowlist` | `tenancy`, `command`, `persistence` (except `persistence/conformance`, which is test support), `offsetstore`, `projection`, `eventstream`, `encryption`, `eventadapter`, everything under `port/` | Only stdlib, other contract packages, `egopb`, `google.golang.org/protobuf/...`, `internal/queue`, `internal/syncmap`, `github.com/google/uuid`, `go.uber.org/atomic` — and stdlib itself excludes `net/http`, `net/rpc`, `database/sql` and everything under them |
| `application-no-runtime` | `migration` | Must not import package `ego`, `internal/extensions` or GoAkt |
| `external-adapter-no-runtime` | nested modules under `publisher/` | Must not import package `ego` or GoAkt |
| `no-cross-module-internal` | every nested module | Must not import root-module `internal/...` |

A failure names the importer, the forbidden import and the rule, for example:

```text
github.com/pablogore/ego/v4/tenancy imports github.com/tochemey/goakt/v4/actor: rule contract-allowlist (design.md §3): ...
```

The fix is almost always to depend on a contract package instead of the runtime. Do not add a baseline entry to silence a new violation.

**Stdlib transport and database packages are forbidden in contracts.** `contract-allowlist` also denies `net/http`, `net/rpc` and `database/sql`, and everything under them, matched by whole path segment (`net/http/httptest` is forbidden; a hypothetical `net/httpx` would not be). gRPC and other third-party transports are already excluded by the closed allowlist; this stdlib denylist closes the remaining gap, and the rest of the standard library — including `net` itself, for value types such as `net.IP` — stays allowed. It applies to direct imports only: the standard library is not a closed set the way the allowlist's third-party targets are, so a transitive path such as `expvar` importing `net/http` internally is possible and is not enforced (design.md §3).

### Baseline: known violations that can only shrink

Violations that cannot be fixed yet are listed in `internal/cmd/archcheck/baseline.go`. Every entry must name an owner, a justification and a removal criterion, or the tool refuses to run. An entry that no longer matches a real violation fails the check as **stale**, so the entry has to be deleted in the same change that fixes the violation. The baseline can shrink, but nothing can quietly stay in it after its violation is gone.

The baseline started with five entries. S1b removed the four publishers importing package `ego` once #111 verified nested modules in CI; they now import `port/publishing`. One entry remains: `migration` importing package `ego` (removed by S3/S4, #103 and #11).

### Adding a layer or changing a rule

1. Declare the layer in `internal/cmd/archcheck/rules/layers.go`: a function taking the root module path and returning a `Layer` with a name and a `Match` function over the package's import path and kind (root or nested module). A new contract package only needs its path added to `contractRoots`.
2. Add the rule to `DefaultRules(rootModulePath string)` in `internal/cmd/archcheck/rules/rules.go`, with an ID, a description, the source (ADR section or issue), allowlist or denylist semantics and, for a denylist rule, a `Reason` func naming the specific forbidden prefix it matched.
3. Add unit tests in `internal/cmd/archcheck/rules/evaluate_test.go`: one graph that breaks the rule and one that satisfies it.
4. Run `go run ./internal/cmd/archcheck` locally. If existing code violates the new rule and cannot be fixed in the same change, add baseline entries with owner, justification and removal criterion, and update the ADR if the rule is normative.

A rule whose layer matches no package fails the check, so a stale module path or a half-finished rename cannot pass vacuously. The flip side: when a change legitimately empties a layer (for example, deleting `migration` or moving the publishers out), remove or retarget its rule in the same change, or CI fails.

The per-package architecture tests (`tenancy_architecture_test.go`, `command_architecture_test.go`, `port/publishing`) stay. They are stricter than the general contract rule (for example, `tenancy` is stdlib-only), and `logger_architecture_test.go` enforces a different policy by scanning source.

## Coverage policy

Coverage is always produced by native `go test -covermode=atomic
-coverpkg=<all included packages> -coverprofile=coverage.out`, never
`go-acc`. `go-acc` is removed from both workflows and from `make
docker-test`. The coverage denominator (`-coverpkg`) is always the full
included package set, in every mode, so a PR's `affected`-mode coverage
number and `main`'s `full`-mode coverage number are directly comparable —
only the numerator (which packages actually ran) differs.

There is no external coverage service. Codecov was removed in #108: it was
inherited from the upstream project this repo was forked from
(`tochemey/ego`), the maintainers do not use it, and every upload was
silently rejected before #108 for lack of a `CODECOV_TOKEN` secret ("Token
required - not valid tokenless upload"). `codecov.yml`, the `codecov/codecov-action`
step, the token check, and the README badge are gone.

Instead, both workflows publish the coverage total to the job itself. A
`Coverage summary` step runs `go tool cover -func=coverage.out` and appends
its final `total:` line to `$GITHUB_STEP_SUMMARY`, alongside the selection
mode, on every run that actually tests something. `build.yml` always
reports a `full`-mode total. `pull_request.yml` reports whatever mode
`ciselect` picked; an `affected`-mode total only reflects the packages
that ran, so it is not directly comparable to a `full`-mode total, even
though the denominator (`-coverpkg`) is the same full package set in both.
A `none`-mode PR run has no `coverage.out`, and the summary says so in one
line instead of running `go tool cover`.

The Go module and build caches come from `actions/setup-go`'s built-in cache
(keyed on `go.sum`). A separate `actions/cache` step over the same paths was
removed: it re-extracted ~700 MB on top of setup-go's restore and failed with
tar "Cannot open: File exists" on every run.

## Race policy

Both lanes run `-race` (`GO_TEST_RACE=1` by default in CI). Measured
locally (`go test -count=1 ./...`, no race, ~585s total, root package
582.5s), the race detector added roughly 2% wall-clock overhead versus a
race-run of the same suite in CI (593.7s of 609.7s in a `-race -p 1` CI
run — see the baseline table below). That overhead is cheap enough, and
the race detector's coverage is valuable enough, that it stays on in both
lanes. This repository's own local rule (never run `-race` locally) is
unaffected — CI runs it, contributors do not need to.

## `-p 1` removed

The previous commands passed `go test ... -p 1`, forcing every package to
build and run strictly one at a time. Nothing in this module's tests needs
that: there are no fixed ports, no shared containers, no shared files, and
no commit message or comment recorded a reason for it. `scripts/ci/go-test.sh`
runs `go test` with Go's normal default parallelism instead.

## `-timeout 30m`

The previous commands passed `-timeout 0` (no timeout). `go test`'s
default per-test-binary timeout is 10 minutes, which is too tight for this
module: the root package alone measured ~594s (~9m54s) under race in CI
(see the baseline below), close enough to the 10-minute default that a
slightly slower CI runner would time it out. `scripts/ci/go-test.sh` uses
an explicit `-timeout 30m` instead of no timeout at all, so a genuine hang
still fails the build instead of running forever, while leaving headroom
over the measured root-package time.

## Baseline (measured 2026-09-23)

| Measurement | Value |
|---|---|
| PR job wall-clock (old, full suite, `-race -p 1`, `go-acc`) | 12m27s–13m16s |
| `build.yml` wall-clock (same) | 10m58s–13m16s |
| "Run tests" step share of the PR job | >95% |
| CI run `35868911889` (`-race -p 1`): root package `github.com/pablogore/ego/v4` | 593.7s of 609.7s total (97%) |
| Same run: the other 13 tested packages | 1–3s each |
| Local, no race, `go test -count=1 ./...` | ~585s total, root package 582.5s |
| Race detector overhead (measured) | ~2% |
| `-p 1` justification found in history | none — no fixed ports, containers, shared files or global env in the tests |
| `testkit` in CI before this change | never ran (`go-acc --ignore test` substring bug) |
| `main` branch protection | none (GitHub API returned 404) |
| `vendor/` | gitignored; regenerated in CI with `go mod tidy && go mod vendor` |

## The honest limit of package selection

The root package, `github.com/pablogore/ego/v4`, imports — directly or
through its own test files — nearly every other package in the module.
Almost any Go source change therefore selects the root package, and the
root package alone is 97% of the measured test time. Selecting fewer
packages cannot get a PR that touches the root package (or that triggers a
full-fallback path) under an 8-minute target, because the root package's
own tests already take about 10 minutes. The lever for that target is not
selection — it is sharding the root package's tests across parallel jobs,
which is out of scope for this change and tracked as a follow-up in the
feature document (`odd/tasks/affected-package-fast-lane.md`).

## Nested module CI (#111)

`benchmark/`, `example/cluster/`, `publisher/kafka`, `publisher/nats`,
`publisher/pulsar` and `publisher/websocket` each carry their own `go.mod`
and are not reached by `go list ./...` from the root module, so they sit
outside `internal/cmd/ciselect`'s own package graph entirely (see
"How the selector decides" above). Before #111, a change confined to one
of these directories classified as `Satellite`, `ciselect` reported
`ModeNone`, and no CI job ever built, vetted, linted or tested it — a pull
request that only touched `publisher/kafka` went green without compiling
Kafka. This section describes how that gap is closed: nested modules get
their own, independent selection decision and their own per-module CI
job, on top of the root package lane described above, which is otherwise
unchanged (docs-only changes still select nothing for the root package,
and a leaf change still gets the root's own fast `affected` lane).

### Module selection rules (module graph, #102)

Since #102 (slice S0 of the ego-arch-006 design, proposed in PR #132 as
`openspec/changes/ego-arch-006/design.md`, §5), `ciselect` selects
**modules** from the repository's module graph instead of from import
statements. The problem it solves: the old rule only knew "the root plus
satellites", so a nested module that required another nested module would
never have been selected by a change to it, a `go.work` change did not
force the full gate, and carving a directory out of the root with a new
`go.mod` could leave the root lane at `none`.

**Discovery.** `ciselect` finds every module the same way as before (the
`findSatelliteDirs` walk, skipping `vendor/`, `testdata/`, `odd/`,
`.codegraph/` and a few other directories; the go command ignores
`testdata/` too, so a fixture `go.mod` there is never a module), adds the
root, and reads each module's `go.mod`
with `go mod edit -json`. That only parses the file: no network, no
module download, no build, and no new dependency in the root `go.mod`.
An in-repository requirement becomes an **edge** only when a `replace`
resolves it to that module's own directory in the working tree (every
nested module today requires the root through `replace … => ../` or
`../../`). A requirement on an in-repository module at a published version
with no local `replace` is **pinned**: it does not compile against the
working tree, so it is reported, never followed. Third-party requirements
are ignored. Every `go` subprocess `ciselect` starts runs with
`GOWORK=off`, so a `go.work` can never satisfy an import a `go.mod` does
not declare.

`ciselect` also parses every module's own `.go` files with `go/parser` in
imports-only mode (#111's approach: tests included, build constraints
ignored, `testdata/` and nested modules' files skipped) and keeps the
imports that belong to *another* in-repository module. That import set
never creates an edge; it **filters** one (step 5). Parsing matters here:
`go list` resolves build tags, so it would miss a `//go:build compat`
importer such as the publishers' #130 compatibility tests.

**Algorithm**, in order:

1. **Global check.** If any changed path is global, every module is
   selected and the root lane runs `full`, with the reason
   "global: `<path>` changed". The global paths are `go.work`,
   `go.work.sum`, `.golangci.yml`, `Makefile`, `Dockerfile.ci`,
   `buf.yaml`, `buf.gen.yaml`, and everything under `.github/`,
   `scripts/ci/`, `internal/cmd/ciselect/` and `protos/`. `-all` and an
   empty changed-file list are treated the same way. The root `go.mod`
   and `go.sum` are deliberately **not** global: they send the root lane
   to `full`, and step 5 then selects every module that requires the
   root (today, all six).
2. **Ownership.** Each changed file belongs to the module with the
   longest directory prefix. Root-owned files go through the root
   classifier described in "How the selector decides", unchanged, so the
   package-level fast lane keeps working. Any file in a nested module
   marks that whole module changed ("changed files in `<dir>`"); a nested
   module is always verified whole with `./...`.
3. **Boundary change.** A nested `go.mod` that was **added or deleted**
   marks its **parent** module (the module that would own the directory
   without that `go.mod`) as fully changed, with the reason "module
   boundary changed: `<dir>/go.mod`". When the parent is the root, the
   root lane runs `full`: carving a directory out of the root can break
   root packages that imported it. An **edited** nested `go.mod` (present
   before and after) has no boundary effect: it marks its own module
   changed with a **changed manifest**.

   Telling an edit from an add or a delete needs the base revision, which
   the new optional `-base <rev>` flag supplies. For each changed nested
   `go.mod`, `ciselect` checks the working tree (head) and
   `git cat-file -e <rev>:<path>` (base). `pull_request.yml` computes
   `git merge-base "$BASE_SHA" "$HEAD_SHA"` next to the changed-file list
   and passes it as `-base`. It must be the merge-base, not the base
   branch tip, because the changed-file list is a three-dot diff
   (`$BASE_SHA...$HEAD_SHA`) taken from the merge-base: a `go.mod` that
   landed on `main` after the branch point would otherwise make the pull
   request's own add look like an edit. The test
   `TestGoModPresence_MergeBaseMatchesThreeDotDiff` builds exactly that
   history. Without `-base` (local runs; `build.yml` uses `-all`), every
   changed nested `go.mod` is treated as a boundary change: the
   conservative behavior. An unknown or option-like `-base` makes
   `ciselect` fail, and the `-all` fallback, which never reads `-base`,
   takes over.
4. **Changed set.** Every nested module marked changed, plus the root when
   its lane is not `none`. A nested module's affected packages are all of
   its packages; the root's are the packages its lane selected.
5. **Reverse-transitive closure with the import filter.** A breadth-first
   walk over the reversed edges, in sorted order so the output is
   deterministic. A module M that requires a reached module D is selected:
   - **unfiltered** when D's `go.mod` or `go.sum` changed, D is the parent
     of a boundary change, or D is the root with lane `full`;
   - **otherwise only if** M's parsed imports name one of D's affected
     packages.

   Why the filter is sound: if D's `go.mod` and `go.sum` did not change, D
   contributes the same build list to M as before, so a change in D can
   only reach M through package code that M imports. For the root, this is
   exactly #111's rule: a root change selects a nested module only if the
   module imports an affected root package. The walk records the first
   chain that reached each module, for example `publisher/kafka ← .` or
   `it ← adapter/a ← port`. A module requiring a reached module that the
   filter rejected says so in the plan: "not affected: requires `<dir>` but
   imports none of its affected packages".
6. **Root lane through a dependency.** When the root is reached through a
   module it requires (the edge passed the filter), the root lane is
   seeded with the root packages whose `Imports`, `TestImports` or
   `XTestImports` (from `go list`) name a package of that module, and the
   usual package closure runs from there. If that module's `go.mod` or
   `go.sum` changed, or no root package in the `go list` graph imports it
   (its only importer sits behind a build tag), the root lane runs `full`.
   Seeding can widen the root lane, which can widen the walk from the
   root, so the two repeat until the root lane is stable. (Nothing in the
   root requires a nested module today, so this only matters once a
   contracts module exists.)
7. **Fail closed.** An unreadable `go.mod`, a `go mod edit` error, a
   `replace` that points an in-repository requirement at some other
   directory, or a local `replace` whose target is inside the repository
   but is not a discovered module makes `ciselect` exit non-zero (a local
   `replace` pointing outside the repository is not an in-repository edge
   and is allowed), and `pull_request.yml` reruns
   it with `-all`. With `-all`, a broken `go.mod` does not fail the
   fallback itself: every discovered module is selected by directory, and
   that module's own verification job reports the breakage.

**What changed for real changes.** Almost nothing today, which is the
point: the graph is in place for the first nested-to-nested edge (#102
S1). Running the selector on `main` at `77beda6` before and after this
change, over the change set of the design's exploration (§6), with
`-base origin/main` as the workflow runs it:

| Changed path | Root lane before → after | Nested modules before → after |
|---|---|---|
| `publisher/kafka/kafka.go` | `none` → `none` | 1 → 1 |
| `publisher/kafka/go.mod` (edit) | `none` → `none` | 1 → 1 |
| `docs/ci.md`, `odd/tasks/x.md` | `none` → `none` | 0 → 0 |
| `internal/cmd/archcheck/main.go`, `migration/migration.go` | `affected` (1) → `affected` (1) | 0 → 0 |
| `internal/ticker/ticker.go`, `port/publishing/publishing.go` | `affected` (3) → `affected` (3) | 6 → 6 |
| `internal/pause/pause.go`, `mocks/ego/…`, `encryption/…`, `persistence/…` | unchanged | 6 → 6 |
| `engine.go`, `egopb/ego.pb.go`, `newmod/go.mod` + `newmod/x.go` | `full` → `full` | 6 → 6 |
| `go.work` | `affected` (2) → **`full` (global)** | 6 → 6 |

Only `go.work` differs. Without `-base`, the `publisher/kafka/go.mod`
edit is treated as a boundary change and runs the full root lane with all
six modules. The coverage denominator (`coverpkg`) is identical in every
row and in `-all`.

### `modules.json` and the job summary

`ciselect` writes the selected module directories to
`<out-dir>/modules.json`, a JSON array of strings — `[]`, never `null`,
when nothing was selected — so a GitHub Actions job can feed it straight
into a matrix's `fromJSON(...)` without any extra parsing step. Its
shape is unchanged by #102: nested module directories only, never the
root. The job summary (`summary.md`) gets a `## Nested modules` section
listing each selected module and its reason, or the line "no nested
modules selected" when none were, followed by a `## Module plan` table
with one row per discovered module, root included: `module | selected |
why`. A module reached through the graph shows its chain as the "why"
(`publisher/kafka ← .`); one that was not selected says "not affected",
or "pinned to `<path>@<version>`; not affected at HEAD" when it requires
an affected module at a published version.

`ciselect` also writes `<out-dir>/plan.json`, the same decision in a form
that does not depend on GitHub Actions, so a later portable pipeline
(Shipwright, #38 CI-008) can reuse it:

```json
{
  "global": false,
  "reasons": ["affected by 1 changed package(s)"],
  "root": {"mode": "affected", "selected": ["github.com/pablogore/ego/v4", "…"]},
  "modules": [
    {"dir": ".", "path": "github.com/pablogore/ego/v4", "selected": true,
     "reason": "affected by 1 changed package(s)", "chain": ["."]},
    {"dir": "publisher/kafka", "path": "github.com/pablogore/ego/v4/publisher/kafka",
     "selected": true, "reason": "publisher/kafka ← .", "chain": ["publisher/kafka", "."]}
  ]
}
```

It lists every discovered module, selected or not, each with a reason;
every list is a JSON array, never `null`. In the `-all` fallback with an
unreadable `go.mod`, modules are discovered by directory only, so their
paths are unknown and each entry omits `path`. No workflow consumes `plan.json`
yet.

### `scripts/ci/verify-module.sh`: what runs for one selected module

For each module `fromJSON(modules.json)` names, `scripts/ci/verify-module.sh
<module-dir>` runs, with `GOWORK=off` so a stray root `go.work` can never
pull the module into the root module's own build:

1. `go mod download`
2. `go build ./...` (into a scratch directory when the module has a
   `main` package, so a verification run never leaves a stray binary in
   the module's own working tree)
3. `go vet ./...`
4. `golangci-lint run` against the **root** `.golangci.yml` — nested
   modules have no lint config of their own — with
   `--modules-download-mode=mod`, overriding the root config's
   `modules-download-mode: vendor`, since nested modules do not check in
   a `vendor/` directory
5. `go test ./...` only when the module has at least one `*_test.go`
   file; a module with none (no nested module today) reports "no tests"
   in the job summary instead of running `go test` against nothing.
   `-race` is added only when `GO_TEST_RACE=1`, which the CI matrix job
   sets; a local run leaves it off by default, per this repository's own
   rule against running the race detector locally.

Any of these steps failing fails the module's own job, and therefore the
whole check — a Kafka build error, a Kafka lint finding or a Kafka test
failure now blocks the PR the same way a root-package failure always did.

### The `modules` matrix job

Both `pull_request.yml` and `build.yml` add a `modules` job that
`needs: build`, runs only `if: needs.build.outputs.modules != '[]'`, and
fans out one `strategy.matrix.module` entry per string in that JSON
array, with `fail-fast: false` so one module's failure does not cancel
the others mid-run. Each matrix job checks out the repository, sets up
the same Go version as the root lane, installs the same pinned
`golangci-lint` version, and runs `scripts/ci/verify-module.sh
"${{ matrix.module }}"` with `GO_TEST_RACE=1`. `build.yml` always selects
every module (it runs `ciselect -all`); `pull_request.yml` selects
whatever the module selection rules above decided for that PR. The module
list is never hand-maintained: it comes from `modules.json`, so a new
nested module is picked up the moment its `go.mod` exists, with no
workflow edit.

### Release verification: two different questions

Two distinct claims exist about a nested module, and this repository
checks both, separately:

- **Does the monorepo build together, right now?** `verify-module.sh`
  above answers this on every PR and on every push to `main`, using the
  module's checked-in `replace github.com/pablogore/ego/v4 => ../../`
  (or `../` for `benchmark`) directive. This is "integrated verification"
  in `openspec/changes/ego-arch-001/design.md` §8.
- **Does a real consumer, resolving the module from the proxy, actually
  get something that builds?** A checked-in `replace` is invisible to a
  consumer — Go ignores `replace` directives in a dependency, only in the
  main module — so integrated verification says nothing about this.
  `scripts/ci/verify-published.sh <module-dir> <ego-version>` answers it:
  it copies the module into a scratch directory, runs
  `go mod edit -dropreplace=github.com/pablogore/ego/v4
  -require=github.com/pablogore/ego/v4@<version>` in one edit (dropping
  the replace and pointing at the target version together, so the module
  graph is never resolved against the old, unpublished requirement before
  the edit takes effect), then `go mod tidy && go build ./...`. If
  `go list -m github.com/pablogore/ego/v4@<version>` cannot even resolve
  the version, it fails fast with one `::error::` line instead of a
  confusing `go.sum`/build error. `release.yml` runs it for each
  publisher, right after that publisher's own `go get`/`go mod tidy` and
  before any tag is created, so a publisher release can never point at a
  root version that turns out not to build.

### Version policy

- The root module is released first, as a semantic-version tag `v4.x.y`.
- Each publisher module is released only against a root version that
  already exists on the module proxy — never against an unpublished
  version, and never verified only through the local `replace`.
  `release.yml` discovers which directories under `publisher/` to release
  from `publisher/*/go.mod` (never a hand-written list), updates each
  one's `github.com/pablogore/ego/v4` requirement to the just-published
  root tag, runs `verify-published.sh` against it, and only then tags
  `publisher/<name>/vX.Y.Z`.
- `benchmark` and `example/cluster` are never released. They exist only
  as integrated-verification consumers (`verify-module.sh` covers them
  in the PR and `main` lanes) and keep their `replace` directive
  permanently.

### Toolchain requirements

CI builds and tests everything — the root module and every nested
module — with Go 1.27.0 (`actions/setup-go`'s `go-version` input). The
root module's own `go.mod` declares `go 1.26.0`; the nested modules
declare `go 1.26.0` (`benchmark`, `example/cluster`, `publisher/kafka`,
`publisher/nats`, `publisher/websocket`) or `go 1.26.2`
(`publisher/pulsar`), and none of them pins a `toolchain` line, so the
installed 1.27.0 toolchain satisfies every one of them without
downloading anything else. `golangci-lint` is pinned to the same version,
`v2.13.1`, for the root lane and for every nested module's own lint step.
