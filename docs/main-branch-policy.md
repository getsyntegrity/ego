# Branch policy: `develop` integrates, `main` releases

Urd has two long-lived branches. `develop` is where work is integrated and `main` is where released code lives. Every merge to `main` publishes a new version, so `main` is never a place for unfinished work.

This document records which branches may reach which, what runs on each event, how the branches should be protected, and what to do when a release goes wrong. The mechanics of each workflow are in [`docs/ci.md`](ci.md).

## Who can target which branch

| Source branch | Target | Purpose |
|---|---|---|
| any feature branch (`feat/*`, `fix/*`, `docs/*`, ...) | `develop` | Normal work. |
| `develop` | `main` | A release. Merging it publishes a minor version unless a `release:*` label says otherwise. |
| `hotfix/*` | `main` | An urgent fix. Merging it publishes a patch version, then a `main` to `develop` pull request is opened automatically. |

Any other pull request to `main` fails the `flow` job of the CI.

## What runs where

| Event | Workflow | What it verifies or does |
|---|---|---|
| Push to a feature branch | nothing | A push alone triggers no workflow. |
| Pull request to `develop` or `main` | `ci`, `pr-meta` | Lint on the diff, root tests in shards, tests with the minimum Go version, build, vet and tests of every nested module, `go mod tidy` in every module, `apidiff`, `govulncheck`, and the `release-note` and owners checks. `ci-ok` aggregates the result. |
| Push to `develop` | `ci`, `security` | The same CI on the merged result, plus CodeQL and strict `govulncheck`. |
| Push to `main` (a merge) | `release` | Computes the version, tags it, publishes the GitHub Release, opens the changelog pull request to `develop`, and for hotfixes the sync pull request. |
| Nightly, and on demand | `security` | CodeQL and strict `govulncheck`. It warns; it does not block. |
| Manual | `go-sdk-update` | Bumps the Go SDK and opens a pull request to `develop`. |

## Protection of the branches

The rules below are GitHub settings, so they have to be applied by a repository administrator.

| Setting | Recommended value | Why |
|---|---|---|
| Required status checks on `main` and `develop` | `ci-ok` and `pr-meta` | `ci-ok` is the single gate for all technical checks; new jobs are added to it without touching the protection. |
| Require branches to be up to date | on | A pull request is verified against the exact result of the merge. |
| Force pushes and deletion | not allowed | The tags and history that consumers resolve must not change. |
| Required reviews | at least one from the code owners (`@pablogore`) | `.github/CODEOWNERS` requests the review; `OWNERS` documents the roles. |
| Direct pushes | not allowed | The changelog and hotfix sync go through pull requests too. |

## Labels drive the version

The version to publish comes from the pull request that lands on `main`. A `release:major`, `release:minor` or `release:patch` label overrides the default (minor from `develop`, patch from `hotfix/*`). While the module path has no `/vN` suffix, only `v0.x` and `v1.x` can be published; moving to `v2.0.0` requires a `release:major` label and a `/v2` module path in the same change. The `api` job blocks a breaking API change on a pull request to `main` unless it carries `release:major` (in `v0.x`, where semver allows breaking changes, it only warns).

## When a release goes wrong

A merge to `main` publishes immediately, so the guard rails are before the merge (`flow`, `api`, `test (min)`) and the response after it is a new version, never a rewrite.

1. **The release workflow failed.** Open the failed `release` run. If the tag was already created, re-running the workflow reuses it and finishes the GitHub Release (the tag is the source of truth; the workflow is idempotent). If nothing was created, re-run the workflow after fixing the cause.
2. **The release contains a bug.** Do not delete or move a published tag: consumers and the Go module proxy may already have cached it. Fix it with a `hotfix/*` pull request to `main`, which publishes the next patch version and is synchronized back to `develop`.
3. **A published version must not be used.** Add a `retract` directive to `go.mod` in the hotfix so `go get` warns consumers away from it.
4. **`main` is red.** `main` only receives merges that already passed `ci-ok`, so a red `main` is usually a flaky test or an external change. Re-run the failed jobs first. If it is a real regression, revert or fix it through a `hotfix/*` pull request; there is no direct push to `main`.
5. **Record it.** Link the failed run, the causing pull request and the fix in the pull request or an issue, and open a `kind/flake` issue when the cause is an unstable test.
