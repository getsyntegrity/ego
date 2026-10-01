# Examples as an independent module with their own CI job (#210, spec D)

Issue: https://github.com/getsyntegrity/ego/issues/210, a follow-up requested by the user on 2026-10-01: "examples, the same: an independent flow like inttest, on develop only".
Branch: `ci/examples-job`, from `ci/inttest-job` (spec C, PR #281). The PR targets `develop` and carries the A, B and C commits until those are merged.

## Problem

`inttest` no longer runs on feature or hotfix pull requests. It runs on each push to `develop`, on the `develop` to `main` release pull request and on manual runs. The user wants the examples to work the same way, but today they are part of the everyday build:

- `example/durablestate`, `example/eventssourced` and `example/saga` are packages of the root module. Every pull request compiles and tests them in the root test shards.
- `example/cluster` is a nested module, and the `modules` matrix builds, vets and tests it on every pull request.
- `example/examplepb` is not only an example. Root tests import it as a fixture: `engine/engine_test.go`, `engine/helper_test.go`, `engine/saga_status_test.go`, `internal/engine/saga/saga_test.go`, `internal/engine/enginetest/callback_saga.go` and `benchmark/benchmark_test.go`. The examples cannot leave the root module until those tests stop depending on it.

## What changes

1. **The fixture moves into the root module.** `example/examplepb` becomes an internal test fixture package, for example `internal/samplepb`, generated from `protos/sample/sample.proto`. The generation config (`buf.gen.yaml` or the `proto` target of the root Makefile) points at the new location. Every importer is updated.
2. **The `example/` module.** A new `example/go.mod` (`github.com/getsyntegrity/ego/example`, with `replace github.com/getsyntegrity/ego => ../`) holds `durablestate`, `eventssourced` and `saga`. `example/cluster` keeps its own module; Go excludes a nested module from its parent. The root `go list ./...` then has no example package. The root Makefile `run-*` targets run from the new module.
3. **The `examples` job in `ci.yml`.**
   - It builds, vets and tests `example` and `example/cluster`.
   - Its triggers are the same as `inttest`: every push to `develop`, the `develop` to `main` release pull request, and `workflow_dispatch`.
   - It is listed in `ci-ok`'s `needs`.
   - `example/cluster` leaves the `modules` matrix, so feature and hotfix pull requests never build the examples.
   - `tidy` already discovers every `go.mod`. Dependabot gets `/example`.
4. **Docs.** `docs/ci.md` gets the job row, the `modules` row without the examples, and a short note on why the examples are outside the everyday build. The README files that tell how to run the examples are updated as well.

## Why this shape

- **A module, not a CI path rule.** As long as the examples sit in the root module, the root test shards compile them on every pull request no matter what the workflow says. A module boundary is the only way to take them out, the same reason `inttest` is a module and not a build tag.
- **The fixture moves first.** Root tests import the generated messages, so if the examples moved first, the root would have to import the new example module, which is backwards. The fixture belongs to the root tests.
- **Same triggers as `inttest`.** The release pull request is the gate for `main`; without it, a broken example could ship in a release. The rejected alternative was a push to `develop` only, which has no gate.

## Scope and constraints

- No behavior change in the examples or the engine. Generated code is regenerated, not hand-edited.
- go-specs v0.3.3 only in any touched test; no testify. No race detector, no workbench.
- No direct push to `develop`/`main`.

## Execution

- TDD: strict (source: user CLAUDE.md). This is a refactor and CI change. RED evidence comes from the build: compile errors after a move, before the imports are fixed. The runners are the existing suites: root `go test ./engine/... ./internal/... ./benchmark/...`, and `go build`, `go vet` and `go test` in `example` and `example/cluster`.
- RDD: off (global).

## Tasks

- [x] **D1 Move the fixture.** `example/examplepb` becomes `internal/samplepb` (or the closest existing internal fixture location), regenerated, with every importer updated. Check: root `go build ./... && go vet ./...`, plus the affected root tests and `benchmark`. Route: delegated writer.
- [x] **D2 `example` module.** `example/go.mod` holds `durablestate`, `eventssourced` and `saga`, and the root Makefile `run-*` targets are updated. Check: `go build ./... && go vet ./... && go test ./...` in `example`; root `go list ./... | rg example` is empty; `go mod tidy -diff` is clean everywhere. Route: delegated writer.
- [x] **D3 `examples` job.** Add the job with the `inttest` triggers, list it in `ci-ok`, remove `example/cluster` from `modules`, and add `/example` to Dependabot. Check: `actionlint`. Route: delegated writer.
- [x] **D4 Docs.** Update `docs/ci.md` and the example READMEs. Check: structural readback. Route: delegated writer.

## Progress and evidence

(Filled in as tasks close. A commit cannot name its own SHA, so the SHAs of D1 to D3 are listed with D4.)

### D1 (route: delegated writer)

- `example/examplepb` moved with `git mv` to `internal/samplepb`. It is a root-module internal package, importable by the nested modules because its path is under the root module path. No existing internal protobuf fixture fit better: `internal/engine/enginetest` holds hand-written behaviors, and `test/data/testpb` is a different schema.
- `go_package` in `protos/sample/sample.proto` is now `github.com/getsyntegrity/ego/internal/samplepb;samplepb`. The Makefile `proto` and `docker-protogen` targets copy `gen/sample` to `internal/samplepb`. `contributing.md` names the new path.
- The file was regenerated with `buf` v1.69.0 and `protoc-gen-go` v1.36.12 (the versions of `Dockerfile.ci` and of the old header), installed with `go install`; not hand-edited. The only diff against the old file is the embedded `go_package` string and its length byte. The proto package (`samplepb`) and every full message name are unchanged.
- Importers updated: `engine/engine_test.go`, `engine/helper_test.go`, `engine/saga_status_test.go`, `internal/engine/saga/saga_test.go`, `internal/engine/enginetest/callback_saga.go`, `benchmark/benchmark_test.go` and the three example mains.
- Checks: root `go build ./... && go vet ./...` ok; `go test -count=1 ./engine/... ./internal/engine/saga/... ./internal/engine/enginetest/...` ok; `benchmark`: `go vet ./...` and compile-only `go test -run XXX` ok; `go mod tidy -diff` clean in root and `benchmark`.

### D2 (route: delegated writer)

- New module `github.com/getsyntegrity/ego/example` (`example/go.mod`, `example/go.sum`): `go 1.26.0`, `replace github.com/getsyntegrity/ego => ../`, `require` of ego `v0.0.0`, and the `armon/go-metrics` exclude block copied from `example/cluster/go.mod`. It holds `durablestate`, `eventssourced` and `saga`. `example/cluster` stays its own module, nested inside; `go list ./...` in `example` lists exactly the three packages. The examples import `internal/samplepb` and other root packages; Go allows that because the importer path is under the root module path.
- The Makefile `run-eventsourced`, `run-durablestate` and `run-saga` targets run `cd example && go run ./<name>`.
- Checks: `example`: `go build ./... && go vet ./... && go test -count=1 ./...` ok (no test files); `example/cluster`: `go vet ./... && go test -count=1 ./...` ok; root `go list ./... | rg example` is empty; `go mod tidy -diff` clean in root, `example`, `example/cluster`, `benchmark` and `inttest`; `gofmt -l` empty.

### D3 (route: delegated writer)

- Job `examples` in `.github/workflows/ci.yml`: `needs: plan`, the same `if:` as `inttest` (push, workflow_dispatch, or a pull request from `develop` to `main`), `ubuntu-latest`, `timeout-minutes: 15`, a matrix over `example` and `example/cluster` (`fail-fast: false`), and one step that runs `go build ./...`, `go vet ./...` and `go test $TESTFLAGS ./...` in the matrix directory. It is listed in `ci-ok` `needs`.
- `example/cluster` left the `modules` matrix; the header comment and the nested-modules comment were updated.
- `.github/dependabot.yml` gets `/example`. `tidy` discovers every `go.mod` by itself. `.github/scripts/test-matrix.sh` builds its list from root `go list ./...`, which no longer has an example package, and `security.yml` builds every `go.mod` already, so neither changed.
- Check: `actionlint .github/workflows/ci.yml` exits 0.

### D4 (route: delegated writer)

- `docs/ci.md`: the `modules` row no longer names `example/cluster`; a new `examples (dir)` row; a new "Examples" section (why the programs are outside the everyday build, the triggers, how to check them locally, how to run one); the local-equivalents paragraph names the exception. `readme.md`, `contributing.md` and `example/cluster/README.md` explain that the examples are their own module and that the `make run-*` targets start from `example/`.
- Check: structural readback of the four documents.

### Commits

- D1 `a21025b`, D2 `70db2b6`, D3 `95167ac`, D4 the commit that holds this line.

## Next step

Open the pull request for `ci/examples-job` (it carries the A, B and C commits until those are merged), and check in its run that the `examples` job is skipped on the feature pull request by design. It runs for the first time on the push to `develop` after the merge.

## Review follow-up

Review of PR #282 found two problems, fixed in three commits after merging the latest spec C commit.

- Merge `eab5961` brings in `ebd9e44` from `ci/inttest-job`. The only conflict was `docs/ci.md`: both the new inttest trade-off paragraph and the Examples section were kept.
- `d34c6d4` fix(example): the examples imported `internal/samplepb`, so a user who copied one got "use of internal package not allowed". The example module now has its own generated `example/examplepb`, from the same `protos/sample/sample.proto`, through a second template `buf.gen.example.yaml` (it overrides `go_package` for `sample/sample.proto` only, and writes to `gen-example/`, which is git-ignored). The Makefile `proto` and `docker-protogen` targets run both templates. `internal/samplepb` stays for the root tests, enginetest and benchmark. `example/cluster` requires `github.com/getsyntegrity/ego/example v0.0.0` with `replace ... => ../`, instead of keeping a third copy. Rejected alternative: a generated copy inside `example/cluster`, which would duplicate 850 generated lines. Generation uses `buf` v1.69.0 and `protoc-gen-go` v1.36.12; regenerating both packages leaves `git status` clean.
- `8342c9d` ci: the `examples` job is gone. `example` and `example/cluster` joined the `modules` matrix, so they are built, vetted and tested on every pull request with Go changes. `examples` left `ci-ok`'s needs. Reason: compiling takes seconds and a feature PR that breaks an example must fail before the merge. `actionlint` is clean.
- The docs commit rewrites the Examples section of `docs/ci.md`, updates the `modules` row, and fixes `contributing.md` and `example/cluster/README.md`.

Proof that no binary links both generated packages (same proto file registered twice would panic at init):

- `go list -deps ./...` in `example` and in `example/cluster` lists `example/examplepb` and never `internal/samplepb`. Per package, each main package depends on exactly one of the two.
- No package outside `example/` imports `example/examplepb`; `go list -deps ./...` in the root, `benchmark` and `inttest` never lists it.
- `rg -n 'ego/internal/' example --type go` returns nothing.

Checks: root `go build`/`go vet` and `go test -count=1 ./engine/... ./internal/engine/saga/... ./internal/engine/enginetest/...` ok; `example` and `example/cluster` `go build`, `go vet`, `go test -count=1` ok; `benchmark` `go vet` ok; `go mod tidy -diff` clean in root, `example`, `example/cluster`, `benchmark`, `inttest`; `unitgate -strict` ok; `gofmt -l` empty.

### Benchmark module after the merge (user, 2026-10-01)

The user asked for `inttest` and `benchmark` to run only on `develop` or `main`, whichever suits the pipeline. Applied: `benchmark` follows the `inttest` rule. `modules` builds and vets it on every pull request with Go changes, as a vet-only include, so a change that breaks it fails before the merge. A new `benchmark` job in `ci.yml` runs its tests (without `-bench`; they start a real goakt actor system) on push to `develop`, the `develop` to `main` release pull request and `workflow_dispatch`. The job is listed in `ci-ok`. Check: `actionlint` is clean.
