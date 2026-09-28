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

- [ ] T1 Add an archcheck guard "no Go files at the module root" (RED), move the root
  package to `engine/` with `git mv`, fix package clauses and in-module importers,
  make the architecture tests resolve the module root (GREEN).
  Route: delegated writer (writer trigger: 80+ files).
- [ ] T2 Update nested modules and tooling: benchmark, example/cluster, test/compat,
  publisher `closure_test.go`, Makefile mockery target, scripts/ci.
  Route: same delegated writer.
- [ ] T3 Update ciselect and archcheck assumptions about the root package and their
  tests. Route: same delegated writer.
- [ ] T4 Docs: readme, docs/ci.md, CHANGELOG `[Unreleased]` with BREAKING note and
  import map. Route: same delegated writer.
- [ ] T5 Full gates for every module, external consumer check without `replace`,
  open the PR. Route: parent + per-action worker.

## Acceptance criteria

- `find . -maxdepth 1 -name '*.go'` prints nothing.
- Root module: `go build ./...`, `go vet ./...`, `go run ./internal/cmd/archcheck`,
  `go test ./...`, `go test -race ./...` pass; each nested module passes
  `scripts/ci/verify-module.sh`.
- No reference to the root package import path remains in Go code, scripts or docs,
  except the CHANGELOG migration note.

## Progress

(empty)
