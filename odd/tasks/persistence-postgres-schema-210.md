# Postgres stores as a module, with versioned schema migrations (#210, spec A of 3)

Issue: https://github.com/getsyntegrity/ego/issues/210.
Branch: `feat/persistence-postgres`, from `origin/develop` at `920d10f`. Target: `develop` through a pull request.

This is the first of three specs that replace the integration gate of PR #278 with a Testcontainers-based `inttest` module.

- **A (this document):** `persistence/postgres` and `persistence.SchemaMigrator`.
- **B (follow-up), `odd/tasks/inttest-testcontainers-210.md`:**
  - the `inttest/` module and its `infra.StartPostgres` helper;
  - the 19 `TestPostgresEventStore_*` tests migrated to a shared container with one database per test;
  - the conformance suites run against `persistence/postgres`;
  - `example/cluster/stores_postgres_test.go` deleted, with its `unitgate` allowlist entry.
- **C (follow-up):**
  - the end-to-end restart flow;
  - a `unitgate` rule that rejects skips under `inttest/`;
  - the `inttest` job in `ci.yml`;
  - the docs.

## Problem

`PostgresEventStore` and `PostgresOffsetStore` live in `example/cluster/stores.go`, which is `package main`. No other module can import them, so a separate integration module cannot test them.

Their schema is also not owned by code. `eventsStoreSchemaDDL` in `stores.go` is never executed at runtime; only the test helper runs it. The deployment source of truth is the hand-synced `init.sql` in `example/cluster/k8s/postgres.yaml`, and `offsets_store` only exists there and in `resources/offsetstore_postgres.sql`. Nothing records which version of the schema a database has, so an upgrade is a manual ALTER recipe from the README.

## What changes

1. **`persistence.SchemaMigrator`**, a new optional interface in the root `persistence` package:
   ```go
   type SchemaMigrator interface {
       Migrate(ctx context.Context) error             // idempotent, safe under concurrent callers
       SchemaVersion(ctx context.Context) (uint, error)
   }
   ```
   No existing interface changes. Adding `Migrate` to `EventsStore` would break every store that implements it, including the in-memory stores of `testkit` and any store written by users. The word "schema" is deliberate: the `migration` package already exists and means data migration (journal/snapshot, tenant adoption).
2. **`conformance.RunSchemaMigratorConformance`** in `persistence/conformance`. It checks four things:
   - running `Migrate` twice is a no-op;
   - N concurrent `Migrate` calls all succeed and leave one consistent schema;
   - `SchemaVersion` reports the latest version;
   - a legacy, pre-versioning schema upgrades correctly.

   The backend supplies the legacy setup through a hook, because only the backend knows what "legacy" looked like.
3. **`persistence/postgres`**, a new nested module with its own `go.mod`, so `pgx` stays out of the root module's dependency graph. `PostgresEventStore` and `PostgresOffsetStore` move there without behavior changes, and `example/cluster` imports them through a `replace` directive, like the publishers.
4. **The Postgres `SchemaMigrator`.** It has three parts:
   - versioned SQL files embedded with `embed.FS` (`001_...sql`, `002_...sql`, ...);
   - a `schema_migrations` table;
   - `pg_advisory_lock` held during `Migrate`, so several cluster nodes can start at once.

   The baseline case needs special handling. A database created by the old ad-hoc DDL has no `schema_migrations` table. `Migrate` detects its current shape and records the matching version instead of re-applying. The runner is a small hand-written loop, not goose or golang-migrate, because the schema has a handful of files and one table of bookkeeping.
5. **`engine.WithSchemaMigration()`**, an opt-in engine option. `Engine.Start` type-asserts every configured store to `persistence.SchemaMigrator` and calls `Migrate` before the engine is marked as started, so no command is accepted on an old schema. Without the option, nothing migrates.

## Why this shape

- **The interfaces stay in the root module.** `persistence` keeps no heavy dependencies; only the backend module pays for `pgx`.
- **Migration happens in `Engine.Start`, not in `NewEngine` or `Connect`.** The engine does not connect stores today; the caller connects them before building the actor system (`example/cluster/main.go:101-113`). `Engine.Start` is the first engine call that takes a context and the last point before `started` becomes true. The rejected alternative was migrating inside `Connect`: every store would migrate implicitly on every connection, which the issue forbids.
- **DB-backed checks of the migrator run in the existing DSN-gated test file for now.** `persistence/postgres` must not depend on infrastructure in its own unit tests. Spec B moves every Postgres-backed test into `inttest` on Testcontainers. Until then, the new migrator checks join `example/cluster/stores_postgres_test.go` and are run locally against a Docker Postgres. They then move with the other 19 in spec B.

## Scope and constraints

- Behavior of both stores must not change. The existing `example/cluster` tests must stay green.
- The root `go.mod` gains no `pgx` and no Testcontainers.
- go-specs v0.3.3 only; no testify, no generated mocks. No race detector, no workbench.
- No direct push to `develop`/`main`.

## Execution

- TDD: strict (source: user CLAUDE.md). Runners: `go test ./persistence/... ./engine/...` at the root, `go test ./...` in `persistence/postgres` and in `example/cluster`. The Postgres-backed checks run locally with `EGO_EXAMPLE_POSTGRES_DSN` pointing at a Docker `postgres:17-alpine`.
- RDD: off (global), so no native review.
- Delivery: one PR to `develop`. The forecast is above 400 authored lines, because the store code moves (about 700 lines, a move, not new logic). The new logic is around 500 lines with tests.

## Tasks

- [x] **A1 Interface and conformance.** `persistence.SchemaMigrator` and `conformance.RunSchemaMigratorConformance`, with a legacy-setup hook. go-specs tests of the suite run against an in-memory fake migrator, plus one deliberately broken fake to prove the suite fails it. Check: `go test ./persistence/...`. Route: delegated writer.
- [x] **A2 Move the stores.** New module `persistence/postgres` containing `PostgresEventStore` and `PostgresOffsetStore`, moved unchanged. `example/cluster` imports it via `replace`. `persistence/postgres` is added to the `modules` matrix in `ci.yml` (`tidy` discovers modules by itself). Check: `go build ./... && go vet ./... && go test ./...` in `example/cluster` and `persistence/postgres`, `go mod tidy -diff` in both, and the root `go.mod` unchanged. Route: delegated writer.
- [x] **A3 Postgres schema migrator.** Embedded versioned SQL, `schema_migrations`, advisory lock, baseline detection. The `SchemaMigratesLegacy*` tests map onto the conformance hook. The schema must also cover `offsets_store`. Check: unit tests of the runner in `persistence/postgres`, then the DSN-gated tests and the conformance against a local Docker Postgres. Route: delegated writer.
- [ ] **A4 Engine option.** `engine.WithSchemaMigration()` migrates every configured store that implements the interface during `Engine.Start`, before `started`, and returns the error. go-specs tests use `mock.Spy`-based fakes and cover these cases: no option, option with a migrator, option with a non-migrator store, and a migrate error. Check: `go test ./engine/...`. Route: delegated writer.
- [ ] **A5 Docs and deploy SQL.** `persistence/postgres` README, and an `example/cluster` README note that `Migrate` replaces the manual ALTER recipe. `k8s/postgres.yaml` keeps working and states that `WithSchemaMigration()` is the preferred path. Check: structural readback, plus `unitgate -strict`. Route: delegated writer.

## Progress and evidence

- **A1** (commit: `1c920ba`, route: delegated writer). RED: `go test ./persistence/conformance` failed to compile (`undefined: SchemaMigratorHarness`, `SchemaT`, `LegacySchema`). GREEN: `go vet ./persistence/...` clean and `go test -count=1 ./persistence/...` ok; 16 passing subtests of the new specs, including 5 flawed fakes (not idempotent, not concurrency-safe, lagging version, non-zero empty version, legacy upgrade fails) that the suite rejects. `unitgate -strict`: ok.
- **A2** (commit: `PENDING_A2`, route: delegated writer). The code moved from `example/cluster/stores.go` into `persistence/postgres/event_store.go` and `offset_store.go`, and the types were renamed for the package: `postgres.EventStore`/`NewEventStore` and `postgres.OffsetStore`/`NewOffsetStore` (behavior unchanged). The 11 argument-validation specs moved with the code into `persistence/postgres/event_store_test.go`; the DB-backed tests stay in `example/cluster/stores_postgres_test.go` (two direct `store.pool` reads became a `countEventRows` helper on its own connection). RED: `go vet` in `persistence/postgres` failed with `undefined: EventStore` before the code moved. GREEN: `go build/vet/test` in `persistence/postgres` and `example/cluster` ok; against Docker `postgres:17-alpine` the 19 Postgres tests of `example/cluster` all pass, 0 skipped; `go mod tidy -diff` clean in root, `persistence/postgres` and `example/cluster`; `persistence/postgres` added to the `ci.yml` matrix, `dependabot.yml` and `docs/ci.md`; `unitgate -strict` ok.
- **A3** (commit: `PENDING_A3`, route: delegated writer). One `SchemaMigrator` (`persistence/postgres/schema_migrator.go`) serves both stores, so `EventStore.Migrate` and `OffsetStore.Migrate` run the same code; five embedded files in `persistence/postgres/schema/` (events_store, its indexes, revisions plus backfill, tenant_metadata, offsets_store); `schema_migrations` table; session-level `pg_advisory_lock` on one pooled connection; each file in its own transaction with its version row. Baseline rule: with no version rows, the version is the longest run of markers from version 1 that exist (`inferSchemaVersion`); an `events_store` without `tenant_id` is refused with `ErrUnsupportedSchema`. RED: `go vet` in `persistence/postgres` failed with `undefined: schemaFile` before the runner existed; GREEN: 53 passing subtests of the pure logic (no DB). DB side, against Docker `postgres:17-alpine`: `TestPostgresSchemaMigratorConformance` passes its 8 checks (empty, latest, idempotent, 8 concurrent migrators, new migrator sees applied schema, 3 legacy shapes), the two `SchemaMigratesLegacy*` tests pass with their original assertions, and all 21 Postgres tests of `example/cluster` pass, 0 skipped. Mutation check: replacing `pg_advisory_lock` by a no-op made `Migrate/ConcurrentCallersAllSucceed` fail (`duplicate key value violates unique constraint "pg_type_typname_nsp_index"`); restored. `go mod tidy -diff` clean, `unitgate -strict` ok.

## Next step

A4.
