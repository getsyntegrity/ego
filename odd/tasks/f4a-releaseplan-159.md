# Feature: releaseplan, a pure release planner with a dry run on main (#159 A2 / F4, PR-A)

Branch: `feat/159-f4a-releaseplan` · Base: `origin/main` `48dc0a4` · Issue: #159 (A2, F4) · Related: #39, #38, #134

## Problem

The release pipeline that design ego-arch-006 calls F4 does not exist, and S2 is gated on it (design §3, D8 option C). Today `.github/workflows/release.yml` decides the release by hand:

- it hard-codes the root module path (`release.yml:90`, `:146`; `scripts/ci/verify-published.sh:35`);
- it has no notion of release order between modules;
- its `major` branch (`release.yml:126-130`) would compute a `publisher/<name>/v2.0.0` tag, which Go rejects for a module path without a `/v2` suffix.

## What changes

A new command, `internal/cmd/releaseplan`, reads every `go.mod` in the repository and computes, without side effects:

1. **Order.** The release order of the released modules, topologically sorted by their in-repository `require` edges (D3: a module is released after everything it requires). A cycle is an error that names the modules involved.
2. **Paths.** Each module's path, read from its `go.mod`, never hard-coded. So the planner keeps working unchanged when D1 migrates the paths.
3. **Tags.** The next tag of each released module under the approved D2 (a) scheme: the root is tagged `vX.Y.Z` and must match its `/vN` suffix; a nested module is tagged `<dir>/vX.Y.Z` and, without a `/vN` suffix, may only carry v0 or v1. A bump that would produce v2+ on a suffix-less path, or a major that does not match the root's suffix, is refused.

`build.yml` runs it as a dry run on every `main` push and writes the plan to the job summary. It publishes nothing and creates no tag.

"Pure" means the output depends only on the `go.mod` files and the explicit inputs (existing tags, requested bump). It does not call git, the network or the GitHub API. `go mod edit -json` is used to read each `go.mod`, as `internal/cmd/ciselect` does (`main.go:379-391`); it only parses the file.

## Scope and constraints

- **In scope:** `internal/cmd/releaseplan/**` with tests, a dry-run step in `.github/workflows/build.yml`, `docs/ci.md`, this document.
- **Out of scope, PR-B:**
  - checking that `build.yml` passed on the commit before any tag is created — a workflow step, not the planner, because doing it inside the same build's dry run would be circular;
  - changes to `release.yml` and `scripts/ci/verify-published.sh`.
- **Not touched:** module paths (D1 is not confirmed), production code, contracts, `pull_request.yml`. This PR does not unblock S2 on its own: S2 still needs the D1 confirmation and the rest of F4.
- **Which modules are released** is an explicit, reviewable input, not inferred. Today `release.yml` releases the root and `publisher/*`. `benchmark`, `example/cluster` and `test/compat` are never released (D5). A released module that requires an unreleased in-repository module is an error.
- **TDD:** strict (user global configuration); runner `go test`. Never `-race`, never the workbench.
- **RDD:** off (global).
- **Route:** delegated direct (one writer; 2+ non-trivial files).
- **Delivery:** one work-unit commit per task on this branch; push and PR after verification.

## Tasks

- [ ] T1 Module discovery and the release graph: read each `go.mod`, keep in-repository requires, topological order, cycle detection, released-requires-unreleased check (tests first).
- [ ] T2 Tag scheme: parse existing tags per module prefix, next version for `patch|minor|major`, D2 (a) naming, refusal of v2+ without `/vN` and of a root major that does not match its suffix (tests first).
- [ ] T3 CLI: flags for repository root, released-module list, existing tags file and bump; JSON plan plus a markdown summary; non-zero exit on any refusal. Tests on a synthetic repository in `testdata`, plus a run against this repository.
- [ ] T4 Dry run in `build.yml` (no publish, no tags) and `docs/ci.md`.

## Checks

- `GOROOT= GOWORK=off GOFLAGS=-mod=mod go test -count=1 ./internal/cmd/releaseplan/...` (no `-race`)
- `go vet` and staticcheck on the new package
- actionlint on `build.yml`
- `go run ./internal/cmd/releaseplan ...` on this repository: order puts the root before the publishers; no refusal for a `patch` bump
- `rg 'pablogore/ego' internal/cmd/releaseplan -g '!*_test.go' -g '!**/testdata/**'` shows only the package's own import paths, never a hard-coded module path used as data

## Progress and evidence

- Created before the first source write.
