# `main` branch policy: one branch, affected checks in PRs, full suite on `main`

Ego has a single long-lived branch, `main`. There is no `develop`. Work happens on short-lived feature and hotfix branches that reach `main` only through a pull request, and every change is verified twice: once by the pull request, on the modules it affects, and once more after the merge, on everything.

This document records what the CI does on each kind of branch, how `main` is protected, and what to do when `main` turns red after a merge. The mechanics of each workflow are in [`docs/ci.md`](ci.md).

## What runs where

| Event | Workflow | What it verifies |
|---|---|---|
| Push to a feature or hotfix branch | nothing | A push alone triggers no workflow. `pull_request.yml` listens only to `pull_request`, and `build.yml` only to pushes to `main`. |
| Pull request opened or updated towards `main` (or towards a `docs/propose-*` branch, which `pull_request.yml` also targets) | `pull_request.yml` | The `plan` job runs `internal/cmd/ciselect` on the diff. The `modules` matrix runs the changed modules and every module that depends on them, transitively, including dependencies used only by tests. `CI Gate` aggregates the result. |
| Push to `main` (every merge, except one that only touches `**/readme.md` or `**/renovate.json`, which `build.yml` ignores through `paths-ignore`) | `build.yml` | `ciselect -all`: the root module and every nested module, the full suite. |
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

**Resolved: the publisher bump no longer touches `main` directly.** `release.yml`'s `prepare-publisher-bump` job (renamed from `release-publishers`, #159 F4 PR-C) used to commit the publishers' `go.mod` bumps and push them straight to `main` with `git push origin HEAD:main`, which a required, strict `CI Gate` check would have rejected with `GH006: Protected branch update failed` the first time a release ever ran. It now stops one step earlier: it pushes the bump commit only to its own branch, `release/publishers-<tag>`, created from `origin/main`, and prints the compare link plus a ready-made `gh pr create --base main --head release/publishers-<tag> ...` command in its step summary. A human runs that command, so the resulting pull request lands through the same branch-protection path described above — `CI Gate` required, `strict` up to date, `enforce_admins` on — like any other PR into `main`. Once that PR is merged, someone with repository access manually dispatches `.github/workflows/release-publishers.yml` (`workflow_dispatch`) with the merge commit's SHA to actually tag and release the publishers; see `docs/ci.md`, "Version policy," for the full three-stage flow.

## Releases and a red `main`

A release must come from a commit of `main` whose `build.yml` run succeeded for that exact commit. The release workflow's gate enforces this; see "Release gate" in [`docs/ci.md`](ci.md).

If the full suite fails on `main` after a merge:

1. **Stop releases.** Do not push any `v*` tag until `main` is green again. The release gate refuses a tag on a commit whose `build.yml` run did not succeed, but do not rely on it alone: announce the freeze to whoever may tag.
2. **Identify the commit.** Open the failed `build.yml` run on `main` (Actions → `build` → branch `main`), note its commit, and read which job failed and why. With squash merges, each commit on `main` is one pull request, so the failing commit names the pull request. If the failure looks unrelated to the change, compare with the previous green run on `main` before blaming the latest merge: it can be a flaky test or an external dependency.
3. **Revert or fix.** Prefer reverting the pull request (`git revert <sha>` on a branch, opened as a pull request) when the fix is not immediate: `main` goes back to its last known-good state and the author re-lands the change with the fix. A forward fix is fine when it is small and obvious. Either way it goes through a pull request and `CI Gate`, like any other change; there is no direct push to `main`.
4. **Confirm `main` is green.** Wait for the `build.yml` run of the revert or fix commit on `main` to succeed. Only then lift the release freeze.
5. **Record it.** Link the failed run, the causing pull request and the revert or fix in the causing pull request or its issue, and open an issue if the pull request's own CI should have caught the failure (a gap in `ciselect`'s selection is a CI bug).
