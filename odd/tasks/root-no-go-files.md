# Root without Go files (#124, #159 A6)

## Objective

Leave zero `*.go` files at the repository root of `github.com/getsyntegrity/ego/v4`.
The root keeps only `go.mod`, `go.sum`, docs, configuration and scripts.

## Problem

The root directory is package `ego`: 28 production files and 54 white-box tests
(Engine, actors, options, public aliases). Every root file shares unexported helpers,
so the package cannot be split without exporting internals.

## What changes

The whole root package moves, unchanged in semantics, to a new package inside the
root module: `github.com/getsyntegrity/ego/v4/engine` (package `engine`). No new
`go.mod` is created. Symbol names stay the same (`ego.NewEngine` becomes
`engine.NewEngine`); renaming would be a refactor, not a move.

## Why this is a public break, and why it ships in v4

`v4.0.0` (tag `6016ede`, 2026-09-28) is published and cached by proxy.golang.org;
it exposes `import "github.com/getsyntegrity/ego/v4"`. Removing the root package
breaks that import path. The strict SemVer answer is a `/v5` module. The user chose
explicitly to ship it in the v4 line (next tag `v4.1.0`) because there are no known
consumers: GitHub code search finds no importer outside this repository.
Rejected alternatives: `/v5` (new major hours after v4.0.0, against the #159 order)
and a root facade (a Go shim at the root contradicts the objective).

## Scope

In: move root package to `engine/`; update importers (compose/goakt, examples,
migration tests, benchmark, example/cluster, test/compat, publisher closure tests);
Makefile mockery, scripts/ci, ciselect and archcheck root assumptions; readme,
docs/ci.md, CHANGELOG with migration map.
Out: tags, releases, behaviour changes, splitting `engine` further (#159 A4/A5).

## Constraints

- TDD: strict (source: user global CLAUDE.md), runner `go test` with `GOWORK=off`.
- RDD: off (global). No native review.
- Delivery: single PR, strategy `exception-ok` (the diff is dominated by renames).
- Conflicts expected with open PR #181 (`internal/cmd/ciselect/selector/classify.go`,
  `modulegraph.go`).

## Tasks

- [x] T1 Add an archcheck guard "no Go files at the module root" (RED), move the root
  package to `engine/` with `git mv`, fix package clauses and in-module importers,
  make the architecture tests resolve the module root (GREEN).
  Route: delegated writer (writer trigger: 80+ files).
- [x] T2 Update nested modules and tooling: benchmark, example/cluster, test/compat,
  publisher `closure_test.go`, Makefile mockery target, scripts/ci.
  Route: same delegated writer.
- [x] T3 Update ciselect and archcheck assumptions about the root package and their
  tests. Route: same delegated writer.
- [x] T4 Docs: readme, docs/ci.md, CHANGELOG `[Unreleased]` with BREAKING note and
  import map. Route: same delegated writer.
- [x] T5 Full gates for every module, external consumer check without `replace`,
  open the PR. Route: parent + per-action worker.

## Acceptance criteria

- `find . -maxdepth 1 -name '*.go'` prints nothing.
- Root module: `go build ./...`, `go vet ./...`, `go run ./internal/cmd/archcheck`,
  `go test ./...`, `go test -race ./...` pass; each nested module passes
  `scripts/ci/verify-module.sh`.
- No reference to the root package import path remains in Go code, scripts or docs,
  except the CHANGELOG migration note.

## Progress

(Writer route: delegated writer, single thread. Trigger: writer trigger, 80+ files renamed.)

### T1 (2d2b613)

- RED: `GOWORK=off go test ./internal/cmd/archcheck` failed to compile (`undefined: rootGoFiles`),
  then with the guard in place `GOWORK=off go run ./internal/cmd/archcheck` on the unmoved tree
  failed: `archcheck: 82 root Go file(s), 0 violation(s), 0 stale baseline entries` (exit 1).
- GREEN: 82 files moved to `engine/` (`package engine`), importers updated; `go build ./...`,
  `go vet ./...` and `go run ./internal/cmd/archcheck` (0 violations) pass. The architecture
  tests find the module root by walking up to `go.mod`; the logger seam file is now
  `engine/logger.go`.
- Rules `application-no-runtime`, `composition-no-runtime` and `external-adapter-no-runtime`
  now forbid `<module>/engine` (helper `enginePackagePath` in `rules/rules.go`).

### T2 (1bee3a4)

- benchmark, example/cluster, test/compat retargeted; local variables named `engine` were
  renamed to `eng` so they do not shadow the package. Publisher `closure_test.go` files reject
  `<module>/engine`. Makefile mockery `--dir engine`. `verify-consumer.sh` blank-imports
  `<root>/engine`.

### T3 (4f8f6d1)

- The selector never needed a root package: `rootImportersOf` scans every root-module package,
  so the "root requires X but no root package imports it" fallback does not fire. Verified on
  `port/publishing/publishing.go` (mode `affected`, 7 of 37 packages) and `engine/engine.go`
  (mode `affected`, engine plus dependents). Only the fixtures changed (`stray.go`).

### T4

- readme, docs/ci.md, docs/adapters.md, CHANGELOG `[Unreleased]` (BREAKING and import map),
  `openspec/config.yaml`. Historical records (`openspec/changes`, `docs/ci/baseline-159-a1.md`)
  are left as written.

### T5 (parent)

- Writer gates (all exit 0): `go build ./...`, `go vet ./...`, archcheck (0 violations),
  `go test -count=1 ./...`, `go test -race -count=1 ./...`, `scripts/ci/verify-module.sh` for
  benchmark, example/cluster, test/compat and the four publishers.
- Parent spot check: `find . -maxdepth 1 -name '*.go'` empty; build, vet, archcheck and
  `go test ./internal/cmd/... ./compose/... ./migration/... ./port/...` pass.
- `scripts/ci/verify-consumer.sh`: OK (root plus four publishers, no replace, local bare clone).
- Real consumer via `GOPROXY=direct` on pushed `239e047`: `go get .../v4/engine@239e047`
  resolves `v4.0.1-0.20260928211616-239e04731a55`, builds and runs; importing the old root
  path fails with "not at required version", as expected for the documented break.
- Not run: mockery regeneration (tool not installed), buf. golangci-lint reports 14
  pre-existing `revive var-declaration` issues in untouched `command/errors.go` and
  `tenancy/errors.go`.
- [x] T5 done when the PR is open; next step: CI on the PR, human review.
