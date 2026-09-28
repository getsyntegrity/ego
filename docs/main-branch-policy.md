# `main` branch policy: one branch, affected checks in PRs, full suite on `main`

Ego has a single long-lived branch, `main`. There is no `develop`. Work happens on short-lived feature and hotfix branches that reach `main` only through a pull request, and every change is verified twice: once by the pull request, on the modules it affects, and once more after the merge, on everything.

This document records what the CI does on each kind of branch, how `main` is protected, and what to do when `main` turns red after a merge. The mechanics of each workflow are in [`docs/ci.md`](ci.md).

## What runs where

| Event | Workflow | What it verifies |
|---|---|---|
| Push to a feature or hotfix branch | nothing | A push alone triggers no workflow. `pull_request.yml` listens only to `pull_request`, and `build.yml` only to pushes to `main`. |
| Pull request opened or updated towards `main` | `pull_request.yml` | The `plan` job runs `internal/cmd/ciselect` on the diff. The `modules` matrix runs the changed modules and every module that depends on them, transitively, including dependencies used only by tests. `CI Gate` aggregates the result. |
| Push to `main` (every merge) | `build.yml` | `ciselect -all`: the root module and every nested module, the full suite. |
| Tag `v*` | `release.yml` | Publishes a release. See "Releases and a red `main`" below. |

**When the root module runs in a pull request.** The matrix entry `modules (.)` appears only when the plan selects the root, because the change touches it or something it depends on. The branch name plays no part: no workflow reads it. When the root runs, `Build (root)` and `Vet (root)` cover the whole root module, while `Run tests (root)` runs only the packages the plan selected, with `-race` (`GO_TEST_RACE: "1"`). A change to a global path (for example anything under `.github/` or `scripts/ci/`) selects everything.

Real plans, measured on 2026-09-28:

| Pull request | Change | Plan |
|---|---|---|
| #168, run `36420966320` | `openspec/` only | `Mode: none`, 0 of 35 packages; `modules` skipped; `CI Gate` green |
| #169, run `36436464001` | includes `.github/workflows/build.yml` (global) | `Mode: full`, 36 of 36; root and all 7 nested modules |

After the merge of #167, run `36420765355` of `build.yml` on `main` ran `plan` with `-all`, the root and all 7 nested modules.

## Protection of `main`

Effective settings, read back from `GET /repos/getsyntegrity/ego/branches/main/protection` on 2026-09-28:

| Setting | Value | Meaning |
|---|---|---|
| Required status checks | `CI Gate`, from the GitHub Actions app (id 15368) | Only a `CI Gate` reported by GitHub Actions counts; a status with the same name from anything else does not. |
| Require branches to be up to date (`strict`) | `true` | A pull request must include the latest `main` before it can merge, so `CI Gate` has verified the exact result of the merge. |
| Enforce for administrators (`enforce_admins`) | `true` | Administrators follow the same rules; nobody bypasses `CI Gate`. |
| Force pushes | not allowed | |
| Deletion | not allowed | |
| Required reviews | none | Review is by convention, not enforced. |
| Merge queue | none | |
| Rulesets | none | The classic branch protection above is the only policy. |

Allowed merge methods are unchanged: squash, merge commit and rebase are all enabled, and the project uses squash. Restricting it to squash only would be a separate decision.

**Consequence of `strict`.** When one pull request merges, every other open pull request must be updated with `main` and pass `CI Gate` again before it can merge. That is the price of verifying the exact merge result without a merge queue.

**Known conflict with `release.yml`.** `release.yml:169-170` commits the publishers' `go.mod` bumps and pushes them directly to `main` (`git push origin HEAD:main`) with the Actions token. With `CI Gate` required, GitHub rejects a direct push of a commit that has no `CI Gate` result (error `GH006`, "Required status check … is expected"). This has been true since `CI Gate` became required, and `enforce_admins` does not change it, because the Actions bot is not an administrator. The release has never run (the repository has no tags), so it has not surfaced yet. How the bump should reach `main` is an open maintainer decision.

## Releases and a red `main`

A release must come from a commit of `main` whose `build.yml` run succeeded for that exact commit. The release workflow's gate enforces this; see "Release gate" in [`docs/ci.md`](ci.md).

If the full suite fails on `main` after a merge:

1. **Stop releases.** Do not push any `v*` tag until `main` is green again. The release gate refuses a tag on a commit whose `build.yml` run did not succeed, but do not rely on it alone: announce the freeze to whoever may tag.
2. **Identify the commit.** Open the failed `build.yml` run on `main` (Actions → `build` → branch `main`), note its commit, and read which job failed and why. With squash merges, each commit on `main` is one pull request, so the failing commit names the pull request. If the failure looks unrelated to the change, compare with the previous green run on `main` before blaming the latest merge: it can be a flaky test or an external dependency.
3. **Revert or fix.** Prefer reverting the pull request (`git revert <sha>` on a branch, opened as a pull request) when the fix is not immediate: `main` goes back to its last known-good state and the author re-lands the change with the fix. A forward fix is fine when it is small and obvious. Either way it goes through a pull request and `CI Gate`, like any other change; there is no direct push to `main`.
4. **Confirm `main` is green.** Wait for the `build.yml` run of the revert or fix commit on `main` to succeed. Only then lift the release freeze.
5. **Record it.** Link the failed run, the causing pull request and the revert or fix in the causing pull request or its issue, and open an issue if the pull request's own CI should have caught the failure (a gap in `ciselect`'s selection is a CI bug).
