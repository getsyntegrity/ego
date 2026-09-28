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
  root module's own steps — vendoring and tidy, `archcheck`, lint,
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
   `plan`'s `mode` is not `none`): vendoring and tidy, `archcheck`, lint,
   then downloads `ciselect-plan` and **runs tests**:
   `scripts/ci/go-test.sh "$RUNNER_TEMP/ci" coverage.out`, with
   `GO_TEST_RACE=1` (the race detector stays on for pull requests), plus
   `govulncheck ./...` (see "Root module in the matrix" below).
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
`modules` always runs here too: it downloads `ciselect-plan`, then runs
`scripts/ci/go-test.sh` with the race detector on, `govulncheck ./...`, and
appends the same coverage summary to the job summary. This is the
mandatory gate and always runs the complete suite, root and every nested
module.

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

`-all -reason "<why>"` produces the full plan used by `main`, release and
manual runs. A non-zero exit is a planning failure: callers must fall back
to `-all` (as the `plan` job does) or fail; they must never run less.

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
github.com/pablogore/ego/v4/tenancy imports github.com/tochemey/goakt/v4/actor: rule contract-allowlist (design.md §3): ...
```

The fix is almost always to depend on a contract package instead of the runtime. Do not add a baseline entry to silence a new violation.

A module-cycle failure names the requiring module as the importer and the required module as the import, and its reason spells out the cycle, for example `requires github.com/pablogore/ego/v4/contracts, which requires it back: github.com/pablogore/ego/v4 -> github.com/pablogore/ego/v4/contracts -> github.com/pablogore/ego/v4`. The go toolchain accepts module cycles, and `ciselect` does not check for them (a cycle there would only widen a selection), so archcheck is where one fails the build. Likewise, Go does not stop a module from importing another in-repository module's `internal/` package, because every module here shares the root module's path prefix; `no-cross-module-internal` does.

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
   `scripts/ci/`, `internal/cmd/ciselect/` and `protos/`. `-all` and an
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
6. `govulncheck ./...` (ego-arch-006 spec 1, C2) — scans the module's
   resolved dependency graph for known vulnerabilities reachable from its
   code. CI always installs `govulncheck` first (the workflow's "Install
   govulncheck" step, `go install golang.org/x/vuln/cmd/govulncheck@latest`),
   so this step always runs there; a local run of `verify-module.sh` without
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
`go mod tidy && go mod vendor`, `archcheck`, the root's own
`golangci-lint-action` lint step, `scripts/ci/go-test.sh` and its coverage
summary) is `if: matrix.module == '.'`; the `Verify module`
(`scripts/ci/verify-module.sh`) step, and installing `golangci-lint`
manually for it, are `if: matrix.module != '.'`. `govulncheck` is
installed once per matrix job unconditionally (both the root and every
nested module need it), then run against the root with
`GOFLAGS=-mod=vendor` right after its own coverage summary, and against
each nested module inside `verify-module.sh` (see above).

### The `ci-gate` job: one required status check

Before this change, neither workflow had a single status check that branch
protection could require: the `modules` job is *skipped* (not green, not
red) whenever `modules.json` is `[]`, and GitHub branch protection cannot
require a check that a run sometimes never reports at all. `ci-gate` fixes
this. It is the last job in both `pull_request.yml` and `build.yml`,
`needs: [plan, modules]`, and runs with `if: always()` so it still runs
even when an earlier job failed. Its one step reads `needs.plan.result` and
`needs.modules.result`, and fails if `plan` did not succeed, or if
`modules` finished as anything other than `success` or `skipped`. Once
`plan` itself succeeded, `modules` can only be "skipped" because `plan`'s
own `modules.json` was `[]` — never a hidden failure. If `plan` itself
fails, `modules` is skipped too (its `needs: plan` was not satisfied), but
`ci-gate` already failed on `plan`'s own result, so that skip changes
nothing. This makes `ci-gate` pass whether the matrix fanned out to zero,
one, or many modules (root included), and fail visibly whenever `plan`, or
a real `modules` failure/cancellation, would otherwise have left branch
protection with nothing to require.

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
`github.com/pablogore/ego/v4/test/compat`, like `benchmark`: nothing outside
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
"github.com/pablogore/ego/v4"` exactly as before the build tag was added.
Consequently:

- A change confined to `publisher/kafka/compat_test.go` alone still selects
  `publisher/kafka` (rule: "changed files in the module's own directory"),
  so the compatibility lane runs whenever that file itself changes.
- A change to a root `.go` file — including `publisher.go`, where the S1
  aliases live — is a full-fallback path, so `ciselect`'s root lane reports
  mode `full`, whose `Selected` set is every included root package; every
  nested module that imports any of them, which today means every nested
  module, is selected too (reason: "imports affected root package
  `github.com/pablogore/ego/v4`"). Observed with `ciselect -changed
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
- `benchmark`, `example/cluster` and `test/compat` are never released.
  They exist only as integrated-verification consumers (`verify-module.sh`
  covers them in the PR and `main` lanes) and keep their `replace`
  directives permanently. Under ADR `ego-arch-006` decision D5 an
  unreleased module is allowed only while no released module requires it
  and while it is listed here; `release.yml` only releases
  `publisher/*`.

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
