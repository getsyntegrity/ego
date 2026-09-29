# How the engine is laid out

The `engine` package is what applications import: `Engine`, `Config`, the options,
the public errors and the four actor types GoAkt places in a cluster. The code that
does the work of those actors used to live in the same package, about eight thousand
lines next to the facade. To find out how an event gets persisted you had to read
past saga, durable state and projection code, and nothing said which piece depended
on which.

The work now lives in small internal packages, one per behavior, and `engine`
delegates to them. Nothing about the public API, the errors it returns or the names
that travel between cluster nodes changed.

## Where to look

| To find out... | Read |
|---|---|
| how an event is persisted | `internal/engine/eventsource/event_sourced_actor.go` (command handling, batching) and `events_writer_actor.go` (the write itself) |
| how snapshots and retention work | `internal/engine/eventsource/snapshots_writer_actor.go` and `events_janitor_actor.go` |
| how durable state is written | `internal/engine/durablestate/durable_state_actor.go` |
| how a saga reacts to events and compensates | `internal/engine/saga/saga_actor.go` |
| how a projection runs | `internal/engine/projection/projection_actor.go`, then `internal/projectionrunner` |
| how a command's deadline, metadata and reply travel | `internal/engine/protocol` |
| how `Dispatch`, `Entity` or `Saga` behave | the files of `engine` listed below |

## Packages

| Package | Responsibility |
|---|---|
| `engine` | Public API: `Engine`, `Config` and options, aliases, public errors, `ClusterKinds()`, the four cluster-kind types and `projectionRunnerError` |
| `internal/engine/protocol` | The command protocol between `Engine` and its actors: command metadata carried in the context, deadline gate, command reply parsing and error classification, tenant binding answer, event and state stream topics |
| `internal/engine/eventsource` | The event sourced actor and its children: events writer, snapshots writer, events janitor (retention), retry with backoff. They share private messages, so they move together |
| `internal/engine/durablestate` | The durable state actor |
| `internal/engine/saga` | The saga actor and the saga status conversion to and from protobuf |
| `internal/engine/projection` | The projection host actor around `internal/projectionrunner`; the escalation error is built by a hook that `engine` sets |
| `internal/extensions` | The GoAkt extensions, the typed extension lookup, `BehaviorFrom`, and the sentinels `ErrMissingRequiredExtensions` and `ErrEntityTenantScopeMissing` (re-exported by `engine` as the same values) |
| `internal/engine/enginetest` | Test fixtures shared by the actor packages and `engine`'s own tests (sample behaviors, a discarding logger). It is a regular package so several test binaries can import it, and it must not import `engine` |

The dependency direction is one way:

```
engine ──> internal/engine/{eventsource, durablestate, saga, projection}
                 └──> internal/engine/protocol, internal/extensions,
                      internal/{instrumentation, goaktlog, projectionrunner, runner, ...},
                      port/*, contract packages, goakt
engine ──> internal/engine/protocol, internal/extensions
```

No internal package imports `engine`: the compiler forbids it, because `engine`
imports them. There is no `common` package; every shared helper has a named home.
`archcheck` also keeps `internal/engine/...` out of the application, composition and
adapter layers (see [CI](./ci.md)).

## Why the cluster kinds stay in `engine`

GoAkt names an actor kind `lower(reflect.TypeOf(actor).Elem().String())`, so
`EventSourcedActor` travels between nodes as `engine.eventsourcedactor` in spawn,
relocation and singleton records. GoAkt also keys supervisor directives by the
error's type name and ships them to peers. If those types moved to another package
they would be renamed, and a cluster running two versions during a rolling upgrade
would stop understanding itself.

So `EventSourcedActor`, `DurableStateActor`, `SagaActor` and `ProjectionActor`, and
the error type `projectionRunnerError` (`engine.projectionRunnerError`), stay declared
in `engine`. Each actor type is a struct with one unexported field that holds the
implementation, and its three GoAkt methods (`PreStart`, `Receive`, `PostStop`) delegate
to it. `engine/cluster_kinds.go` holds them, and `TestClusterKindsExposesEgoActors`
pins the four names. `TestProjectionSupervisorContract` pins the supervisor key.

Two alternatives were rejected. Embedding the internal type would add an exported
embedded field of an internal type to the public API. Accepting the rename would break
rolling upgrades.

The projection actor is the one place where the internal package needs something that
must stay in `engine`: the escalation error. `ProjectionActor.PreStart` gives the
implementation a function that wraps the runner's error in `projectionRunnerError`
before it delegates, so an actor GoAkt builds by reflection gets it too.

The three persistence children of the event sourced actor are not cluster kinds. They
are spawned locally by their parent and never resolved by name on a peer, so they moved
with it. The only visible effect is the kind label on GoAkt's optional lifecycle
metrics, which is now `eventsource.<name>` instead of `engine.<name>`.

## The events writer

The child that writes an entity's events to the store and publishes them once the
write is confirmed was first extracted on its own as `internal/eventswriter` (#193).
It is now part of `internal/engine/eventsource`, next to the actor that spawns it,
because the two share private request and response messages and reading the write
path should not mean hopping between packages. That move is one isolated commit
(`refactor(engine): fold internal/eventswriter into internal/engine/eventsource`), so
it can be reverted if a separate package is preferred. `internal/extensions` keeps
`Require`, the typed lookup #193 introduced, and the missing-extension sentinel.

## Files of `engine`

| File | Holds |
|---|---|
| `engine.go` | `Engine`, `NewEngine`, extension validation, `Start`, `Stop`, `Started`, `ActorSystem` |
| `errors.go` | The public error values and `BehaviorPlacementError` |
| `entities.go` | `Entity`, `DurableStateEntity`, `EntityExists`, `EraseEntity` and their spawns |
| `spawn_tenancy.go` | Tenant scope, the spawn binding query and existing-spawn resolution |
| `commands.go` | `Dispatch`, `SendCommand`, metadata derivation, legacy result mapping |
| `sagas.go` | `Saga`, `SagaStatus` |
| `projections.go` | Projection start, stop, rebuild, status and lag |
| `streams.go` | `Subscribe`, event and state publishers and their stream loops |
| `spawn_options.go`, `spawn_config.go` | GoAkt spawn options, supervisors and placement; the spawn option types |
| `cluster_kinds.go` | The four cluster-kind types and `ClusterKinds()` |
| `projection_actor.go` | `projectionRunnerError`, the projection supervisor and `NewProjectionActor` |
| `behavior.go`, `saga.go`, `option.go`, `publisher.go`, `retention.go`, ... | The public aliases, options and types |

## Changing it

Put behavior in the package that owns it and keep `engine` a thin facade. If a new
piece of code is shared by two actor packages, give it a home in `internal/engine/protocol`
(if it belongs to the command protocol) or `internal/extensions` (if it is about GoAkt
extensions); a fixture that only tests need goes in `internal/engine/enginetest`. Never
rename or move one of the four cluster-kind types or `projectionRunnerError` without a
migration plan for mixed-version clusters.
