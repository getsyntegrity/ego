# Nested module CI (#111)

## Problem

The repository has six nested Go modules: `publisher/{kafka,nats,pulsar,websocket}`,
`benchmark` and `example/cluster`. None of them is inside the root
`go list ./...` graph, and no CI job builds them. When a pull request touches
only `publisher/kafka`, the test selector (`internal/cmd/ciselect`) classifies
every file as `ClassSatellite`, returns `ModeNone`, and the check goes green
without compiling Kafka (`selector/select.go:129-146`). The release workflow
also runs `go get ego@VERSION` while the local `replace ../../` stays in place,
so the published root version is never actually built against.

## What changes

1. `ciselect` learns about modules. A changed file under a nested module
   selects that module. A root change selects every nested module that
   directly imports a package in the root affected set; nested imports are
   read with `go/parser` (no network, no `go list` inside the module). `-all`,
   a changed `go.mod`/`go.sum` at the root, or a changed CI path
   (`.github/`, `scripts/ci/`, `internal/cmd/ciselect/`, `.golangci.yml`)
   selects all modules. The selection is written to `modules.json` and to
   the job summary with one reason per module.
2. `scripts/ci/verify-module.sh <dir>` builds, vets, lints (root
   `.golangci.yml`) and tests one module, with the race detector in CI.
3. `pull_request.yml` and `build.yml` fan out one matrix job per selected
   module, fed by `modules.json`. The module list is never written by hand.
4. `release.yml` discovers publishers from `publisher/*/go.mod` and, after
   `go get`, builds each publisher with its `replace` dropped against the
   proxy. A missing root tag fails with a clear message.
5. `docs/ci.md` and `CHANGELOG.md` describe the module lane, the toolchain
   requirements and the version policy.

Rejected alternative: a single job that loops over all modules. With no
branch protection on `main`, every check counts, and per-module jobs make
failures and selection reasons visible without reading logs.

Rejected alternative: selecting all modules on every root change. Simpler, but
it drops the root fast lane from #108 for leaf changes. The import-intersection
rule keeps leaf changes cheap and still catches real reverse consumers.

## Constraints

- TDD: strict (source: user global CLAUDE.md). Runner: `go test ./internal/cmd/ciselect/...`.
- No `-race` in local runs; CI keeps its existing race policy.
- Root fast lane and `ModeNone` for docs-only changes are preserved.
- RDD: off (global). Delivery follows ordinary repository policy.

## Tasks

- [x] T1 `selector`: module selection (satellite change, reverse consumer via
      direct imports, full on `-all`/go.mod/CI path), `Result.Modules` with
      reasons, summary section. Check: `go test ./internal/cmd/ciselect/...`.
      Route: delegated writer (writer trigger: selector + tests + main).
- [x] T2 `ciselect` main: parse nested imports, write `modules.json`.
      Check: run ciselect on a kafka-only change list → kafka selected;
      docs-only → `[]`.
- [ ] T3 `scripts/ci/verify-module.sh` + matrix jobs in `pull_request.yml` and
      `build.yml`. Check: script passes locally for all six modules;
      `actionlint` if available.
- [ ] T4 `release.yml`: discovered publishers + published-version build with
      `replace` dropped. Check: `scripts/ci/verify-published.sh` fails
      clearly for an unpublished version (v4.4.3) and passes for a published one.
- [ ] T5 Docs: `docs/ci.md`, `CHANGELOG.md`, toolchain notes. Check: readback.

## Acceptance (from #111)

- A Kafka-only change runs Kafka build, vet and lint; the check cannot pass without them.
- All six modules are discovered and verified in the full gate; no static list.
- Root-only leaf changes keep the fast lane.
- A failing nested build/test/vet/lint fails the PR check.
- Selection and executed checks are visible in job summaries.
- Release verification fails clearly if the root version is not published.
- Tests cover satellite-only changes and module discovery.

## Delivery

Forecast about 600 authored lines. Single PR with one commit per task
(assumption, see PR description); it can be split at T3/T4 if a reviewer asks.

## Progress

### T1+T2 (commit c22056d)

- TDD: RED observed first — `internal/cmd/ciselect/selector/modules_test.go`
  and `internal/cmd/ciselect/modules_test.go` were written against
  `Options.Modules`, `Module`, `ModuleSelection`, `Result.Modules` and
  `discoverModuleImports`, none of which existed yet:
  `GOROOT= go test ./internal/cmd/ciselect/...` failed to compile
  (`opts.Modules undefined`, `undefined: Module`, `undefined: ModuleSelection`,
  `undefined: discoverModuleImports`).
- Implemented `selector.Select` as `selectRoot` (unchanged root logic) plus
  `selectModules` (new, independent of root Mode): changed-file-under-dir,
  root-affected-set intersection, and a closed full-gate set
  (`-all`, root `go.mod`/`go.sum`, `.github/`, `scripts/ci/`,
  `internal/cmd/ciselect/`, `.golangci.yml`). Updated
  `TestSelect_SatelliteOnlyIsNone` per the task's own instruction — it now
  asserts the root lane still reports `ModeNone` for a `benchmark`-only
  change, but the `benchmark` module is now selected (the gap it used to
  encode).
- `GOROOT= go test ./internal/cmd/ciselect/... -v`: all 28 tests pass (19
  selector, 1 main-level discovery test, 8 new module-selection tests).
- Local toolchain note: this environment's `GOTOOLCHAIN=auto` resolves
  `go` to the `go1.27.1` toolchain module while the shell exports
  `GOROOT=/home/pablog/sdk/go1.26.6`, which breaks any command that
  compiles stdlib (`go vet`, `golangci-lint`) with
  `compile: version "go1.26.6" does not match go tool version "go1.27.1"`
  or, for `golangci-lint` (built with go1.26.6), a stdlib typecheck error
  in `internal/poll` — reproduced identically on the untouched
  `internal/cmd/archcheck` package, confirming it is pre-existing and
  environmental, not caused by this change. Worked around by running with
  the matching `go1.26.6` SDK explicitly
  (`PATH=/home/pablog/sdk/go1.26.6/bin:$PATH GOROOT=/home/pablog/sdk/go1.26.6
  GOTOOLCHAIN=local`), which satisfies the root `go.mod`'s `go 1.26.0`
  directive: `go vet ./internal/cmd/...` clean, `golangci-lint run
  ./internal/cmd/...` → `0 issues`.
- Manual check: `go run ./internal/cmd/ciselect -changed <kafka-only>
  -out-dir <dir>` → `modules.json` = `["publisher/kafka"]`, mode `none`;
  same for a docs-only change → `modules.json` = `[]`, mode `none`.
