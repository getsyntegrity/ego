# Adopt the provided CI/CD pipeline (develop/main model)

## Objective

Replace ego's entire CI/CD pipeline with the pipeline delivered in `ego-pipeline.zip`
(source copy: `/home/pablog/.claude/jobs/0a9fef40/tmp/inner/ego/`), adapted only where the
zip carries values from another organization that cannot work in `getsyntegrity/ego`.

## Problem and why

The current pipeline (`build.yml`, `pull_request.yml`, `release.yml`, `release-publishers.yml`,
`stale.yml`, `scripts/ci/*`, and the Go CI tools under `internal/cmd/*`) is trunk-based and
tag-triggered. The user decided to discard it completely and adopt the zip's model instead:
`develop` as the integration branch, PRs `develop`/`hotfix/*` -> `main`, a single required
check `ci-ok`, automatic tag + GitHub Release on merge to `main` (version from
`next-version.sh` and `release:*` labels), a `release-note` PR gate, CodeQL, apidiff and a
Go SDK update workflow.

Rejected alternative: merging the zip's features into the current workflows while keeping the
custom gates (nested-module matrix, `archcheck`, `ciselect`, `vulngate`, release gate,
proxy/sumdb wait, publisher bump flow). The user explicitly chose full replacement.

## Scope and adaptations (authorized)

Taken from the zip as-is: structure, jobs, scripts, templates, `.golangci.yml`,
`.goreleaser.yaml`, `OWNERS`, `dependabot.yml`, `.go-version`.

Adapted because the zip values belong to another organization:

- `runs-on: generic-s` -> `ubuntu-latest` (the label does not exist for this repo).
- `GOPRIVATE` for `parkmobileusa`/`easyparkgroup` and the `insteadOf` token injection removed.
- `ORG_CHECKOUT_TOKEN` falls back to `github.token` when the secret is absent.
- Slack notification is skipped when `SLACK_BOT_TOKEN` is absent; `#epm-us-*` channels removed.
- Comments translated to English (repository artifact language).
- Nested modules (`publisher/*`, `benchmark`, `example/cluster`, `test/compat`): the zip only
  handles the root module. Minimal adaptation: `tidy` checks every `go.mod`, a job
  builds/vets/tests each nested module, and `dependabot.yml` lists each module directory.

Out of scope (user/remote decisions): creating the `develop` branch on GitHub, branch
protection (`ci-ok` as the required check), repository secrets, `release:*` labels
(`.github/scripts/labels.sh` creates them).

## TDD

Strict TDD is enabled globally, but this change is CI configuration and shell scripts with no
Go behavior; RED/GREEN does not apply. Ordinary functional checks below replace it.

## Checks

- `actionlint` on all workflows (pinned via `go run github.com/rhysd/actionlint/cmd/actionlint@latest`).
- `bash -n` and `shellcheck` (if available) on `.github/scripts/*.sh`.
- `go build ./... && go vet ./...` and `go test ./...` (no `-race`) after deleting `internal/cmd/*`.
- `next-version.sh develop` from repo root prints `version=v0.1.0`.
- `rg` finds no remaining references to deleted files outside `odd/`, `openspec/`, `CHANGELOG.md`.

## Tasks

- [x] T1 Remove the current pipeline: workflows, `scripts/ci/`, `internal/cmd/*`, old `.md`
      issue templates, `renovate.json`, and dangling references (Makefile, docs). Route: delegated.
- [x] T2 Import the zip pipeline with the adaptations above. Route: delegated.
- [x] T3 Update `docs/ci.md`, `docs/main-branch-policy.md`, `contributing.md`, `readme.md` to the
      develop/main model and new checks. Route: delegated.

Route evidence: 2+ non-trivial files per task -> writer trigger; one bounded writer runs T1-T3.

## Delivery

Branch `ci/replace-pipeline`, one work-unit commit per task. Strategy `exception-ok`: the change
is mostly deletions plus copied config, a single PR is the reviewable unit.

## Progress

T1 done (commit recorded below). Checks: `go build ./... && go vet ./...` OK; `go mod tidy` no change;
Makefile `docker-test` now runs plain `go test -coverprofile`; comments naming archcheck reworded.
T2 done. Checks: actionlint v1.7.12 clean; `bash -n` OK on all 5 scripts (shellcheck not installed);
root `go build/vet/test ./...` green; each nested module build/vet/test green and `go mod tidy` clean;
`next-version.sh develop` on a tag-less repo prints `version=v0.1.0` (local clone has v4.x tags, so it
refuses there by design). Decisions: go-setup lost its `token`/`private` inputs (callers updated);
notify takes `channel` (from `vars.SLACK_CHANNEL`) and skips without token or channel; jobs that write
(changelog, sync_develop, go-sdk-update) got explicit `permissions` because the token can fall back to
`github.token`; root `SECURITY.md` (a GoAkt copy) removed in favor of `.github/SECURITY.md`; new `modules`
matrix job in ci.yml is part of `ci-ok`. `.go-version` 1.27.1 kept (release exists).
T3 done. `docs/ci.md` (1820 lines about the old pipeline) rewritten as a short guide; `docs/main-branch-policy.md`,
`contributing.md` and the `readme.md` badge/pre-release line updated. Dangling-reference `rg` over the repo
(excluding odd/, openspec/, CHANGELOG.md) returns nothing.

Pending for the user (GitHub settings): create `develop`; protect `main`/`develop` with `ci-ok` + `pr-meta`;
run `.github/scripts/labels.sh`; enable "Allow GitHub Actions to create pull requests"; optional
`ORG_CHECKOUT_TOKEN`, `SLACK_BOT_TOKEN`, `vars.SLACK_CHANNEL`.
Known gap: the pipeline only tags the root module; the nested publisher modules get no `publisher/*/vX.Y.Z` tags.
