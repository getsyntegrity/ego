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
   to `$RUNNER_TEMP/changed.txt`.
3. **Select packages**: `go run ./internal/cmd/ciselect -changed
   "$RUNNER_TEMP/changed.txt" -out-dir "$RUNNER_TEMP/ci"`. If that command
   fails for any reason (a `go list` error, an unreadable file, a bug in
   the selector itself), the workflow logs a `::warning::` and re-runs with
   `-all -reason "selector failed; full-suite fallback"` instead — if
   *that* also fails, the job fails. Selection is never silently skipped.
   The selector's own `summary.md` is appended to the job's
   `$GITHUB_STEP_SUMMARY`, so the exact package list and the reason for it
   are visible on every PR run, not just inferred from logs.
4. **Run tests**: `scripts/ci/go-test.sh "$RUNNER_TEMP/ci" coverage.out`,
   with `GO_TEST_RACE=1` (the race detector stays on for pull requests).
5. **Codecov upload**: only when the selector's mode was `full`. A partial
   package selection produces a partial coverage profile, and uploading
   that as "project coverage" would misstate it — Codecov's percentage
   would swing based on which packages happened to be touched, not on
   actual coverage change. When the mode is `affected` or `none`, the
   workflow instead writes a line to the job summary explaining that
   coverage was not uploaded and that `main`'s full-suite run remains the
   source of truth.

### `build.yml` (the full-suite gate)

Runs on every push to `main`, and can be triggered manually for any ref
via the Actions "Run workflow" button (`workflow_dispatch`). It runs
`go run ./internal/cmd/ciselect -all -out-dir "$RUNNER_TEMP/ci"` — always
the full suite, no change detection — appends the summary to the job
summary the same way, then `scripts/ci/go-test.sh` with the race detector
on, and always uploads to Codecov. This is the mandatory gate and the only
source Codecov's project coverage is uploaded from.

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
| Full-fallback    | Exact files `go.mod`, `go.sum`, `Makefile`, `Dockerfile.ci`, `.golangci.yml`, `codecov.yml`, `buf.yaml`, `buf.gen.yaml`; directories `.github/`, `protos/`, `internal/cmd/ciselect/`, `scripts/ci/`, `egopb/`; and any `.go` file directly in the module root (the shared root package) | Forces mode `full` |
| Satellite        | A directory that has its own `go.mod` on disk (`benchmark/`, `example/cluster/`, `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`)                          | Selects nothing for that file; recorded as "not covered by this lane" |
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

## Coverage policy

Coverage is always produced by native `go test -covermode=atomic
-coverpkg=<all included packages> -coverprofile=coverage.out`, never
`go-acc`. `go-acc` is removed from both workflows and from `make
docker-test`. The coverage denominator (`-coverpkg`) is always the full
included package set, in every mode, so a PR's `affected`-mode coverage
number and `main`'s `full`-mode coverage number are directly comparable —
only the numerator (which packages actually ran) differs.

Only `build.yml`'s full-suite run uploads to Codecov. `pull_request.yml`
uploads only when its own selection happened to be `full`; an `affected`
or `none` PR run never uploads, so Codecov's project coverage always
reflects a complete run.

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

## Multi-module path (#104)

`benchmark/`, `example/cluster/`, `publisher/kafka`, `publisher/nats`,
`publisher/pulsar` and `publisher/websocket` each carry their own `go.mod`
and are not reached by `go list ./...` from the root module, so they are
outside `internal/cmd/ciselect`'s package graph entirely. The selector
already accepts `-module-dir` and `-repo-root` flags so a future change
can point it at one of those satellite modules and run it again for that
module's own graph — the selection algorithm itself does not assume the
module is the repository root. Today, a change confined to a satellite
module classifies as `Satellite` and selects nothing; `summary.md` records
it as "not covered by this lane (see #104)" so that limitation is visible
on every PR that touches one, rather than silently skipped. Wiring CI to
actually run a satellite module's own tests is left to #104.
