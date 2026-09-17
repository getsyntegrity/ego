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

package ego

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/pablogore/ego/v4/egopb"
	mocks "github.com/pablogore/ego/v4/mocks/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
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

// TestSagaActorEventContext covers tasks.md 1.1-1.3.
func TestSagaActorEventContext(t *testing.T) {
	t.Run("legacy mode passthrough returns (parent, nil)", func(t *testing.T) {
		// 1.3
		s := &SagaActor{tenantAware: false}
		parent := context.WithValue(context.Background(), struct{}{}, "marker")
		event := newAnyEvent(t, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, nil)

		got, err := s.eventContext(parent, event)
		require.NoError(t, err)
		assert.Equal(t, parent, got, "legacy mode must return parent unchanged")
	})

	t.Run("reconstructs tenant and administrative scope from valid metadata", func(t *testing.T) {
		// 1.1 (table test)
		tenantTC, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		admin, err := tenancy.NewAdministrative("ops-bot", "crypto-shred")
		require.NoError(t, err)
		adminTC, err := tenancy.NewAdministrativeContext(admin)
		require.NoError(t, err)

		cases := []struct {
			name string
			tc   tenancy.TenantContext
		}{
			{"tenant scope", tenantTC},
			{"administrative scope", adminTC},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s := &SagaActor{tenantAware: true}
				event := newAnyEvent(t, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, tenantMetadata(t, tc.tc))

				ctx, err := s.eventContext(context.Background(), event)
				require.NoError(t, err)

				got, ok := tenancy.From(ctx)
				require.True(t, ok, "eventContext must attach the reconstructed TenantContext")
				assert.Equal(t, tc.tc, got)
			})
		}
	})

	t.Run("absent or malformed tenant metadata returns wrapped ErrInvalid, parent unchanged", func(t *testing.T) {
		// 1.2
		s := &SagaActor{tenantAware: true}

		t.Run("absent metadata", func(t *testing.T) {
			event := newAnyEvent(t, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, nil)
			ctx, err := s.eventContext(context.Background(), event)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tenancy.ErrInvalid))
			assert.Nil(t, ctx)
		})

		t.Run("malformed metadata", func(t *testing.T) {
			event := newAnyEvent(t, "p1", 1, &testpb.AccountCreated{AccountId: "p1"}, map[string]string{"ego.tenant.scope": "not-a-real-scope"})
			ctx, err := s.eventContext(context.Background(), event)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tenancy.ErrInvalid))
			assert.Nil(t, ctx)
		})
	})
}

// --- Phase 2: bind-on-first-event + VerifyUnchanged (SG4) -------------------

// newBoundSagaActor builds a minimal, directly-constructed SagaActor (no
// actor system) suitable for exercising handleStreamEvent/processAction/
// sendCommand/compensate in isolation, mirroring
// durable_state_actor_tenant_persist_test.go's direct-struct-construction
// style for unit-level method tests.
func newBoundSagaActor(t *testing.T, behavior SagaBehavior) *SagaActor {
	t.Helper()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(context.Background()))
	t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

	return &SagaActor{
		behavior:     behavior,
		eventsStore:  store,
		currentState: behavior.InitialState(),
		status:       SagaRunning,
		sagaID:       behavior.ID(),
		tenantAware:  true,
		logger:       DiscardLogger,
	}
}

func TestSagaActorBindOnFirstEvent(t *testing.T) {
	t.Run("binds to the first relevant tenant event it acts on", func(t *testing.T) {
		// 2.1. The behavior returns a non-noop SagaAction (an event to persist for
		// its own state) to prove the event is one this saga acts on, not just
		// one it happened to observe (SG4 correction, see the "does not bind on
		// an unrelated event" test below).
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		var handleEventCalls int
		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			handleEvent: func(_ context.Context, _ Event, _ State) (*SagaAction, error) {
				handleEventCalls++
				return &SagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)

		event := newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA))
		s.handleStreamEvent(event)

		assert.Equal(t, 1, handleEventCalls, "HandleEvent must run for a validly tenant-scoped event")
		assert.Equal(t, tenantA, s.boundTenant, "boundTenant must be seeded from the first relevant event")
	})

	t.Run("does not bind on an unrelated event, binds once the saga's own event arrives", func(t *testing.T) {
		// Codex P1 (PR #78, saga_actor.go handleStreamEvent): every saga
		// subscribes to the shared eventsTopic, so the first event observed may
		// belong to a different tenant and be irrelevant to this saga. Binding
		// must not commit to that tenant before HandleEvent's own relevance
		// filter has a chance to reject it.
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		tenantB, err := tenancy.NewTenantContext("globex")
		require.NoError(t, err)

		var handleEventCalls int
		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			handleEvent: func(_ context.Context, event Event, _ State) (*SagaAction, error) {
				handleEventCalls++
				created, ok := event.(*testpb.AccountCreated)
				if !ok || created.GetAccountId() != "watched-entity" {
					return &SagaAction{}, nil
				}
				return &SagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)

		unrelated := newAnyEvent(t, "entity-noise", 1, &testpb.AccountCreated{AccountId: "entity-noise"}, tenantMetadata(t, tenantB))
		s.handleStreamEvent(unrelated)

		assert.Equal(t, 1, handleEventCalls, "HandleEvent must still see the event to decide relevance")
		assert.Equal(t, noTenantContext, s.boundTenant, "an event the behavior ignores must never seed boundTenant")

		watched := newAnyEvent(t, "watched-entity", 1, &testpb.AccountCreated{AccountId: "watched-entity"}, tenantMetadata(t, tenantA))
		s.handleStreamEvent(watched)

		assert.Equal(t, 2, handleEventCalls)
		assert.Equal(t, tenantA, s.boundTenant, "boundTenant must bind to the saga's own tenant, not an earlier unrelated tenant's noise event")
	})

	t.Run("a second event from a different tenant is rejected once bound", func(t *testing.T) {
		// 2.2
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		tenantB, err := tenancy.NewTenantContext("globex")
		require.NoError(t, err)

		var handleEventCalls int
		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			handleEvent: func(_ context.Context, _ Event, _ State) (*SagaAction, error) {
				handleEventCalls++
				return &SagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)

		firstEvent := newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA))
		s.handleStreamEvent(firstEvent)
		require.Equal(t, 1, handleEventCalls)
		require.Equal(t, tenantA, s.boundTenant)

		boundLatest, err := s.eventsStore.GetLatestEvent(context.Background(), s.sagaID)
		require.NoError(t, err)
		require.NotNil(t, boundLatest, "the first, relevant event must have persisted a saga event")

		secondEvent := newAnyEvent(t, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(t, tenantB))
		s.handleStreamEvent(secondEvent)

		assert.Equal(t, 1, handleEventCalls, "HandleEvent must not run for a foreign-tenant event once bound")
		assert.Equal(t, tenantA, s.boundTenant, "boundTenant must remain unchanged after a rejected foreign event")
		assert.Equal(t, SagaRunning, s.status, "status must be untouched by a rejected event")

		latest, err := s.eventsStore.GetLatestEvent(context.Background(), s.sagaID)
		require.NoError(t, err)
		assert.Equal(t, boundLatest.GetSequenceNumber(), latest.GetSequenceNumber(), "no additional saga event may be persisted for a rejected foreign-tenant event")
	})

	t.Run("tenant-less or malformed event at the reset site is rejected, saga still consumes the next valid event", func(t *testing.T) {
		// 2.3
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		var handleEventCalls int
		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			handleEvent: func(_ context.Context, _ Event, _ State) (*SagaAction, error) {
				handleEventCalls++
				return &SagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)

		tenantlessEvent := newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, nil)
		s.handleStreamEvent(tenantlessEvent)

		assert.Zero(t, handleEventCalls, "HandleEvent must never run for a tenant-less event in tenant-aware mode")
		assert.Equal(t, noTenantContext, s.boundTenant, "a rejected event must not seed boundTenant")
		assert.Equal(t, SagaRunning, s.status)

		validEvent := newAnyEvent(t, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(t, tenantA))
		s.handleStreamEvent(validEvent)

		assert.Equal(t, 1, handleEventCalls, "the saga must still be able to consume a later, validly-scoped event")
		assert.Equal(t, tenantA, s.boundTenant)
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

		actorSystem, err := goakt.NewActorSystem("TestCompensateSystem", goakt.WithLogger(newLoggerAdapter(DiscardLogger)))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		t.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })

		probe := &ctxCapturingActor{reply: reply}
		_, err = actorSystem.Spawn(ctx, targetID, probe, goakt.WithLongLived())
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			compensate: func(_ context.Context, _ State) ([]SagaCommand, error) {
				return []SagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}, Timeout: 3 * time.Second}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)
		s.boundTenant = tenantA
		s.actorSystem = actorSystem

		compensateCtx, err := s.compensationContext()
		require.NoError(t, err)
		s.compensate(compensateCtx, DiscardLogger, actorSystem)

		observed, ok := probe.observedTenant()
		require.True(t, ok, "the compensation command must carry a TenantContext")
		assert.Equal(t, tenantA, observed)
		assert.Equal(t, SagaCompleted, s.status)
	})

	t.Run("unbound tenant-aware timeout fails closed: SagaFailed, zero dispatches", func(t *testing.T) {
		// 2.5
		var dispatchCount int
		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			compensate: func(_ context.Context, _ State) ([]SagaCommand, error) {
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
	t.Run("tenant-aware mode writes tenant_metadata", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		behavior := &callbackSagaBehavior{id: "saga-" + uuid.NewString()}
		s := newBoundSagaActor(t, behavior)

		ctx, err := tenancy.Attach(context.Background(), tenantA)
		require.NoError(t, err)

		err = s.persistAndApplyEvents(ctx, []Event{&testpb.AccountCreated{AccountId: "entity-1"}})
		require.NoError(t, err)

		latest, err := s.eventsStore.GetLatestEvent(context.Background(), s.sagaID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		assert.Equal(t, tenantMetadata(t, tenantA), latest.GetTenantMetadata())
	})

	t.Run("legacy mode writes no tenant metadata", func(t *testing.T) {
		behavior := &callbackSagaBehavior{id: "saga-" + uuid.NewString()}
		s := newBoundSagaActor(t, behavior)
		s.tenantAware = false

		err := s.persistAndApplyEvents(context.Background(), []Event{&testpb.AccountCreated{AccountId: "entity-1"}})
		require.NoError(t, err)

		latest, err := s.eventsStore.GetLatestEvent(context.Background(), s.sagaID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		assert.Empty(t, latest.GetTenantMetadata())
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

	actorSystem, err := goakt.NewActorSystem("TestSendCommandSystem", goakt.WithLogger(newLoggerAdapter(DiscardLogger)))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	t.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })

	probe := &ctxCapturingActor{reply: reply}
	_, err = actorSystem.Spawn(ctx, targetID, probe, goakt.WithLongLived())
	require.NoError(t, err)
	time.Sleep(300 * time.Millisecond)

	var handleResultCalled bool
	behavior := &callbackSagaBehavior{
		id: "saga-" + uuid.NewString(),
		handleResult: func(_ context.Context, _ string, _ State, _ State) (*SagaAction, error) {
			handleResultCalled = true
			return &SagaAction{Complete: true}, nil
		},
	}
	s := newBoundSagaActor(t, behavior)
	s.actorSystem = actorSystem
	s.boundTenant = tenantA

	sendCtx, err := tenancy.Attach(context.Background(), tenantA)
	require.NoError(t, err)
	s.sendCommand(sendCtx, SagaCommand{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}, Timeout: 3 * time.Second})

	observed, ok := probe.observedTenant()
	require.True(t, ok, "sendCommand must dispatch on a ctx where tenancy.Require succeeds with the bound tenant")
	assert.Equal(t, tenantA, observed)
	assert.True(t, handleResultCalled)
}

// --- Phase 4: replay validation against first-replayed-event boundTenant (SG5) --

func TestSagaActorRecoverReplayTenantValidation(t *testing.T) {
	t.Run("establishes boundTenant from the first successfully-decoded replayed event", func(t *testing.T) {
		// 4.1
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(context.Background()))
		t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

		sagaID := "saga-" + uuid.NewString()
		event := newAnyEvent(t, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA))
		require.NoError(t, store.WriteEvents(context.Background(), []*egopb.Event{event}))

		behavior := &callbackSagaBehavior{id: sagaID}
		s := &SagaActor{
			behavior:    behavior,
			eventsStore: store,
			sagaID:      sagaID,
			tenantAware: true,
			logger:      DiscardLogger,
		}

		require.NoError(t, s.recover(context.Background()))
		assert.Equal(t, tenantA, s.boundTenant)
		assert.EqualValues(t, 1, s.eventsCounter)
	})

	t.Run("a later replayed event from a different tenant fails recovery closed", func(t *testing.T) {
		// 4.2
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		tenantB, err := tenancy.NewTenantContext("globex")
		require.NoError(t, err)

		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(context.Background()))
		t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

		sagaID := "saga-" + uuid.NewString()
		firstEvent := newAnyEvent(t, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA))
		secondEvent := newAnyEvent(t, sagaID, 2, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(t, tenantB))
		require.NoError(t, store.WriteEvents(context.Background(), []*egopb.Event{firstEvent, secondEvent}))

		behavior := &callbackSagaBehavior{id: sagaID}
		s := &SagaActor{
			behavior:    behavior,
			eventsStore: store,
			sagaID:      sagaID,
			tenantAware: true,
			logger:      DiscardLogger,
		}

		err = s.recover(context.Background())
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrDenied), "a later event disagreeing with the first must fail recovery with ErrDenied")
	})

	t.Run("a replayed event with absent tenant metadata fails PreStart with ErrInvalid", func(t *testing.T) {
		// 4.3
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(context.Background()))
		t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

		sagaID := "saga-" + uuid.NewString()
		event := newAnyEvent(t, sagaID, 1, &testpb.AccountCreated{AccountId: "entity-1"}, nil)
		require.NoError(t, store.WriteEvents(context.Background(), []*egopb.Event{event}))

		behavior := &callbackSagaBehavior{id: sagaID}
		s := &SagaActor{
			behavior:    behavior,
			eventsStore: store,
			sagaID:      sagaID,
			tenantAware: true,
			logger:      DiscardLogger,
		}

		err := s.recover(context.Background())
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid), "absent tenant metadata on a replayed event must fail closed with ErrInvalid, no backfill")
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
		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			handleEvent: func(_ context.Context, _ Event, _ State) (*SagaAction, error) {
				handleEventCalls++
				return &SagaAction{Commands: []SagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}}}}, nil
			},
		}
		s := newBoundSagaActor(t, behavior)

		actorSystem, err := goakt.NewActorSystem("TestDurableBindSystem", goakt.WithLogger(newLoggerAdapter(DiscardLogger)))
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

		latest, err := s.eventsStore.GetLatestEvent(context.Background(), s.sagaID)
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
		behavior := &callbackSagaBehavior{
			id: sagaID,
			handleEvent: func(_ context.Context, _ Event, _ State) (*SagaAction, error) {
				return &SagaAction{Commands: []SagaCommand{{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 1}}}}, nil
			},
			applyEvent: func(_ context.Context, _ Event, state State) (State, error) {
				applyEventCalls++
				return state, nil
			},
		}

		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(context.Background()))
		t.Cleanup(func() { _ = store.Disconnect(context.Background()) })

		actorSystem, err := goakt.NewActorSystem("TestRestartBindSystem", goakt.WithLogger(newLoggerAdapter(DiscardLogger)))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(context.Background()))
		t.Cleanup(func() { _ = actorSystem.Stop(context.Background()) })

		probe := &ctxCapturingActor{reply: &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{PersistenceId: targetID}}}}
		_, err = actorSystem.Spawn(context.Background(), targetID, probe, goakt.WithLongLived())
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		first := &SagaActor{
			behavior:     behavior,
			eventsStore:  store,
			currentState: behavior.InitialState(),
			status:       SagaRunning,
			sagaID:       sagaID,
			tenantAware:  true,
			logger:       DiscardLogger,
			actorSystem:  actorSystem,
		}
		first.handleStreamEvent(newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA)))
		require.Equal(t, tenantA, first.boundTenant, "the bind must complete before dispatchActionEffects is reached")

		// Simulate a restart: a fresh instance, same sagaID and store.
		second := &SagaActor{
			behavior:    behavior,
			eventsStore: store,
			sagaID:      sagaID,
			tenantAware: true,
			logger:      DiscardLogger,
		}
		require.NoError(t, second.recover(context.Background()))
		assert.Equal(t, tenantA, second.boundTenant, "recover() must reconstruct boundTenant from the durable marker left by the Commands-only first action")
		assert.Zero(t, applyEventCalls, "the marker must never reach behavior.ApplyEvent")

		foreignEvent := newAnyEvent(t, "entity-2", 1, &testpb.AccountCreated{AccountId: "entity-2"}, tenantMetadata(t, tenantB))
		second.handleStreamEvent(foreignEvent)
		assert.Equal(t, tenantA, second.boundTenant, "a foreign-tenant event after restart must be rejected, not silently rebind an unowned-looking saga")
		assert.Equal(t, SagaRunning, second.status)
	})

	t.Run("a failed first WriteEvents leaves zero residual appropriation", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		tenantB, err := tenancy.NewTenantContext("globex")
		require.NoError(t, err)

		failingStore := new(mocks.EventsStore)
		failingStore.EXPECT().WriteEvents(mock.Anything, mock.Anything).Return(assert.AnError).Once()

		var handleEventCalls int
		behavior := &callbackSagaBehavior{
			id: "saga-" + uuid.NewString(),
			handleEvent: func(_ context.Context, _ Event, _ State) (*SagaAction, error) {
				handleEventCalls++
				if handleEventCalls == 1 {
					// Commands-only: the durable record must be the marker
					// written by persistTenantBinding, which this sub-test
					// makes fail.
					return &SagaAction{Commands: []SagaCommand{{EntityID: "target-x", Command: &testpb.CreateAccount{AccountBalance: 1}}}}, nil
				}
				// The retry carries its own Events, so it never reaches
				// dispatchActionEffects/sendCommand in this sub-test.
				return &SagaAction{Events: []Event{&testpb.AccountCreated{AccountId: "saga-record"}}}, nil
			},
		}
		s := &SagaActor{
			behavior:     behavior,
			eventsStore:  failingStore,
			currentState: behavior.InitialState(),
			status:       SagaRunning,
			sagaID:       behavior.ID(),
			tenantAware:  true,
			logger:       DiscardLogger,
			// actorSystem is intentionally left nil: if dispatchActionEffects
			// ran despite the failed persist, sendCommand would panic
			// dereferencing it — proving the effect never fires, not merely
			// that boundTenant looks clean.
		}

		event := newAnyEvent(t, "entity-1", 1, &testpb.AccountCreated{AccountId: "entity-1"}, tenantMetadata(t, tenantA))
		require.NotPanics(t, func() { s.handleStreamEvent(event) })

		assert.Equal(t, 1, handleEventCalls)
		assert.Equal(t, noTenantContext, s.boundTenant, "a failed tenant-binding write must leave the saga unbound, not appropriated in memory")
		assert.Equal(t, SagaRunning, s.status)

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

// TestSagaActorCheckStateReadTenant covers the finding that SagaActor.Receive
// dispatched *egopb.GetStateCommand straight to replyWithState, bypassing
// every tenant check: any caller who knew a sagaID could read another
// tenant's saga state even though the instance is bound to exactly one
// tenant for its lifetime (SG4).
func TestSagaActorCheckStateReadTenant(t *testing.T) {
	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	t.Run("legacy mode is always a no-op", func(t *testing.T) {
		s := &SagaActor{tenantAware: false}
		assert.NoError(t, s.checkStateReadTenant(context.Background()))
	})

	t.Run("unbound tenant-aware saga: any resolved tenant may read (no owner yet)", func(t *testing.T) {
		s := &SagaActor{tenantAware: true}
		ctx, err := tenancy.Attach(context.Background(), tenantA)
		require.NoError(t, err)
		assert.NoError(t, s.checkStateReadTenant(ctx))
	})

	t.Run("unbound tenant-aware saga: missing tenant context is rejected", func(t *testing.T) {
		s := &SagaActor{tenantAware: true}
		err := s.checkStateReadTenant(context.Background())
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrMissing))
	})

	t.Run("bound saga: the matching tenant succeeds", func(t *testing.T) {
		s := &SagaActor{tenantAware: true, boundTenant: tenantA}
		ctx, err := tenancy.Attach(context.Background(), tenantA)
		require.NoError(t, err)
		assert.NoError(t, s.checkStateReadTenant(ctx))
	})

	t.Run("bound saga: a foreign tenant is rejected", func(t *testing.T) {
		s := &SagaActor{tenantAware: true, boundTenant: tenantA}
		ctx, err := tenancy.Attach(context.Background(), tenantB)
		require.NoError(t, err)
		err = s.checkStateReadTenant(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrDenied))
	})

	t.Run("bound saga: missing tenant context is rejected", func(t *testing.T) {
		s := &SagaActor{tenantAware: true, boundTenant: tenantA}
		err := s.checkStateReadTenant(context.Background())
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrMissing))
	})
}
