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
- [ ] T2 GREEN — mechanical rename per the table, `buf` regeneration of the two `.pb.go`, badges, `golangci-lint fmt`; `verify-consumer.sh` passes; build/vet/test/lint/archcheck in every module.
- [ ] T3 CHANGELOG entry, `docs/ci.md`, and CI wiring: a consumer job in `pull_request.yml` (when the plan is `full`) and `build.yml` (always), included in `CI Gate`.
- [ ] T4 Push the branch, open the PR, update #134 (acceptance criteria: `go list -m` alone does not count; the public-proxy check is pending until the root tag). Route: inline (`gh`).

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
  `PATH=/home/pablog/sdk/go1.26.6/bin:/home/pablog/go/bin:...
  GOROOT= GOWORK=off ./scripts/ci/verify-consumer.sh`):
  ```
  go: creating new go.mod: module example.com/verifyconsumer
  go: github.com/pablogore/ego/v4/publisher/kafka@v0.1.0: invalid version: unknown revision v4/publisher/kafka/v0.1.0
  ```
  Exit code 1, as expected: the publisher's `go get` fails to resolve
  `v4/publisher/kafka/v0.1.0` because the tag prefix does not match a real
  directory (reproduces #134).
- Commit: `test(ci): add a clean-consumer resolution check for published module paths (#134)` — SHA `<filled after commit>`.

## Next step

T2 (GREEN): the mechanical module-path rename.
