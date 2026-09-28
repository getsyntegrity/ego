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

Both workflows are now split into a cheap `plan` job and a `modules` matrix
job that also runs the root module (ego-arch-006 spec 1, C2), so a pull
request that never needs the root module's own checks never pays for them:

- **`plan`** only checks out the repository, sets up Go, computes the diff
  (`pull_request.yml`) or always requests the full suite (`build.yml`),
  runs `internal/cmd/ciselect`, and uploads its whole output directory —
  `mode`, `packages.txt`, `coverpkg`, `modules.json`, `plan.json` and
  `summary.md` — as a `ciselect-plan` artifact (`actions/upload-artifact`).
  It never installs dependencies, vendors, lints or tests anything.
- **`modules`** (`needs: plan`) fans out one job per string in `plan`'s
  `modules.json`, which now includes `.` (the root module's directory)
  whenever the root lane is not `none`, exactly like any nested module (see
  "The `modules` matrix job" below). The root entry of the matrix downloads
  the `ciselect-plan` artifact (`actions/download-artifact`) and runs the
  root module's own steps — vendoring and tidy, an explicit `go build ./...`
  and `go vet ./...` over the **whole** root module, `archcheck`, lint,
  `scripts/ci/go-test.sh` and the coverage summary — against the exact
  decision `plan` already made, instead of recomputing it: a second,
  independent `ciselect` run could in principle disagree with `plan`'s (a
  flaky `go list`, a different fallback path taken), which would let the
  root entry test something other than what `plan`, and every nested
  module's selection, already agreed on. On `pull_request.yml` the whole
  matrix (root included) is skipped when `plan` selected nothing (`modules`
  == `[]`); on `build.yml`, `-all` always selects the root and every nested
  module.
- **`ci-gate`** (`needs: [plan, modules]`) is the one required status
  check; see "The `ci-gate` job" below.

### `pull_request.yml` (the fast lane)

1. **`plan`**: checkout, Go setup, module cache — no vendoring, no lint.
2. **Determine changed files**: `git diff --name-only --no-renames
   "$BASE_SHA...$HEAD_SHA"` between the PR's base and head commits, written
   to `$RUNNER_TEMP/changed.txt`, and `git merge-base "$BASE_SHA"
   "$HEAD_SHA"` (the revision that three-dot diff starts from), written to
   `$RUNNER_TEMP/base.txt`.
3. **Select packages and modules**: `go run ./internal/cmd/ciselect -changed
   "$RUNNER_TEMP/changed.txt" -base "$(cat "$RUNNER_TEMP/base.txt")"
   -out-dir "$RUNNER_TEMP/ci"` (`-base` is explained in "Module selection
   rules" below). If that command
   fails for any reason (a `go list` error, an unreadable file, a bug in
   the selector itself), the workflow logs a `::warning::` and re-runs with
   `-all -reason "selector failed; full-suite fallback" -out-dir` instead
   — if *that* also fails, the `plan` job fails, and `modules` never runs
   for it (it `needs: plan`), so `ci-gate` fails visibly instead of
   silently testing less. Selection is never silently skipped. The
   selector's own `summary.md` is appended to the job's
   `$GITHUB_STEP_SUMMARY`, so the exact package list and the reason for it
   are visible on every PR run, not just inferred from logs. `ciselect`'s
   output directory is uploaded as the `ciselect-plan` artifact for the
   root entry of `modules` to reuse.
4. **`modules`, root entry** (`matrix.module == '.'`, present only when
   `plan`'s `mode` is not `none`): vendoring and tidy, then an explicit
   `go build ./...` and `go vet ./...` over the **whole** root module —
   independent of whatever `ciselect` selected for this PR — then
   `archcheck`, lint, then downloads `ciselect-plan` and **runs tests**:
   `scripts/ci/go-test.sh "$RUNNER_TEMP/ci" coverage.out`, with
   `GO_TEST_RACE=1` (the race detector stays on for pull requests), plus
   `govulncheck -format json ./...` gated by `internal/cmd/vulngate` (see
   "Root module in the matrix" and "The govulncheck exception gate
   (vulngate)" below). Build and vet always cover every root-module
   package; only the test step is scoped to `ciselect`'s selection — in
   `affected` mode that means the packages `ciselect` selected, in `full`
   mode every included package (see "Modes and fail-safe rules" below).
5. **Coverage summary**: when `coverage.out` was produced, the root entry
   runs `go tool cover -func=coverage.out`, takes its final `total:` line,
   and appends it to the job's `$GITHUB_STEP_SUMMARY` together with the
   selection mode. In `affected` mode that total only reflects the
   packages that actually ran — the denominator (`-coverpkg`) is still
   every included package, so an `affected`-mode total is not directly
   comparable to a `full`-mode total. When the mode is `none`, no
   `coverage.out` exists (and the root entry of `modules` never even runs);
   a `none`-mode PR still gets a step summary from `plan` explaining why.

### `build.yml` (the full-suite gate)

Runs on every push to `main`, and can be triggered manually for any ref
via the Actions "Run workflow" button (`workflow_dispatch`). Its `plan` job
runs `go run ./internal/cmd/ciselect -all -out-dir "$RUNNER_TEMP/ci"` —
always the full suite, no change detection — appends the summary to the
job summary the same way, and uploads it as `ciselect-plan`. `-all` always
selects the root module (mode `full`, never `none`), so the root entry of
`modules` always runs here too: it downloads `ciselect-plan`, runs an
explicit `go build ./...` and `go vet ./...` over the whole root module
(the same unconditional steps `pull_request.yml`'s root entry runs), then
`scripts/ci/go-test.sh` with the race detector on and every included
package selected (`-all`'s `full` mode), `govulncheck -format json ./...`
gated by `internal/cmd/vulngate`, and appends the same coverage summary to
the job summary. This is the mandatory gate and always runs the complete
suite, root and every nested module — `main` and `workflow_dispatch` never
run in `affected` mode. `build.yml` also has a third, independent job,
`release-plan` — a dry run of `internal/cmd/releaseplan` computing the
release order and next tag of every released module, with no publish and
no tag — see "Release plan dry run (releaseplan)" below.

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
| Full-fallback    | Exact files `go.mod`, `go.sum`, `Makefile`, `Dockerfile.ci`, `.golangci.yml`, `buf.yaml`, `buf.gen.yaml`; directories `.github/`, `protos/`, `internal/cmd/ciselect/`, `internal/cmd/vulngate/`, `scripts/ci/`, `egopb/`; and any `.go` file directly in the module root (the shared root package) | Forces mode `full` |
| Satellite        | A directory that has its own `go.mod` on disk (`benchmark/`, `example/cluster/`, `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `test/compat`)                          | Selects nothing in the root lane; selects that module in the nested module lane |
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
`internal/pause` and the root package `github.com/getsyntegrity/ego/v4`: no
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

### Running the plan outside GitHub Actions (#159, C6)

The impact plan does not depend on GitHub Actions. `ciselect` reads a plain
newline-separated list of changed paths and writes its whole decision to a
directory, so any CI engine (including a future Shipwright pipeline, #38)
or a developer can compute the same plan the `plan` job computes:

```sh
git diff --name-only --no-renames "$(git merge-base origin/main HEAD)...HEAD" > /tmp/changed.txt
GOWORK=off go run ./internal/cmd/ciselect \
  -changed /tmp/changed.txt \
  -base "$(git merge-base origin/main HEAD)" \
  -out-dir /tmp/ci
cat /tmp/ci/summary.md      # human-readable: each module, why it was selected, what runs
cat /tmp/ci/modules.json    # the matrix: selected module dirs, "." (root) first when selected
cat /tmp/ci/plan.json       # machine-readable: every module, selected or not, with its reason chain
```

`-all -reason "<why>"` produces the full plan used by `main` (push) and
manual `workflow_dispatch` runs in `build.yml`. `release.yml` performs no
verification of its own — it relies on `main` having already passed — so it
never runs `ciselect`, build, vet or tests. A non-zero exit is a planning
failure: callers must fall back to `-all` (as the `plan` job does) or fail;
they must never run less.

The measurements that motivated this split (module graph, `go list -deps`
closures, per-job timings with run IDs, and the selector's output for a
publisher leaf, a shared contract, `testkit` and a runtime package) are
recorded in [`docs/ci/baseline-159-a1.md`](ci/baseline-159-a1.md). The
headline: in full mode about 92% of the root job is the test step, and
most pull requests reach full mode because any `.go` file directly in the
repository root is a full-fallback path. Emptying the root (#124) is what
unlocks most of the saving; this spec only removes the unconditional
prerequisite work.

## Architecture boundary check

Both workflows run `go run ./internal/cmd/archcheck` right after dependencies are installed and before the linter, so a broken layer boundary fails the run in seconds instead of after the test suite. It enforces the dependency rules of the ego-arch-001 ADR (`openspec/changes/ego-arch-001/design.md` §3), tracked by [#107](https://github.com/getsyntegrity/ego/issues/107).

The tool reads the import graph in two ways:

- **Root module:** `go list -e -json ./...`, which gives each package's production imports with build constraints resolved. A package that fails to load fails the check (fail closed).
- **Nested modules** (`publisher/*`, `benchmark`, `example/cluster`, `test/compat`): every non-test `.go` file is parsed in imports-only mode with `go/parser`. No module download or network is needed, which keeps the step at well under a second.
- **Module table** (#102, ego-arch-006 slice S1): every module's path and its `require` lines, read with `go mod edit -json` in the root and in every nested module directory (offline, the same reader `ciselect` uses), narrowed to requirements that name another in-repository module. A package belongs to the module with the longest path that prefixes its import path. The two module-aware rules below need this table; without it they match nothing, and the zero-match check fails the run.

Each rule applies to one layer and checks the direct import edges of every package in it. Contract layers use a closed allowlist, and every allowed target is itself runtime-free, so a transitive path to GoAkt cannot open without adding a new direct edge that the check sees. `rules.Evaluate` counts how many packages each rule's layer actually matched, and fails the whole run — naming the empty rule and its layer — if any rule matched zero packages: that is almost always a sign the root module path or a layer definition is wrong, not that the layer is genuinely empty, since a check that matches nothing passes vacuously instead of catching anything.

**Build-constraint coverage.** The root module is loaded via `go list`, which resolves build constraints (`//go:build` tags, `_linux.go`-style suffixes) for the CI host's own `GOOS`/`GOARCH` only; a file restricted to another platform is not part of the graph `go list` reports, so an import that only exists on a platform the CI host does not build for is not checked. Nested modules are parsed with `go/parser` directly, ignoring build constraints entirely, so a file's imports are read regardless of which platform it is restricted to; this can only over-report a nested module's imports, never miss one, so it stays fail-safe in the direction that matters for this check.

| Rule | Applies to | Constraint |
|---|---|---|
| `contract-allowlist` | `tenancy`, `command`, `persistence` (except `persistence/conformance`, which is test support), `offsetstore`, `projection`, `eventstream`, `encryption`, `eventadapter`, everything under `port/` | Only stdlib, other contract packages, `egopb`, `google.golang.org/protobuf/...`, `internal/queue`, `internal/syncmap`, `github.com/google/uuid`, `go.uber.org/atomic` — and stdlib itself excludes `net/http`, `net/rpc`, `database/sql` and everything under them |
| `application-no-runtime` | `migration` | Must not import package `ego`, `internal/extensions` or GoAkt |
| `external-adapter-no-runtime` | nested modules under `publisher/` | Must not import package `ego` or GoAkt |
| `no-cross-module-internal` | every package of every module, root and nested | Must not import an `internal/...` package that belongs to a different in-repository module, in either direction (generalized in ego-arch-006 slice S1 from "nested module to root `internal/`") |
| `no-module-cycle` | the module table (`go.mod` requirements) | No in-repository module may require, directly or through other in-repository modules, a module that requires it back; every requirement edge on a cycle is reported (ego-arch-001 design §3, ego-arch-006 slice S1) |
| `composition-no-runtime` | `compose`, everything under `compose/internal/` | Must not import package `ego`, `internal/extensions` or GoAkt (ego-arch-003 design §D8) |
| `composition-leaf` | root-module packages outside `compose/`, except `main` packages and `example/...` | Must not import `compose` or anything under it (ego-arch-003 design §D8) |
| `external-adapter-no-composition` | nested modules under `publisher/`, every package including `main` packages and examples inside them | Must not import `compose` or anything under it; the composition root depends on adapters, not the reverse (ego-arch-004 design §D7). The test side is covered by each publisher's `closure_test.go`, because archcheck does not read `_test.go` files |

A failure names the importer, the forbidden import and the rule, for example:

```text
github.com/getsyntegrity/ego/v4/tenancy imports github.com/tochemey/goakt/v4/actor: rule contract-allowlist (design.md §3): ...
```

The fix is almost always to depend on a contract package instead of the runtime. Do not add a baseline entry to silence a new violation.

A module-cycle failure names the requiring module as the importer and the required module as the import, and its reason spells out the cycle, for example `requires github.com/getsyntegrity/ego/v4/contracts, which requires it back: github.com/getsyntegrity/ego/v4 -> github.com/getsyntegrity/ego/v4/contracts -> github.com/getsyntegrity/ego/v4`. The go toolchain accepts module cycles, and `ciselect` does not check for them (a cycle there would only widen a selection), so archcheck is where one fails the build. Likewise, Go does not stop a module from importing another in-repository module's `internal/` package, because every module here shares the root module's path prefix; `no-cross-module-internal` does.

The summary line counts modules too. On the S1 branch: `archcheck: 8 modules checked, 44 packages checked, 182 edges checked, 1 baselined, 0 violation(s), 0 stale entries` (before S1: `37 packages checked, 157 edges checked, 1 baselined`; the generalized `no-cross-module-internal` now also inspects root-module packages, and `test/compat` adds one package).

**Stdlib transport and database packages are forbidden in contracts.** `contract-allowlist` also denies `net/http`, `net/rpc` and `database/sql`, and everything under them, matched by whole path segment (`net/http/httptest` is forbidden; a hypothetical `net/httpx` would not be). gRPC and other third-party transports are already excluded by the closed allowlist; this stdlib denylist closes the remaining gap, and the rest of the standard library — including `net` itself, for value types such as `net.IP` — stays allowed. It applies to direct imports only: the standard library is not a closed set the way the allowlist's third-party targets are, so a transitive path such as `expvar` importing `net/http` internally is possible and is not enforced (design.md §3).

### Baseline: known violations that can only shrink

Violations that cannot be fixed yet are listed in `internal/cmd/archcheck/baseline.go`. Every entry must name an owner, a justification and a removal criterion, or the tool refuses to run. An entry that no longer matches a real violation fails the check as **stale**, so the entry has to be deleted in the same change that fixes the violation. The baseline can shrink, but nothing can quietly stay in it after its violation is gone.

The baseline started with five entries. S1b removed the four publishers importing package `ego` once #111 verified nested modules in CI; they now import `port/publishing`. One entry remains: `migration` importing package `ego` (removed by S3/S4, #103 and #11).

### Adding a layer or changing a rule

1. Declare the layer in `internal/cmd/archcheck/rules/layers.go`: a function taking the root module path and returning a `Layer` with a name and a `Match` function over the package's import path and kind (root or nested module). A new contract package only needs its path added to `contractRoots`.
2. Add the rule to `DefaultRules(rootModulePath string)` in `internal/cmd/archcheck/rules/rules.go`, with an ID, a description, the source (ADR section or issue), allowlist or denylist semantics and, for a denylist rule, a `Reason` func naming the specific forbidden prefix it matched.
3. Add unit tests in `internal/cmd/archcheck/rules/evaluate_test.go`: one graph that breaks the rule and one that satisfies it.
4. Run `go run ./internal/cmd/archcheck` locally. If existing code violates the new rule and cannot be fixed in the same change, add baseline entries with owner, justification and removal criterion, and update the ADR if the rule is normative.

**Adapter module roots.** `ExternalAdapterLayer` in `layers.go` matches only nested modules under `publisher/`, and both adapter rules (`external-adapter-no-runtime` and `external-adapter-no-composition`) use it. When the first adapter module outside `publisher/` appears (a store or telemetry adapter), add its directory root to that one layer in the same change, so both rules cover it; where such modules live is decided when the first one arrives (ego-arch-004 design §9, O6). Give the new module a `closure_test.go` like the publishers', which rejects GoAkt, the root package and `compose` in its unit-test closure.

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
| CI run `35868911889` (`-race -p 1`): root package `github.com/getsyntegrity/ego/v4` | 593.7s of 609.7s total (97%) |
| Same run: the other 13 tested packages | 1–3s each |
| Local, no race, `go test -count=1 ./...` | ~585s total, root package 582.5s |
| Race detector overhead (measured) | ~2% |
| `-p 1` justification found in history | none — no fixed ports, containers, shared files or global env in the tests |
| `testkit` in CI before this change | never ran (`go-acc --ignore test` substring bug) |
| `main` branch protection | none (GitHub API returned 404) |
| `vendor/` | gitignored; regenerated in CI with `go mod tidy && go mod vendor` |

## The honest limit of package selection

The root package, `github.com/getsyntegrity/ego/v4`, imports — directly or
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
`publisher/pulsar`, `publisher/websocket` and `test/compat` (#102 S1) each
carry their own `go.mod`
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
`go list` resolves build tags, so it would miss a build-tagged importer
such as the `//go:build compat` files the publishers carried until #102
S1 moved those checks into `test/compat`.

**Algorithm**, in order:

1. **Global check.** If any changed path is global, every module is
   selected and the root lane runs `full`, with the reason
   "global: `<path>` changed". The global paths are `go.work`,
   `go.work.sum`, `.golangci.yml`, `Makefile`, `Dockerfile.ci`,
   `buf.yaml`, `buf.gen.yaml`, and everything under `.github/`,
   `scripts/ci/`, `internal/cmd/ciselect/`, `internal/cmd/vulngate/` (it
   decides every module's govulncheck result) and `protos/`. `-all` and an
   empty changed-file list are treated the same way. The root `go.mod`
   and `go.sum` are deliberately **not** global: they send the root lane
   to `full`, and step 5 then selects every module that requires the
   root (today, all seven).
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
`-base origin/main`. At `77beda6` that is also the merge-base, which is
what the workflow passes:

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

**Measured selection after S1 (`test/compat`, the first nested-to-nested
edge).** `test/compat` requires the root and the four publishers through
local `replace`, so it now has four in-repository edges besides the root.
`ciselect -changed <file> -base origin/main` on the S1 branch
(`origin/main` = `9084b80`, `GOFLAGS=-mod=vendor` as in CI):

| Changed path | Root lane | Selected modules | `test/compat` reason (chain) |
|---|---|---|---|
| `port/publishing/publishing.go` | `affected` | all seven | `test/compat ← .` (it imports the affected root package `ego` directly, so the walk reaches it from the root first) |
| `publisher.go` (the root file holding the aliases) | `full` | all seven | `test/compat ← .` |
| `publisher/kafka/kafka.go` | `none` | `publisher/kafka`, `test/compat` | `test/compat ← publisher/kafka` (unfiltered: the publisher is a fully changed nested module) |
| `migration/migration.go`, `compose/spec.go` | `affected` | none | not selected: "requires `.` but imports none of its affected packages" |
| `docs/ci.md` | `none` | none | not selected |

The design (ego-arch-006 §6, S1 checks) expected the chain
`test/compat ← publisher/… ← .` for a `port/publishing` change. The
selection is the same; the recorded chain is the shorter one because the
walk records the first chain that reaches a module, and `test/compat`
imports package `ego`, which the root lane selects. The nested-to-nested
edge itself is the `publisher/kafka/kafka.go` row. A leaf publisher change
now also verifies `test/compat`: one more module job on such a PR.

### `modules.json` and the job summary

`ciselect` writes the selected module directories to
`<out-dir>/modules.json`, a JSON array of strings — `[]`, never `null`,
when nothing was selected — so a GitHub Actions job can feed it straight
into a matrix's `fromJSON(...)` without any extra parsing step. Since
ego-arch-006 spec 1 (C2), `.` (the root module) appears in this list
exactly like any nested module's directory, root first, whenever the
root's own `Plan` entry is selected (its lane is not `none`) — the
`modules` matrix job runs the root module the same way it runs every
nested one (see "Root module in the matrix" below). Before that change the
list held nested module directories only, never the root; the job summary
(`summary.md`) still gets a `## Nested modules` section
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
  "root": {"mode": "affected", "selected": ["github.com/getsyntegrity/ego/v4", "…"]},
  "modules": [
    {"dir": ".", "path": "github.com/getsyntegrity/ego/v4", "selected": true,
     "reason": "affected by 1 changed package(s)", "chain": ["."]},
    {"dir": "publisher/kafka", "path": "github.com/getsyntegrity/ego/publisher/kafka",
     "selected": true, "reason": "publisher/kafka ← .", "chain": ["publisher/kafka", "."]}
  ]
}
```

It lists every discovered module, selected or not, each with a reason;
every list is a JSON array, never `null`. In the `-all` fallback with an
unreadable `go.mod`, modules are discovered by directory only, so their
paths are unknown and each entry omits `path`. No workflow consumes `plan.json`
yet.

### `scripts/ci/verify-module.sh`: what runs for one selected nested module

For each **nested** module `fromJSON(modules.json)` names (every entry
except `.`; the root module's own steps are described in "Root module in
the matrix" below), `scripts/ci/verify-module.sh <module-dir>` runs, with
`GOWORK=off` and `GOFLAGS=` (both cleared/forced so a stray root `go.work`
or an inherited `GOFLAGS=-mod=vendor` can never change what the module
builds against):

1. `go mod download`
2. `go mod tidy -diff` — fails the job with the printed diff when the
   module's `go.mod`/`go.sum` do not already match what `go mod tidy`
   would write (#122 follow-up: this was an acceptance criterion of #122
   itself but was never actually wired into CI until this check). `go mod
   tidy` has no `-tags` flag, so it considers every file in the module,
   whatever its build tags.
3. `go build ./...` (into a scratch directory when the module has a
   `main` package, so a verification run never leaves a stray binary in
   the module's own working tree)
4. `go vet ./...`
5. `golangci-lint run` against the **root** `.golangci.yml` — nested
   modules have no lint config of their own — with
   `--modules-download-mode=mod`, overriding the root config's
   `modules-download-mode: vendor`, since nested modules do not check in
   a `vendor/` directory
6. `govulncheck -format json ./...` (ego-arch-006 spec 1, C2), gated by
   `internal/cmd/vulngate` against a reviewed, expiring exception list
   (`scripts/ci/govulncheck-allow.json`) — see "The govulncheck exception
   gate (vulngate)" below for what the gate does and why a bare
   `govulncheck ./...` was not enough. CI always installs `govulncheck`
   first, pinned to a fixed version (the workflow's "Install govulncheck"
   step, `go install golang.org/x/vuln/cmd/govulncheck@v1.8.0`), so this
   step always runs there; a local run of `verify-module.sh` without
   `govulncheck` on `PATH` prints one line and skips it instead of failing
   a contributor's machine for a tool they have not installed.
7. `go test ./...` only when the module has at least one `*_test.go`
   file; a module with none (no nested module today) reports "no tests"
   in the job summary instead of running `go test` against nothing.
   `-race` is added only when `GO_TEST_RACE=1`, which the CI matrix job
   sets; a local run leaves it off by default, per this repository's own
   rule against running the race detector locally.

There is no build-tag lane any more. #122 added one (a `-tags compat`
vet, lint and test pass for modules with a `//go:build compat` file); #102
S1 removed it together with the last such files, because the checks it
ran now live in their own module, `test/compat` (next section but one).

Any of these steps failing fails the module's own job, and therefore the
whole check — a Kafka build error, an untidy Kafka `go.mod`/`go.sum`, a
Kafka lint finding or a Kafka test failure now blocks the PR the same way
a root-package failure always did.

### The `modules` matrix job

Both `pull_request.yml` and `build.yml` add a `modules` job that
`needs: plan` (the cheap planning job — see "What each workflow runs"
above), runs only `if: needs.plan.outputs.modules != '[]'`, and
fans out one `strategy.matrix.module` entry per string in that JSON
array, with `fail-fast: false` so one module's failure does not cancel
the others mid-run. Every matrix job checks out the repository and sets up
the same Go version; from there the steps differ by whether
`matrix.module == '.'` (see "Root module in the matrix" below) or a nested
module, which installs the same pinned `golangci-lint` version and runs
`scripts/ci/verify-module.sh "${{ matrix.module }}"` with `GO_TEST_RACE=1`.
`build.yml` always selects every module, root included (it runs
`ciselect -all`); `pull_request.yml` selects whatever the module selection
rules above decided for that PR. The module list is never hand-maintained:
it comes from `modules.json`, so a new nested module is picked up the
moment its `go.mod` exists, with no workflow edit. `modules` depends on
`plan`, not on a separate heavy root job, so a PR that only touches a
nested module runs its module job without waiting on (or paying for) the
root module's vendoring, archcheck and lint.

### Root module in the matrix (ego-arch-006 spec 1, C2)

Before this change the root module was a permanent, unconditional job:
every PR paid for its vendoring, `archcheck` and lint even when nothing in
the root lane was affected, and root and nested-module CI were two
differently-shaped jobs. Now the root module is `.` in `modules.json`,
selected on exactly the same rule as any nested module — `plan`'s root
`Plan` entry is `Selected` (its lane is not `none`) — and it runs inside
the same `modules` matrix job, from the same directory (`.`) with the same
`GOWORK=off` discipline nested modules already used.

**Where `.` comes from.** `internal/cmd/ciselect`'s `writeOutputs` builds
`modules.json` from every `Selected` entry of `result.Plan` (which already
lists the root first, then nested modules sorted by directory), not from
`result.Modules` (which stays nested-only — it also drives the `summary.md`
"Nested modules" section, a human-facing pointer to the nested module lane
that predates this change and is not about the workflow matrix). This was
a deliberate choice among two ways to signal "the root is selected too" to
the workflow:

- **Chosen: fold `.` into `modules.json` itself.** One JSON array is
  already the single source of truth the matrix reads
  (`fromJSON(needs.plan.outputs.modules)`); `Plan`'s root entry already
  carries exactly the right boolean (`Selected`), computed the same way as
  every nested module's, so no new field or output was needed.
- **Rejected: a separate `root_selected` output.** This would need a
  second output threaded through `plan`'s `GITHUB_OUTPUT`, a second
  `if:` on a dedicated root-only job (reintroducing the pre-this-task
  split this task removes), and two places for a reader of the workflow to
  check "is the root affected?" instead of one. It would not make the
  selection logic any different — `Plan`'s root `Selected` field already
  exists and already means exactly this — it would only duplicate it.

**Root-only steps.** Inside the `modules` job, every step that only makes
sense for the root module (downloading the `ciselect-plan` artifact,
`go mod tidy && go mod vendor`, an explicit `go build ./...` and
`go vet ./...` over the whole root module, `archcheck`, the root's own
`golangci-lint-action` lint step, `scripts/ci/go-test.sh` and its coverage
summary) is `if: matrix.module == '.'`; the `Verify module`
(`scripts/ci/verify-module.sh`) step, and installing `golangci-lint`
manually for it, are `if: matrix.module != '.'`. The explicit `Build
(root)`/`Vet (root)` steps mirror what `verify-module.sh` already does for
every nested module (its own `go build ./...`/`go vet ./...`, steps 3 and
4) — until this change, the root module was the only one with no explicit
build or vet step of its own. `archcheck` (`go list -e -json ./...` in
`internal/cmd/archcheck/loader.go`) and the `golangci-lint-action` lint
step already cover the whole root module unconditionally, same as the new
build/vet steps; only `scripts/ci/go-test.sh`'s test step is scoped to
`ciselect`'s selection — the packages it selected in `affected` mode, or
every included package in `full` mode (see "Modes and fail-safe rules").
Both new steps run with `GOFLAGS=-mod=vendor`, right after `go mod tidy &&
go mod vendor` and `go mod download`, and before `archcheck`, so they see
the same vendored dependency tree every later root step does; `./...` from
the repository root lists only root-module packages — each nested module
has its own `go.mod`, so the go tool already excludes it, and `vendor/`
itself is never listed either (`GOROOT= GOWORK=off GOFLAGS=-mod=vendor
go list ./...` returns 47 packages, 0 of them under `publisher/` or
`vendor/`). `govulncheck` is
installed once per matrix job unconditionally (both the root and every
nested module need it), then run against the root with
`GOFLAGS=-mod=vendor` right after its own coverage summary, and against
each nested module inside `verify-module.sh` (see above). Both scans write
`-format json` to a per-matrix-entry report path (`Compute the govulncheck
report path`, which also sanitizes `matrix.module` into an artifact name,
since an artifact name cannot contain `/`) and hand that report to
`internal/cmd/vulngate`, which decides pass or fail (see the next section).
A separate `Upload the govulncheck report` step, `if: always()`, keeps the
report as a build artifact even when the scan or the gate fails, so a
reviewer deciding whether a new exception is warranted does not have to
reproduce the scan locally.

### The govulncheck exception gate (vulngate)

The first real run of this pipeline (draft PR #167, run `36371025289`)
failed `publisher/pulsar`'s new `govulncheck` step on three findings with no
available fix: GO-2026-5046, GO-2026-5047 and GO-2026-5048, all in
`github.com/hamba/avro/v2` (pulled in indirectly through
`github.com/apache/pulsar-client-go`). A bare `govulncheck ./...` step has
no way to accept a specific, reviewed finding without either failing every
PR that touches `publisher/pulsar` forever, or dropping the check entirely
and losing coverage for every *future* finding too. `internal/cmd/vulngate`
is the narrow fix: it reads a `govulncheck -format json` report and decides
pass or fail against a small, reviewed, expiring exception list,
`scripts/ci/govulncheck-allow.json`.

**Why `-format json` and not the plain text/exit code.** `govulncheck
-format json` always exits `0`, even when it finds vulnerabilities — the
exit code alone cannot gate CI. The workflow step and `verify-module.sh`
therefore capture the scan's own exit code first (a real scan failure, for
example a network error talking to the vulnerability database, fails the
step immediately, before `vulngate` ever runs) and only then hand the
report to `vulngate`, which is the one that turns "these vulnerabilities
were found" into pass or fail.

**What counts as a blocking finding.** `govulncheck`'s JSON report is a
stream of objects (`{"config":...}`, `{"progress":...}`, `{"osv":...}`,
`{"finding":...}`); `vulngate` decodes it with `json.Decoder` and looks only
at `finding` messages. Each `finding.trace` lists call frames from the
vulnerable symbol itself (frame 0) to the entry point in the scanned
module's own code (the last frame) — verified against a real
`govulncheck -format json` run in this repository, since the format is
undocumented outside `golang.org/x/vuln`'s own internal packages. A finding
blocks only when frame 0 names a function: that is exactly the set
`govulncheck`'s text mode reports under "Your code is affected". A
vulnerability that is only required (frame 0 names just a module) or only
imported (frame 0 also names a package, but no function) never blocks —
this repository's own `publisher/nats` module has exactly such a
`golang.org/x/crypto` finding today, and it correctly never appears in the
gate's summary.

**The allow file.** Every entry in `scripts/ci/govulncheck-allow.json` is a
JSON object with eight required fields — `module` (repo-relative, `.` for
the root), `id` (the OSV identifier), `vulnerable_module` (the dependency
the finding is actually in, which can differ from `module` when the
vulnerability arrives transitively), `owner`, `reason`, `exposure`,
`removal` and `review_by` (`YYYY-MM-DD`). The file is decoded with
`json.Decoder.DisallowUnknownFields`, and every field is validated
non-empty with `review_by` parsed as a real date, so a typo'd field name or
a missing field fails the load loudly instead of silently granting less (or
more) than a reviewer intended.

**Matching.** A blocking finding is excepted only when an allow-file entry
matches on all three of `module` (the directory `vulngate -module` was
given), `id`, and `vulnerable_module`; an entry for a different module does
not apply and the finding blocks. `id` and `vulnerable_module` together
identify one exception, not `id` alone: `vulngate` tracks every blocking
finding, and every allow-file entry scoped to the module, by the pair
(`id`, `vulnerable_module`), because the same OSV ID can legitimately block
through more than one dependency module (an OSV record can list several
affected modules, e.g. a package and its fork). When the same `id` starts
blocking through a *different* `vulnerable_module` than an existing entry
names, that is **two** distinct, independently actionable facts, not one
finding reported twice, and `vulngate` reports both: the new pair blocks
(nothing names it), and the old entry goes stale (its own exact pair no
longer has any blocking finding) — the blocked item's message names the
stale entry's `vulnerable_module` explicitly, so a reader is not left to
guess why the ID looks both new and already-listed. An excepted entry whose
`review_by` has passed (`today > review_by`, `today` defaulting to now in
UTC or overridden with `-today` for tests) fails as expired instead of
excepted — an exception cannot silently outlive its review; the day named
by `review_by` itself is still valid, only the day after it is expired. An
entry scoped to a module this run did not scan is ignored entirely: never
matched, and never reported stale (a `publisher/kafka` scan never
evaluates, and never flags as stale, an entry written for
`publisher/pulsar`).

**Adding or retiring an entry.** To accept a new, reviewed finding, add an
entry naming the exact scanned module, OSV ID and vulnerable dependency
module, with a real `owner`, a `reason` explaining why no fix is available
(or why the fix cannot be adopted yet), an `exposure` statement about
whether and how this codebase actually reaches the vulnerable code, a
`removal` criterion (what change makes the entry obsolete), and a
`review_by` date no more than a few months out. To retire an entry, delete
it — the gate does not need to be told to stop excepting something. If the
gate's summary reports the entry stale (the finding is already gone) or
expired (`review_by` has passed with the finding still present), that is
the gate telling you the entry needs a human decision, not an automatic
extension.

**Wiring.** Both `verify-module.sh` (nested modules) and the workflows'
"govulncheck (root)" step run `govulncheck -format json ./... > "$report"`
and then `go -C "$repo_root" run ./internal/cmd/vulngate -module
<module-dir> -report "$report" -allow
"$repo_root/scripts/ci/govulncheck-allow.json"` — always invoked with the
repository root as `go`'s working directory (`-C`, or already being there
for the root step), never the current module's own directory, since
`internal/cmd/vulngate` lives in the root module. `vulngate` is standard
library only, so this works with no network access and with no `vendor/`
directory present, which matters for a nested-module job: unlike the root
entry, it never runs `go mod vendor` for the root module first.

### Release plan dry run (releaseplan) (#159, F4 PR-A)

`build.yml` has a third job, `release-plan`, alongside `plan` and
`modules` (no `needs:` between them — it runs in parallel, so it never
holds back the actual test/build gate). It runs `internal/cmd/
releaseplan` as a pure dry run: given every `go.mod` in the repository
and the explicit list of released module directories in
`scripts/ci/release-modules.txt` (today `.` and the four `publisher/*`
directories — read off `.github/workflows/release.yml`'s own publisher
discovery, kept here instead of duplicating that shell logic), it
computes the release order and the next tag of each released module and
writes `plan.json` and `summary.md`. It never calls git to *decide*
anything (only `git tag -l`, piped to a file, to hand it the existing
tags an actual release would see), never publishes, and never creates a
tag — that remains `release.yml`'s job (a separate workflow, triggered by
a real `v*` tag push, PR-B). The job:

1. Checks out with `fetch-depth: 0` and `fetch-tags: true` (a shallow
   clone would hide the tags `releaseplan` needs to compute the *next*
   version, not just the first one).
2. Writes `git tag -l` to a temp file and runs `go run ./internal/cmd/
   releaseplan -repo-root . -release scripts/ci/release-modules.txt -tags
   <that file> -bump patch -out-dir "$RUNNER_TEMP/releaseplan"`.
3. Appends `summary.md` to the job summary (`$GITHUB_STEP_SUMMARY`), the
   same pattern `plan`'s own step uses for `ciselect`'s summary.
4. Uploads `plan.json` (and `summary.md`) as the `release-plan` artifact.

The job's own `permissions: contents: read` is the most that step needs
(checkout and `git tag -l` are both read-only); it does not inherit
whatever broader default the workflow would otherwise have. Every `${{
}}` this job's steps need lives in a `with:` field or an `env:` mapping,
never spliced directly into a `run:` shell body — the same rule
`pull_request.yml`/`build.yml` already follow elsewhere in this repository
(a raw `${{ }}` inside a shell script is a script-injection vector: a
value containing shell metacharacters would be interpolated as source,
not passed as data).

**Why `ci-gate` requires it.** `release-plan`'s only purpose is to catch a
broken release plan before anyone relies on it for a real release — a
newly nested module missing from `release-modules.txt`, a cycle, a tag
scheme violation. A dry run nobody has to look at is not a dry run
anyone benefits from, so its failure fails `CI Gate` on `main` exactly
like `plan` or `modules` failing does (see below); unlike `modules`,
`release-plan` has no matrix and no legitimate skip condition, so
`success` is the only acceptable outcome.

### The `ci-gate` job: one required status check

Before this change, neither workflow had a single status check that branch
protection could require: the `modules` job is *skipped* (not green, not
red) whenever `modules.json` is `[]`, and GitHub branch protection cannot
require a check that a run sometimes never reports at all. `ci-gate` fixes
this. It is the last job in both `pull_request.yml` and `build.yml` (on
`build.yml`, `needs: [plan, modules, release-plan, consumer]`;
`pull_request.yml` has no `release-plan` job, see "Release plan dry run"
above, and its `needs: [plan, modules, consumer]`), and runs with `if:
always()` so it still runs even when an earlier job failed. Its one step
reads `needs.plan.result`, `needs.modules.result`, `needs.consumer.result`
and, on `build.yml`, `needs.release-plan.result`, and fails if `plan` did
not succeed, if `modules` finished as anything other than `success` or
`skipped`, if `release-plan` (on `build.yml`) did not succeed, or if
`consumer` finished as anything other than `success` or (on
`pull_request.yml` only) `skipped`. Once `plan` itself succeeded,
`modules` can only be "skipped" because `plan`'s own `modules.json` was
`[]`, and `consumer` (on `pull_request.yml`) can only be "skipped"
because `plan`'s own `mode` was not `full` — never a hidden failure in
either case (`consumer` never skips on `build.yml`, see "Verify clean
consumer" above, so `success` is the only acceptable outcome there). If
`plan` itself fails, `modules` and `consumer` are skipped too (their
`needs: plan` was not satisfied), but `ci-gate` already failed on `plan`'s
own result, so that skip changes nothing. This makes `ci-gate` pass
whether the matrix fanned out to zero, one, or many modules (root
included) and whether `consumer` ran or was legitimately skipped, and
fail visibly whenever `plan`, a real `modules` or `consumer`
failure/cancellation, or (on `build.yml`) a broken release plan, would
otherwise have left branch protection with nothing to require.

**Required check name: `CI Gate`.** Configure branch protection to require
this one check (the job's `name:`, not its `ci-gate` id) on `main`; no
other job needs to be listed, because `ci-gate` already depends on
everything that must pass.

### Compatibility checks: the `test/compat` module (#102, S1)

The historical alias and sentinel checks between the four publishers and
package `ego` (`ego.EventPublisher`, `ego.StatePublisher`,
`ego.ErrPublisherNotStarted`, ADR `ego-arch-001` §5, S1 criterion 3, kept
until [#124](https://github.com/getsyntegrity/ego/issues/124)) live in
`test/compat`, a nested module that is **never released** (ADR
`ego-arch-006`, slice S1, decision D5). It requires the root module and the
four publishers, each through a local `replace` (Go honors `replace` only in
the main module, so it lists its whole in-repository closure), and nothing
requires it. Its module path follows the current scheme,
`github.com/getsyntegrity/ego/test/compat`, like `benchmark`: nothing outside
the repository ever resolves it, so it needs none of the module-path
decisions (D1–D3) the contracts module waits for.

Each publisher's `compat_test.go` held two kinds of checks. They moved as
follows:

- **The eight compile-time alias assertions** move to `test/compat`
  unchanged: `_ ego.EventPublisher = (*<pub>.EventsPublisher)(nil)` and
  `_ ego.StatePublisher = (*<pub>.DurableStatePublisher)(nil)`, for kafka,
  nats, pulsar and websocket.
- **The runtime sentinel check** ("`Publish` on a stopped publisher returns
  an error that matches `ego.ErrPublisherNotStarted`", events and state, per
  publisher) is split in two:
  - inside each publisher module, `TestPublishBeforeStartMatchesPublishingSentinel`
    in `publisher_contract_test.go` checks that `Publish` before `Start`
    returns an error matching `publishing.ErrPublisherNotStarted`, for
    events and state. It builds the stopped publisher with a struct
    literal, which only code inside the package can do. `publisher/websocket`
    no longer has this test: since ego-arch-004 spec 2 (#158) the equivalent
    sentinel check is PT-1 of `port/publishing/publishingtest`, run in its
    `conformance_test.go`: `Publish` after `Close`, on a publisher that
    really was connected and closed, returns an error matching the same
    sentinel;
  - in `test/compat`, `TestEgoSentinelIsThePublishingSentinel` checks that
    `ego.ErrPublisherNotStarted == publishing.ErrPublisherNotStarted`, and
    `errors.Is` in both directions.

  Together the two halves prove the original check: `ego.ErrPublisherNotStarted`
  is defined as `publishing.ErrPublisherNotStarted`, so any error that matches
  one matches the other. The split was a maintainer decision on PR #142
  (recorded in `openspec/changes/ego-arch-006/design.md` §6 S1). It keeps
  `test/compat` free of `reflect`/`unsafe`: a module outside the publisher
  packages cannot build a stopped publisher any other way, because every
  constructor dials its broker and Pulsar has no embeddable server.

What this changes for the publishers: `compat_test.go` is gone, so no file
in a publisher imports package `ego` any more, in any build. `go mod tidy`
therefore drops every indirect requirement that only `ego`'s import needed
(GoAkt, Olric, OpenTelemetry and their dependencies) from each publisher's
`go.mod` and `go.sum`. It also **adds** a few `// indirect` lines that pin
versions the publisher already resolved through the root's requirements:
`github.com/prometheus/client_golang` in `publisher/kafka`, and twelve
modules in `publisher/pulsar` (`testcontainers-go`, the `moby` and `docker`
clients, `gopsutil` and their dependencies, plus `golang.org/x/crypto`).
`publisher/nats` and `publisher/websocket` only lose lines. The versions a
publisher actually builds and tests with do not change: the module
versions behind `go list -deps -test ./...` are identical to `main` in all
four publishers (kafka 21 modules, nats 13, pulsar 72, websocket 7). The
publishers still require the root module itself for `egopb` and
`port/publishing` until slice S3. Each publisher keeps
`publisher_contract_test.go` (the `publishing`-only assertions, including
the runtime half of the sentinel check above, which websocket now runs as
PT-1) and `TestUnitTestClosureExcludesRuntimeAndRoot`.

`test/compat` is verified like any other nested module: the `modules` job
discovers it from its `go.mod` (no workflow change), and
`verify-module.sh test/compat` runs download, `go mod tidy -diff`, build,
vet, lint and `go test ./...`. The selector picks it for any change that
reaches the root packages it imports or any publisher (see "Measured
selection after S1" above).

**Cost.** `test/compat` builds all four publishers plus the root package
`ego` with GoAkt, so it is now the slowest module job: about 195 s in PR
run 36331397582 (`publisher/pulsar` took about 154 s, the other modules 30
to 64 s). It runs on every leaf publisher PR, because a change to any
publisher selects it (`test/compat ← publisher/<name>`).

### Compatibility lane (#122) — historical, superseded by S1

*This section records how #122 worked. Since #102 S1 the `compat` build
tag, the four `compat_test.go` files and `verify-module.sh`'s tag lane no
longer exist; see the previous section. The measurements below are from
#122.*

S1b (above) switched the four publishers' *production* build to
`port/publishing`, but each module's *tests* still imported package `ego`
directly, through `compat_test.go`, to check the historical S1
compatibility aliases (`ego.EventPublisher`, `ego.StatePublisher`,
`ego.ErrPublisherNotStarted` — ADR `ego-arch-001` §5, S1 criterion 3, kept
until [#124](https://github.com/getsyntegrity/ego/issues/124)). That import
pulled the whole GoAkt runtime back into `go list -deps -test ./...`: 45
GoAkt packages and the root package itself, even though production code
never touched either. `#122` fixes the *test* closure the same way S1b fixed
the production one, without weakening the compatibility check itself.

**Mechanism: a build tag, not a new module or package.** Each publisher's
`compat_test.go` now starts with `//go:build compat` and keeps only the
`ego`-alias assertions; a new `publisher_contract_test.go`, with no build
tag, keeps the equivalent `publishing`-only assertions, so the default
build never has to choose between "test the aliases" and "test the
contract" — it always gets the contract, and the alias check moves to a
second, explicit build. `go build`, `go test` and `go list` all skip a file
whose build tag is unset by default, so `go list -deps -test ./...` (no
`-tags`) no longer sees `compat_test.go`'s import of `ego` at all, and
therefore never resolves GoAkt for it either. Passing `-tags=compat`
restores exactly the old, single-file behavior.

Two alternatives were rejected:

- **A subpackage inside the same module** (e.g. `publisher/kafka/compat`).
  `go list -deps -test ./...` at the module root already walks every
  subpackage, so this would not remove anything from the closure the
  acceptance criteria measure — it only adds indirection.
- **A separate nested Go module per publisher** (its own `go.mod` under
  `publisher/kafka/compat/`). Promoting a boundary to its own module is a
  heavier decision, gated on design.md §6 (a real benefit beyond compile
  time, its own CI discovery, a release story, no hidden coupling); a
  four-line historical alias check does not clear that bar, and it would
  need a change to `internal/cmd/ciselect`'s nested-module discovery, which
  #122 is explicitly scoped to avoid (see below).
- **A separate GitHub Actions job**, distinct from the `modules` matrix job,
  dedicated to the compatibility lane. This would need its own selection
  logic duplicating (or worse, diverging from) the module-selection rules
  above, its own checkout/setup-go/golangci-lint-install steps, and its own
  `needs:`/`if:` wiring in both `pull_request.yml` and `build.yml` — for a
  check whose environment (the module's own `go.mod`, its checked-in
  `replace`, `GOWORK=off`, the same `GO_TEST_RACE` policy) is otherwise
  identical to the module job that already exists. Running it as additional
  steps inside the same `verify-module.sh` invocation, for the same module,
  gets an equally separate `go test -tags compat ./...` build (Go treats a
  different `-tags` value as a wholly separate build; nothing here shares a
  package cache entry with the untagged build) without duplicating any
  workflow wiring, and it needed zero changes to either workflow file.

**Where it runs: the same module job, one conditional step.**
`scripts/ci/verify-module.sh` now detects a `compat`-tagged file in the
module it is verifying (`grep -rl '^//go:build compat$'`) and, only when one
exists, runs `go vet -tags compat ./...`, `golangci-lint run --build-tags
compat ...` and `go test -tags compat ./...` immediately after the
untagged equivalents — inside the same `modules` matrix job described
above, with the same `replace`, the same `GOWORK=off`, and the same
`GO_TEST_RACE` policy. No new workflow job, and no change to
`pull_request.yml` or `build.yml`: a module with no compat-tagged file (all
of `benchmark`, `example/cluster`, and any future nested module without one)
pays nothing extra.

**Why this is required-equivalent, with no `ciselect` change.**
`internal/cmd/ciselect` decides whether a publisher module is selected at
all by parsing every `.go` file in it — including `_test.go` files — with
`go/parser` in imports-only mode (`discoverModuleImports`,
`internal/cmd/ciselect/main.go`). That parser never evaluates build
constraints, so it still sees `compat_test.go`'s `import
"github.com/getsyntegrity/ego/v4"` exactly as before the build tag was added.
Consequently:

- A change confined to `publisher/kafka/compat_test.go` alone still selects
  `publisher/kafka` (rule: "changed files in the module's own directory"),
  so the compatibility lane runs whenever that file itself changes.
- A change to a root `.go` file — including `publisher.go`, where the S1
  aliases live — is a full-fallback path, so `ciselect`'s root lane reports
  mode `full`, whose `Selected` set is every included root package; every
  nested module that imports any of them, which today means every nested
  module, is selected too (reason: "imports affected root package
  `github.com/getsyntegrity/ego/v4`"). Observed with `ciselect -changed
  <publisher.go>`: mode `full`, `modules.json` =
  `["benchmark","example/cluster","publisher/kafka","publisher/nats","publisher/pulsar","publisher/websocket"]`.

So a PR that could actually break an alias always selects the four
publisher modules, and each selected module's job now runs the
compatibility lane. No branch-protection or required-check change was
needed either way: this repository has none configured today (see the
baseline table below), so "required" here means "the same `modules` job
that already gates every other publisher check," not a GitHub-enforced
status.

**What did *not* change: `go.mod`, `go.sum`, the module graph.**
`go mod tidy -diff` reports no diff in any of the four publishers, before or
after. `go mod tidy` has no `-tags` flag (`go help mod tidy`), so — unlike
`go build`/`go test`/`go list`, which only look at the default build unless
told otherwise — it conservatively keeps requirements for every custom
build tag a module's files use, including `compat`. `github.com/tochemey/goakt/v4`
therefore stays an indirect requirement in every publisher's `go.mod`,
needed to build the compatibility lane, and `go.sum` keeps its checksums,
so `go test -tags compat ./...` never needs a network fetch. The win is
entirely in the *test-compilation* closure (`go list -deps -test ./...`),
not the module graph (`go mod graph`, byte-for-byte unchanged) or the
declared requirements. Measured per publisher, `GOWORK=off`:

| Module | prod deps | default test-closure deps | default test-closure GoAkt/root | compat-tagged test-closure deps | `go mod tidy -diff` |
|---|---|---|---|---|---|
| kafka | 299 (unchanged) | 611 → 313 | 45+1 → 0 | 611 (unchanged) | no diff |
| nats | 256 (unchanged) | 565 → 273 | 45+1 → 0 | 565 (unchanged) | no diff |
| pulsar | 577 (unchanged) | 822 → 585 | 45+1 → 0 | 822 (unchanged) | no diff |
| websocket | 239 (unchanged) | 551 → 256 | 45+1 → 0 | 551 (unchanged) | no diff |

("default" = `go list -deps[-test] ./...`, no `-tags`; "compat-tagged" = the
same command with `-tags compat`, which reproduces the pre-#122 closure
exactly, confirming the historical check still exercises everything it
used to.)

**Regression guard.** Each publisher module gained
`TestUnitTestClosureExcludesRuntimeAndRoot`, which shells out to `go list
-deps -test ./...` from inside the test binary and fails if
`github.com/tochemey/goakt/v4` (any subpackage) or the root package
reappears. It is a normal, untagged test, so it runs on every `go test
./...` and fails first if this ever regresses; it was observed failing
(RED) against the pre-#122, single-file `compat_test.go` before the split.

**Timing.** Clean-`GOCACHE` `go test -count=1 ./...` wall time, one
machine, Go 1.27.1, no `-race` (this repository's local rule — CI's own
`-race` numbers are a separate, later measurement):

| Module | Before (single untagged `compat_test.go`) | After (default lane) |
|---|---|---|
| kafka | 30.1s | 13.3s |
| nats | 34.0s | 15.4s |
| pulsar | 55.8s | 29.1s |
| websocket | 33.8s | 18.2s |

The compatibility lane itself (`-tags compat`) still compiles GoAkt, so its
own wall time stays close to the "before" column (kafka measured 26.7s) —
expected, since it is deliberately running the same historical build the
default lane used to run on every `go test`.

### Release verification: three different questions

Three distinct claims exist about a nested module, and this repository
checks each of them, separately:

- **Does the monorepo build together, right now?** `verify-module.sh`
  above answers this on every PR and on every push to `main`, using the
  module's checked-in `replace github.com/getsyntegrity/ego/v4 => ../../`
  (or `../` for `benchmark`) directive. This is "integrated verification"
  in `openspec/changes/ego-arch-001/design.md` §8.
- **Does a real consumer, with no local `replace`, resolve the published
  module paths at all — the exact tags a release would create, from a
  local remote it never had to actually publish?** `verify-consumer.sh`,
  below, answers this before any tag exists.
- **Does a real consumer, resolving the module from the public proxy,
  actually get something that builds?** A checked-in `replace` is
  invisible to a consumer — Go ignores `replace` directives in a
  dependency, only in the main module — so integrated verification says
  nothing about this, and neither does `verify-consumer.sh`, which never
  talks to the public proxy. `scripts/ci/verify-published.sh
  <module-dir> <ego-version>` answers it:
  it copies the module into a scratch directory, runs
  `go mod edit -dropreplace=github.com/getsyntegrity/ego/v4
  -require=github.com/getsyntegrity/ego/v4@<version>` in one edit (dropping
  the replace and pointing at the target version together, so the module
  graph is never resolved against the old, unpublished requirement before
  the edit takes effect), then `go mod tidy && go build ./...`. If
  `go list -m github.com/getsyntegrity/ego/v4@<version>` cannot even resolve
  the version, it fails fast with one `::error::` line instead of a
  confusing `go.sum`/build error. `release.yml` runs it for each
  publisher, right after that publisher's own `go get`/`go mod tidy` and
  before any tag is created, so a publisher release can never point at a
  root version that turns out not to build.

### Verify clean consumer (`scripts/ci/verify-consumer.sh`)

`scripts/ci/verify-consumer.sh` proves that every published module path in
this repository resolves the way an outside consumer resolves it: a plain
`go get`, with no local `replace`, against the exact tags a release would
create (#134). It is the local-remote, pre-tag counterpart to
`verify-published.sh` above: it runs on every full-suite CI run, long
before any root tag exists to make the public-proxy check meaningful.

**Why `go list -m <path>@<tag>` alone is not acceptance evidence.** It
only asks the VCS whether *some* module exists at that path and tag; it
succeeds even when the tagged commit's own `go.mod` declares a different
module path — exactly the defect #134 was filed for, where every
publisher's `go.mod` still declared the root module's own path with
`/publisher/<name>` appended (repeating the root's `/v4` segment in the
middle of the nested module's path), a path with no corresponding
directory. It also never executes a single
line of the module's code, so a corrupted generated file compiles but is
never caught: a hand-edited protobuf `go_package` is one such case — the
path sits inside a length-prefixed serialized descriptor, so a text
rename still compiles but panics at init (measured in the pre-migration
spike for #134). Only `go get` (which validates the module's own declared
path against the requested import path), plus `go build` and `go run`
(which executes every package's `init`), catch both failure modes; the
script requires all three.

**What it does.** It discovers everything it needs instead of hard-coding
it: the root module path from the repository's own `go.mod`; the released
publisher directories from `scripts/ci/release-modules.txt`; each
publisher's own module path and the root version it requires from its own
`go.mod`. (All released publishers must require the same root version, or
the script fails with a clear message naming the mismatch.) It then:

1. Clones the committed `HEAD` into a temporary bare repository —
   uncommitted changes are never part of what it checks.
2. Creates release tags **only in that temporary clone**: the root tag
   (the version every publisher already requires), each publisher's
   `<dir>/<VERIFY_CONSUMER_PUBLISHER_VERSION>` (default `v0.1.0`), and one
   negative tag, `<first publisher dir>/v2.0.0`, which must be *rejected*
   — a `v2+` tag on a module path with no `/v2` suffix is invalid under
   Go's own major-version rule, and the script fails if it somehow
   resolves.
3. Points Go at that local clone instead of the real one, through an
   isolated git configuration (`GIT_CONFIG_GLOBAL`, `protocol.file.allow`,
   a `url.insteadOf` rule) and `GOPRIVATE` set to the repository's own
   path, so nothing here ever reaches the public module proxy or
   checksum database for this repository's own paths.
4. From a fresh, empty consumer module, blank-imports the root module's
   root package and every publisher's package (a blank import runs every
   package's `init`, catching a corrupted descriptor a plain build
   cannot), `go get`s each publisher at its tag, `go mod tidy`s, `go
   build`s, and `go run`s the result — the run must print its success
   line and exit `0`.
5. Asserts, with an explicit message on failure: the consumer's `go.mod`
   carries no `replace`; `go list -m all` shows every publisher and the
   root at the expected version; each publisher resolves from the
   subdirectory it actually lives in (`go list -m -f
   '{{.Origin.Subdir}}'`, or `go list -m -json` and its `Origin.Subdir`
   field on an older `go` that lacks the template field); and the negative
   `v2.0.0` tag is rejected.

**Usage.** `scripts/ci/verify-consumer.sh` — no arguments, run from
anywhere inside the repository. `VERIFY_CONSUMER_PUBLISHER_VERSION`
overrides the publisher tag version it creates in the temporary clone
(default `v0.1.0`); the root tag is never configurable, since it is
always the version the released publishers themselves require. Locally,
it needs `jq` on `PATH` and reuses the caller's ordinary `GOCACHE`; when
the caller already has a populated module download cache, its
`cache/download` directory is added to `GOPROXY` as a read-only source
for third-party modules only, purely so a repeated local run does not
re-download the world (`GOPRIVATE` already bypasses any proxy for this
repository's own paths, so this can never mask a stale copy of them).

**CI wiring.** A `consumer` job runs it in both `pull_request.yml` and
`build.yml`, and both `ci-gate` jobs require it to succeed or be
legitimately skipped, exactly like `modules` (see "The `ci-gate` job"
above). In `pull_request.yml` it only needs `plan` and only runs when
`plan` already selected `full` mode: a PR confined to, say, one nested
module's business logic never pays for an extra clone and module
download, while any change that could actually affect module-path
resolution (`go.mod`, a workflow, anything under `scripts/ci/`) already
forces `full` mode on its own (see "Modes and fail-safe rules" above). In
`build.yml` it always runs, with no mode condition, because `plan` there
always selects the full suite anyway.

### Version policy

- The root module is released first, as a semantic-version tag `v4.x.y`.
- Each publisher module is released only against a root version that
  already exists on the module proxy — never against an unpublished
  version, and never verified only through the local `replace`. Getting a
  publisher from "root just tagged" to "publisher tagged and released" is
  a three-stage flow, not one job, because `main`'s branch protection
  (`docs/main-branch-policy.md`) does not let any workflow push a commit
  to `main` or open a pull request on its own.

  **Stage 1 — `release.yml`'s `prepare-publisher-bump` job.** Still
  triggered by pushing a root `v*` tag, and still running after `gate`
  and `release-ego` (see "Release gate" below). It discovers which
  directories under `publisher/` to release from `publisher/*/go.mod`
  (never a hand-written list), then for each one runs
  `go get github.com/getsyntegrity/ego/v4@<tag>`, `go mod tidy` and
  `scripts/ci/verify-published.sh`, exactly as before this change. What
  changed is what happens to the result: instead of committing and
  pushing straight to `main`, the job creates a branch
  `release/publishers-<tag>` from `origin/main`, commits the bumped
  `go.mod`/`go.sum` on it, and pushes only that branch — never `main`,
  and no publisher tag or GitHub release is created in this job. Its step
  summary prints the compare link and the exact command to open the PR by
  hand, for example:

  ```bash
  gh pr create --base main --head release/publishers-v4.5.0 \
    --title "chore(publishers): update ego dependency to v4.5.0" \
    --body "..."
  ```

  **Stage 2 — a human opens and merges the bump PR.** Because a person,
  not a bot, runs that `gh pr create` command, the pull request is
  ordinary: it goes through the same required `CI Gate` check and the
  same review conventions as any other PR into `main`
  (`docs/main-branch-policy.md`). This is also why no CI approval-bypass
  machinery is needed anywhere in this flow — nothing here ever tries to
  push to or merge into `main` on its own.

  **Stage 3 — the dispatched `release-publishers.yml` continuation.**
  Once the bump PR is merged, someone with repository access manually
  dispatches `.github/workflows/release-publishers.yml`
  (`workflow_dispatch`) with four inputs: `sha` (the merge commit on
  `main`), `ego_version` (the root tag the publishers must require, e.g.
  `v4.5.0`), `bump` (`patch`/`minor`/`major`, default `patch`), and
  `dry_run` (boolean, default `true`). The workflow validates every
  precondition before doing anything: `sha` and `ego_version` are
  well-formed, `sha` is genuinely reachable from `origin/main`,
  `build.yml` completed successfully for that exact `sha` (reusing the
  same `internal/cmd/releasegate` check "Release gate" below describes),
  every released publisher's `go.mod` actually requires the root at
  `ego_version`, and none of the tags it is about to create already
  exist locally or on `origin`. `dry_run: true` (the default) is safe to
  run at any time — it runs every one of those checks and prints the
  resulting plan (exactly which publisher tags would be created) without
  creating anything. Setting `dry_run: false` performs the real work: it
  re-checks ancestry and tag conflicts once more immediately before
  tagging (origin state can move between planning and tagging), then
  creates the publisher tags at `sha`, pushes them atomically, and
  creates their GitHub releases — the same `gh release create` step the
  old single-stage job used to run, preserved verbatim.
- `benchmark`, `example/cluster` and `test/compat` are never released.
  They exist only as integrated-verification consumers (`verify-module.sh`
  covers them in the PR and `main` lanes) and keep their `replace`
  directives permanently. Under ADR `ego-arch-006` decision D5 an
  unreleased module is allowed only while no released module requires it
  and while it is listed here; `release.yml` only releases
  `publisher/*`.

This section describes what `release.yml` and `release-publishers.yml` do
today. `release.yml` still hard-codes the root module path and has no
explicit notion of release order or of a tag's major version matching its
module path's `/vN` suffix. `internal/cmd/releaseplan` (see "Release plan
dry run (releaseplan)" above) computes the same root-first order and the
D2 (a) tag scheme from `scripts/ci/release-modules.txt` and each module's
own `go.mod` for the `build.yml` dry run; `release-publishers.yml` reuses
that same package's decision logic — extended with the continuation
checks named above (SHA/version format, required-version, publishers-only
tag computation, tag-conflict detection) — for the real tag computation
in stage 3, instead of duplicating that logic in shell.

### Toolchain requirements

CI builds and tests everything — the root module and every nested
module — with Go 1.27.0 (`actions/setup-go`'s `go-version` input). The
root module's own `go.mod` declares `go 1.26.0`; the nested modules
declare `go 1.26.0` (`benchmark`, `example/cluster`, `publisher/kafka`,
`publisher/nats`, `publisher/websocket`) or `go 1.26.2`
(`publisher/pulsar`, and `test/compat`, which requires it), and none of them pins a `toolchain` line, so the
installed 1.27.0 toolchain satisfies every one of them without
downloading anything else. `golangci-lint` is pinned to the same version,
`v2.13.1`, for the root lane and for every nested module's own lint step.

> **Note (#159, F4).** If PR #169 (branch `feat/159-f4a-releaseplan`, the
> `internal/cmd/releaseplan` module-and-tag planner) merges before this
> section does, this file will need a small rebase: that PR appends its
> own new section here too, at the same end-of-file location.

### Release gate (#159, F4 PR-B)

Before this change, pushing a tag matching `v*` immediately created a
GitHub Release and started bumping and tagging the four `publisher/*`
modules — nothing checked that the tagged commit had ever actually passed
CI. A human could tag a broken, unreviewed, or simply wrong commit and the
whole publish chain would run anyway.

`.github/workflows/release.yml` now runs a `gate` job first, before
`release-ego` (which creates the GitHub Release) and before
`prepare-publisher-bump` (which needs `release-ego` and, as of #159 F4
PR-C, only bumps each publisher's `go.mod`/`go.sum` and pushes a
`release/publishers-<tag>` branch — it creates no publisher tags itself;
see "Version policy" above): `gate → release-ego →
prepare-publisher-bump`. `gate` fails the whole workflow — nothing
downstream ever starts — unless the exact tagged commit both is on `main`
and has a `build.yml` run that completed with conclusion `success`.

**Why the exact SHA, not "a recent green build."** A green `build.yml` run
on a *different* commit — an earlier commit on the same branch, or a
commit that has since been amended or rebased away — says nothing about
whether the code actually being tagged builds and passes. The gate only
ever asks GitHub for runs whose `head_sha` equals the tagged commit's own
SHA (`GET
/repos/{owner}/{repo}/actions/workflows/build.yml/runs?head_sha=<sha>`),
never a run on a nearby commit and never the branch's latest run.

**Dereferencing the tag.** A lightweight tag already points straight at a
commit. An *annotated* tag (the kind `git tag -a` creates, carrying its
own message, tagger, and date) points at a separate tag object, which in
turn points at a commit — possibly through a chain of more than one tag
object. `git rev-parse "$TAG^{commit}"` (the `gate` job's first step,
`Resolve the tagged commit and its main membership`) dereferences either
shape down to the actual commit in one call; a bare `git rev-parse "$TAG"`
would instead return an annotated tag's own object SHA, which `build.yml`
never runs against and which `head_sha` would therefore never match.

**Main membership.** The tagged commit must also be reachable from
`origin/main` — a tag on a commit that was never merged (a stray local
commit, a force-pushed-away commit, a commit only on some other branch) is
rejected outright, independent of anything `build.yml` ever reported.
This is computed with plain `git`, not the GitHub compare API: `git fetch
origin main --quiet` followed by `git merge-base --is-ancestor <sha>
origin/main` inside the same job that already checked out the repository
with `fetch-depth: 0`, needing no extra API call, no pagination, and no
extra permission scope beyond `contents: read`. The compare API
(`GET /repos/{owner}/{repo}/compare/main...<sha>`) was the rejected
alternative: it would work too, but it needs its own error handling for a
commit GitHub has never indexed, and it adds a second API surface for no
benefit over a `git` command the job already has the history for.

**The most recent completed run governs.** Among the runs matching the
exact SHA, `internal/cmd/releasegate`'s pure decision function,
`Decide` (`internal/cmd/releasegate/decision.go`), looks only at the most
recent one — ordered by GitHub's `run_started_at` (when this run's current
attempt actually started), not `created_at` (when the run object was
first recorded), falling back to `created_at` only when `run_started_at`
is absent; ties broken by the higher `run_attempt`, then by the higher
run ID (GitHub Actions run IDs are assigned monotonically instance-wide).
`created_at` alone is not enough: **re-running** a workflow run (from the
Actions UI, the API, or `gh run rerun`) keeps the same run ID and the same
`created_at`, but advances `run_started_at` and `run_attempt` — verified
against real run
[35120281495](https://github.com/getsyntegrity/ego/actions/runs/35120281495)
(`run_attempt: 2`, `created_at` 2026-09-16T16:11:21Z, `run_started_at`
2026-09-16T16:22:19Z, eleven minutes later). A run that was re-run *after*
another run's `created_at` is the fresher evidence even though its own
`created_at` is older; sorting by `created_at` alone would rank the two
backwards and let a stale re-run's original, superseded result outvote
the real most recent one.

- If that latest run has not completed yet (`status` is `queued`,
  `in_progress`, `waiting`, `pending`, or `requested`), the gate **waits**
  and polls again.
- If it completed with conclusion `success`, the gate **passes**.
- If it completed with any other conclusion (`failure`, `cancelled`,
  `skipped`, `timed_out`, ...), the gate **fails**, naming the conclusion
  and the run's URL.
- If no run at all exists yet for the SHA, the gate **waits**.

Two alternative rules were considered and rejected. "Any run for this SHA
ever succeeded" would let a commit pass even after a later re-run
regressed it — the same SHA can genuinely be re-run more than once (a
manual `workflow_dispatch` re-run, most commonly), and a newer failure on
the exact commit being tagged is exactly the signal this gate exists to
catch. "The oldest completed run governs" has the opposite, equally wrong
problem: it would keep failing a commit whose first CI run was flaky and
failed, even after a later re-run on the very same SHA turned green. Only
"the most recent completed run governs" treats a later, more-informed
signal about the same commit as authoritative in both directions.

**`workflow_dispatch` runs count too.** `build.yml` also triggers on
`workflow_dispatch` (see "What each workflow runs" above), and the gate's
GitHub query filters by `head_sha` only — never by `branch` or `event`
(see `internal/cmd/releasegate/client.go`'s `ListBuildRuns` doc comment).
A green manually-triggered re-run against the exact tagged SHA is judged
just as strong evidence that `build.yml` passed for that commit as an
automatic push-triggered run would be; filtering it out at the API layer
would only create a confusing case where the gate says "no run found" for
a commit an operator can see is green in the Actions tab.

**The bounded wait.** A tag is typically pushed for a commit that just
landed on `main`, so its `build.yml` run (triggered by that same push to
`main`) may still be in flight, or GitHub's API may not have indexed it
yet, when `release.yml`'s tag-triggered run starts. The `gate` job polls
every 30 seconds (`-interval`) for up to 20 minutes (`-timeout`) before
giving up, via `internal/cmd/releasegate/main.go`'s bounded wait loop
(`waitForGate`). Each sleep between polls is clamped to whatever time is
actually left before the deadline (PR #171 review, R3/minor): `-interval`
is an operator-set flag, and nothing stops it from being configured
larger than `-timeout`; without the clamp, that single sleep would run
past the deadline before the loop ever got to check it again, silently
turning a short `-timeout` into a much longer real wait. Once the timeout
expires, the gate fails with a clear message naming the SHA and the last
known state — a fetch error, a decision reason (still pending, or no run
found at all), or both when the loop saw one of each before giving up —
it never hangs the workflow indefinitely, and it never
silently treats "still waiting" as success. The off-main case is checked
once, up front, without ever calling GitHub's API: main reachability
cannot change while the job runs, so polling for it would only waste time
and API calls.

**A transient GitHub API error does not fail the gate outright.** A
`ListBuildRuns` call can fail for reasons that have nothing to do with the
tagged commit's CI state — a `5xx` from GitHub, a rate limit, a network
blip. Before PR #171's review, `waitForGate` treated any such error as an
immediate, terminal failure, which meant a single flaky API call could
sink an otherwise-green release. It is now treated exactly like a pending
run: logged, and retried on the same bounded-wait schedule (clamped
sleep included) until either a fetch succeeds or the deadline passes.
This stays fail-closed — `Pass` is only ever returned immediately after a
fetch that succeeded AND whose `Decide` result was itself `Pass`; a run
of errors can only ever lead to `Fail` at the deadline, never to a `Pass`
by default. The deadline message names the last fetch error, the last
decision reason `Decide` produced, or both, so an operator can tell "GitHub
was unreachable" apart from "still genuinely waiting on a pending run".

**Not every API error is worth retrying (#159 T2, PR #171 follow-up).**
`ListBuildRuns` (`internal/cmd/releasegate/client.go`) classifies each
non-2xx response so `waitForGate` knows whether waiting could possibly
help:

- **Retried until the deadline:** any `5xx` server error, `429 Too Many
  Requests`, a network/transport error (the connection failed before a
  response even came back), and a `403 Forbidden` that GitHub is actually
  using as a rate-limit response. A `403` is recognized as a rate limit,
  not a permission error, when the response carries an
  `X-RateLimit-Remaining: 0` header, carries a `Retry-After` header, or
  its body's message mentions a rate limit (GitHub's secondary rate limit
  has no dedicated header — only that prose message, e.g. "You have
  exceeded a secondary rate limit").
- **Failed immediately, no retry:** `401 Unauthorized` (the token is
  invalid or missing the `actions:read` scope — that does not fix itself
  while this process sits and waits), `404 Not Found` (the repo, workflow
  file, or SHA is not visible to this token — waiting does not make it
  appear), and a `403 Forbidden` that is *not* a rate limit by the test
  above (a genuine permission problem, not a throttle).

`waitForGate` tells the two apart with `errors.Is(err,
ErrPermanentGitHubError)`, a sentinel `ListBuildRuns` wraps into the error
it returns for the fail-fast cases. Before this change, a `401` or `404`
would burn the entire `-timeout` budget retrying every `-interval`, even
though the very first response already proved retrying was pointless;
now it fails on the first attempt, with a message that says the error is
permanent and why.

**How to test the gate without publishing anything.** Every call the gate
makes is a read-only GitHub API request; it creates nothing, tags
nothing, and pushes nothing. Run it directly, from a checkout with a full
history (`git fetch --all` or an equivalent clone) and a token that can
read Actions runs:

```sh
GITHUB_TOKEN=$(gh auth token) go run ./internal/cmd/releasegate \
  -repo getsyntegrity/ego \
  -sha <full 40-character commit SHA> \
  -on-main=true \
  -timeout 0
```

`-timeout 0` makes it check exactly once and return immediately instead
of waiting — the right mode for a manual dry run. `-on-main` is not
computed for you by this command; pass `true` or `false` yourself (or
compute it first with `git merge-base --is-ancestor <sha> origin/main`).
`-timeout` and `-interval` accept any Go duration (`20m`, `90s`, ...).

Observed against the real `getsyntegrity/ego` repository (2026-09-28, read
only, nothing published):

| Case | SHA | `-on-main` | Result |
|---|---|---|---|
| Green run on main | `8b3962acc109ac06da3a4ada4c3186be7d46cfa5` | `true` | **PASS** — run [36420765355](https://github.com/getsyntegrity/ego/actions/runs/36420765355), conclusion `success` |
| PR-branch commit, off main | `743692a7804005408355e5066d165debc885f9f4` (branch `feat/159-f4a-releaseplan`, PR #169) | `false` | **FAIL** — "not reachable from origin/main" (checked before any GitHub call) |
| Same commit, `-on-main` forced `true` (illustration only — it is not really on main) | `743692a7804005408355e5066d165debc885f9f4` | `true` | **FAIL** ("timed out after 0s ... no build.yml run found yet") — `build.yml` never runs on a PR branch, only on push to `main` or `workflow_dispatch` |
| Real failed run on main | `ddf9337092a5b4e43a6d90897914f34ed52f453f` | `true` | **FAIL** — run [35120281495](https://github.com/getsyntegrity/ego/actions/runs/35120281495), conclusion `failure` |

No `cancelled` `build.yml` run on `main` was found to test the same way
(`gh api ".../runs?branch=main&status=cancelled"` returned none) — that
path is covered by `internal/cmd/releasegate`'s own unit test fixtures
instead (`TestDecide_CancelledConclusionFails`,
`decision_test.go`), which do not depend on any particular commit's real
CI history continuing to exist.

**How the publisher-bump-vs-branch-protection conflict was resolved.**
Until #159's F4 PR-C, the `release-publishers` job (`release.yml:169-170`
at the time) ran right after `release-ego`, committed each publisher's
bumped `go.mod`/`go.sum`, and pushed the result straight to `main` with
`git push origin HEAD:main`. `main` is a protected branch with a
required, strict status check ("CI Gate," see "The `ci-gate` job" above)
and `enforce_admins: true`, so a direct push of a brand-new commit that
had never run through "CI Gate" would have been rejected by GitHub with
`GH006: Protected branch update failed`. The gate work described in this
section deliberately left that step untouched at the time — the conflict
was filed here as open, rather than silently worked around.

It is resolved now. `release.yml`'s publisher job (renamed
`prepare-publisher-bump`) no longer pushes to `main` at all, and no
workflow opens a pull request on its own. See "Version policy" above for
the resulting three-stage flow: `release.yml` pushes a bump branch, a
human opens and merges the ordinary, normally-reviewed pull request it
prints the command for, and an explicitly dispatched
`release-publishers.yml` run does the actual publisher tagging once that
PR has landed on `main`.
