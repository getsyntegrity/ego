# CI/CD pipeline

This document explains how Ego is built, tested and released. The branching rules that surround the pipeline are in [`docs/main-branch-policy.md`](main-branch-policy.md), and the everyday contributor steps are in [`contributing.md`](../contributing.md).

Ego is a Go library, so a release is a git tag: `go get` resolves the tag, and nothing else has to be built or uploaded. The pipeline therefore does two jobs. On every pull request it proves the change is safe. On every merge to `main` it turns the merge into a semantic version tag plus a GitHub Release, without a human running any release command.

## The branch model

There are two long-lived branches.

- `develop` is the integration branch. Feature pull requests target it.
- `main` holds released code. Only two kinds of pull request may target it: `develop` to `main` (a normal release) and `hotfix/*` to `main` (an urgent fix). The `flow` job of the CI fails any other source branch.

The day-to-day path is: open a branch from `develop`, open a pull request back to `develop`, merge it once `ci-ok` is green. When enough work has accumulated, open a pull request from `develop` to `main`. Merging that pull request publishes the release. For an urgent fix, branch `hotfix/<name>` from `main`, open the pull request to `main`, and after the release the pipeline opens a `main` to `develop` pull request so the fix is not lost.

## What runs on a pull request

Everything is in `.github/workflows/ci.yml` and reports into one required check, `ci-ok`. Branch protection only has to require `ci-ok` (and `pr-meta`); adding a new job to the pipeline means listing it in the `needs` of `ci-ok`, and branch protection does not change.

| Job | What it does |
|---|---|
| `flow` | Rejects pull requests to `main` that do not come from `develop` or `hotfix/*`. On pull requests to `main` it also computes the version that will be published and prints it in the run summary. It fails early if the bump cannot be published, for example a major bump without `/vN` in `go.mod`. |
| `lint` | `golangci-lint` with `.golangci.yml`. On pull requests it only blocks issues introduced by the diff (`only-new-issues`), so existing problems do not stop new work. |
| `plan`, `test (shard N)`, `test-report` | The root module tests, split into shards by real timings from the previous run (see "Slow packages" below). `test-report` merges coverage, lists the slowest tests and stores the timings for next time. Pull requests that only touch Markdown, `CHANGELOG/`, `OWNERS` or issue templates skip the tests; `ci-ok` still reports. |
| `test (min)` | Builds and vets with the minimum Go version declared in `go.mod`. On pull requests to `main` it also runs every test with that version. |
| `modules (dir)` | Ego has nested Go modules (`benchmark`, `example/cluster`, `inttest`, `persistence/postgres`, `publisher/kafka`, `publisher/nats`, `publisher/pulsar`, `publisher/websocket`, `test/compat`). `./...` at the root does not reach them, so this job builds and vets each one and tests all of them except `inttest`, which is only built and vetted here (`go vet` compiles its test files). Its tests need Docker and run in the `inttest` job. |
| `inttest` | Runs the integration tests of the `inttest` module on real containers (see "Integration tests" below). It runs on every pull request to `main`, on manual runs, and on pull requests to `develop` that touch `inttest/`, `persistence/`, `example/cluster/`, `publisher/`, `compose/` or `engine/`. Otherwise it is skipped, and `ci-ok` accepts that. |
| `unit-gate` | The unit-test rules in `docs/testing/go-specs.md` (no testify, no generated mocks, go-specs, no real resources in unit tests), plus the rule that nothing under `inttest/` skips. |
| `tidy` | Runs `go mod tidy` in the root module and in every nested module and fails if `go.mod` or `go.sum` change. |
| `api` | Compares the public API with `apidiff`. Against `develop` it only warns. Against the latest tag (pull requests to `main`) it fails when the API breaks and the release is not labelled `release:major`. |
| `vuln` | `govulncheck`. It fails only when the code calls a vulnerable function. |
| `ci-ok` | Passes when every job above succeeded or was legitimately skipped. This is the single required status check. |

The race detector is not used anywhere in this pipeline. The Go version comes from `.go-version` in every job, through the `go-setup` composite action, so nothing pins it by hand. `TEST_SHARDS`, `COVERAGE_MIN` and `TESTFLAGS` are set at the top of `ci.yml`; coverage is only reported for now (`COVERAGE_MIN` is `0`).

### Slow packages

`gotestsum tool ci-matrix` only moves whole packages between shards, so one package that takes minutes would set the wall time of the whole run. The `plan` job therefore runs `.github/scripts/test-matrix.sh`. From the timings of the previous run it finds every package slower than `SPLIT_THRESHOLD` seconds and splits it by top-level test into about `time / SPLIT_TARGET` shards, balancing them with longest-test-first. Each of those shards runs `go test -run '^(TestA|TestB)$'` for its share; the script checks that every test listed by `go test -list` lands in exactly one shard. The remaining packages still go whole into `TEST_SHARDS` shards. Nothing is hard-coded, so a renamed or reorganized package is picked up from its timings. Coverage profiles of the shards of one package overlap; `go tool cover` sums duplicated blocks, so `test-report` just concatenates them.

The two knobs live at the top of `ci.yml`: `SPLIT_THRESHOLD` (default `90`, seconds a package may take before it is split) and `SPLIT_TARGET` (default `90`, aimed seconds per shard of a split package). Without timings (first run, empty cache) nothing is split.

The `pr-meta` workflow is separate because it also runs when the pull request description is edited, and that should not re-run the tests. It requires a `release-note` block and checks that `OWNERS` and `.github/CODEOWNERS` list the same people.

A push to `develop` runs the same CI (without `lint` and the pull-request-only jobs) so the merged result is validated and the test timings used by later pull requests stay fresh.

## Integration tests

Unit tests never leave the process. Tests that need a real system, today a Postgres database, live in the `inttest/` module and run against containers they start themselves.

### Where they live

`inttest/` is a nested Go module (`github.com/getsyntegrity/ego/inttest`) with `replace` directives to the root and to `persistence/postgres`, so it always tests the code in the working tree. pgx and Testcontainers appear only in its `go.mod` and in `persistence/postgres/go.mod`, never in the root one.

- `inttest/infra` starts the infrastructure. `StartPostgres` returns a handle, and `NewDatabase(t)` creates an empty database with a unique name on it and drops it when the test ends.
- `inttest/postgres` holds the Postgres event store tests and the store and schema conformance suites.
- `inttest/flows` holds end-to-end flows through the engine. `restart_test.go` runs an engine on a Postgres events store, stops the whole actor system, starts a new one on the same database and checks that the entity comes back with its state.

### Why they never skip

The old Postgres tests called `t.Skip` when `EGO_EXAMPLE_POSTGRES_DSN` was not set, and `go test` reports a skip as a pass, so they looked green in every CI run without running. The `inttest` module removes the cause instead of auditing it afterwards:

- Each test package starts its container from `TestMain`. If Docker or the container is not available, `TestMain` exits non-zero and the run fails with the reason. There is no variable to forget.
- The `unit-gate` job fails on a call to `Skip`, `Skipf` or `SkipNow` on any receiver, and on `testing.Short`, in any Go file under `inttest/`. No allowlist can excuse it.
- A nested module is opt-in by directory, not by build tag. The root `go test ./...` never reaches it, and `cd inttest && go test ./...` always runs everything in it.

### Run them locally

You need Docker and nothing else. No database, no environment variable:

```sh
cd inttest && go test -count=1 ./...
```

The first run pulls the images. Without Docker the run fails; it does not skip.

### Add an infra helper

For a new system (Kafka, NATS or Pulsar are the planned ones), add one file to `inttest/infra`:

1. Write `StartX(ctx) (*X, error)` on top of the Testcontainers module for that system. It takes no `testing.TB`, because `TestMain` has none, and returns an error that says Docker may be the problem.
2. Pin the image to an exact tag, never `latest`, and use the module's readiness wait strategy so the function returns only when the system accepts connections.
3. Give the handle a `Terminate(ctx)` and a per-test isolation method in the style of `NewDatabase` (a database, a topic or a subject with a unique name, removed in `t.Cleanup`).
4. In the test package, start it once in `TestMain`, terminate it after `m.Run`, and have every test call `t.Parallel()` and take its own isolated resource. One container per package, never one per test.
5. Use go-specs and `Eventually` for anything asynchronous. No `time.Sleep`.

### When CI runs them

The `inttest` job runs on every pull request to `main` and on `workflow_dispatch`. On a pull request to `develop` it runs only when the diff touches `inttest/`, `persistence/`, `example/cluster/`, `publisher/`, `compose/` or `engine/`. A separate `paths-filter` step in `plan` decides that, because the filter the tests use removes Markdown files and this one has to match any file. The job has no `services:` section: `ubuntu-latest` already has Docker and the tests start what they need.

## The release note block

Every pull request body contains a fenced block:

````
```release-note
Adds the `WithTimeout` option to the client.
```
````

Write the note as a consumer of the library would want to read it. Write `NONE` when there is no user-visible change. If consumers must act when upgrading, include the words `action required`: the note is then also listed under "Urgent Upgrade Notes". `pr-meta` fails when the block is empty. Pull requests labelled `skip-changelog` or `kind/deps`, and the `develop` to `main` release pull request itself, are exempt.

These notes are the release notes. `.github/scripts/changelog.sh` collects them from every pull request merged since the previous tag, groups them by the pull request's `kind/*` label, adds a dependency section from the `go.mod` diff, and produces the text of the GitHub Release.

## Versions and labels

The next version is computed by `.github/scripts/next-version.sh` from the previous stable tag reachable from `main`:

1. A `release:major`, `release:minor` or `release:patch` label on the pull request wins.
2. Otherwise a `hotfix/*` branch is a patch and a `develop` branch is a minor.
3. Any other source is a patch and prints a warning.

With no tag yet, the first release from `develop` is `v0.1.0`. The module path is `github.com/getsyntegrity/ego` without a `/vN` suffix, so it can only publish `v0.x` and `v1.x`. A `release:major` bump to `v2.0.0` or higher fails in `flow` until `go.mod` declares `/v2` (Go's semantic import versioning). The `kind/*` labels (`kind/feature`, `kind/bug`, `kind/breaking`, `kind/deprecation`, `kind/deps`, `kind/chore`, `kind/docs`, `kind/flake`) decide the CHANGELOG section. `.github/scripts/labels.sh` creates all labels.

## What happens on merge to main

`.github/workflows/release.yml` runs on every push to `main`:

1. It finds the pull request that produced the commit and reads its source branch and labels.
2. It computes the version with `next-version.sh` (or reuses the tag if the commit already has one, which makes re-runs safe).
3. It creates the annotated tag and pushes it.
4. It generates the release notes with `changelog.sh` and publishes the GitHub Release with GoReleaser. The library builds no binaries (`.goreleaser.yaml` has `builds: skip`).
5. The `changelog` job opens, or updates, a `docs/changelog` pull request to `develop` that writes `CHANGELOG/CHANGELOG-X.Y.md`. It cannot push to `develop` directly because of branch protection.
6. For a hotfix, the `sync_develop` job opens a `main` to `develop` pull request.
7. The `notify` job posts to Slack if it is configured.

Releases never run in parallel (`concurrency: release`, without cancellation).

## Other workflows

- `security.yml` runs CodeQL and a strict `govulncheck` on pushes to `develop`, on a nightly schedule and on demand. It warns; it does not block pull requests.
- `go-sdk-update.yml` is manual. Run it from the Actions tab with a Go version (or `latest`). It runs `.github/scripts/go-sdk-update.sh`, which rewrites `.go-version` and the `toolchain` line of every `go.mod`, and opens a pull request to `develop`. Tick `raise_min` only when the minimum Go version for consumers should also move.
- `.github/dependabot.yml` opens weekly Go dependency pull requests (root and every nested module) and monthly GitHub Actions updates, all against `develop` and labelled `kind/deps`.

## Optional secrets and variables

Nothing below is required for the pipeline to work.

- `ORG_CHECKOUT_TOKEN` is a personal access token. Workflows use `secrets.ORG_CHECKOUT_TOKEN || github.token`. Without it, the pipeline still works, but pull requests it creates (changelog, hotfix sync, Go SDK update) are made with the default token, and GitHub does not start workflows for events caused by that token. Those pull requests then need a manual re-run or a push to trigger `ci-ok`. The repository setting "Allow GitHub Actions to create and approve pull requests" must be enabled for them to be opened at all.
- `SLACK_BOT_TOKEN` and the repository variable `SLACK_CHANNEL` (for example `#ego-releases`) enable Slack notifications for releases and the nightly security scan. If either is missing, the notify step is skipped silently.

## Manual setup that the repository still needs

These steps live in GitHub settings, so no commit can do them:

1. Create the `develop` branch from `main` and make it the default branch if you want pull requests to target it by default.
2. Protect `main` and `develop` and require the status checks `ci-ok` and `pr-meta`. Because the release tag is pushed with the workflow token, keep "Restrict who can push" compatible with GitHub Actions.
3. Run `.github/scripts/labels.sh` once (needs an authenticated `gh`) to create the `kind/*`, `release:*`, `skip-changelog` and `needs-triage` labels.
4. Optionally add the secrets and variable described above.

## Local equivalents

The Makefile targets `docker-lint`, `docker-test`, `docker-mock` and `docker-protogen` run inside `Dockerfile.ci` and are meant for contributors who do not have the toolchain installed. To check a change like the CI does, run `go build ./... && go vet ./... && go test ./...` in the root and in each nested module, `go mod tidy` in each, and `golangci-lint run`. The one exception is `inttest`, whose tests need Docker: run `cd inttest && go test -count=1 ./...` there. To preview the next version: `.github/scripts/next-version.sh develop release:minor` (it needs the tags of the repository).
