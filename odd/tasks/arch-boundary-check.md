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

- [ ] **T1** Rule engine (`internal/cmd/archcheck/rules`): layer table, edge evaluation, baseline
  with stale-entry detection, deterministic sorted output.
  Check: RED then GREEN unit tests with in-memory graphs — forbidden import fails with importer,
  import and rule in the message; allowed graph passes; baselined violation passes; stale baseline
  entry fails.
- [ ] **T2** Loaders and CLI (`internal/cmd/archcheck/main.go`): root module via `go list`, nested
  modules via `go/parser`; repository baseline.
  Check: `go run ./internal/cmd/archcheck` exits 0 on the branch; a throwaway GoAkt import in a
  contract package makes it exit 1 with an actionable message.
- [ ] **T3** Wire the step into `pull_request.yml` and `build.yml`.
  Check: workflow YAML readback; CI run on the PR.
- [ ] **T4** Document it in `docs/ci.md` (rules, baseline policy, how to add a layer) and
  `CHANGELOG.md`. Check: structural readback.

## Acceptance criteria (#107)

Fails on a forbidden fixture; passes on the allowed graph; runs fast and separately from the suite;
the message names importer, import and rule; every baseline entry has owner, justification and
removal criterion; automated, not review-only; documented update procedure.

## Progress and evidence

_(updated per task)_
