# Rename the project `ego` to `urd`

## Problem and goal

The project ships as `github.com/getsyntegrity/ego`. It is being rebranded as
**Urd — event sourcing for Go**, with the module path
`github.com/getsyntegrity/urd`. This branch prepares the code, docs and CI;
the GitHub repository rename, tags and releases are done manually by the
maintainer (see T5).

The rename must not break data already persisted by `ego` users, nor rolling
upgrades between old and new nodes. So names that are stored or sent on the
wire keep their `ego` spelling; only Go paths, labels, docs and free-text
messages change.

## Decisions (approved 2026-10-02)

| ID | Item | Decision |
|----|------|----------|
| E1 | proto `package egopb` | keep (type names are persisted in `Any` payloads) |
| E2 | Go package/dir `egopb` | keep (buf lint `PACKAGE_SAME_GO_PACKAGE` ties it to E1) |
| E3 | metadata keys `ego.cmd.*`, `ego.tenant.*`, `ego.adoption.receipt`, reserved prefix `ego.` | keep (persisted; strict unmarshal) |
| E4 | GoAkt extension ID strings | keep the strings; Go constant names may change |
| E5 | OpenTelemetry metric/span/attribute names | keep for now (assumed; no answer given) |
| E6 | NATS stream names `ego-events`, `ego-durable-states` | keep (server-side state) |
| E7 | publisher IDs / Kafka client ID (`ego-kafka`, ...) | rename to `urd-*` |
| E8 | Postgres table `ego_schema_migrations`, advisory lock key | keep |
| E9 | env vars `EGO_*` | rename to `URD_*`, still read `EGO_*` as a fallback |
| E10 | parsed error grammar `ego: concurrency conflict: grammar=v1` | keep; free-text `ego:` / `eGo:` messages become `urd:` |
| E11 | `openspec/`, `odd/tasks/` history, `.spec-governance/`, past CHANGELOG entries | keep as history |
| E12 | first version | `v0.1.0` for every module; old tags reset by the maintainer |

There is no root Go package (the repo root has no `.go` files, enforced by
archcheck), so no `package urd` is introduced.

## Tasks

TDD: strict mode on (user global config). Runner: `go test ./...` per module.
Race detector only where the user asked: `go test -race -count=1 ./...` on
the root module in T4.

- [x] T1 — Module paths: rewrite every `go.mod`, import, `replace`, hardcoded
  module-path string (closure/architecture tests, `unitgate/scan.go`,
  `benchmark/Makefile`), proto `go_package` options and
  `buf.gen.example.yaml`; regenerate with `make docker-protogen`;
  `go mod tidy` in every module. Check: `go build ./... && go vet ./...` in
  every module. Route: delegated (writer trigger: hundreds of files).
- [x] T2 — Approved group-E changes: E7 publisher IDs, E9 env vars with
  fallback, E10 free-text error messages, local `ego*` identifiers and
  `Package ego` doc comments. Check: unit tests per module. Route: delegated.
- [x] T3 — Docs, CI and branding: README (title, tagline, install, "Formerly
  ego" note with sed migration), badges, docs/, `.github/` templates and
  SECURITY, Makefiles, Dockerfiles, `example/cluster` k8s/Makefile, workflows,
  scripts; add `MIGRATION.md`. Check: structural readback, the grep in T4.
  Route: delegated.
- [x] T4 — Verification: build/vet/test per module, race on root, leftover
  `ego` grep with every hit explained, `go list -m all` has no old path,
  `git diff develop... --stat` has no unrelated changes. Route: delegated.
- [x] T5 — Hand-off checklist for the maintainer (deprecation commit on the
  old path, repo rename, remote update, tag reset and `v0.1.0` tags,
  pkg.go.dev indexing, external references). Route: inline.

## Progress

Base: `origin/develop` at `afaafb3`. Branch: `refactor/rename-to-urd`.

- T1 done (dfe3462 module path, ce18478 proto go_package + regen). `make docker-protogen` ran; pb.go changed only the go_package string (same length, no version-header changes). Per module `go build ./... && go vet ./...` clean (10 modules); root `go test ./...` 39 packages ok, none failing. Also fixed `.github/scripts/count-tests.sh`. Leftover `getsyntegrity/ego` only in `.github/ISSUE_TEMPLATE/*.yml` (URLs, T3).
- T2 done (5c4a26b publisher/client IDs, 3659bcb URD_* env vars with EGO_* fallback, 365a38b `urd:` error messages, f8f0e79 identifiers and doc comments, plus a final one-line comment fix in the docs commit). RED/GREEN: new `publisher/*/id_test.go` failed with `"ego-kafka"`, `"ego-nats"`, `"ego-pulsar"`, `"ego-websocket"`, `"ego-kafka-publisher"` and passed after the change; `engine/telemetry_dump_env_test.go` failed to compile (helper missing) then passed; `port/runtime/errors_test.go` expectations failed on `eGo:` then passed. Per-module build/vet clean; `go test ./...` passes in root, 4 publishers, persistence/postgres, test/compat, benchmark, inttest; `example` has no tests. The only env var with a code reader was `EGO_TELEMETRY_CONTRACT_DUMP`; no workflow or Makefile sets any `EGO_*` variable.
- Release-note deprecations: env var `EGO_TELEMETRY_CONTRACT_DUMP` (test-only) now `URD_TELEMETRY_CONTRACT_DUMP`, old name still read; publisher `ID()` values and Kafka client ID now `urd-*`; free-text error messages now start with `urd:` (and name `engine.WithProjection` / `engine.WithTenant` / `engine.WithEntityFamilies`); `ErrMissingRequiredExtensions` text says "urd extensions"; the `ego: concurrency conflict` grammar is unchanged.
- T3 done (b3fa435 readme and docs, 411b4a1 MIGRATION.md and changelog, e3e076c .github, 7293eed Makefile/Dockerfile.ci/benchmark Makefile, 813c4dc example/cluster plus the `compose/goakt/app_test.go` label, 1a19a45 go.mod comments). Checks: `go build ./...` clean in root, `example`, `persistence/postgres`, `publisher/kafka`; `go vet ./...` clean in `example`; `go test ./compose/...` ok; `example/cluster/k8s/*.yaml` parse as valid YAML. Leftover `ego` hits are kept names (egopb, `ego.*` keys, `ego_*` metrics, `ego_schema_migrations`, `protos/ego`, `gen/ego`), historical ADR IDs, the fork credit, the MIGRATION/"Formerly ego" notes, and unit-migration test names. Open item for the maintainer: `assets/logo.png` and `assets/logo.svg` still draw "eGo" (image content, alt text now says Urd).
- T4 done (2b33411 gofmt import order in three `persistence/*_test.go` files that the module rename left unsorted, 92b8a0a `TestEngineClusterKindsExposesUrdActors`, `TestUrdSpawnOptionsResolveThroughRuntime`, `TestUrdSentinelIsThePublishingSentinel` plus their doc references; none starts with `TestCluster`, so CI lanes are unchanged). All 10 modules: `go build ./...`, `go vet ./...` and plain `go test ./...` pass (root 39 packages ok, benchmark 1, inttest 3 with Docker, postgres 1, each publisher 1, test/compat 1, `example` has no tests); no failures, no skipped packages. `go list -m all | rg getsyntegrity/ego` is empty in every module. `go test -race -count=1 ./...` on the root module: 39 packages ok. gofmt clean for every Go file changed since `afaafb3`. Leftover `ego` word hits are all explained: kept names (E1/E3/E4/E5/E6/E8/E10: `egopb`, `ego.*` keys and the reserved `ego.` prefix, `Ego*ExtensionID` values, `ego_*`/tracer names, `ego-events`, `ego_schema_migrations`, `ego: concurrency conflict`), change and ADR identifiers in comments (`ego-arch-004`, `EGO-TENANT-002`, ...) and `openspec/`, `odd/`, `.spec-governance/` history, the fork credit, "Formerly ego" and MIGRATION.md, and the `eGo` text inside `assets/logo.svg`. Local `develop` is stale (an ancestor of `afaafb3`), so the diff check used `afaafb3`: only rename-related files changed.

## Hand-off checklist (T5, for the maintainer; not executed)

Facts this checklist relies on (queried 2026-10-02 from proxy.golang.org):
the old root path `github.com/getsyntegrity/ego` has no tagged versions; each
old publisher path (`.../ego/publisher/{kafka,nats,pulsar,websocket}`) has
`v0.1.0` cached; `persistence/postgres`, `example`, `inttest` were never
published. Old and new module paths live in the SAME repository, so they share
one tag namespace: a tag created for the old path is also seen by the new path.
That is why the deprecation tags below are deleted again once the proxy has
cached them, and why the new modules start at `v0.1.0` only after the reset.

The release workflow (`.github/workflows/release.yml`) tags only the root
module (next minor after the latest `v*` tag). Every submodule `go.mod`
requires the root at `v0.0.0` plus a local `replace`; consumers ignore the
`replace`, so submodules must require a real root version before they are
tagged (step 4).

### 1. Deprecate the old module paths (repo still named `ego`)

```sh
cd ~/workspace/getsyntegrity/ego
git fetch origin && git switch -c chore/deprecate-ego-module origin/develop
for f in $(git ls-files '*go.mod'); do
  sed -i '0,/^module /s##// Deprecated: moved to github.com/getsyntegrity/urd\nmodule #' "$f"
done
git diff --stat            # 10 go.mod files, one comment line each
git commit -am "chore: deprecate the ego module paths in favor of urd"
git push -u origin chore/deprecate-ego-module
# Versions must be newer than what the proxy has for the OLD paths.
git tag v0.0.1
for p in kafka nats pulsar websocket; do git tag "publisher/$p/v0.1.1"; done
git push origin v0.0.1 publisher/{kafka,nats,pulsar,websocket}/v0.1.1
# Make the proxy cache them (this is what makes the deprecation visible):
GOPROXY=https://proxy.golang.org go list -m github.com/getsyntegrity/ego@v0.0.1
for p in kafka nats pulsar websocket; do
  GOPROXY=https://proxy.golang.org go list -m "github.com/getsyntegrity/ego/publisher/$p@v0.1.1"
done
# Confirm: prints "(deprecated)" after the version.
GOPROXY=https://proxy.golang.org go list -m -u github.com/getsyntegrity/ego/publisher/kafka@latest
```

### 2. Rename the repository

```sh
gh repo rename urd --repo getsyntegrity/ego
```

### 3. Point local clones at the new URL

```sh
git -C ~/workspace/getsyntegrity/ego remote set-url origin git@github.com:getsyntegrity/urd.git
git -C ~/workspace/getsyntegrity/ego fetch origin
```

### 4. Reset all tags, merge, then tag v0.1.0

```sh
cd ~/workspace/getsyntegrity/ego
# 4a. Delete every remote and local tag (old upstream tags + the step-1 tags).
#     The proxy keeps serving what it already cached for the old paths.
git ls-remote --tags origin | awk '{print $2}' | grep -v '\^{}$' \
  | sed 's#refs/tags/##' | xargs -r -n 50 git push origin --delete
git tag -l | xargs -r git tag -d
gh release list --repo getsyntegrity/urd --limit 200   # delete stale releases if any:
# gh release delete <tag> --repo getsyntegrity/urd --yes
# 4b. Open and merge the rename PR into develop (CI must pass).
gh pr create --repo getsyntegrity/urd --base develop --head refactor/rename-to-urd \
  --title "refactor!: rename the project to urd" --body-file MIGRATION.md
# 4c. Merge develop -> main as usual: release.yml computes and creates v0.1.0
#     for the root module (no v* tag exists any more). Verify:
GOPROXY=https://proxy.golang.org go list -m github.com/getsyntegrity/urd@v0.1.0
# 4d. Submodules: require the published root, then tag them.
git switch -c chore/require-urd-v0.1.0 origin/develop
for d in persistence/postgres publisher/kafka publisher/nats publisher/pulsar publisher/websocket; do
  (cd "$d" && go mod edit -require=github.com/getsyntegrity/urd@v0.1.0 && go mod tidy)
done
git commit -am "build: require github.com/getsyntegrity/urd v0.1.0 in submodules"
# PR -> develop -> main as usual, then on the merged main commit:
for d in persistence/postgres publisher/kafka publisher/nats publisher/pulsar publisher/websocket; do
  git tag "$d/v0.1.0"
done
git push origin persistence/postgres/v0.1.0 publisher/{kafka,nats,pulsar,websocket}/v0.1.0
```

### 5. Index the new modules on pkg.go.dev

```sh
for m in "" /persistence/postgres /publisher/kafka /publisher/nats /publisher/pulsar /publisher/websocket; do
  GOPROXY=https://proxy.golang.org go list -m "github.com/getsyntegrity/urd$m@v0.1.0"
done
```

Then open https://pkg.go.dev/github.com/getsyntegrity/urd/engine and press
"Request" if the page is not there yet.

### 6. External references

- Repo description and topics: `gh repo edit getsyntegrity/urd --description "Urd — event sourcing for Go"`.
- Branch protection for `main` and `develop`: check in Settings → Branches (rules usually survive a rename; confirm).
- Actions secrets and variables referenced by name: unchanged by a rename; check any that embed the repo name.
- Security advisories and Discussions links now resolve via the redirect; the templates already point at `urd`.
- Downstream repos importing `github.com/getsyntegrity/ego/...`: run the sed snippet from `MIGRATION.md`.
- Logo: `assets/logo.png` and `assets/logo.svg` still draw the "eGo" wordmark.
- After the merge, remove the worktree: `git -C ~/workspace/getsyntegrity/ego worktree remove ../ego-worktrees/rename-to-urd`.
