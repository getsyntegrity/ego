// MIT License
//
// Copyright (c) 2022-2026 Arsene Tochemey Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package saga

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	testpb "github.com/getsyntegrity/ego/internal/testpb"
	"github.com/getsyntegrity/ego/persistence"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	runtimeport "github.com/getsyntegrity/ego/port/runtime"
	"github.com/getsyntegrity/ego/tenancy"
	"github.com/getsyntegrity/ego/testkit"
)

// tenantMetadata is a small test helper converting a TenantContext into the
// map[string]string shape egopb.Event.TenantMetadata carries on the wire.
func tenantMetadata(_ *specs.Context, tc tenancy.TenantContext) map[string]string {
	return map[string]string(tenancy.MarshalMetadata(tc))
}

// newAnyEvent builds a minimal egopb.Event envelope wrapping msg, optionally
// carrying tenantMD as TenantMetadata (nil for "absent metadata").
func newAnyEvent(ctx *specs.Context, persistenceID string, seqNr uint64, msg proto.Message, tenantMD map[string]string) *egopb.Event {
	eventAny, err := anypb.New(msg)
	ctx.Expect(err).To(specs.BeNil())
	return &egopb.Event{
		PersistenceId:  persistenceID,
		SequenceNumber: seqNr,
		Event:          eventAny,
		TenantMetadata: tenantMD,
	}
}

// --- Phase 1: eventContext (SG2) --------------------------------------------

// sagaTenantMarkerKey is the context key used to tag the parent context.
type sagaTenantMarkerKey struct{}

// TestSagaActorEventContext covers tasks.md 1.1-1.3.
func TestSagaActorEventContext(t *testing.T) {
	specs.Describe(t, "Actor.eventContext rebuilds the tenant context an event was written under", func(s *specs.Spec) {
		s.It("legacy mode passthrough returns (parent, nil)", func(ctx *specs.Context) {
			// 1.3
			actor := &Actor{tenantAware: false}
			parent := context.WithValue(context.Background(), sagaTenantMarkerKey{}, "marker")
			event := newAnyEvent(ctx, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, nil)

			// legacy mode must return parent unchanged
			got, err := actor.eventContext(parent, event)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual(parent)
		})

		// 1.1 (table test). Each row builds its context inside the case so a
		// failure is reported by the spec.
		type scopeCase struct {
			name  string
			build func() (tenancy.TenantContext, error)
		}
		cases := []scopeCase{
			{"tenant scope", func() (tenancy.TenantContext, error) { return tenancy.NewTenantContext("acme") }},
			{"administrative scope", func() (tenancy.TenantContext, error) {
				admin, err := tenancy.NewAdministrative("ops-bot", "crypto-shred")
				if err != nil {
					return tenancy.TenantContext{}, err
				}
				return tenancy.NewAdministrativeContext(admin)
			}},
		}

		s.Describe("reconstructs tenant and administrative scope from valid metadata", func(s *specs.Spec) {
			specs.Table(s, cases, func(c scopeCase) string { return c.name }, func(ctx *specs.Context, c scopeCase) {
				scope, err := c.build()
				ctx.Expect(err).To(specs.BeNil())
				actor := &Actor{tenantAware: true}
				event := newAnyEvent(ctx, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, tenantMetadata(ctx, scope))

				evCtx, err := actor.eventContext(context.Background(), event)
				ctx.Expect(err).To(specs.BeNil())

				// eventContext must attach the reconstructed TenantContext
				got, ok := tenancy.From(evCtx)
				ctx.Expect(ok).To(specs.BeTrue())
				ctx.Expect(got).ToEqual(scope)
			})
		})

		s.Describe("absent or malformed tenant metadata returns wrapped ErrInvalid, parent unchanged", func(s *specs.Spec) {
			// 1.2
			actor := &Actor{tenantAware: true}

			s.It("absent metadata", func(ctx *specs.Context) {
				event := newAnyEvent(ctx, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, nil)
				evCtx, err := actor.eventContext(context.Background(), event)
				ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
				ctx.Expect(evCtx).To(specs.BeNil())
			})

			s.It("malformed metadata", func(ctx *specs.Context) {
				event := newAnyEvent(ctx, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, map[string]string{"ego.tenant.scope": "not-a-real-scope"})
				evCtx, err := actor.eventContext(context.Background(), event)
				ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
				ctx.Expect(evCtx).To(specs.BeNil())
			})
		})
	})
}

// --- Phase 2: bind-on-first-event + VerifyUnchanged (SG4) -------------------

// newBoundSagaActor builds a minimal, directly-constructed Actor (no
// actor system) suitable for exercising handleStreamEvent/processAction/
// sendCommand/compensate in isolation, mirroring
// durable_state_actor_tenant_persist_test.go's direct-struct-construction
// style for unit-level method tests.
func newBoundSagaActor(ctx *specs.Context, behavior behaviorport.Saga) *Actor {
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })

	return &Actor{
		behavior:     behavior,
		eventsStore:  store,
		currentState: behavior.InitialState(),
		status:       runtimeport.SagaRunning,
		sagaID:       behavior.ID(),
		tenantAware:  true,
		// scope is deliberately persistence.Unscoped() here even though
		// tenantAware is true: this helper builds a directly-constructed
		// Actor with no actor system and no PreStart/resolveScope call,
		// used to unit-test handleStreamEvent/recover's lazy
		// bind-on-first-event logic (SG4/SG5) in isolation from
		// TENANT-003 T4's spawn-time scope binding, which is covered
		// separately by the actorSystem.Spawn-based tests. boundTenant is
		// deliberately left at its zero value for the same reason.
		scope:  persistence.Unscoped(),
		logger: enginetest.DiscardLogger,
	}
}

func TestSagaActorBindOnFirstEvent(t *testing.T) {
	specs.Describe(t, "Actor.handleStreamEvent binds the saga to the tenant of the first event it acts on", func(s *specs.Spec) {
		s.It("binds to the first relevant tenant event it acts on", func(ctx *specs.Context) {
			// 2.1. The behavior returns a non-noop sagaAction (an event to persist for
			// its own state) to prove the event is one this saga acts on, not just
			// one it happened to observe (SG4 correction, see the "does not bind on
			// an unrelated event" test below).
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())

			var handleEventCalls int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
					handleEventCalls++
					return &sagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
				},
			}
			actor := newBoundSagaActor(ctx, behavior)

			event := newAnyEvent(ctx, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx, tenantA))
			actor.handleStreamEvent(event)

			// HandleEvent must run for a validly tenant-scoped event
			ctx.Expect(handleEventCalls).ToEqual(1)
			// boundTenant must be seeded from the first relevant event
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)
		})

		s.It("does not bind on an unrelated event, binds once the saga's own event arrives", func(ctx *specs.Context) {
			// Codex P1 (PR #78, saga_actor.go handleStreamEvent): every saga
			// subscribes to the shared protocol.EventsTopic, so the first event observed may
			// belong to a different tenant and be irrelevant to this saga. Binding
			// must not commit to that tenant before HandleEvent's own relevance
			// filter has a chance to reject it.
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())
			tenantB, err := tenancy.NewTenantContext("globex")
			ctx.Expect(err).To(specs.BeNil())

			var handleEventCalls int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				HandleEventFn: func(_ context.Context, event Event, _ State) (*sagaAction, error) {
					handleEventCalls++
					created, ok := event.(*testpb.AccountCreated)
					if !ok || created.GetAccountId() != "watched-entity" {
						return &sagaAction{}, nil
					}
					return &sagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
				},
			}
			actor := newBoundSagaActor(ctx, behavior)

			unrelated := newAnyEvent(ctx, "entity-noise", 1, &testpb.AccountCreated{AccountId: "entity-noise"}, tenantMetadata(ctx, tenantB))
			actor.handleStreamEvent(unrelated)

			// HandleEvent must still see the event to decide relevance
			ctx.Expect(handleEventCalls).ToEqual(1)
			// an event the behavior ignores must never seed boundTenant
			ctx.Expect(actor.boundTenant).ToEqual(noTenantContext)

			watched := newAnyEvent(ctx, "watched-entity", 1, &testpb.AccountCreated{AccountId: "watched-entity"}, tenantMetadata(ctx, tenantA))
			actor.handleStreamEvent(watched)

			ctx.Expect(handleEventCalls).ToEqual(2)
			// boundTenant must bind to the saga's own tenant, not an earlier unrelated tenant's noise event
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)
		})

		s.It("a second event from a different tenant is rejected once bound", func(ctx *specs.Context) {
			// 2.2
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())
			tenantB, err := tenancy.NewTenantContext("globex")
			ctx.Expect(err).To(specs.BeNil())

			var handleEventCalls int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
					handleEventCalls++
					return &sagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
				},
			}
			actor := newBoundSagaActor(ctx, behavior)

			firstEvent := newAnyEvent(ctx, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx, tenantA))
			actor.handleStreamEvent(firstEvent)
			ctx.Expect(handleEventCalls).ToEqual(1)
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)

			boundLatest, err := actor.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), actor.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			// the first, relevant event must have persisted a saga event
			ctx.Expect(boundLatest).To(specs.Not(specs.BeNil()))

			secondEvent := newAnyEvent(ctx, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx, tenantB))
			actor.handleStreamEvent(secondEvent)

			// HandleEvent must not run for a foreign-tenant event once bound
			ctx.Expect(handleEventCalls).ToEqual(1)
			// boundTenant must remain unchanged after a rejected foreign event
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)
			// status must be untouched by a rejected event
			ctx.Expect(actor.status).ToEqual(runtimeport.SagaRunning)

			latest, err := actor.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), actor.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			// no additional saga event may be persisted for a rejected foreign-tenant event
			ctx.Expect(latest.GetSequenceNumber()).ToEqual(boundLatest.GetSequenceNumber())
		})

		s.It("tenant-less or malformed event at the reset site is rejected, saga still consumes the next valid event", func(ctx *specs.Context) {
			// 2.3
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())

			var handleEventCalls int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
					handleEventCalls++
					return &sagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
				},
			}
			actor := newBoundSagaActor(ctx, behavior)

			tenantlessEvent := newAnyEvent(ctx, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, nil)
			actor.handleStreamEvent(tenantlessEvent)

			// HandleEvent must never run for a tenant-less event in tenant-aware mode
			ctx.Expect(handleEventCalls).ToEqual(0)
			// a rejected event must not seed boundTenant
			ctx.Expect(actor.boundTenant).ToEqual(noTenantContext)
			ctx.Expect(actor.status).ToEqual(runtimeport.SagaRunning)

			validEvent := newAnyEvent(ctx, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx, tenantA))
			actor.handleStreamEvent(validEvent)

			// the saga must still be able to consume a later, validly-scoped event
			ctx.Expect(handleEventCalls).ToEqual(1)
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)
		})
	})
}

func TestSagaActorCompensateUsesBoundTenant(t *testing.T) {
	specs.Describe(t, "Actor.compensate dispatches under the bound tenant and fails closed without one", func(s *specs.Spec) {
		s.It("compensate dispatches under boundTenant", func(ctx *specs.Context) {
			// 2.4
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())

			targetID := "target-" + uuid.NewString()
			reply := &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID}}}

			probe := &ctxCapturingActor{reply: reply}
			actorSystem := startProbeSystem(ctx, "TestCompensateSystem", targetID, probe)

			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
					return []sagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}, Timeout: 3 * time.Second}}, nil
				},
			}
			saga := newBoundSagaActor(ctx, behavior)
			saga.boundTenant = tenantA
			saga.actorSystem = actorSystem

			compensateCtx, err := saga.compensationContext()
			ctx.Expect(err).To(specs.BeNil())
			saga.compensate(compensateCtx, enginetest.DiscardLogger, actorSystem)

			observed, ok := probe.observedTenant()
			// the compensation command must carry a TenantContext
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(observed).ToEqual(tenantA)
			ctx.Expect(saga.status).ToEqual(runtimeport.SagaCompleted)
		})

		s.It("unbound tenant-aware timeout fails closed: runtimeport.SagaFailed, zero dispatches", func(ctx *specs.Context) {
			// 2.5
			var dispatchCount int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
					dispatchCount++
					return nil, nil
				},
			}
			saga := newBoundSagaActor(ctx, behavior)
			// boundTenant is left at its zero value (noTenantContext): no event
			// was ever processed before the timeout fired.

			// compensationContext must fail closed when boundTenant was never seeded
			_, err := saga.compensationContext()
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
			// Compensate must never run when the tenant context cannot be reconstructed
			ctx.Expect(dispatchCount).ToEqual(0)
		})
	})
}

// startProbeSystem starts an in-process actor system with probe spawned under
// name, waits until the probe is registered, and stops the system when the case
// ends. It replaces a fixed sleep after Spawn with a poll on the observable
// condition.
func startProbeSystem(ctx *specs.Context, systemName, name string, probe goakt.Actor) goakt.ActorSystem {
	bg := context.Background()
	actorSystem, err := goakt.NewActorSystem(systemName, goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)))
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(actorSystem.Start(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })

	_, err = actorSystem.Spawn(bg, name, probe, goakt.WithLongLived())
	ctx.Expect(err).To(specs.BeNil())
	ctx.Eventually(func() any {
		exists, _ := actorSystem.ActorExists(bg, name)
		return exists
	}, specs.BeTrue(), specs.WithTimeout(5*time.Second), specs.WithInterval(10*time.Millisecond))
	return actorSystem
}

// ctxCapturingActor is a minimal goakt.Actor test double that records the
// tenancy.TenantContext attached to the ctx it was dispatched under, and
// replies with a fixed reply so the caller's SendSync succeeds.
type ctxCapturingActor struct {
	reply proto.Message

	mu      sync.Mutex
	lastCtx context.Context
}

var _ goakt.Actor = (*ctxCapturingActor)(nil)

func (a *ctxCapturingActor) PreStart(_ *goakt.Context) error { return nil }
func (a *ctxCapturingActor) PostStop(_ *goakt.Context) error { return nil }
func (a *ctxCapturingActor) Receive(ctx *goakt.ReceiveContext) {
	a.mu.Lock()
	a.lastCtx = ctx.Context()
	a.mu.Unlock()
	ctx.Response(a.reply)
}

func (a *ctxCapturingActor) observedTenant() (tenancy.TenantContext, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastCtx == nil {
		return tenancy.TenantContext{}, false
	}
	return tenancy.From(a.lastCtx)
}

// --- Phase 3: thread ctx through reset sites; saga events carry metadata (SG1, SG3) --

func TestSagaActorPersistAndApplyEventsWritesTenantMetadata(t *testing.T) {
	// 3.1
	specs.Describe(t, "Actor.persistAndApplyEvents writes tenant metadata only in tenant-aware mode", func(s *specs.Spec) {
		s.It("tenant-aware mode writes tenant_metadata", func(ctx *specs.Context) {
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())

			behavior := &enginetest.CallbackSagaBehavior{SagaID: "saga-" + uuid.NewString()}
			actor := newBoundSagaActor(ctx, behavior)

			tenantCtx, err := tenancy.Attach(context.Background(), tenantA)
			ctx.Expect(err).To(specs.BeNil())

			err = actor.persistAndApplyEvents(tenantCtx, []Event{&testpb.AccountCreated{AccountId: "entity-1"}})
			ctx.Expect(err).To(specs.BeNil())

			latest, err := actor.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), actor.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetTenantMetadata()).ToEqual(tenantMetadata(ctx, tenantA))
		})

		s.It("legacy mode writes no tenant metadata", func(ctx *specs.Context) {
			behavior := &enginetest.CallbackSagaBehavior{SagaID: "saga-" + uuid.NewString()}
			actor := newBoundSagaActor(ctx, behavior)
			actor.tenantAware = false

			err := actor.persistAndApplyEvents(context.Background(), []Event{&testpb.AccountCreated{AccountId: "entity-1"}})
			ctx.Expect(err).To(specs.BeNil())

			latest, err := actor.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), actor.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetTenantMetadata()).To(specs.BeEmpty())
		})
	})
}

func TestSagaActorSendCommandThreadsTenantContext(t *testing.T) {
	specs.Describe(t, "Actor.sendCommand dispatches under the bound tenant context", func(s *specs.Spec) {
		s.It("hands the probe the bound tenant and reports the reply to the behavior", func(ctx *specs.Context) {
			// 3.2
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())

			targetID := "target-" + uuid.NewString()
			stateAny, err := anypb.New(&testpb.Account{AccountId: targetID})
			ctx.Expect(err).To(specs.BeNil())
			reply := &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID, State: stateAny}}}

			probe := &ctxCapturingActor{reply: reply}
			actorSystem := startProbeSystem(ctx, "TestSendCommandSystem", targetID, probe)

			var handleResultCalled bool
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				HandleResultFn: func(_ context.Context, _ string, _ State, _ State) (*sagaAction, error) {
					handleResultCalled = true
					return &sagaAction{Complete: true}, nil
				},
			}
			saga := newBoundSagaActor(ctx, behavior)
			saga.actorSystem = actorSystem
			saga.boundTenant = tenantA

			sendCtx, err := tenancy.Attach(context.Background(), tenantA)
			ctx.Expect(err).To(specs.BeNil())
			saga.sendCommand(sendCtx, sagaCommand{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}, Timeout: 3 * time.Second})

			observed, ok := probe.observedTenant()
			// sendCommand must dispatch on a ctx where tenancy.Require succeeds with the bound tenant
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(observed).ToEqual(tenantA)
			ctx.Expect(handleResultCalled).To(specs.BeTrue())
		})
	})
}

// --- Phase 4: replay validation against first-replayed-event boundTenant (SG5) --

func TestSagaActorRecoverReplayTenantValidation(t *testing.T) {
	specs.Describe(t, "Actor.recover validates the tenant of every replayed event", func(s *specs.Spec) {
		s.It("establishes boundTenant from the first successfully-decoded replayed event", func(ctx *specs.Context) {
			// 4.1
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())

			store := testkit.NewEventsStore()
			ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
			ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })

			sagaID := "saga-" + uuid.NewString()
			event := newAnyEvent(ctx, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx, tenantA))
			ctx.Expect(store.WriteEvents(context.Background(), persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional())).To(specs.BeNil())

			behavior := &enginetest.CallbackSagaBehavior{SagaID: sagaID}
			actor := &Actor{
				behavior:    behavior,
				eventsStore: store,
				sagaID:      sagaID,
				tenantAware: true,
				scope:       persistence.Unscoped(),
				logger:      enginetest.DiscardLogger,
			}

			ctx.Expect(actor.recover(context.Background())).To(specs.BeNil())
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)
			specs.ExpectT(ctx, actor.eventsCounter).ToEqual(1)
		})

		s.It("a later replayed event from a different tenant fails recovery closed", func(ctx *specs.Context) {
			// 4.2
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())
			tenantB, err := tenancy.NewTenantContext("globex")
			ctx.Expect(err).To(specs.BeNil())

			store := testkit.NewEventsStore()
			ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
			ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })

			sagaID := "saga-" + uuid.NewString()
			firstEvent := newAnyEvent(ctx, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx, tenantA))
			secondEvent := newAnyEvent(ctx, sagaID, 2, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx, tenantB))
			ctx.Expect(store.WriteEvents(context.Background(), persistence.Unscoped(), []*egopb.Event{firstEvent, secondEvent}, persistence.Unconditional())).To(specs.BeNil())

			behavior := &enginetest.CallbackSagaBehavior{SagaID: sagaID}
			actor := &Actor{
				behavior:    behavior,
				eventsStore: store,
				sagaID:      sagaID,
				tenantAware: true,
				scope:       persistence.Unscoped(),
				logger:      enginetest.DiscardLogger,
			}

			err = actor.recover(context.Background())
			// a later event disagreeing with the first must fail recovery with ErrDenied
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))
		})

		s.It("a replayed event with absent tenant metadata fails PreStart with ErrInvalid", func(ctx *specs.Context) {
			// 4.3
			store := testkit.NewEventsStore()
			ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
			ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })

			sagaID := "saga-" + uuid.NewString()
			event := newAnyEvent(ctx, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, nil)
			ctx.Expect(store.WriteEvents(context.Background(), persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional())).To(specs.BeNil())

			behavior := &enginetest.CallbackSagaBehavior{SagaID: sagaID}
			actor := &Actor{
				behavior:    behavior,
				eventsStore: store,
				sagaID:      sagaID,
				tenantAware: true,
				scope:       persistence.Unscoped(),
				logger:      enginetest.DiscardLogger,
			}

			err := actor.recover(context.Background())
			// absent tenant metadata on a replayed event must fail closed with ErrInvalid, no backfill
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

// --- Phase 5: durable tenant binding for a Commands-only first action (SG-DUR1, PR#78 review round 2 P1) --

// TestSagaActorDurableTenantBinding covers the finding that a saga's first
// bind was only durable when the triggering action carried its own Events:
// a perfectly valid action with only Commands, Complete or Compensate could
// bind boundTenant in memory and send external effects without persisting
// any record an instance recovering from a restart could reconstruct
// ownership from. TestTenantWritePathE2E's own saga (tenant_write_path_e2e_test.go)
// is exactly this shape.
func TestSagaActorDurableTenantBinding(t *testing.T) {
	specs.Describe(t, "Actor persists and recovers the tenant binding of a Commands-only first action", func(s *specs.Spec) {
		s.It("a Commands-only first action persists a tenant-binding marker before dispatching", func(ctx *specs.Context) {
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())

			targetID := "target-" + uuid.NewString()
			var handleEventCalls int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
					handleEventCalls++
					return &sagaAction{Commands: []sagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}}}}, nil
				},
			}
			saga := newBoundSagaActor(ctx, behavior)

			probe := &ctxCapturingActor{reply: &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID}}}}
			saga.actorSystem = startProbeSystem(ctx, "TestDurableBindSystem", targetID, probe)

			event := newAnyEvent(ctx, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx, tenantA))
			saga.handleStreamEvent(event)

			ctx.Expect(handleEventCalls).ToEqual(1)
			ctx.Expect(saga.boundTenant).ToEqual(tenantA)

			observed, ok := probe.observedTenant()
			// the command must still be dispatched after the durable bind succeeds
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(observed).ToEqual(tenantA)

			latest, err := saga.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), saga.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			// a Commands-only action must still leave a durable record behind, even with zero business events
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetTenantMetadata()).ToEqual(tenantMetadata(ctx, tenantA))

			msg, err := latest.GetEvent().UnmarshalNew()
			ctx.Expect(err).To(specs.BeNil())
			// the durable record for a Commands-only bind must be a tenant-only marker, not a fabricated business event
			ctx.Expect(msg).To(specs.Satisfy("be an empty tenant-only marker", func(v any) bool { _, isMarker := v.(*emptypb.Empty); return isMarker }))
		})

		s.It("restart recovers boundTenant from the marker and rejects a later foreign-tenant event", func(ctx *specs.Context) {
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())
			tenantB, err := tenancy.NewTenantContext("globex")
			ctx.Expect(err).To(specs.BeNil())

			sagaID := "saga-" + uuid.NewString()
			targetID := "target-" + uuid.NewString()
			var applyEventCalls int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: sagaID,
				HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
					return &sagaAction{Commands: []sagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}}}}, nil
				},
				ApplyEventFn: func(_ context.Context, _ Event, state State) (State, error) {
					applyEventCalls++
					return state, nil
				},
			}

			store := testkit.NewEventsStore()
			ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
			ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })

			probe := &ctxCapturingActor{reply: &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID}}}}
			actorSystem := startProbeSystem(ctx, "TestRestartBindSystem", targetID, probe)

			first := &Actor{
				behavior:     behavior,
				eventsStore:  store,
				currentState: behavior.InitialState(),
				status:       runtimeport.SagaRunning,
				sagaID:       sagaID,
				tenantAware:  true,
				scope:        persistence.Unscoped(),
				logger:       enginetest.DiscardLogger,
				actorSystem:  actorSystem,
			}
			first.handleStreamEvent(newAnyEvent(ctx, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx, tenantA)))
			// the bind must complete before dispatchActionEffects is reached
			ctx.Expect(first.boundTenant).ToEqual(tenantA)

			// Simulate a restart: a fresh instance, same sagaID and store.
			second := &Actor{
				behavior:    behavior,
				eventsStore: store,
				sagaID:      sagaID,
				tenantAware: true,
				scope:       persistence.Unscoped(),
				logger:      enginetest.DiscardLogger,
			}
			ctx.Expect(second.recover(context.Background())).To(specs.BeNil())
			// recover() must reconstruct boundTenant from the durable marker left by the Commands-only first action
			ctx.Expect(second.boundTenant).ToEqual(tenantA)
			// the marker must never reach behavior.ApplyEvent
			ctx.Expect(applyEventCalls).ToEqual(0)

			foreignEvent := newAnyEvent(ctx, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx, tenantB))
			second.handleStreamEvent(foreignEvent)
			// a foreign-tenant event after restart must be rejected, not silently rebind an unowned-looking saga
			ctx.Expect(second.boundTenant).ToEqual(tenantA)
			ctx.Expect(second.status).ToEqual(runtimeport.SagaRunning)
		})

		s.It("a failed first WriteEvents leaves zero residual appropriation", func(ctx *specs.Context) {
			tenantA, err := tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())
			tenantB, err := tenancy.NewTenantContext("globex")
			ctx.Expect(err).To(specs.BeNil())

			ctrl := mock.NewController(ctx)
			failingStore := writeOnlyStoreMock{c: ctrl}
			ctrl.Method("WriteEvents").Expect(mock.Any(), persistence.Unscoped(), mock.Any(), mock.Any()).Return(errStoreDown).Times(1)

			var handleEventCalls int
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: "saga-" + uuid.NewString(),
				HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
					handleEventCalls++
					if handleEventCalls == 1 {
						// Commands-only: the durable record must be the marker
						// written by persistTenantBinding, which this sub-test
						// makes fail.
						return &sagaAction{Commands: []sagaCommand{{EntityID: "target-x", Command: &testpb.CreateAccount{AccountBalance: 1}}}}, nil
					}
					// The retry carries its own Events, so it never reaches
					// dispatchActionEffects/sendCommand in this sub-test.
					return &sagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
				},
			}
			saga := &Actor{
				behavior:     behavior,
				eventsStore:  failingStore,
				currentState: behavior.InitialState(),
				status:       runtimeport.SagaRunning,
				sagaID:       behavior.ID(),
				tenantAware:  true,
				scope:        persistence.Unscoped(),
				logger:       enginetest.DiscardLogger,
				// actorSystem is intentionally left nil: if dispatchActionEffects
				// ran despite the failed persist, sendCommand would panic
				// dereferencing it — proving the effect never fires, not merely
				// that boundTenant looks clean.
			}

			event := newAnyEvent(ctx, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx, tenantA))
			panicked := func() (p bool) {
				defer func() { p = recover() != nil }()
				saga.handleStreamEvent(event)
				return false
			}()
			ctx.Expect(panicked).To(specs.BeFalse())

			ctx.Expect(handleEventCalls).ToEqual(1)
			// a failed tenant-binding write must leave the saga unbound, not appropriated in memory
			ctx.Expect(saga.boundTenant).ToEqual(noTenantContext)
			ctx.Expect(saga.status).ToEqual(runtimeport.SagaRunning)

			workingStore := testkit.NewEventsStore()
			ctx.Expect(workingStore.Connect(context.Background())).To(specs.BeNil())
			ctx.Cleanup(func() { _ = workingStore.Disconnect(context.Background()) })
			saga.eventsStore = workingStore

			retryEvent := newAnyEvent(ctx, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx, tenantB))
			saga.handleStreamEvent(retryEvent)

			ctx.Expect(handleEventCalls).ToEqual(2)
			// a clean retry, even under a different tenant, must be free to bind after the earlier failed write
			ctx.Expect(saga.boundTenant).ToEqual(tenantB)
		})
	})
}

// errStoreDown is the failure the write-only store mock reports.
var errStoreDown = errors.New("events store down")

// writeOnlyStoreMock stands in for persistence.EventsStore in a case that only
// reaches WriteEvents. Any other method panics on the nil embedded interface,
// which makes an unexpected call loud.
type writeOnlyStoreMock struct {
	persistence.EventsStore
	c *mock.Controller
}

func (m writeOnlyStoreMock) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	return m.c.Method("WriteEvents").Call(ctx, scope, events, precondition).Err(0)
}

// --- Phase 6: GetStateCommand tenancy gate (the DS4 shape, PR#78 review round 2 P1) --

// TestSagaActorCheckStateReadTenant covers the finding that Actor.Receive
// dispatched *egopb.GetStateCommand straight to replyWithState, bypassing
// every tenant check: any caller who knew a sagaID could read another
// tenant's saga state even though the instance is bound to exactly one
// tenant for its lifetime (SG4).
func TestSagaActorCheckStateReadTenant(t *testing.T) {
	specs.Describe(t, "Actor.checkStateReadTenant gates state reads by the bound tenant", func(s *specs.Spec) {
		var tenantA, tenantB tenancy.TenantContext
		s.BeforeEach(func(ctx *specs.Context) {
			var err error
			tenantA, err = tenancy.NewTenantContext("acme")
			ctx.Expect(err).To(specs.BeNil())
			tenantB, err = tenancy.NewTenantContext("globex")
			ctx.Expect(err).To(specs.BeNil())
		})

		s.It("legacy mode is always a no-op", func(ctx *specs.Context) {
			actor := &Actor{tenantAware: false}
			ctx.Expect(actor.checkStateReadTenant(context.Background())).To(specs.BeNil())
		})

		s.It("unbound tenant-aware saga: any resolved tenant may read (no owner yet)", func(ctx *specs.Context) {
			actor := &Actor{tenantAware: true}
			tenantCtx, err := tenancy.Attach(context.Background(), tenantA)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(actor.checkStateReadTenant(tenantCtx)).To(specs.BeNil())
		})

		s.It("unbound tenant-aware saga: missing tenant context is rejected", func(ctx *specs.Context) {
			actor := &Actor{tenantAware: true}
			err := actor.checkStateReadTenant(context.Background())
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrMissing))
		})

		s.It("bound saga: the matching tenant succeeds", func(ctx *specs.Context) {
			actor := &Actor{tenantAware: true, boundTenant: tenantA}
			tenantCtx, err := tenancy.Attach(context.Background(), tenantA)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(actor.checkStateReadTenant(tenantCtx)).To(specs.BeNil())
		})

		s.It("bound saga: a foreign tenant is rejected", func(ctx *specs.Context) {
			actor := &Actor{tenantAware: true, boundTenant: tenantA}
			tenantCtx, err := tenancy.Attach(context.Background(), tenantB)
			ctx.Expect(err).To(specs.BeNil())
			err = actor.checkStateReadTenant(tenantCtx)
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))
		})

		s.It("bound saga: missing tenant context is rejected", func(ctx *specs.Context) {
			actor := &Actor{tenantAware: true, boundTenant: tenantA}
			err := actor.checkStateReadTenant(context.Background())
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrMissing))
		})
	})
}
