# Writing an adapter

An adapter is the code that connects Ego to one piece of infrastructure: a store, a publisher, an
encryptor, a tenant resolver. This guide explains how to add one **without editing any file outside
your own module**. The one exception is a line in archcheck when your adapter opens a new family of
adapter modules (step 2).

The rules behind the guide are in [`openspec/changes/ego-arch-004/design.md`](../openspec/changes/ego-arch-004/design.md)
(the adapter SPI, "service provider interface": what an adapter implements so core can use it). The
running example is `publisher/websocket`, the first adapter that adopted the model.

## The model in one paragraph

An adapter implements a **port**: an interface owned by a contract package, such as
`publishing.EventPublisher`. It can also have **optional capabilities**, such as "has work to do
before first use" (`adapter.Starter`) or "can report readiness" (`adapter.Pinger`). It **declares**
what it is through `Describe()`, which returns an `adapter.Descriptor`: the ports it serves, a name,
and the optional capabilities it implements. Core never type-asserts those optional interfaces
itself; it asks one accessor per interface (`adapter.Describe`, `adapter.StarterOf`,
`adapter.PingerOf`, `tenancy.AsFixedTenantResolver`). Declaring is optional in v4: an adapter
without `Describe` keeps working exactly as before, but it cannot be inspected or validated before
it runs.

A capability is **mandatory** when the port's own interface has the method (every store has `Ping`,
so readiness is implied for stores and is never declared), **optional** when your type implements
the capability's interface and declares it, and **unsupported** when it does neither.

## 1. Pick the port

Find the contract package that owns the interface you implement:

| You are writing | Contract package | Port name constant |
|---|---|---|
| an events, state or snapshot store | `persistence` | `persistence.PortEventsStore`, `PortStateStore`, `PortSnapshotStore` |
| an offset store for projections | `offsetstore` | `offsetstore.PortOffsetStore` |
| an events or state publisher | `port/publishing` | `publishing.PortEventPublisher`, `PortStatePublisher` |
| an encryptor | `encryption` | `encryption.PortEncryptor` |
| a tenant resolver | `tenancy` | `tenancy.PortTenantResolver` |

If none fits, you need a new contract under `port/`. That is an architecture decision (an ADR
change), not an adapter.

## 2. Create the module

Put the adapter in its own directory with its own `go.mod`, under the family's adapter root.
Publishers live under `publisher/` today (`publisher/websocket`, `publisher/kafka`, ...). For a new
family of adapters, the root is decided when its first module arrives (maintainer decision O6,
design §9); adding that root to `ExternalAdapterLayer` in `internal/cmd/archcheck/rules/layers.go`
is the one-line change mentioned above.

Import only contract packages and `egopb`. Never import package `engine`, the GoAkt runtime, or
`compose` and anything under it: archcheck's rules `external-adapter-no-runtime` and
`external-adapter-no-composition` reject that in production code. archcheck does not read test
files, so copy `closure_test.go` from an existing publisher; it runs `go list -deps -test ./...`
and fails if your tests pull in the runtime, the engine package or the composition root.

## 3. Implement the port

Implement the interface and add a compile-time assertion next to the type, as
`publisher/websocket/websocket.go` does:

```go
var (
	_ publishing.EventPublisher = (*EventsPublisher)(nil)
	_ adapter.Describer         = (*EventsPublisher)(nil)
)
```

## 4. Declare the descriptor

Implement `Describe()`. List every port the type implements, a short name, and every optional
capability you implement. Leave out capabilities the port already implies (never declare
`adapter.CapReady` on a store). The websocket publisher serves one port and has no optional
capability, because it dials in its constructor (no `Start`) and has no `Ping`:

```go
func (x *EventsPublisher) Describe() adapter.Descriptor {
	return adapter.Descriptor{
		Ports: []adapter.Port{publishing.PortEventPublisher},
		Name:  "websocket",
	}
}
```

A publisher that dials in `Start` and can report readiness would declare both:

```go
Capabilities: []adapter.Capability{adapter.CapStart, adapter.CapReady},
```

A tenant resolver that implements `tenancy.FixedTenantResolver` declares `tenancy.CapFixedTenant`.
That capability means "can be asked for a fixed tenant", not "has one": a multi-tenant resolver may
implement the interface and still answer `(zero, false)`.

**Declare exactly what you implement.** Two checks keep the declaration honest, and both fail on a
mismatch in either direction:

- `compose.Spec.Validate` rule **V8**, when a consumer wires the adapter: the slot's port must be in
  `Ports` (V8a), and for the capabilities compose knows (`CapStart` and `CapReady` on publishers,
  `CapFixedTenant` on the tenant resolver) declared and implemented must agree (V8b). For example, a
  resolver that implements `FixedTenantResolver` without declaring `CapFixedTenant` fails V8. A
  capability compose does not know is accepted there and left to your conformance tests.
- `adaptertest` check AT-1, in your own tests (step 6).

A nil or typed-nil value counts as undeclared: the accessors never call its methods.

## 5. Lifecycle and ownership

Who starts and who closes an adapter depends on its ownership, fixed by ego-arch-003 design §D5:

| Adapter | Owned by | Started by | Closed by |
|---|---|---|---|
| Stores | the consumer | the consumer, before `compose/goakt.New` (`Connect`) | the consumer, after `App.Stop` returns (`Disconnect`); the composition root only pings them |
| Publishers | the composition root, once `New` succeeds | the composition root, in the "attach publishers" start step (`Start`, then `Ping`) | the composition root, on every terminal path |

An **owned** adapter (a publisher) follows rules L1–L6 of design §D4:

- Do your I/O in `Start`, not in the constructor (L6; existing publishers that dial in their
  constructor keep doing so under decision O5).
- If `Start` fails, release whatever it acquired before returning the error (L1): nobody undoes a
  failed `Start` for you.
- Make `Close` idempotent, safe on a value that was never started, and safe after a failed `Start`
  (L2). When publisher *k* fails to start, the composition root closes every publisher that was not
  attached, started or not.
- Return from `Close` by the context's deadline (L3); every adapter shares one shutdown budget.
- After `Close`, operations fail with the port's documented error and never block (L5; for
  publishers, `publishing.ErrPublisherNotStarted`).

A **borrowed** adapter (a store) implements `Connect`, `Disconnect` and `Ping` as its port
documents. `Ping` means "ready to serve now" and may open a connection; that connection stays the
consumer's to close (L4). The composition root never connects or closes a store.

## 6. Run the conformance suites

From your adapter's tests, run `port/adapter/adaptertest` always, plus your port's own suite:
`port/publishing/publishingtest` for publishers, `persistence/conformance` for stores. The websocket
publisher runs both against an `httptest` server (`publisher/websocket/conformance_test.go`):

```go
results := adaptertest.Run(t, adaptertest.Target{
	Port:      publishing.PortEventPublisher,
	Ownership: adaptertest.Owned,
	New: func(*testing.T) (any, error) {
		return NewEventsPublisher(&Config{URL: srv.URL()})
	},
	Stall: srv.Stall,
})

results = publishingtest.RunEvents(t, publishingtest.EventsTarget{
	New: func(*testing.T) (publishing.EventPublisher, error) {
		return NewEventsPublisher(&Config{URL: srv.URL()})
	},
	Received: func(ctx context.Context, _ *testing.T, want *egopb.Event) error {
		return srv.await(ctx, want)
	},
})
```

Fill `Target` as far as you can: `Ownership`, plus the `FailStart` hook (a value whose `Start`
fails) and the `Stall` hook (make the backend stop answering) when your adapter supports them. A
check the suite cannot run is reported as "not exercised", never as passed; assert the exact set of
outcomes you expect, as the websocket tests do. Return `adaptertest.ErrUnreachable` from a factory
only when the backing service is genuinely absent (no broker in this environment); any other error
fails the check.

## 7. Wire it

The consumer builds the adapter and places it in a named `compose.Spec` field:

```go
events, err := websocket.NewEventsPublisher(&websocket.Config{URL: url})
if err != nil {
	return err
}
app, err := egoakt.New(compose.Spec{
	Name:            "orders",
	Families:        compose.EventSourced,
	EventsStore:     store, // connected by the consumer before New
	EventPublishers: []publishing.EventPublisher{events},
})
if err != nil {
	return err // V1–V8 failed; the consumer still owns events and must close it
}
defer app.Stop(ctx) // once New succeeded: always Stop, never close a publisher yourself
if err := app.Start(ctx); err != nil {
	return err // a *compose.StartError; rollback already ran and closed the publishers
}
```

Stores: connect before `New`, disconnect after `Stop`. Publishers: never close one after `New`
succeeded; `Stop` (or a failed `Start`) does it. When an owned adapter fails `Start` or `Ping`, the
error is a `*compose.StartError` with `Step` `"attach publishers"`, naming the publisher by `ID()`
and, when it declares one, by its descriptor name.

## 8. CI

Nothing to register. `ciselect` discovers the module from its `go.mod`, and the module is verified
like any other nested module (`scripts/ci/verify-module.sh <dir>`). If the module is released, list
it in [`docs/ci.md`](ci.md).

## 9. Pull request checklist

- [ ] `go run ./internal/cmd/archcheck` is green.
- [ ] The closure test (`closure_test.go`) is green.
- [ ] The conformance suites are green; anything skipped is skipped only through
      `adaptertest.ErrUnreachable`, with the reason in the pull request.
- [ ] `Describe` lists exactly the ports and optional capabilities the type implements.
- [ ] `apidiff` on your module reports additions only.
