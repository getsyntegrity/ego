# Migrating from ego to Urd

## Why

The project was published as `github.com/getsyntegrity/ego`. It is now called
**Urd — event sourcing for Go** and lives at `github.com/getsyntegrity/urd`.
The code behaves the same. What changed is the name in Go module paths, in a
few labels, and in free-text messages.

The rename had one hard rule: it must not break data that `ego` already wrote,
and it must not break a cluster where old and new nodes run side by side. So a
name that is stored or sent over the wire keeps its `ego` spelling. The list is
in [Intentionally not renamed](#intentionally-not-renamed).

## New module path

Every module changes its prefix from `github.com/getsyntegrity/ego` to
`github.com/getsyntegrity/urd`. The path suffix is unchanged.

| Old module path | New module path |
|-----------------|-----------------|
| `github.com/getsyntegrity/ego` | `github.com/getsyntegrity/urd` |
| `github.com/getsyntegrity/ego/persistence/postgres` | `github.com/getsyntegrity/urd/persistence/postgres` |
| `github.com/getsyntegrity/ego/publisher/kafka` | `github.com/getsyntegrity/urd/publisher/kafka` |
| `github.com/getsyntegrity/ego/publisher/nats` | `github.com/getsyntegrity/urd/publisher/nats` |
| `github.com/getsyntegrity/ego/publisher/pulsar` | `github.com/getsyntegrity/urd/publisher/pulsar` |
| `github.com/getsyntegrity/ego/publisher/websocket` | `github.com/getsyntegrity/urd/publisher/websocket` |
| `github.com/getsyntegrity/ego/example` | `github.com/getsyntegrity/urd/example` |
| `github.com/getsyntegrity/ego/benchmark` | `github.com/getsyntegrity/urd/benchmark` |
| `github.com/getsyntegrity/ego/inttest` | `github.com/getsyntegrity/urd/inttest` |
| `github.com/getsyntegrity/ego/test/compat` | `github.com/getsyntegrity/urd/test/compat` |

The last four are repository-internal modules (examples, benchmarks and tests);
applications normally depend on the first six only.

## No root package, same imports

The repository root holds no Go files, so there is no `package urd`. The public
API is still the package `engine`, now at `github.com/getsyntegrity/urd/engine`.
Code that already imported `github.com/getsyntegrity/ego/engine` only needs the
new prefix: `engine.NewEngine`, `engine.Config` and the rest keep their names.
The generated protobuf package keeps its directory name, `egopb`, so the import
is `github.com/getsyntegrity/urd/egopb`.

## How to migrate

Rewrite the old prefix in your Go files and `go.mod`, then tidy:

```sh
grep -rl 'github.com/getsyntegrity/ego' --include='*.go' --include='go.mod' . \
  | xargs sed -i 's#github.com/getsyntegrity/ego#github.com/getsyntegrity/urd#g' && go mod tidy
```

On macOS, use `sed -i ''` instead of `sed -i`. If you vendor dependencies, run
`go mod vendor` afterwards. Check `go.sum` and any `replace` directive by hand.

## Changed user-visible values

- **Publisher IDs.** `ID()` of the built-in publishers is now `urd-kafka`,
  `urd-nats`, `urd-pulsar` and `urd-websocket` (was `ego-*`). If you route or
  filter on these strings, update the match.
- **Kafka client ID.** The Sarama `ClientID` set in `publisher/kafka/config.go`
  is now `urd-kafka-publisher`. Broker-side quotas or ACLs keyed on the old
  client ID need the new value.
- **Error message prefix.** Free-text errors start with `urd:` instead of `ego:`
  or `eGo:`. This affects the sentinels in `port/runtime/errors.go`, for example
  `ErrSpawnTenantUndetermined`, `ErrNotACommand`, `ErrEntityFamilyNotDeclared`
  and `ErrUnsupported`. Compare with `errors.Is`, not with the message text.
  Messages that name options now say `engine.WithTenant`, `engine.WithProjection`
  and `engine.WithEntityFamilies`.
- **Environment variable.** `URD_TELEMETRY_CONTRACT_DUMP` replaces
  `EGO_TELEMETRY_CONTRACT_DUMP`. The old name is still read as a fallback, and is
  deprecated; it will be removed in a later release. It is a test-only switch.

- **OpenTelemetry names.** Metric, span and attribute names, and the tracer and
  meter scope names, now use `urd` instead of `ego`. Dashboards, alerts and
  trace queries that use the old names must be updated; there is no alias, so
  both spellings never appear together. The Prometheus form replaces dots with
  underscores (`ego_commands_total` is now `urd_commands_total`).

  | Kind | Old name | New name |
  | --- | --- | --- |
  | Metric | `ego.commands.total` | `urd.commands.total` |
  | Metric | `ego.commands.duration` | `urd.commands.duration` |
  | Metric | `ego.events.persisted.total` | `urd.events.persisted.total` |
  | Metric | `ego.projection.events.processed.total` | `urd.projection.events.processed.total` |
  | Metric | `ego.entities.active` | `urd.entities.active` |
  | Metric | `ego.projections.active` | `urd.projections.active` |
  | Metric | `ego.projection.lag_ms` | `urd.projection.lag_ms` |
  | Metric | `ego.projection.latest_offset` | `urd.projection.latest_offset` |
  | Metric | `ego.projection.events_behind` | `urd.projection.events_behind` |
  | Span | `ego.command` | `urd.command` |
  | Span | `ego.send_command` | `urd.send_command` |
  | Attribute | `ego.persistence_id` | `urd.persistence_id` |
  | Attribute | `ego.command_type` | `urd.command_type` |
  | Attribute | `ego.entity_id` | `urd.entity_id` |

## Intentionally not renamed

These names keep the `ego` spelling. Each one is stored or exchanged outside
the Go source, so renaming it would break existing data or mixed-version
clusters.

- **Protobuf package `egopb` and the Go package `egopb`.** The proto package is
  part of the type URL of every `Any` payload that was persisted (for example
  `type.googleapis.com/egopb.Event`). Renaming it would make stored payloads
  undecodable. The Go package name stays equal to it because `buf` lint
  (`PACKAGE_SAME_GO_PACKAGE`) requires that.
- **Metadata keys `ego.cmd.*`, `ego.tenant.*`, `ego.adoption.receipt`, and the
  reserved prefix `ego.`.** They are written into stored events, and metadata is
  unmarshalled strictly, so a renamed key would be rejected or lost.
- **GoAkt extension ID strings.** Nodes of different versions look extensions up
  by these strings; changing them would break rolling upgrades. Only the Go
  constant names may change.
- **NATS stream names `ego-events` and `ego-durable-states`.** A stream is
  server-side state; a new name would point at empty streams.
- **Postgres table `ego_schema_migrations` and its advisory lock key.** A new
  table name would make `Migrate` believe nothing was applied and run every
  migration again; a new lock key would not exclude old nodes.
- **Error grammar `ego: concurrency conflict: grammar=v1`.** Callers parse this
  exact string to detect a concurrency conflict, so it is part of the contract.

## Versioning

Urd starts at `v0.1.0` for every module. The old `ego` tags are not valid for
the new path: Go resolves versions per module path, so `go get
github.com/getsyntegrity/urd@v0.1.0` is the first installable version. The old
module `github.com/getsyntegrity/ego` will carry a `Deprecated:` notice that
points here.
