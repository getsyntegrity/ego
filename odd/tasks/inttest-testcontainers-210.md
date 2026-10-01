# `inttest` module on Testcontainers (#210, spec B of 3)

Issue: https://github.com/getsyntegrity/ego/issues/210. Previous spec: `odd/tasks/persistence-postgres-schema-210.md` (spec A, PR #279).
Branch: `test/inttest-testcontainers`, from `feat/persistence-postgres` (spec A). The PR targets `develop` and carries the spec A commits until #279 is merged; CI only runs on PRs to `develop` and `main`, so a PR stacked on the spec A branch would get no CI.
Next spec, C (follow-up):
- an end-to-end restart flow in `inttest/flows`;
- a `unitgate` rule that rejects `t.Skip`/`testing.Short` under `inttest/`;
- the `inttest` job in `ci.yml`, with `modules`/`tidy` coverage of the new modules;
- the docs.

## Problem

The 19 `TestPostgresEventStore_*` tests in `example/cluster/stores_postgres_test.go` depend on infrastructure given from outside, through `EGO_EXAMPLE_POSTGRES_DSN`. When the variable is missing they call `t.Skip`, and `go test` reports the skip as "ok". So they skip in every CI run and still look green. PR #278 tried to audit that after the fact with a manifest and a gate; that design is rejected.

The fix removes the cause instead: each integration test package starts its own Postgres with Testcontainers. If Docker or the container is not available, the test fails. Nothing skips, so nothing needs auditing.

## What changes

1. **The `inttest/` module**, with its own `go.mod` (`github.com/getsyntegrity/ego/inttest`). It has `replace` directives to `../` and `../persistence/postgres`. pgx and Testcontainers live only in `inttest/go.mod` and `persistence/postgres/go.mod`, never in the root.
2. **`inttest/infra`** helpers.
   - `StartPostgres(ctx, tb)` is built on `github.com/testcontainers/testcontainers-go/modules/postgres`. It uses the module's readiness wait strategy and an exact image tag (a `postgres:17.x-alpine` tag, never `latest`). It returns a handle that can create a fresh database per test.
   - It fails with a clear message when the container cannot start.
   - The package is laid out so `StartKafka`, `StartNATS` and `StartPulsar` can be added later.
3. **`inttest/postgres`**: the event store tests.
   - One container per package, started in `TestMain` and terminated at the end.
   - Each test creates its own database on that container, so every test calls `t.Parallel()`.
   - It holds the 19 migrated tests with their names and assertions. It also holds `conformance.RunEventsStoreConformance` and `conformance.RunSchemaMigratorConformance` (with its legacy cases), run against `persistence/postgres`.
4. **`example/cluster/stores_postgres_test.go` is deleted**, together with its entry in `.github/unit-test-gate-resources.txt`.
5. **`unitgate` treats `inttest/` as outside the unit lane.** Real resources are the point of that module, so the gate's real-resource rule does not apply there. Spec C adds the opposite rule, no skips under `inttest/`.

## Why this shape

- **One container per package, one database per test.** A container per test multiplies startup time by 20. A shared database forces serial tests and invites cross-test leaks. `CREATE DATABASE` on a shared container is cheap and gives full isolation, so `t.Parallel()` is safe.
- **A nested module, not build tags.** Build tags hide files from the compiler, which is the same "silently not running" failure in another form. A module is opt-in by directory: the root `go test ./...` never reaches it, and `cd inttest && go test ./...` always runs everything in it.
- **Testcontainers version.** It follows the latest stable `testcontainers-go`, aligned with `publisher/pulsar/go.mod`, which already has it indirectly at v0.44.0.

## Scope and constraints

- No `t.Skip`, no `testing.Short()`, no `os.Getenv` for infrastructure. No `time.Sleep` or fixed waits; event-driven waits use go-specs `Eventually`.
- go-specs v0.3.3 only, with no testify and no generated mocks. If go-specs cannot express something (for example `t.Parallel()` inside `Describe`), open an issue in `getsyntegrity/go-specs` and work around it.
- The root `go.mod` gains no pgx and no Testcontainers. No race detector, no workbench.
- No direct push to `develop`/`main`.

## Execution

- TDD: strict (source: user CLAUDE.md). The runner is `cd inttest && go test -count=1 ./...`, which needs Docker.
- RDD: off (global).

## Tasks

- [x] **B1 Module, infra and gate scope.** `inttest/go.mod`, `inttest/infra` (`StartPostgres`, per-test database), a smoke spec proving that a database is created and reachable, and `unitgate` excluding `inttest/` from the real-resource rule, with a `unitgate` test. Check: `cd inttest && go test ./infra/...`, `go test ./.github/scripts/unitgate`, `go run ./.github/scripts/unitgate -strict`. Route: delegated writer.
- [ ] **B2 Migrate the 19 tests.** Move them into `inttest/postgres`, with `TestMain` owning the container and each test getting its own database and calling `t.Parallel()`. Names and assertions are unchanged. Delete `example/cluster/stores_postgres_test.go` and its allowlist entry. Check: `cd inttest && go test -count=1 ./postgres/...` shows all 19 passed, and `example/cluster` `go test ./...` stays green. Route: delegated writer.
- [ ] **B3 Conformance suites.** In `inttest/postgres`, run `RunEventsStoreConformance` and `RunSchemaMigratorConformance` against `persistence/postgres`, including the legacy cases moved from spec A. Check: same command, with conformance subtests passing. Route: delegated writer.
- [ ] **B4 Evidence.** Record these here:
  - wall-clock time of `cd inttest && go test -count=1 ./...`;
  - the run with Docker stopped, which must FAIL, with its error output;
  - that the root `go test ./...` compiles nothing from `inttest`;
  - `go mod tidy -diff` clean in every touched module;
  - that the root `go.mod` has no pgx or Testcontainers.

  Route: delegated writer.

## Progress and evidence

### B1 (route: delegated writer)

- Versions: `testcontainers-go` and `modules/postgres` v0.44.0 (latest stable; `go list -m -versions` ends at v0.44.0, so `publisher/pulsar` already matches). Image `postgres:17.6-alpine`, pulled to check it exists.
- API of `inttest/infra`: `StartPostgres(ctx) (*Postgres, error)`, `(*Postgres).NewDatabase(tb) (dsn string)` (unique database, dropped with `WITH (FORCE)` in `t.Cleanup`), `(*Postgres).Terminate(ctx) error`. It takes no `testing.TB` because `TestMain` has none.
- RED: with only `infra/postgres_test.go` written, `go test ./infra/` gave `no non-test Go files ... [build failed]`. GREEN: `ok github.com/getsyntegrity/ego/inttest/infra 5.597s`, both specs pass.
- RED for the gate: the new `inttest` spec in `resources_test.go` failed with `expected [... calls net.Dial ... reads the DSN variable ...] to be empty`. GREEN after `insideInttest` in `resources.go`; `go test ./.github/scripts/unitgate` ok and `go run ./.github/scripts/unitgate -strict` prints `unit-test gate: ok (0 pending entries, 41 resource entries)`.
- go-specs: `t.Parallel()` before `specs.Describe` works, so no issue was needed.

## Next step

B2.
