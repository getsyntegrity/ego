# Issue #159, step A1 — CI impact-analysis baseline

Repository: `getsyntegrity/ego`, commit `57c4b11ffcd156d758daa1fa51b90ea5fc144878`
(`origin/main` on 2026-09-27). All commands below were run read-only from a
checkout of that commit; nothing in the repository was modified.

## 1. Module graph (8 `go.mod` files)

Found with `fd -H '^go\.mod$' --type f`:

| Dir | Module path | Go directive | Requires (in-repo) | Replace (in-repo) |
|---|---|---|---|---|
| `.` (root) | `github.com/pablogore/ego/v4` | 1.26.0 | — (none) | — (none) |
| `benchmark` | `.../v4/benchmark` | 1.26.0 | root `v4.4.3` | root `=> ../` |
| `example/cluster` | `.../v4/example/cluster` | 1.26.0 | root `v4.4.3` | root `=> ../../` |
| `publisher/kafka` | `.../v4/publisher/kafka` | 1.26.0 | root `v4.4.3` | root `=> ../../` |
| `publisher/nats` | `.../v4/publisher/nats` | 1.26.0 | root `v4.4.3` | root `=> ../../` |
| `publisher/pulsar` | `.../v4/publisher/pulsar` | 1.26.2 | root `v4.4.3` | root `=> ../../` |
| `publisher/websocket` | `.../v4/publisher/websocket` | 1.26.0 | root `v4.4.3` | root `=> ../../` |
| `test/compat` | `.../v4/test/compat` | 1.26.2 | root `v4.4.3` + all 4 publishers `v0.0.0` | root + all 4 publishers, each `=> ../../...` |

So the in-repo dependency graph is a star with `test/compat` as an extra
sink: every satellite requires root; `test/compat` additionally requires all
four publisher modules directly (comment in `test/compat/go.mod` cites
`openspec/changes/ego-arch-006/design.md §4`: Go only honors `replace` in the
*main* module, so `test/compat` must list a local replace for its whole
in-repo closure itself rather than relying on the publishers' own replace of
root).

`internal/cmd/ciselect`'s own module-graph builder (`discoverModules` in
`internal/cmd/ciselect/main.go`) confirms the same edges at runtime: it reads
each `go.mod` with `go mod edit -json` and only counts a requirement as a
graph edge (`ModuleInfo.Deps`) when it resolves to that module's own
directory via a local `replace`; a `replace` pointing anywhere else inside
the repo but not at a discovered module is a hard error.

## 2. Per-module package counts and dependency-closure sizes

Command pattern (run from each module directory, `GOWORK=off` — there is no
`go.work` in this repo, so this is a no-op but was passed as instructed):

```
GOWORK=off go list ./...
GOWORK=off go list -deps ./...        | wc -l   # production closure
GOWORK=off go list -deps -test ./...  | wc -l   # + test closure
```

| Module | Packages (`go list ./...`) | Prod deps closure | Test deps closure | goakt in prod closure | goakt in test closure | Root pkgs in test closure |
|---|---:|---:|---:|:---:|:---:|---:|
| root (`.`) | 46 | 578 | 660 | yes (45 matches*) | yes | 107 |
| `benchmark` | 1 | 1 (see note) | 558 | no | yes (45) | 25 |
| `example/cluster` | 1 | 1037 | 1052 | yes (46) | yes (46) | 25 |
| `publisher/kafka` | 1 | 297 | 312 | no | no | 5 |
| `publisher/nats` | 1 | 252 | 270 | no | no | 5 |
| `publisher/pulsar` | 1 | 572 | 580 | no | no | 5 |
| `publisher/websocket` | 1 | 236 | 258 | no | no | 8 |
| `test/compat` | 1 | 1 (see note) | 889 | no | yes (45) | 27 |

\* "goakt matches" = `rg -c 'tochemey/goakt' <deps-list>`; the count is how
many distinct import paths under that prefix appear (goakt itself splits
into several `github.com/tochemey/goakt/v4/...` sub-packages), not a
duplicate-count artifact.

**Important gap/finding**: `benchmark` and `test/compat` each have exactly
**one** production `.go` file in their own package tree, and it isn't real
production code:
- `benchmark/` contains only `benchmark_test.go` (no non-test `.go` file at
  all), so its package is empty for `go list -deps ./...` — hence "1" (the
  package identity itself, no dependency edges resolved).
- `test/compat/` contains `doc.go` (a doc-only file with no imports) plus
  `publisher_compat_test.go`.

This means the *production* dependency closure of these two modules is
meaningless as a signal; their real footprint only shows up in the
**`-test`** closure (558 and 889 packages respectively), which is what
actually gets built when their tests run. Anyone using "prod closure size"
as an impact proxy for these two modules would need to use the test closure
instead.

`goakt` (`github.com/tochemey/goakt/v4`) is a direct or transitive dependency
of: root, `example/cluster` (prod and test), and transitively pulled into
`benchmark` and `test/compat`'s **test** closures only (via the root module
they import in tests). It is not a dependency, direct or transitive, of any
of the four publisher modules (`kafka`, `nats`, `pulsar`, `websocket`) in
either closure — those only depend on root's public port/publishing
contracts, not on the GoAkt-bound runtime.

## 3. Root module: file counts and test import surface

`fd -d 1 '\.go$' .` at the repository root:

- **28** production `.go` files directly in the root directory (not
  `_test.go`).
- **54** `_test.go` files directly in the root directory.
- **82** total root-dir `.go` files.

Of those 54 root test files, **38** directly import
`"github.com/pablogore/ego/v4/testkit"` (via
`rg -l '"github.com/pablogore/ego/v4/testkit"' --glob '*_test.go' --max-depth 1 .`).

The full set of in-repo packages imported by root-dir test files (union
across all 54 files, via `rg -o 'github\.com/pablogore/ego/v4[a-zA-Z0-9_/.-]*'`):

```
command, egopb, encryption, eventadapter, eventstream, example/examplepb,
internal/extensions, internal/pause, internal/syncmap, mocks/ego,
mocks/encryption, mocks/eventadapter, mocks/offsetstore, mocks/persistence,
offsetstore, persistence, port/behavior, port/publishing, port/runtime,
projection, tenancy, test/data/testpb, testkit
```

This is a wide fan-in: root-dir tests alone touch 22 distinct in-repo
packages, `testkit` being the single most common one (38/54 files, 70%).

## 4. CI timing (build.yml and pull_request.yml)

**Correction to the issue's premise**: the task asked to sample
"~5 successful PR runs and ~3 main runs" from `build.yml` alone. That is not
possible, because `build.yml` only triggers `on: push: branches: [main]`
(plus `workflow_dispatch`) — every one of its last 30+ runs has
`headBranch: main`, `event: push`. The workflow that actually runs on pull
requests, with the `-changed`/`-base` "affected" selection path, is the
separate `.github/workflows/pull_request.yml` (`on: pull_request`, branches
`main` and `docs/propose-*`). I sampled both:

- 3 successful `build.yml` (main/push) runs, including the run named in the
  issue: `36364860773`, `36362316778`, `36359576125`.
- 5 successful `pull_request.yml` runs from distinct branches: `36356822832`
  (feat/147-s4-3-runtime-interfaces), `36358576067`
  (docs/154-relocation-default), `36358874339` (feat/147-s4-4-app-runtime),
  `36361787325` (fix/153-saga-status), `36362792658`
  (feat/106-spec3-composition).

Commands:
```
gh run list --repo getsyntegrity/ego --workflow build.yml --limit 30 --json ...
gh run list --repo getsyntegrity/ego --workflow pull_request.yml --limit 15 --json ...
gh run view <id> --repo getsyntegrity/ego --json jobs
```

### `build.yml` (push-to-main; always `ciselect -all`, i.e. full mode)

| Run | `build` job total | Run tests step | % of job in "Run tests" |
|---|---:|---:|---:|
| 36359576125 | 430s | 400s | 93% |
| 36362316778 | 429s | 400s | 93% |
| 36364860773 (issue-cited) | 429s | 393s | 92% |

Every other `build`-job step (`Vendoring and Tidy`, `Install dependencies`,
`Check architecture boundaries`, `Run Linter`, `Select packages`) takes 0–6s
each, ~15s combined. The `modules` matrix (7 nested-module jobs, run in
parallel after `build` completes) each takes 20–70s wall time; `example/cluster`
is consistently the slowest (`Verify module` step 30–37s; it has the largest
prod closure, 1037 packages). End-to-end pipeline wall clock for a full run
is ~8.2–8.5 minutes (build job ~429s + ~2–3s scheduling gap + ~65–70s for the
slowest parallel module job).

### `pull_request.yml` (PR; `ciselect -changed/-base`, mode depends on the diff)

| Run | Branch | Mode | `build` job total | Run tests step | `modules` job |
|---|---|---|---:|---:|---|
| 36362792658 | feat/106-spec3-composition | full | 436s | 399s | ran, 7/7 modules |
| 36361787325 | fix/153-saga-status | full | 428s | 398s | ran, 7/7 modules |
| 36358576067 | docs/154-relocation-default | full | 426s | 398s | ran, 7/7 modules |
| 36356822832 | feat/147-s4-3-runtime-interfaces | full | 428s | 398s | ran, 7/7 modules |
| **36358874339** | **feat/147-s4-4-app-runtime** | **affected** | **39s** | **8s** | **skipped (0 selected)** |

Only 1 of the 5 sampled PR runs actually hit `affected` mode; the other 4
fell back to `full` even though they were ordinary feature-branch pushes.
Fetching the `Select packages` step log for `36361787325`
(`gh api repos/getsyntegrity/ego/actions/jobs/108740247387/logs`) shows why:

```
**Mode:** `full`
**Reason(s):**
- global: protos/ego/ego.proto changed
- egopb/ego.pb.go changed (under full-fallback path egopb/)
- engine.go changed (shared root package)
- protos/ego/ego.proto changed (under full-fallback path protos/)
- saga_actor.go changed (shared root package)
- saga_status_test.go changed (shared root package)
**Selected:** 33 of 33 included packages.
```

That PR touched a root-dir `.go` file (`engine.go`, `saga_actor.go`), which
the selector always treats as "shared root package" → full suite, *and* it
regenerated protobuf code, which is a separate `full-fallback` path. The one
run that got `affected` mode (`36358874339`, job
`108731929438`) only touched `compose/goakt/app.go`,
`compose/goakt/runtime_e2e_test.go`, `compose/goakt/runtime_test.go`,
`internal/runtimeconsumer/closure_test.go`,
`internal/runtimeconsumer/consumer.go` plus two doc/no-test files — no
root-dir file, no proto, no `go.mod`/`go.sum` — so it got
`**Mode:** affected`, `**Selected:** 2 of 33 included packages`
(`compose/goakt`, `internal/runtimeconsumer`), and the `modules` job was
skipped entirely (`needs.build.outputs.modules == '[]'`). That single data
point is >10x faster end-to-end (~40s vs. ~500s) than every full-mode run
sampled, which is the best empirical evidence in this sample of what the
selector is capable of saving when it actually gets to run in `affected`
mode.

**Gap**: 5 PR runs and 3 main runs is a small sample; I did not attempt a
statistically representative sample of `affected`-vs-`full` mode frequency
across the full run history — that would need scanning `Select packages`
step logs across many more runs, which I did not do here.

## 5. `internal/cmd/ciselect` selection rules and 4 scenarios

Read: `internal/cmd/ciselect/main.go`, `internal/cmd/ciselect/selector/{classify,select,modulegraph,graph}.go`,
their `_test.go` files, and `.github/workflows/{build,pull_request}.yml`.

**How CI invokes it**: `pull_request.yml` runs
`go run ./internal/cmd/ciselect -changed "$RUNNER_TEMP/changed.txt" -base "$(git merge-base BASE HEAD)" -out-dir "$RUNNER_TEMP/ci"`,
falling back to `-all` on any selector failure. `build.yml` always runs
`-all`. Because the tool takes a plain newline-separated changed-file list
and writes to an arbitrary `-out-dir`, it can be run locally exactly as CI
does, and it was run that way from the repository root.

Rule summary from the source:
- **`ClassFullFallback`** (`classify.go`): exact files `go.mod`, `go.sum`,
  `Makefile`, `Dockerfile.ci`, `.golangci.yml`, `buf.yaml`, `buf.gen.yaml`;
  prefix dirs `.github/`, `protos/`, `internal/cmd/ciselect/`, `scripts/ci/`,
  `egopb/`; **and any `.go` file sitting directly in the module root
  directory** (`isRootPackageGoFile`) — this last rule is a blunt instrument:
  it doesn't check what the file actually imports or is imported by, it
  forces full mode for *any* of the 82 root-dir `.go` files.
- **Global paths** (`modulegraph.go`, separate table): `go.work`,
  `go.work.sum`, `.golangci.yml`, `Makefile`, `Dockerfile.ci`, `buf.yaml`,
  `buf.gen.yaml`, plus prefix dirs `.github/`, `scripts/ci/`,
  `internal/cmd/ciselect/`, `protos/` — these force **every module**
  (including all satellites) to be selected, not just the root lane.
- **`ClassSatellite`**: a changed file under a nested module's directory —
  contributes to that module's own selection only (via the module-graph
  closure below), not to the root package lane.
- **`ClassNoTest`**: `*.md`, `LICENSE`, `renovate.json`, `openspec/`,
  `.spec-governance/`, `assets/` — no test run needed.
- Otherwise **`ClassPackage`**: resolved to the owning package by directory,
  and `affectedByChange` (`graph.go`) computes the reverse-import closure
  (packages whose non-test `Imports` transitively reach a changed package,
  plus any package whose `TestImports`/`XTestImports` directly names
  something in that closure) to produce the `affected`-mode subset. If that
  subset equals the full included set, mode still degrades to `full`.
- **Module-graph propagation** (`modulegraph.go`): a changed nested module
  selects itself, plus every module that transitively requires it *and*
  actually imports one of its affected packages (the "import filter"); the
  root's own affected-package set can, in turn, "seed" and select any nested
  module whose imported root packages match.

### Scenario walkthroughs (read + actually run locally)

Synthetic one-line changed-file lists were written to a scratch directory
and run with
`GOWORK=off go run ./internal/cmd/ciselect -changed <file> -out-dir <dir>`
from the repository root for each (34 = the root module's "included",
i.e. coverage-denominator, package count; it excludes `egopb`, `example`,
`mocks`, `test` segments from the 46 packages `go list ./...` reports).

**a) Change in `publisher/kafka` leaf** (`publisher/kafka/kafka.go`):
```
Mode: none   Selected: 0 of 34 included (root) packages
Classified: satellite
Nested modules selected: publisher/kafka (itself), test/compat (test/compat ← publisher/kafka)
```
Root's own test run is entirely skipped; only `kafka`'s own module-verify
job and `test/compat`'s (because `test/compat` imports the kafka publisher
package) run. `nats`, `pulsar`, `websocket`, `benchmark`, `example/cluster`
are untouched.

**b) Change in a shared contract package in root** (`port/behavior/behavior.go`
— a subdirectory package, distinct from a literal root-dir file):
```
Mode: affected   Selected: 6 of 34 (., compose/goakt, internal/runtimeconsumer, migration, port/behavior, port/runtime)
Nested modules selected: benchmark, example/cluster, test/compat (all "← .", because they import the root package itself, which is in the affected set)
publisher/kafka|nats|pulsar|websocket: NOT selected ("requires . but imports none of its affected packages")
```
So `port/behavior` is not treated as a root-dir full-fallback file (it isn't
one) — it goes through the normal reverse-import graph, producing a
non-trivial but real subset. Note it is *not* the same as scenario (d): it
pulls in `port/runtime`, `migration` and both compose/goakt-adjacent
packages, plus 3 of the 7 satellite modules (because they import the root
package, which the affected set includes).

**c) Change in `testkit`** (`testkit/scenario.go`):
```
Mode: affected   Selected: 5 of 34 (., compose/goakt, internal/extensions, migration, testkit)
Nested modules selected: benchmark, example/cluster, test/compat (same "← ." reasoning as above)
publisher/*: NOT selected
```
`testkit` is used only in test files across most of its importers, so the
selection is driven by the `TestImports`/`XTestImports` half of
`affectedByChange`, not by production `Imports` — confirming the design
comment in `graph.go` about `testkit` deliberately not being caught by the
"test" exclusion segment (a whole-segment match, not substring).

**d) Change in a runtime/GoAkt-ish root package** (`compose/goakt/app.go`):
```
Mode: affected   Selected: 1 of 34 (compose/goakt only)
Nested modules selected: none
```
This matches real production evidence: PR run `36358874339` (which touched
`compose/goakt/app.go` + 2 more compose/goakt test files +
`internal/runtimeconsumer/consumer.go` + 1 more) got
`Mode: affected`, `Selected: 2 of 33`, `modules` job skipped, and a
39-second `build` job total (8s test step) — the fastest run in the entire
sample.

For contrast I also ran the two edge cases the source predicts explicitly:
- A literal root-dir file (`engine.go`) → `Mode: full`, `Selected: 34 of 34`,
  reason `"engine.go changed (shared root package)"`.
- A global path (`.github/workflows/build.yml`) → every one of the 7 nested
  modules plus the root lane selected, reason
  `"global: .github/workflows/build.yml changed"`.

`ciselect`'s own unit tests pass at this commit:
`GOWORK=off go test ./internal/cmd/ciselect/...` → both packages
`ok` (0.303s, 0.004s).

## Gaps / things not measured

- Did not attempt a large-sample statistical breakdown of how often PR runs
  actually land in `affected` vs. `full` mode across the repo's whole
  history — only 5 recent PR runs were sampled (4 full, 1 affected).
- Did not measure cold-cache timing (all sampled runs presumably benefited
  from `actions/setup-go@v7`'s dependency cache — cache hits/misses were not
  inspected).
