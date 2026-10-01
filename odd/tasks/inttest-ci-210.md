# `inttest` in CI: a restart flow, a no-skip rule and the job (#210, spec C of 3)

Issue: https://github.com/getsyntegrity/ego/issues/210. Previous specs:
- spec A, `odd/tasks/persistence-postgres-schema-210.md` (PR #279);
- spec B, `odd/tasks/inttest-testcontainers-210.md` (PR #280).

Branch: `ci/inttest-job`, from `test/inttest-testcontainers` (spec B). The PR targets `develop` and carries the commits of A and B until they are merged. This spec closes the chain.

## Problem

After spec B, the integration tests live in `inttest/` and start their own Postgres with Testcontainers. Three gaps remain:

- No test proves a whole flow through the engine against a real store; the current tests exercise the store alone.
- Nothing stops a future change from adding a `t.Skip` back under `inttest/`, which would bring the original problem back.
- CI never runs `inttest`.

## What changes

1. **`inttest/flows`, one end-to-end restart flow.** It builds an engine whose event store is `persistence/postgres`, sends commands to an entity, stops and restarts the actor system, and asserts that the entity state is recovered from Postgres. It uses one container per package (from `TestMain`), a database per test and `t.Parallel()`. Async assertions use go-specs `Eventually`. The flow stays small and deterministic.
2. **A no-skip rule in the existing `unitgate`**, not a new tool. It fails on `t.Skip`, `t.Skipf`, `t.SkipNow` and `testing.Short` in any file under `inttest/`, and a `unitgate` test covers it.
3. **The `inttest` job in `.github/workflows/ci.yml`.**
   - It uses `./.github/actions/go-setup`, `ubuntu-latest` (Docker is already available) and a `timeout-minutes`.
   - It runs `cd inttest && go test -count=1 -timeout=15m ./...`.
   - It has no `services:` and no DSN env var.
   - It runs on every PR to `main` and on `workflow_dispatch`. It runs on PRs to `develop` only when one of these paths changes: `inttest/**`, `persistence/**`, `example/cluster/**`, `publisher/**`, `compose/**`, `engine/**`. That uses a separate `dorny/paths-filter` step in `plan`, because the existing one uses `predicate-quantifier: every`.
   - It is listed in `ci-ok`'s `needs`; `ci-ok` already accepts `skipped`.
   - `modules` builds and vets `inttest` and builds, vets and tests `persistence/postgres`. It must not test `inttest`, so the unit lanes never run it. `tidy` already discovers every `go.mod`.
4. **Docs.**
   - In `docs/ci.md`: the `inttest` row of the job table, and an "Integration tests" section covering where they live, how to run them locally with only Docker, and how to add an infra helper.
   - A link to it from `docs/testing/go-specs.md`.

## Why this shape

- **One job inside `ci.yml`, not a separate `integration.yml`.** `ci-ok` is the only required check, and a job in the same workflow reaches it through `needs` without touching branch protection. A separate workflow would need its own required check or a cross-workflow wait. There is no concrete reason for either.
- **The no-skip rule lives in `unitgate`.** The gate already scans every test file statically and is cheap enough to run on every PR. A new tool would duplicate its scanner.

## Scope and constraints

- Out of scope: the Kafka, NATS and Pulsar helpers and their flows. `inttest/infra` already has room for them.
- go-specs v0.3.3 only; no testify, no generated mocks, no sleeps. No race detector, no workbench.
- No direct push to `develop`/`main`.

## Execution

- TDD: strict (source: user CLAUDE.md). Runners:
  - `cd inttest && go test -count=1 ./...` (Docker);
  - `go test ./.github/scripts/unitgate`.
- RDD: off (global).

## Tasks

- [x] **C1 Restart flow.** `inttest/flows`: engine on `persistence/postgres`, commands, actor-system restart, and an assertion that the state is recovered. Check: `cd inttest && go test -count=1 ./flows/...`. Route: delegated writer.
- [ ] **C2 No-skip rule.** Add the rule to `unitgate` and cover it with a test. Check: `go test ./.github/scripts/unitgate` and `go run ./.github/scripts/unitgate -strict`. Route: delegated writer.
- [ ] **C3 CI job.** Add the `inttest` job, the paths filter, the `ci-ok` needs entry, and the `modules` coverage for both new modules. Check: `actionlint`, plus a green CI run of the PR in which `inttest` really ran. Route: delegated writer, then a parent check of the CI run.
- [ ] **C4 Docs.** Update `docs/ci.md` and `docs/testing/go-specs.md`. Check: structural readback. Route: delegated writer.

## Progress and evidence

### C1 (route: delegated writer)

- Flow: `inttest/flows/restart_test.go` (spec) and `node_test.go` (helper). A `node` is a Postgres `EventStore`, a goakt actor system with a unique name and no remoting, and an engine built with `engine.WithSchemaMigration()`, so `Start` creates the schema and the opt-in option is proven end to end. The entity is the `enginetest` account. The first node creates it with 500, credits 250 and gets revision 2. It is stopped (engine, system, store). A second node on the same database spawns the same account; a `TestNoEvent` command, which produces no event, returns the recovered state, read inside `Eventually`: balance 750, revision 2. A further credit of 50 gives 800 at revision 3.
- RED: with only `restart_test.go` written, `go vet ./flows/` gave `undefined: startNode`. GREEN: `go test -count=1 ./flows/` gave `ok ... 2.383s`, one test passed, none skipped.
- `go vet ./...`, `gofmt -l inttest` and `go mod tidy -diff` are clean in `inttest`.

## Next step

C1.
