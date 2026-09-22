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
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// tenancyProbeCreditEventSourcedBehavior is entity B's behavior in
// TestTenantWritePathE2E: an EventSourcedBehavior that reacts to
// *testpb.CreditAccount (the command the saga dispatches) and records the ctx
// it was invoked with, so the test can prove the tenant identity resolved at
// Engine.SendCommand for entity A survives the saga hop unchanged all the way
// to entity B's own HandleCommand.
type tenancyProbeCreditEventSourcedBehavior struct {
	id string

	mu          sync.Mutex
	invocations int
	lastCtx     context.Context
}

var _ EventSourcedBehavior = (*tenancyProbeCreditEventSourcedBehavior)(nil)

func newTenancyProbeCreditEventSourcedBehavior(id string) *tenancyProbeCreditEventSourcedBehavior {
	return &tenancyProbeCreditEventSourcedBehavior{id: id}
}

func (x *tenancyProbeCreditEventSourcedBehavior) ID() string { return x.id }

func (x *tenancyProbeCreditEventSourcedBehavior) InitialState() State {
	return new(testpb.Account)
}

func (x *tenancyProbeCreditEventSourcedBehavior) HandleCommand(ctx context.Context, command Command, _ State) (events []Event, err error) {
	x.mu.Lock()
	x.invocations++
	x.lastCtx = ctx
	x.mu.Unlock()

	switch cmd := command.(type) {
	case *testpb.CreditAccount:
		return []Event{
			&testpb.AccountCredited{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: cmd.GetBalance(),
			},
		}, nil
	default:
		return nil, errors.New("unhandled command")
	}
}

func (x *tenancyProbeCreditEventSourcedBehavior) HandleEvent(_ context.Context, event Event, _ State) (state State, err error) {
	switch evt := event.(type) {
	case *testpb.AccountCredited:
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: evt.GetAccountBalance(),
		}, nil
	default:
		return nil, errors.New("unhandled event")
	}
}

func (x *tenancyProbeCreditEventSourcedBehavior) MarshalBinary() (data []byte, err error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *tenancyProbeCreditEventSourcedBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

func (x *tenancyProbeCreditEventSourcedBehavior) invocationCount() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.invocations
}

func (x *tenancyProbeCreditEventSourcedBehavior) observedTenant() (tenancy.TenantContext, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.lastCtx == nil {
		return tenancy.TenantContext{}, false
	}
	return tenancy.From(x.lastCtx)
}

// TestTenantWritePathE2E proves spec.md's "Real-Dispatch End-to-End Tenant
// Integrity" requirement with a genuine cross-actor dispatch: unlike
// TestSendCommandTenantResolution (engine_test.go:232), which stops at
// entity A's own HandleCommand, this drives the full chain
// Engine.SendCommand -> entity A -> saga -> SagaCommand -> entity B, and
// asserts entity B's HandleCommand observes the *same* TenantContext entity
// A's did (tasks.md 5.1/5.2, design.md SG1-SG5).
func TestTenantWritePathE2E(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	resolver := &countingTenantResolver{id: "acme"}
	engine := newTestEngine(t, "TenantWritePathE2E", store, WithTenantResolver(resolver))
	require.NoError(t, engine.Start(ctx))

	entityAID := uuid.NewString()
	entityBID := uuid.NewString()

	entityA := NewAccountEventSourcedBehavior(entityAID)
	require.NoError(t, engine.Entity(ctx, entityA))

	entityB := newTenancyProbeCreditEventSourcedBehavior(entityBID)
	require.NoError(t, engine.Entity(ctx, entityB))

	sagaID := "saga-" + uuid.NewString()
	saga := &callbackSagaBehavior{
		id: sagaID,
		handleEvent: func(_ context.Context, event Event, state State) (*SagaAction, error) {
			created, ok := event.(*testpb.AccountCreated)
			if !ok || created.GetAccountId() != entityAID {
				return &SagaAction{}, nil
			}
			return &SagaAction{
				Commands: []SagaCommand{
					{EntityID: entityBID, Command: &testpb.CreditAccount{AccountId: entityBID, Balance: created.GetAccountBalance()}, Timeout: 5 * time.Second},
				},
			}, nil
		},
		handleResult: func(_ context.Context, _ string, _ State, _ State) (*SagaAction, error) {
			return &SagaAction{Complete: true}, nil
		},
	}
	require.NoError(t, engine.Saga(ctx, saga, 0))

	_, _, err := engine.SendCommand(ctx, entityAID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return entityB.invocationCount() == 1
	}, 10*time.Second, 50*time.Millisecond, "entity B's HandleCommand must eventually run via the saga hop")

	// TENANT-003 T4: Engine.Entity/Engine.Saga now also resolve the tenant
	// once each, at spawn, to bind the spawned actor's persistence.Scope
	// before it ever reads a store. This test spawns entity A, entity B,
	// and the saga (3 spawn-time resolves), then sends one command via
	// Engine.SendCommand (1 more) — 4 total. The saga's own dispatch to
	// entity B (sendCommand) never re-resolves: it reuses the
	// already-bound/reconstructed TenantContext on its ctx, which is what
	// the observedTenant assertion below proves.
	assert.EqualValues(t, 4, resolver.callCount(), "Resolve must be invoked once per spawn (entity A, entity B, the saga) plus once at Engine.SendCommand, never again downstream")

	tcA, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)
	wantTenant, err := tenancy.NewTenantContext(tcA)
	require.NoError(t, err)

	observed, ok := entityB.observedTenant()
	require.True(t, ok, "entity B's HandleCommand must observe a TenantContext attached to its ctx")
	assert.Equal(t, wantTenant, observed, "entity B must observe the same tenant entity A's command was resolved under")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineSagaStatusTenantIsolation covers the PR#78 review round 2 P1
// finding that Engine.SagaStatus sent its query under the caller's raw ctx,
// never resolving or attaching a TenantContext, so SagaActor's own gate
// (checkStateReadTenant) always saw a tenant-less ctx: a tenant-aware
// resolver would reject every caller, tenant-matching or not, defeating the
// isolation checkStateReadTenant is supposed to enforce.
func TestEngineSagaStatusTenantIsolation(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	resolver := &countingTenantResolver{id: "acme"}
	engine := newTestEngine(t, "SagaStatusTenantIsolation", store, WithTenantResolver(resolver))
	require.NoError(t, engine.Start(ctx))

	entityAID := uuid.NewString()
	entityA := NewAccountEventSourcedBehavior(entityAID)
	require.NoError(t, engine.Entity(ctx, entityA))

	sagaID := "saga-" + uuid.NewString()
	saga := &callbackSagaBehavior{
		id: sagaID,
		handleEvent: func(_ context.Context, event Event, _ State) (*SagaAction, error) {
			created, ok := event.(*testpb.AccountCreated)
			if !ok || created.GetAccountId() != entityAID {
				return &SagaAction{}, nil
			}
			return &SagaAction{Complete: true}, nil
		},
	}
	require.NoError(t, engine.Saga(ctx, saga, 0))

	_, _, err := engine.SendCommand(ctx, entityAID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, statusErr := engine.SagaStatus(ctx, sagaID, 5*time.Second)
		return statusErr == nil
	}, 10*time.Second, 50*time.Millisecond, "the saga must bind to acme via the triggering event before SagaStatus can succeed")

	t.Run("the tenant the saga bound to can read its own status", func(t *testing.T) {
		info, err := engine.SagaStatus(ctx, sagaID, 5*time.Second)
		require.NoError(t, err, "SagaStatus must resolve and attach the caller's tenant, not send the query under a bare ctx")
		assert.NotNil(t, info)
	})

	t.Run("a different resolved tenant is rejected, not just any tenant-less caller", func(t *testing.T) {
		resolver.id = "globex"
		defer func() { resolver.id = "acme" }()

		_, err := engine.SagaStatus(ctx, sagaID, 5*time.Second)
		require.Error(t, err, "SagaStatus for a saga bound to a different tenant must be rejected")
	})

	require.NoError(t, engine.Stop(ctx))
}
