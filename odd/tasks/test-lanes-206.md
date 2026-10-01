# Cluster and race test lanes in CI (#206, spec 2 of 2)

Issue: https://github.com/getsyntegrity/ego/issues/206.
Previous spec: `odd/tasks/cluster-tests-206.md` (PR #286). It names every multi-node test `TestCluster*` and guards that convention with the `cluster-name` rule in `unitgate`.
Branch: `ci/test-lanes-206`, from `test/cluster-tests-206`. The PR targets `develop` and carries the spec 1 commits until #286 is merged.

## Problem

After spec 1, the multi-node tests can be selected by name, but CI still runs them inside the normal test shards. Nothing runs the in-process suites under `-race` either. Two acceptance criteria of #206 remain open:
- a separate lane for the cluster tests;
- the in-process suites under `-race` in CI, with per-lane parity evidence.

## What changes

1. **The normal shards skip the cluster tests.** Each shard's `go test`/`gotestsum` command gets `-skip '^TestCluster'`. `.github/scripts/test-matrix.sh` builds `-run '^(TestA|TestB)$'` patterns for packages it splits, so the skip must compose with them: a split shard still skips the cluster tests, and every non-cluster test stays in exactly one shard.
2. **A `cluster` job** runs `go test -count=1 -run '^TestCluster'` on the packages that contain cluster tests (`engine`, `compose/goakt`). `go test` reports `no tests to run` as a pass, so the job counts the `TestCluster*` pass events in the `-json` output and fails if there are none.
3. **A `race` job** runs `go test -race -count=1 -skip '^TestCluster'` on `engine/...`, `internal/engine/...`, `internal/projectionrunner`, `compose/goakt/...`, `internal/extensions` and `migration`. The cluster tests are not run under the race detector for now.
4. **Triggers and gate.** Both jobs use the `inttest` triggers: push to `develop`, the `develop` to `main` release pull request and `workflow_dispatch`. Both are listed in `ci-ok`, which already accepts `skipped`. Both have timeouts and need no `services:` and no Docker.
5. **CI evidence.** `workflow_dispatch` only exists for workflows on the default branch, and `ci.yml` is not on `main` yet. So the evidence comes from a temporary commit that only adds `pull_request` to the `if:` of `cluster` and `race` (message `ci(tmp): run race and cluster on this PR for evidence (#206), reverted next`), followed by its revert in a separate commit. This document records the run link and both SHAs, and the PR's final diff carries no trace of the temporary change. If the `race` run finds races, the work stops there: they are reported in a separate issue and production code is not fixed in this PR.
6. **Parity per lane.** For each package, every test runs in exactly one lane, normal or cluster, and the count still matches the spec 1 baseline of 1402 tests.
7. **Docs.**
   - `docs/ci.md` gets a lane table (unit and component shards, `cluster`, `race`, `inttest`) that says what runs where and when, and states the `TestCluster` naming rule.
   - `docs/testing/go-specs.md` explains how to write a new cluster test.
   - #206 gets a comment summarizing "lanes, not modules; names, not tags", with the PR link.

## Why this shape

The decisions from spec 1 stay: no new `go.mod`, no build tags, and selection by name. Both jobs live inside `ci.yml` so they reach the single required check, `ci-ok`, through `needs`, as `inttest` and `benchmark` already do.

## Scope and constraints

- Out of scope:
  - moving tests to `inttest` or to another module;
  - build tags;
  - rolling out `t.Parallel()`;
  - fixing any race the job finds;
  - running the cluster tests under `-race`.
- `-race` is never run locally (user rule). It only runs in CI, which the task requested explicitly. No workbench.
- No direct push to `develop` or `main`.

## Execution

- TDD: strict (source: user CLAUDE.md). It applies to any script logic, such as the zero-count guard and the `test-matrix.sh` composition, which are checked against crafted input. The runners are the existing test suites, `go test -json` for the counts, and `actionlint`.
- RDD: off (global).

## Tasks

- [x] **T1 Shards skip the cluster tests.** Add `-skip '^TestCluster'` to the shard command and make it compose with `test-matrix.sh`. Check: local proof that a split-package shard pattern plus the skip excludes the `TestCluster*` tests and keeps every other test in exactly one shard; `actionlint`. Route: delegated writer.
- [x] **T2 `cluster` and `race` jobs.** Add both jobs with the zero-count guard, the triggers, timeouts and the `ci-ok` entry. Check: `actionlint`; the guard script tested locally on a zero-test and a non-zero JSON input. Route: delegated writer.
- [x] **T3 Parity per lane.** For each package, run normal (`-skip`) and cluster (`-run`) locally without `-race`, show that the counts add up to 1402, and record it here. Route: delegated writer.
- [x] **T4 Docs.** Update `docs/ci.md` and `docs/testing/go-specs.md`. Check: structural readback. Route: delegated writer.
- [x] **T5 CI evidence and #206.** Push the temporary commit, record the `race` and `cluster` run links, revert, check that the final diff is clean, and comment on #206. Route: inline (parent).

## Progress and evidence

- **T1 done** (commit `ffa8df1`). Route: delegated writer. `ci.yml` gets one workflow-level variable, `CLUSTER_TESTS: "^TestCluster"`, used by every lane so the regex lives in one place. The shard step adds `-skip "$CLUSTER_TESTS"` after the optional `-run "$RUN"`: for a split package Go applies both, `-run` selects the shard's share and `-skip` removes the cluster tests from it. `test-matrix.sh` reads the same variable (default `^TestCluster`) and drops matching names from the `go test -list` result before it builds the bins, so a split package never distributes a cluster test and the rule "every listed test lands in exactly one shard" covers exactly the tests the shards run. An empty `CLUSTER_TESTS` turns the filter off. If a package had only cluster tests, the filtered list is empty and the package stays whole, where `-skip` leaves nothing to run.
  - Proof (a one-off local harness that feeds `.github/scripts/test-matrix.sh` a crafted `.test-timings` file): a crafted timing file makes `engine` a 400 s package (cluster tests 20 s each) and the real script splits it into 5 shards out of 7. Default run: 205 tests listed, 8 cluster, 197 non-cluster; no cluster name appears in any shard's `-run` pattern; all 197 non-cluster tests are in exactly one shard; the shards execute 0 cluster tests. Control with `CLUSTER_TESTS=""`: all 8 cluster tests are distributed (the filter is what removes them).
  - `-run` plus `-skip` really compose: `go test -run '^(TestEngineClusterKindsExposesEgoActors|TestClusterEngineNeutralBehaviors)$' -skip '^TestCluster' ./engine` ran only the first (`--- PASS: TestEngineClusterKindsExposesEgoActors`). `actionlint ci.yml`: clean.
  - Decision on `test (min)`: it still runs everything, cluster tests included. It is the only job that runs them with the minimum Go version, and the only full run of a `hotfix/*` PR to `main`, where the cluster job does not run. Its cost is unchanged. A comment in `ci.yml` says so. `test-report` timings now come from shards that no longer run the cluster tests (about 58 s of `engine`), so the next plan splits on the non-cluster time.
- **T2 done** (commit `8642aa6`). Route: delegated writer. Jobs `cluster` and `race` added before `unit-gate`, both with the `inttest` condition verbatim, `needs: plan`, no services, and both listed in `ci-ok`; header comments updated. `cluster` runs `go test -count=1 -timeout=15m -run "$CLUSTER_TESTS" -json ./engine/ ./compose/goakt/...` into `$RUNNER_TEMP/cluster.json`, keeps the exit status, counts the distinct top-level passing `TestCluster*` tests with `jq`, writes a step summary, fails on a non-zero status or on zero tests, and uploads `cluster-results`. `race` runs `go test -race -count=1 -timeout=25m -skip "$CLUSTER_TESTS"` on the 11 in-scope packages (`go list` of the six patterns returns the same 11 packages as the spec 1 inventory); its comment says the cluster tests are out of scope under `-race`.
  - Guard proof (a one-off local harness that extracts the real `run:` block of the `cluster` job from `ci.yml`, replaces only its `go test` line with a copy of a fixture JSON, and runs it with `bash -eo pipefail`): a JSON with no top-level `TestCluster*` pass (only a subtest, a non-cluster test, a package event and a non-JSON line) printed `0 top-level ... passed` and `::error::the cluster lane ran no top-level ^TestCluster test`, exit 1. The real `go test -json -run '^TestCluster'` output printed `9 top-level ... passed` and exit 0. The same real output with `go test` status 1 printed `::error::go test failed in the cluster lane (exit status 1)`, exit 1. `actionlint ci.yml`: clean.
- **T3 done.** Route: delegated writer. Per lane, no `-race`, `go test -count=1 -json` over the 11 packages, counted with `.github/scripts/count-tests.sh` (versioned in #286). The set comparison checked that the sorted test names of the two lanes are disjoint and that their union equals the unfiltered run. All three runs exit 0 and no test failed or skipped; no flake appeared, so no re-run was needed.

  | package | normal (`-skip`) | cluster (`-run`) | sum | baseline |
  |---|---|---|---|---|
  | compose/goakt | 24 + 39 = 63 | 1 + 4 = 5 | 68 | 68 |
  | engine | 197 + 407 = 604 | 8 + 27 = 35 | 639 | 639 |
  | internal/engine/durablestate | 53 | 0 | 53 | 53 |
  | internal/engine/enginetest | 26 | 0 | 26 | 26 |
  | internal/engine/eventsource | 197 | 0 | 197 | 197 |
  | internal/engine/projection | 13 | 0 | 13 | 13 |
  | internal/engine/protocol | 36 | 0 | 36 | 36 |
  | internal/engine/saga | 92 | 0 | 92 | 92 |
  | internal/extensions | 37 | 0 | 37 | 37 |
  | internal/projectionrunner | 84 | 0 | 84 | 84 |
  | migration | 157 | 0 | 157 | 157 |
  | **sum** | 395 + 967 = 1362 | 9 + 31 = 40 | **1402** | **1402** |

  The name sets are disjoint, and their union equals the set of a third, unfiltered run (404 top-level + 998 subtests = 1402), so every test runs in exactly one lane. The normal lane has no `TestCluster*` name; every top-level name in the cluster lane starts with `TestCluster`.
  - Verification of record: `go test -count=1 -skip '^TestCluster' ./...` at the root is green (every package ok); `go test -count=1 -run '^TestCluster' ./engine/ ./compose/goakt/...` is green with 9 top-level tests PASS; `go run ./.github/scripts/unitgate -strict`: ok (0 pending entries, 40 resource entries).

- **T4 done.** Route: delegated writer. `docs/ci.md`: job table rows for `plan`/`test`, `test (min)`, and new rows for `cluster` and `race`; the sentence "the race detector is not used anywhere" is corrected; "Slow packages" explains how `-run` and `-skip` compose; a new "Test lanes" section holds the lane table (what runs, and when: feature PRs, push to `develop`, release PR, hotfix PR, dispatch), the `TestCluster` naming rule with the `cluster-name` gate rule and its blind spots, the reason for the lane, why `test (min)` does not skip, who fixes a red post-merge lane (the `inttest` rule), and the `-race` gap. `docs/testing/go-specs.md`: a "Writing a cluster test" section. Check (structural readback): every internal link resolves to an existing heading (`ci.md#test-lanes`, `testing/go-specs.md#writing-a-cluster-test`), the tables render with a constant column count, and the facts match `ci.yml` (conditions, packages, timeouts).

## How to rerun the per-lane counts

From the repository root, without `-race`:

```sh
P="./engine ./internal/engine/... ./internal/projectionrunner ./compose/goakt/... ./internal/extensions ./migration"
.github/scripts/count-tests.sh -skip '^TestCluster' $P                           # normal:  top=395 sub=967 total=1362
.github/scripts/count-tests.sh -run '^TestCluster' ./engine ./compose/goakt/...  # cluster: top=9 sub=31 total=40
.github/scripts/count-tests.sh $P                                                # all:     top=404 sub=998 total=1402
```

## CI evidence (T5)

`workflow_dispatch` is only offered for workflows on the default branch, and `ci.yml` is not on `main` yet. So, as agreed in review, the evidence comes from a temporary commit:

- **Temporary commit** `6496b46`, `ci(tmp): run race and cluster on this PR for evidence (#206), reverted next`. It only added `github.event_name == 'pull_request' ||` to the `if:` of `cluster` and `race`.
- **Run** https://github.com/getsyntegrity/ego/actions/runs/36941277783 at `6496b46`, green.
  - `cluster`: `9 top-level ^TestCluster tests passed (go test exit status 0)`.
  - `race`: all 11 packages report `ok`, with **0** `WARNING: DATA RACE`.

    | Package | Time |
    |---|---|
    | `engine` | 7.8s |
    | `internal/engine/durablestate` | 1.3s |
    | `internal/engine/enginetest` | 1.0s |
    | `internal/engine/eventsource` | 22.9s |
    | `internal/engine/projection` | 6.1s |
    | `internal/engine/protocol` | 1.0s |
    | `internal/engine/saga` | 13.1s |
    | `internal/projectionrunner` | 2.2s |
    | `compose/goakt` | 1.1s |
    | `internal/extensions` | 1.1s |
    | `migration` | 1.1s |

  - `ci-ok`: green.
- **Revert** `7db17b6`, in its own commit. The diff of `.github/workflows/ci.yml` between `6496b46~1` and the branch head is empty, so `ci.yml` carries no trace of the temporary change. The squash merge also removes it from the history of `develop`.

No race was found, so no separate issue was needed.

## Next step

Review and merge #286, then this PR.