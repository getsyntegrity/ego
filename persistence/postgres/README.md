# persistence/postgres

PostgreSQL implementations of eGo's `persistence.EventsStore` and `offsetstore.OffsetStore`, plus the schema
they need and a way to keep that schema up to date.

This is a separate Go module (`github.com/getsyntegrity/ego/persistence/postgres`). The root `ego` module does
not depend on `pgx`; only a program that imports this package does.

```go
import "github.com/getsyntegrity/ego/persistence/postgres"

eventStore := postgres.NewEventStore(dsn)
if err := eventStore.Connect(ctx); err != nil { /* ... */ }
defer eventStore.Disconnect(ctx)

offsetStore := postgres.NewOffsetStore(dsn)
if err := offsetStore.Connect(ctx); err != nil { /* ... */ }
defer offsetStore.Disconnect(ctx)
```

Connecting does not touch the schema. Both stores expect the tables described below to exist.

## Keeping the schema up to date

Both stores implement `persistence.SchemaMigrator` (`Migrate` and `SchemaVersion`). The simplest way to use
it is the engine option, which migrates every configured store that has it, when the engine starts:

```go
cfg := engine.NewConfig(eventStore,
    engine.WithOffsetStore(offsetStore),
    engine.WithSchemaMigration(),
)
// ... build the actor system with cfg.GoaktOptions(), then:
if err := eng.Start(ctx); err != nil { /* a failed migration stops here */ }
```

`Engine.Start` runs `Migrate` before the engine accepts a command. The stores must already be connected, and
without `WithSchemaMigration()` nothing migrates: the schema stays your responsibility, as before. You can
also call `eventStore.Migrate(ctx)` yourself, for example from a deploy job.

Both stores run the same migrator over the whole schema, so a database they share is migrated once, by
whichever runs first. If you prefer to manage the pool yourself, `postgres.NewSchemaMigrator(pool)` gives you
the same thing.

### How it works

The schema is a list of numbered SQL files embedded in the module, in [`schema/`](./schema). `Migrate`:

1. takes a Postgres advisory lock (`pg_advisory_lock`), so several nodes that start together queue up
   instead of racing, and the ones that arrive late find nothing left to do;
2. creates the `ego_schema_migrations` table if it is missing, one row per version already applied;
3. applies each file above the recorded version, in order. Every file runs in its own transaction together
   with the row that records it, so a failure leaves the database at the last complete version, and the next
   `Migrate` resumes from there.

`SchemaVersion` returns the highest recorded version, or `0` for a database that was never migrated.

### Versions

| Version | File | Adds |
| --- | --- | --- |
| 1 | `001_events_store.sql` | `events_store`, keyed by `(tenant_id, persistence_id, sequence_number)` |
| 2 | `002_events_store_indexes.sql` | the four indexes of `events_store` |
| 3 | `003_events_store_revisions.sql` | `events_store_revisions`, with a backfill from `events_store` |
| 4 | `004_events_store_tenant_metadata.sql` | the nullable `tenant_metadata` JSONB column |
| 5 | `005_offsets_store.sql` | `offsets_store` and its indexes |

### A database created by hand

A database created from the old `init.sql` or from the earlier DDL has the tables but no
`ego_schema_migrations`. `Migrate` does not apply the files again: it inspects the tables, columns and indexes,
works out the longest run of versions from 1 that are already there, records them, and applies only the
rest. Because every file is idempotent, a version it cannot prove (an index that was never created, say) is
simply applied again.

One shape is refused with `postgres.ErrUnsupportedSchema`: an `events_store` with no `tenant_id` column, from
before the scoped stores. Its primary key has to change, which can rewrite a large table, so that step stays
manual. The `ALTER` recipe is in the [cluster example README](../../example/cluster/README.md#tenant-column-tenant_id).

### Adding a schema file

1. Add `schema/NNN_what_it_does.sql`, where `NNN` is the next number. Versions run `1, 2, 3` with no gaps,
   and the loader rejects a gap, a duplicate, an empty file or a name without a number.
2. Write it so it can run twice (`IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`, an upsert for a backfill).
3. Never edit a file that has been released. A change to the schema is a new file.
4. Add the object the file creates to `baselineMarkers` in `schema.go`, so a database created by hand can be
   recognised at that version. A test fails if the two lists have different lengths.
5. Raise the latest version in the tests that name it (`postgresLatestSchemaVersion` in
   `example/cluster/stores_postgres_test.go`) and run the Postgres-backed tests.

## Testing

The pure logic of the runner (reading and ordering the files, choosing the pending ones, inferring the
version of a database created by hand) is covered by unit tests that need no database:

```sh
go test ./...
```

What the SQL does to a real Postgres is checked with `conformance.RunSchemaMigratorConformance` and the store
conformance suites, which run from `example/cluster` when `EGO_EXAMPLE_POSTGRES_DSN` is set:

```sh
docker run -d --rm --name ego-pg -e POSTGRES_PASSWORD=pg -p 55432:5432 postgres:17-alpine
export EGO_EXAMPLE_POSTGRES_DSN="postgres://postgres:pg@localhost:55432/postgres?sslmode=disable"
(cd ../../example/cluster && go test -count=1 ./...)
```
