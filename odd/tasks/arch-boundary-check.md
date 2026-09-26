# Feature: enforce architectural dependency boundaries in CI (#107, ADR slice S2)

Branch: `ci/107-arch-boundary-check` · Base: `origin/main` `a5265aa` · Epic: #10 · Issue: #107

## Problem

The layer rules of the ego-arch-001 ADR (`openspec/changes/ego-arch-001/design.md` §3) are review
rules today. Three packages (`tenancy`, `command`, `port/publishing`) have their own hand-written
architecture test, but nothing stops a new contract package, the `migration` application or a
publisher module from importing the GoAkt runtime. A regression is caught only if a reviewer notices.

## What changes

A new tool, `internal/cmd/archcheck`, reads the import graph and checks every import edge against a
table of layers. Each layer names its packages and what they may import. A violation prints the
importing package, the forbidden import and the rule it breaks, then the tool exits non-zero.

Known violations that cannot be fixed yet live in an explicit **baseline**. Every baseline entry has
an owner, a justification and a removal criterion. An entry that no longer matches a real violation
also fails the check, so the baseline can only shrink.

The tool runs as its own step in `pull_request.yml` and `build.yml`, before the test suite, so a
boundary break fails in seconds instead of after the full run.

### How the graph is read

- **Root module:** `go list -e -json ./...` gives each package's production `Imports`, with build
  constraints resolved by the toolchain. A direct-edge check is enough: contract layers use a closed
  allowlist, and every allowed target (stdlib, `egopb`, the protobuf runtime, `uuid`, `atomic`,
  `internal/queue`, `internal/syncmap`) is itself runtime-free, so no transitive path to GoAkt can
  open without adding a new, visible edge.
- **Nested modules** (`publisher/*`, `benchmark`, `example/cluster`): parsed with `go/parser` in
  imports-only mode. This needs no module download and no network, which keeps the step fast. Only
  direct imports matter for the nested-module rules.

Rejected alternative: `go list -deps` over every module. It needs every nested module's dependencies
downloaded (Kafka, Pulsar and NATS clients) and turns a seconds-long check into minutes.

Rejected alternative: extend `ciselect`. Selection decides which tests run; the rule check decides
whether the graph is legal. Mixing them would make one tool's fallback hide the other's failure.

## Rules (first version)

| Rule | Layer | Constraint | Source |
|---|---|---|---|
| `contract-allowlist` | Contract packages: `tenancy`, `command`, `persistence`, `offsetstore`, `projection`, `eventstream`, `encryption`, `eventadapter`, `port/...` | May import only stdlib, other contract packages, `egopb`, `google.golang.org/protobuf/...`, `internal/queue`, `internal/syncmap`, `github.com/google/uuid`, `go.uber.org/atomic` | design.md §3 |
| `application-no-runtime` | Application: `migration` | Must not import the runtime adapter (`ego`, `internal/extensions`) or GoAkt | #107 scope |
| `external-adapter-no-runtime` | Nested adapter modules: `publisher/*` | Must not import the root package `ego` or GoAkt | design.md §3 |
| `no-cross-module-internal` | Every nested module | Must not import root-module `internal/...` | design.md §3 |

## Baseline at the start

| Importer | Import | Rule | Owner | Removal criterion |
|---|---|---|---|---|
| `publisher/kafka`, `nats`, `pulsar`, `websocket` | `github.com/pablogore/ego/v4` | `external-adapter-no-runtime` | @pablogore | S1b: publishers import `port/publishing` once #111 builds nested modules |
| `migration` | `github.com/pablogore/ego/v4` | `application-no-runtime` | @pablogore | S3/S4 (#103, #11): runtime-neutral contracts for what `migration` uses |

## Constraints

- TDD strict (global configuration); runner `go test`; no `-race` locally.
- No new third-party dependency.
- Existing architecture tests stay; this tool is a superset for layers, not a replacement for the
  stricter `tenancy` stdlib-only test or the logger source scan.
- Route: **delegated direct** — one writer for T1–T2 (writer trigger: 2+ non-trivial files);
  T3–T4 inline.

## Tasks

- [x] **T1** Rule engine (`internal/cmd/archcheck/rules`): layer table, edge evaluation, baseline
  with stale-entry detection, deterministic sorted output.
  Check: RED then GREEN unit tests with in-memory graphs — forbidden import fails with importer,
  import and rule in the message; allowed graph passes; baselined violation passes; stale baseline
  entry fails.
  Route: delegated direct (writer trigger). Commit `05a49dc`.
- [x] **T2** Loaders and CLI (`internal/cmd/archcheck/main.go`): root module via `go list`, nested
  modules via `go/parser`; repository baseline.
  Check: `go run ./internal/cmd/archcheck` exits 0 on the branch; a throwaway GoAkt import in a
  contract package makes it exit 1 with an actionable message.
  Route: delegated direct (writer trigger). Commit `1866d0a`.
- [x] **T3** Wire the step into `pull_request.yml` and `build.yml`.
  Check: workflow YAML readback; CI run on the PR.
  Route: inline. Step "Check architecture boundaries" added after "Install dependencies", before the
  linter, in both workflows, with `GOFLAGS=-mod=vendor` like the selector step. YAML parses.
- [x] **T4** Document it in `docs/ci.md` (rules, baseline policy, how to add a layer) and
  `CHANGELOG.md`. Check: structural readback.

## Acceptance criteria (#107)

Fails on a forbidden fixture; passes on the allowed graph; runs fast and separately from the suite;
the message names importer, import and rule; every baseline entry has owner, justification and
removal criterion; automated, not review-only; documented update procedure.

## Progress and evidence

### T1 (rule engine) — done, commit `05a49dc`

- Files: `internal/cmd/archcheck/rules/{graph,layers,rules,baseline,evaluate,report}.go`,
  `evaluate_test.go`.
- TDD: wrote `evaluate_test.go` against the not-yet-existing package, moved the implementation
  files aside, `go test` failed with `undefined: Graph/Package/RootModule/...` (RED, build
  failure), restored the implementation, `go test` passed all 16 test functions (GREEN).
- `go vet ./internal/cmd/archcheck/...`: no issues. `gofmt -l`: clean.
- The `contract-allowlist` layer matches the 8 named contracts and `port/...` (and their
  subpackages), explicitly excluding `persistence/conformance` (test support, not a contract),
  per the task spec.

### T2 (loaders + CLI) — done, commit `1866d0a`

- Files: `internal/cmd/archcheck/{main,loader,baseline}.go`, `loader_test.go`, plus a 1-line
  staticcheck fixup in `rules/rules.go` and a wording fixup in `rules/report.go`.
- TDD: wrote `loader_test.go` first (t.TempDir fixtures for `parseGoModModulePath`,
  `loadNestedModule`, `discoverNestedModuleDirs`); `go test` failed with `undefined:
  parseGoModModulePath/loadNestedModule/discoverNestedModuleDirs` (RED), then implemented
  `loader.go` until GREEN.
- Verification (all observed on this branch, from the repository root):
  1. `go build ./... && go vet ./internal/cmd/archcheck/...` → both clean, no output.
  2. `go test -count=1 ./internal/cmd/archcheck/...` (no `-race`) → `ok` both packages
     (`archcheck`, `archcheck/rules`).
  3. `go run ./internal/cmd/archcheck` → exit 0,
     `archcheck: 14 packages checked, 94 edges checked, 5 baselined, 0 violation(s), 0 stale entries`,
     wall time ≈0.39s (`time` output: `0,39s total`).
  4. Fixture proof: added `tenancy/zz_violation.go` importing
     `github.com/tochemey/goakt/v4/actor`; `go run ./internal/cmd/archcheck` → exit 1, reported
     `github.com/pablogore/ego/v4/tenancy imports github.com/tochemey/goakt/v4/actor: rule
     contract-allowlist (design.md §3): ...`; file deleted, `git status` confirmed clean.
  5. Stale proof: temporarily appended a bogus baseline entry
     (`tenancy -> github.com/tochemey/goakt/v4/nonexistent`) to `repoBaseline`; `go run
     ./internal/cmd/archcheck` → exit 1, reported "baseline entry ... no longer matches a
     violation; delete it"; reverted, re-ran → exit 0 again.
  6. `GOFLAGS=-mod=vendor go run ./internal/cmd/archcheck` (after `go mod vendor`) → exit 0, same
     summary line.
  7. `golangci-lint run --config .golangci.yml ./internal/cmd/archcheck/...` → `0 issues.` (one
     staticcheck S1008 finding was fixed in `rules.go` before this final run).
  8. `gofmt -l internal/cmd/archcheck` → empty output (clean).
- Real baseline used matches the spec exactly: `publisher/kafka`, `publisher/nats`,
  `publisher/pulsar`, `publisher/websocket` (each a single-package nested module, one production
  import of the root package) under `external-adapter-no-runtime`, and `migration` under
  `application-no-runtime`. No deviation from the task's proposed baseline was found; the real
  import graph matches it exactly (verified via `rg` before writing loaders, and confirmed by the
  tool's own 0-violation/0-stale run above).
- Open note: `example/cluster` (a nested module, not under `publisher/`) imports the root package
  `github.com/pablogore/ego/v4` directly. This is **not** a violation under the current rule
  table: `external-adapter-no-runtime` only covers `publisher/*`, and
  `no-cross-module-internal` only forbids `internal/...` imports, not the root package itself.
  This matches design.md's classification of `example/cluster` as an "unreleased consumer,
  unchanged" rather than an external adapter, so no rule was written for it — flagging this in
  case the next slice wants to tighten that.

T3 (workflow wiring) and T4 (docs/CHANGELOG) are not part of this writer's scope and remain
unstarted.

### T3 — done, commit `260c778`
Step "Check architecture boundaries" in `pull_request.yml` and `build.yml`, after "Install
dependencies" and before the linter, `GOFLAGS=-mod=vendor`. Both files parse as YAML. The CI run on
the PR is the remaining proof.

### T4 — done
`docs/ci.md` gains "Architecture boundary check" (rules table, baseline policy, how to add a layer);
`CHANGELOG.md` Improvements entry. Check: structural readback; names cited in the doc
(`DefaultRules`, `contractRoots`, `baseline.go`) verified against the code.

### Delivery note
The feature is ~1,200 production lines plus ~540 test lines, above the ~400-line heuristic. It ships
as one PR with one commit per task: the engine, the loaders and the wiring are only useful together,
and splitting them would land an inert half. Review commit by commit.

**Review:** RDD is off (global); no native review ran.
