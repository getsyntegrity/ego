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

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	mocks "github.com/getsyntegrity/ego/mocks/persistence"
	"github.com/getsyntegrity/ego/persistence"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	runtimeport "github.com/getsyntegrity/ego/port/runtime"
	"github.com/getsyntegrity/ego/tenancy"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// tenantMetadata is a small test helper converting a TenantContext into the
// map[string]string shape egopb.Event.TenantMetadata carries on the wire.
func tenantMetadata(t *testing.T, tc tenancy.TenantContext) map[string]string {
	t.Helper()
	return map[string]string(tenancy.MarshalMetadata(tc))
}

// newAnyEvent builds a minimal egopb.Event envelope wrapping msg, optionally
// carrying tenantMD as TenantMetadata (nil for "absent metadata").
func newAnyEvent(t *testing.T, persistenceID string, seqNr uint64, msg proto.Message, tenantMD map[string]string) *egopb.Event {
	t.Helper()
	eventAny, err := anypb.New(msg)
	require.NoError(t, err)
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
			event := newAnyEvent(ctx.T, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, nil)

			// legacy mode must return parent unchanged
			got, err := actor.eventContext(parent, event)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual(parent)
		})

		// 1.1 (table test)
		tenantTC, err := tenancy.NewTenantContext("acme")
		if err != nil {
			t.Fatalf("tenancy.NewTenantContext: %v", err)
		}
		admin, err := tenancy.NewAdministrative("ops-bot", "crypto-shred")
		if err != nil {
			t.Fatalf("tenancy.NewAdministrative: %v", err)
		}
		adminTC, err := tenancy.NewAdministrativeContext(admin)
		if err != nil {
			t.Fatalf("tenancy.NewAdministrativeContext: %v", err)
		}

		cases := []struct {
			name string
			tc   tenancy.TenantContext
		}{
			{"tenant scope", tenantTC},
			{"administrative scope", adminTC},
		}

		s.Describe("reconstructs tenant and administrative scope from valid metadata", func(s *specs.Spec) {
			for _, tc := range cases {
				s.It(tc.name, func(ctx *specs.Context) {
					actor := &Actor{tenantAware: true}
					event := newAnyEvent(ctx.T, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, tenantMetadata(ctx.T, tc.tc))

					evCtx, err := actor.eventContext(context.Background(), event)
					ctx.Expect(err).To(specs.BeNil())

					// eventContext must attach the reconstructed TenantContext
					got, ok := tenancy.From(evCtx)
					ctx.Expect(ok).To(specs.BeTrue())
					ctx.Expect(got).ToEqual(tc.tc)
				})
			}
		})

		s.Describe("absent or malformed tenant metadata returns wrapped ErrInvalid, parent unchanged", func(s *specs.Spec) {
			// 1.2
			actor := &Actor{tenantAware: true}

			s.It("absent metadata", func(ctx *specs.Context) {
				event := newAnyEvent(ctx.T, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, nil)
				evCtx, err := actor.eventContext(context.Background(), event)
				ctx.Expect(err).To(specs.Not(specs.BeNil()))
				ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
				ctx.Expect(evCtx == nil).To(specs.BeTrue())
			})

			s.It("malformed metadata", func(ctx *specs.Context) {
				event := newAnyEvent(ctx.T, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, map[string]string{"ego.tenant.scope": "not-a-real-scope"})
				evCtx, err := actor.eventContext(context.Background(), event)
				ctx.Expect(err).To(specs.Not(specs.BeNil()))
				ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
				ctx.Expect(evCtx == nil).To(specs.BeTrue())
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
func newBoundSagaActor(t *testing.T, behavior behaviorport.Saga) *Actor {
	t.Helper()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(context.Background()))
	t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

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
			actor := newBoundSagaActor(ctx.T, behavior)

			event := newAnyEvent(ctx.T, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx.T, tenantA))
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
			actor := newBoundSagaActor(ctx.T, behavior)

			unrelated := newAnyEvent(ctx.T, "entity-noise", 1, &testpb.AccountCreated{AccountId: "entity-noise"}, tenantMetadata(ctx.T, tenantB))
			actor.handleStreamEvent(unrelated)

			// HandleEvent must still see the event to decide relevance
			ctx.Expect(handleEventCalls).ToEqual(1)
			// an event the behavior ignores must never seed boundTenant
			ctx.Expect(actor.boundTenant).ToEqual(noTenantContext)

			watched := newAnyEvent(ctx.T, "watched-entity", 1, &testpb.AccountCreated{AccountId: "watched-entity"}, tenantMetadata(ctx.T, tenantA))
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
			actor := newBoundSagaActor(ctx.T, behavior)

			firstEvent := newAnyEvent(ctx.T, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx.T, tenantA))
			actor.handleStreamEvent(firstEvent)
			ctx.Expect(handleEventCalls).ToEqual(1)
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)

			boundLatest, err := actor.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), actor.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			// the first, relevant event must have persisted a saga event
			ctx.Expect(boundLatest == nil).To(specs.BeFalse())

			secondEvent := newAnyEvent(ctx.T, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx.T, tenantB))
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
			actor := newBoundSagaActor(ctx.T, behavior)

			tenantlessEvent := newAnyEvent(ctx.T, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, nil)
			actor.handleStreamEvent(tenantlessEvent)

			// HandleEvent must never run for a tenant-less event in tenant-aware mode
			ctx.Expect(handleEventCalls).ToEqual(0)
			// a rejected event must not seed boundTenant
			ctx.Expect(actor.boundTenant).ToEqual(noTenantContext)
			ctx.Expect(actor.status).ToEqual(runtimeport.SagaRunning)

			validEvent := newAnyEvent(ctx.T, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx.T, tenantA))
			actor.handleStreamEvent(validEvent)

			// the saga must still be able to consume a later, validly-scoped event
			ctx.Expect(handleEventCalls).ToEqual(1)
			ctx.Expect(actor.boundTenant).ToEqual(tenantA)
		})
	})
}

func TestSagaActorCompensateUsesBoundTenant(t *testing.T) {
	t.Run("compensate dispatches under boundTenant", func(t *testing.T) {
		// 2.4
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		ctx := context.Background()
		targetID := "target-" + uuid.NewString()
		reply := &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID}}}

		actorSystem, err := goakt.NewActorSystem("TestCompensateSystem", goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		t.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })

		probe := &ctxCapturingActor{reply: reply}
		_, err = actorSystem.Spawn(ctx, targetID, probe, goakt.WithLongLived())
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: "saga-" + uuid.NewString(),
			CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
				return []sagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}, Timeout: 3 * time.Second}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)
		s.boundTenant = tenantA
		s.actorSystem = actorSystem

		compensateCtx, err := s.compensationContext()
		require.NoError(t, err)
		s.compensate(compensateCtx, enginetest.DiscardLogger, actorSystem)

		observed, ok := probe.observedTenant()
		require.True(t, ok, "the compensation command must carry a TenantContext")
		assert.Equal(t, tenantA, observed)
		assert.Equal(t, runtimeport.SagaCompleted, s.status)
	})

	t.Run("unbound tenant-aware timeout fails closed: runtimeport.SagaFailed, zero dispatches", func(t *testing.T) {
		// 2.5
		var dispatchCount int
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: "saga-" + uuid.NewString(),
			CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
				dispatchCount++
				return nil, nil
			},
		}
		s := newBoundSagaActor(t, behavior)
		// boundTenant is left at its zero value (noTenantContext): no event
		// was ever processed before the timeout fired.

		_, err := s.compensationContext()
		require.Error(t, err, "compensationContext must fail closed when boundTenant was never seeded")
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
		assert.Zero(t, dispatchCount, "Compensate must never run when the tenant context cannot be reconstructed")
	})
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
			actor := newBoundSagaActor(ctx.T, behavior)

			tenantCtx, err := tenancy.Attach(context.Background(), tenantA)
			ctx.Expect(err).To(specs.BeNil())

			err = actor.persistAndApplyEvents(tenantCtx, []Event{&testpb.AccountCreated{AccountId: "entity-1"}})
			ctx.Expect(err).To(specs.BeNil())

			latest, err := actor.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), actor.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest == nil).To(specs.BeFalse())
			ctx.Expect(latest.GetTenantMetadata()).ToEqual(tenantMetadata(ctx.T, tenantA))
		})

		s.It("legacy mode writes no tenant metadata", func(ctx *specs.Context) {
			behavior := &enginetest.CallbackSagaBehavior{SagaID: "saga-" + uuid.NewString()}
			actor := newBoundSagaActor(ctx.T, behavior)
			actor.tenantAware = false

			err := actor.persistAndApplyEvents(context.Background(), []Event{&testpb.AccountCreated{AccountId: "entity-1"}})
			ctx.Expect(err).To(specs.BeNil())

			latest, err := actor.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), actor.sagaID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest == nil).To(specs.BeFalse())
			ctx.Expect(len(latest.GetTenantMetadata())).ToEqual(0)
		})
	})
}

func TestSagaActorSendCommandThreadsTenantContext(t *testing.T) {
	// 3.2
	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)

	ctx := context.Background()
	targetID := "target-" + uuid.NewString()
	stateAny, err := anypb.New(&testpb.Account{AccountId: targetID})
	require.NoError(t, err)
	reply := &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID, State: stateAny}}}

	actorSystem, err := goakt.NewActorSystem("TestSendCommandSystem", goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	t.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })

	probe := &ctxCapturingActor{reply: reply}
	_, err = actorSystem.Spawn(ctx, targetID, probe, goakt.WithLongLived())
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)

	var handleResultCalled bool
	behavior := &enginetest.CallbackSagaBehavior{
		SagaID: "saga-" + uuid.NewString(),
		HandleResultFn: func(_ context.Context, _ string, _ State, _ State) (*sagaAction, error) {
			handleResultCalled = true
			return &sagaAction{Complete: true}, nil
		},
	}
	s := newBoundSagaActor(t, behavior)
	s.actorSystem = actorSystem
	s.boundTenant = tenantA

	sendCtx, err := tenancy.Attach(context.Background(), tenantA)
	require.NoError(t, err)
	s.sendCommand(sendCtx, sagaCommand{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}, Timeout: 3 * time.Second})

	observed, ok := probe.observedTenant()
	require.True(t, ok, "sendCommand must dispatch on a ctx where tenancy.Require succeeds with the bound tenant")
	assert.Equal(t, tenantA, observed)
	assert.True(t, handleResultCalled)
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
			ctx.T.Cleanup(func() { _ = store.Disconnect(context.Background()) })

			sagaID := "saga-" + uuid.NewString()
			event := newAnyEvent(ctx.T, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx.T, tenantA))
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
			ctx.T.Cleanup(func() { _ = store.Disconnect(context.Background()) })

			sagaID := "saga-" + uuid.NewString()
			firstEvent := newAnyEvent(ctx.T, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(ctx.T, tenantA))
			secondEvent := newAnyEvent(ctx.T, sagaID, 2, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(ctx.T, tenantB))
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
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			// a later event disagreeing with the first must fail recovery with ErrDenied
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))
		})

		s.It("a replayed event with absent tenant metadata fails PreStart with ErrInvalid", func(ctx *specs.Context) {
			// 4.3
			store := testkit.NewEventsStore()
			ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
			ctx.T.Cleanup(func() { _ = store.Disconnect(context.Background()) })

			sagaID := "saga-" + uuid.NewString()
			event := newAnyEvent(ctx.T, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, nil)
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
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
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
	t.Run("a Commands-only first action persists a tenant-binding marker before dispatching", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		targetID := "target-" + uuid.NewString()
		var handleEventCalls int
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: "saga-" + uuid.NewString(),
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				handleEventCalls++
				return &sagaAction{Commands: []sagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}}}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)

		actorSystem, err := goakt.NewActorSystem("TestDurableBindSystem", goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(context.Background()))
		t.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })
		s.actorSystem = actorSystem

		probe := &ctxCapturingActor{reply: &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID}}}}
		_, err = actorSystem.Spawn(context.Background(), targetID, probe, goakt.WithLongLived())
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		event := newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA))
		s.handleStreamEvent(event)

		require.Equal(t, 1, handleEventCalls)
		assert.Equal(t, tenantA, s.boundTenant)

		observed, ok := probe.observedTenant()
		require.True(t, ok, "the command must still be dispatched after the durable bind succeeds")
		assert.Equal(t, tenantA, observed)

		latest, err := s.eventsStore.GetLatestEvent(context.Background(), persistence.Unscoped(), s.sagaID)
		require.NoError(t, err)
		require.NotNil(t, latest, "a Commands-only action must still leave a durable record behind, even with zero business events")
		assert.Equal(t, tenantMetadata(t, tenantA), latest.GetTenantMetadata())

		msg, err := latest.GetEvent().UnmarshalNew()
		require.NoError(t, err)
		_, isMarker := msg.(*emptypb.Empty)
		assert.True(t, isMarker, "the durable record for a Commands-only bind must be a tenant-only marker, not a fabricated business event")
	})

	t.Run("restart recovers boundTenant from the marker and rejects a later foreign-tenant event", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		tenantB, err := tenancy.NewTenantContext("globex")
		require.NoError(t, err)

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
		require.NoError(t, store.Connect(context.Background()))
		t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

		actorSystem, err := goakt.NewActorSystem("TestRestartBindSystem", goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(context.Background()))
		t.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })

		probe := &ctxCapturingActor{reply: &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID}}}}
		_, err = actorSystem.Spawn(context.Background(), targetID, probe, goakt.WithLongLived())
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

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
		first.handleStreamEvent(newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA)))
		require.Equal(t, tenantA, first.boundTenant, "the bind must complete before dispatchActionEffects is reached")

		// Simulate a restart: a fresh instance, same sagaID and store.
		second := &Actor{
			behavior:    behavior,
			eventsStore: store,
			sagaID:      sagaID,
			tenantAware: true,
			scope:       persistence.Unscoped(),
			logger:      enginetest.DiscardLogger,
		}
		require.NoError(t, second.recover(context.Background()))
		assert.Equal(t, tenantA, second.boundTenant, "recover() must reconstruct boundTenant from the durable marker left by the Commands-only first action")
		assert.Zero(t, applyEventCalls, "the marker must never reach behavior.ApplyEvent")

		foreignEvent := newAnyEvent(t, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(t, tenantB))
		second.handleStreamEvent(foreignEvent)
		assert.Equal(t, tenantA, second.boundTenant, "a foreign-tenant event after restart must be rejected, not silently rebind an unowned-looking saga")
		assert.Equal(t, runtimeport.SagaRunning, second.status)
	})

	t.Run("a failed first WriteEvents leaves zero residual appropriation", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		tenantB, err := tenancy.NewTenantContext("globex")
		require.NoError(t, err)

		failingStore := new(mocks.EventsStore)
		failingStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(assert.AnError).Once()

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
		s := &Actor{
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

		event := newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA))
		require.NotPanics(t, func() { s.handleStreamEvent(event) })

		assert.Equal(t, 1, handleEventCalls)
		assert.Equal(t, noTenantContext, s.boundTenant, "a failed tenant-binding write must leave the saga unbound, not appropriated in memory")
		assert.Equal(t, runtimeport.SagaRunning, s.status)

		workingStore := testkit.NewEventsStore()
		require.NoError(t, workingStore.Connect(context.Background()))
		t.Cleanup(func() { _ = workingStore.Disconnect(context.Background()) })
		s.eventsStore = workingStore

		retryEvent := newAnyEvent(t, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(t, tenantB))
		s.handleStreamEvent(retryEvent)

		assert.Equal(t, 2, handleEventCalls)
		assert.Equal(t, tenantB, s.boundTenant, "a clean retry, even under a different tenant, must be free to bind after the earlier failed write")
	})
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
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
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
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))
		})

		s.It("bound saga: missing tenant context is rejected", func(ctx *specs.Context) {
			actor := &Actor{tenantAware: true, boundTenant: tenantA}
			err := actor.checkStateReadTenant(context.Background())
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrMissing))
		})
	})
}
