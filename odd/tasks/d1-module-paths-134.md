# Feature: D1 module path migration and a clean-consumer resolution check (#134)

Branch: `feat/134-d1-module-paths` · Base: `origin/main` `50c4a4f` · Issue: #134 · Related: #102 (ADR `openspec/changes/ego-arch-006`, D1/D2), #159, #39

## Problem

The four publishers cannot be installed from outside the repository. Each `go.mod` declares a path with the root's major suffix in the middle (`github.com/pablogore/ego/v4/publisher/kafka`), so Go looks for the directory `v4/publisher/kafka`, which does not exist. On top of that, every module still carries the old `pablogore` identity while the repository lives at `getsyntegrity/ego`.

## What changes

The maintainers confirmed D1 on 2026-09-28:

| Module (directory) | New path | Tags |
|---|---|---|
| root (`.`) | `github.com/getsyntegrity/ego/v4` | `vX.Y.Z` |
| `publisher/kafka`, `nats`, `pulsar`, `websocket` | `github.com/getsyntegrity/ego/publisher/<name>` | `publisher/<name>/vX.Y.Z` (v0/v1 only; v2+ needs a `/v2` path) |
| `benchmark`, `example/cluster`, `test/compat` (never released, D5) | `github.com/getsyntegrity/ego/<dir>` | none |

One mechanical pull request does the whole rename, because a half-renamed tree does not build. Generated protobuf code is regenerated with `buf`, never text-edited: `go_package` sits inside a length-prefixed serialized descriptor, and a text rename compiles but panics in `init` (measured in the pre-PR spike: `slice bounds out of range [-4:]`).

A new script, `scripts/ci/verify-consumer.sh`, proves the result the way a consumer sees it: it clones the committed `HEAD` into a temporary bare repository, creates the release tags there only, points Go at it through an isolated git configuration, and from an empty module runs `go get`, `go build` and `go run` of every publisher with no `replace`. CI runs it, so the evidence is in the repository and in every relevant run, not in a one-off log.

## Why

- `go list -m <path>@<tag>` alone is not acceptance evidence: the spike showed it succeeding even when the tagged `go.mod` declared another path. Only `go get` plus a build and a run catch that, and only a run catches the corrupted descriptor.
- Without a root tag, no publisher resolves externally, not even by pseudo-version, because each requires the exact root version. The public-proxy check therefore stays pending until the root tag is published; the local-remote check is the pre-tag evidence.

## Scope and constraints

- **In scope:** every `go.mod` module/require/replace line, every import, `protos/*.proto` `go_package`, regenerated `egopb/ego.pb.go` and `test/data/testpb/test.pb.go`, CI/release scripts and workflows that write the path literally, `internal/cmd/{archcheck,ciselect}` literals, `benchmark/Makefile`, publisher `closure_test.go`, `readme.md` badges, docs, a new `CHANGELOG.md` entry, `scripts/ci/verify-consumer.sh` and its CI wiring, `docs/ci.md`.
- **Preserved as historical:** `openspec/**`, `odd/**` (other than this document), `docs/ci/baseline-159-a1.md`, and the existing `CHANGELOG.md` entries (a new entry is added instead).
- **Out of scope:** `github.com/pablogore/kit-logger` (separate module, #39 REL-007); the `@pablogore` owner handle in `.github/CODEOWNERS`; the pre-existing `go_package` of `protos/test/test.proto`'s odd `…/tests/v4/…` segment beyond the owner rename; the first root version number (`releaseplan`'s `v4.0.1` vs the `v4.4.3` the publishers require), resolved separately before the first release.
- **No tag is created or pushed, and no release is started.**
- **TDD:** strict (user global configuration); runner `go test`, plus `scripts/ci/verify-consumer.sh` as the behavioral check. Never `-race` locally, never the workbench.
- **RDD:** off (global).
- **Delivery:** one PR (maintainer instruction), one work-unit commit per task. It exceeds the ~400-line heuristic by nature (mechanical rename over ~227 files); this is expected and not split.

## Tasks

- [x] T1 RED — add `scripts/ci/verify-consumer.sh` and observe it fail on the unmigrated tree (reproduces #134). Route: delegated writer (T1–T3 together; 2+ non-trivial files).
- [x] T2 GREEN — mechanical rename per the table, `buf` regeneration of the two `.pb.go`, badges, `golangci-lint fmt`; `verify-consumer.sh` passes; build/vet/test/lint/archcheck in every module.
- [x] T3 CHANGELOG entry, `docs/ci.md`, and CI wiring: a consumer job in `pull_request.yml` (when the plan is `full`) and `build.yml` (always), included in `CI Gate`.
- [x] T4 Push the branch, open the PR, update #134 (acceptance criteria: `go list -m` alone does not count; the public-proxy check is pending until the root tag). Route: inline (`gh`).

## Acceptance criteria

- `rg --hidden 'pablogore/ego'` outside the preserved historical files finds nothing.
- `scripts/ci/verify-consumer.sh` fails on `50c4a4f` and passes on the branch head.
- `go build ./...`, `go vet ./...`, `go test ./...` pass in all eight modules; archcheck and `golangci-lint run` pass in the root.
- The two regenerated `.pb.go` files are byte-identical to `buf generate` output.

## Progress

### T1 (RED) — done

- Added `scripts/ci/verify-consumer.sh`. It discovers the root module path
  and released publisher list/paths/required-root-version from `go.mod` and
  `scripts/ci/release-modules.txt` (no hard-coded paths), clones the
  committed HEAD into a temporary bare repo, tags only that clone (root
  version + each `<dir>/v0.1.0` + a negative `<first pub>/v2.0.0`), and
  resolves every published path from an empty consumer module with an
  isolated git config (`GIT_CONFIG_GLOBAL`, `url.insteadOf`) and
  `GOPRIVATE` pointed at the local clone — no public proxy/sumdb traffic
  for this repo's own paths.
- RED evidence, run on unmigrated `50c4a4f` (env:
  `PATH=<go1.26 toolchain>/bin:$GOPATH/bin:...
  GOROOT= GOWORK=off ./scripts/ci/verify-consumer.sh`):
  ```
  go: creating new go.mod: module example.com/verifyconsumer
  go: github.com/pablogore/ego/v4/publisher/kafka@v0.1.0: invalid version: unknown revision v4/publisher/kafka/v0.1.0
  ```
  Exit code 1, as expected: the publisher's `go get` fails to resolve
  `v4/publisher/kafka/v0.1.0` because the tag prefix does not match a real
  directory (reproduces #134).
- Commit: `test(ci): add a clean-consumer resolution check for published module paths (#134)` — SHA `b9765c1`.

### T2 (GREEN) — done

- Renamed `github.com/pablogore/ego` → `github.com/getsyntegrity/ego` in 226
  tracked files (`rg -l --hidden` outside `openspec/**`, `odd/**`,
  `CHANGELOG.md`, `docs/ci/baseline-159-a1.md`, piped to `sd`); `kit-logger`
  untouched (different path, regex required a trailing `/ego` word
  boundary). Then dropped `/v4` for nested modules only (`publisher/*`,
  `benchmark`, `example/cluster`, `test/compat`) with the Rust-regex `sd`
  pattern from D1. Fixed the four `readme.md` shields.io badges that used
  the bare `pablogore/ego` form (no `github.com/` prefix).
- Regenerated `egopb/ego.pb.go` and `test/data/testpb/test.pb.go` with
  `buf generate` (buf v1.73.0, protoc-gen-go v1.36.12); confirmed
  byte-identical to a fresh `buf generate` output both right after
  copying and again at the end of T2, after every other edit.
  `example/examplepb/sample.pb.go` (tochemey's own `go_package`) is
  unchanged, as expected.
- `golangci-lint fmt --config .golangci.yml ./...` made zero additional
  changes: the renamed import blocks were already gofmt/goimports-clean
  (the existing local-import group stayed separate from third-party
  imports; `sd`'s rename never merged or reordered a group).
- **Judgement call — `internal/cmd/archcheck` needed a real code fix, not
  just a text rename.** `ExternalAdapterLayer` (`internal/cmd/archcheck/rules/layers.go`)
  matched a publisher package by `rootModulePath + "/publisher"`
  (`github.com/getsyntegrity/ego/v4/publisher`). Since D1 drops `/v4` for
  nested modules, no publisher package import path starts with that
  prefix any more, so both adapter rules
  (`external-adapter-no-runtime`, `external-adapter-no-composition`)
  matched zero packages and archcheck refused to run at all
  ("rule(s) matched zero packages in the graph"). Added
  `repoPathFromModule` (strips a trailing Go major-version path element,
  `/vN`, N≥2) and made `ExternalAdapterLayer` match against the
  repository path instead of the root module path. Updated the rule
  engine's own unit fixtures (`evaluate_test.go`, a new `repoRoot`
  const; `adapter_composition_test.go`) to the same `repoRoot`-prefixed
  publisher paths, since they had encoded the old (pre-D1) assumption.
  This is squarely in the feature's stated scope
  ("`internal/cmd/{archcheck,ciselect}` literals") — it just turned out
  to be a logic fix, not a literal. `internal/cmd/ciselect` needed no
  equivalent fix: it only ever matches root-module packages.
- `go mod tidy` was needed (go.sum unaffected in every case, confirmed by
  `git diff --stat`) in `publisher/nats`, `publisher/websocket`,
  `benchmark`, `example/cluster` and `test/compat`: renaming
  `pablogore` → `getsyntegrity` in place shifted a `require`/`replace`
  block out of the alphabetical order `go mod tidy` enforces (`kafka` and
  `pulsar` happened to stay in order). `test/compat`'s reordering also
  covers its `replace` block, since dropping `/v4` from the four
  publisher paths moved them ahead of the root's own `v4`-suffixed path
  too.
- Verification, `PATH=<go1.26 toolchain>/bin:$GOPATH/bin:...
  GOROOT= GOWORK=off`, all commands reported per instructions:
  - root: `GOFLAGS=-mod=mod go build ./...` → clean. `go vet ./...` →
    clean. `go run ./internal/cmd/archcheck` →
    `8 modules checked, 55 packages checked, 213 edges checked, 0
    baselined, 0 violation(s), 0 stale entries`.
    `go test -count=1 ./...` → all 33 root packages `ok` (or
    `[no test files]`), exit 0 (re-run after the archcheck fix and every
    nested `go mod tidy`, to cover the final tree).
  - `golangci-lint run --modules-download-mode=mod --timeout 10m --config
    .golangci.yml` (root's own `.golangci.yml` sets
    `modules-download-mode: vendor`, which needs `go mod vendor` first —
    the environment note says to use `GOFLAGS=-mod=mod` locally instead,
    so the flag override avoids ever creating `vendor/`) → **14
    pre-existing `revive: var-declaration` findings in `command/errors.go`
    and `tenancy/errors.go`**, confirmed unrelated to this change: `git
    diff 50c4a4f -- command/errors.go tenancy/errors.go` is empty (neither
    file was touched by the rename), and running the identical lint
    command against a `git archive 50c4a4f` checkout reproduces the exact
    same 14 findings. This is pre-existing lint debt on `main`, not
    something D1 introduced; **flagging for your decision** — fix it here
    (out of the stated mechanical scope) or leave it for a separate,
    unrelated cleanup. Not fixed in this PR.
  - Each nested module (`publisher/{kafka,nats,pulsar,websocket}`,
    `benchmark`, `example/cluster`, `test/compat`), via
    `GO_TEST_RACE=0 scripts/ci/verify-module.sh <dir>` (download, `go mod
    tidy -diff`, build, vet, `golangci-lint run` against the root
    `.golangci.yml`, `govulncheck` gated by
    `scripts/ci/govulncheck-allow.json`, `go test -count=1`): all seven
    →  **0 lint issues, 0 blocked govulncheck findings** (pulsar has 3
    pre-existing, already-excepted findings, owner `@pablogore`, review
    2026-12-28 — untouched by this change), tests `ok`.
  - `rg --hidden 'pablogore/ego' -g '!openspec/**' -g '!odd/**' -g
    '!docs/ci/baseline-159-a1.md' -g '!CHANGELOG.md'` → no matches.
  - `verify-consumer.sh` on committed HEAD `04a61f8` (GREEN): resolves
    root `github.com/getsyntegrity/ego/v4@v4.4.3` and all four publishers
    at `v0.1.0` with no local `replace`, runs the consumer binary
    (`verify-consumer: resolved, built and ran every published module
    path with no local replace`), and correctly rejects the negative
    `publisher/kafka@v2.0.0` tag. Ends `verify-consumer.sh: OK`.
- Commit: `refactor!: migrate module paths to github.com/getsyntegrity/ego (#134)` — SHA `04a61f8`.

### T3 — done

- `CHANGELOG.md`: added a new `[Unreleased]` → `💥 Breaking Changes` entry
  at the top (existing entries, including the older fork-move entry,
  left untouched — confirmed `git diff CHANGELOG.md` has no `-` lines
  besides the diff header). States the D1 table, that a publisher path
  could never resolve before this change, that a root pseudo-version pin
  keeps building but must rewrite imports to upgrade, and that no root
  tag exists yet.
- `docs/ci.md`: expanded "Release verification: two different questions"
  to three (integrated / clean-consumer / published), and added a new
  "Verify clean consumer" section: what it proves, why `go list -m`
  alone is insufficient, the 5-step mechanism, `VERIFY_CONSUMER_PUBLISHER_VERSION`,
  and the CI wiring. Updated "The `ci-gate` job" section for the new
  `consumer` need. Every literal `pablogore` path already inside the file
  had been mechanically renamed in T2 (it matched the same repo-wide
  regex); this task only added new prose and fixed the two subsequent
  cross-references.
- CI wiring: added a `consumer` job (`name: Verify clean consumer`) to
  both `.github/workflows/pull_request.yml` (`needs: plan`, `if:
  needs.plan.outputs.mode == 'full'`) and `.github/workflows/build.yml`
  (`needs: plan`, unconditional — `plan` there always selects `full`).
  Both use the same checkout (`fetch-depth: 0`) and `actions/setup-go@v7`
  (`go-version: "1.27.0"`, `cache-dependency-path: "**/*.sum"`) shape as
  the existing jobs, then run `scripts/ci/verify-consumer.sh`. Added
  `consumer` to `ci-gate`'s `needs` in both workflows and to its result
  check, treated like `modules` (success or `skipped` — `skipped` only on
  `pull_request.yml`, since `build.yml`'s `consumer` has no skip
  condition, so only `success` is accepted there); updated both gate
  step's echoed labels and comments to match.
- Verification:
  - `python3 -c 'import yaml,sys;yaml.safe_load(open(sys.argv[1]))' .github/workflows/pull_request.yml` → OK (parses).
  - Same for `.github/workflows/build.yml` → OK.
  - `actionlint`: not installed in this environment; skipped per
    instructions ("if actionlint is available run it").
  - No Go code changed in this task; root/nested build-vet-test suites
    are unaffected and were not re-run for T3 alone.
- Commit: `ci: verify a clean consumer resolves the published module paths (#134)` — SHA `50c0c26`.
- Caught by the final `rg` residual check (T2/T3's own acceptance
  criterion) before reporting done: the new `docs/ci.md` prose I added
  had spelled out the old `github.com/pablogore/ego/v4/publisher/<name>`
  path verbatim as an illustration. Fixed with a small follow-up commit,
  `docs(ci): avoid restating the old owner path literally in docs/ci.md`
  — SHA `d3a14b4`. `rg --hidden 'pablogore/ego\b' -g '!openspec/**' -g
  '!odd/**' -g '!CHANGELOG.md' -g '!docs/ci/baseline-159-a1.md'` now
  finds nothing.

### T4 — done

- Branch pushed; PR #174 opened against `main` (label `enhancement`, `Refs #134`, not `Closes`: the public-proxy check stays pending).
- Parent-side independent checks before opening it: `scripts/ci/verify-consumer.sh` re-run on the branch head (OK); `ciselect` decision identical on `main` and the branch for 8 synthetic changes; `git tag -l` empty.
- #134 body: acceptance criteria rewritten (`go list -m` alone is not acceptance; local-remote check via `verify-consumer.sh`; public-proxy check pending until the root tag; no tag published; `v4.0.1` vs `v4.4.3` resolved separately before the first release). A comment links #174.

## Next step

CI on #174, maintainer review and merge. The first `v*` tag stays on hold until the public-proxy check can run after the root tag, and the `v4.0.1` vs `v4.4.3` question is settled.
