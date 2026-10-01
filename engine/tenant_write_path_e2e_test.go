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

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/tenancy"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
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
	specs.Describe(t, "a command resolved at Engine.SendCommand keeps its tenant across the saga hop to another entity", func(s *specs.Spec) {
		s.It("reaches entity B's HandleCommand under the same tenant", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedEventsStore(ctx)

			resolver := &countingTenantResolver{id: "acme"}
			engine := newSpecsEngine(ctx, "TenantWritePathE2E", store, WithTenantResolver(resolver))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityAID := uuid.NewString()
			entityBID := uuid.NewString()

			entityA := NewAccountEventSourcedBehavior(entityAID)
			ctx.Expect(engine.Entity(bg, entityA, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			entityB := newTenancyProbeCreditEventSourcedBehavior(entityBID)
			ctx.Expect(engine.Entity(bg, entityB, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			sagaID := "saga-" + uuid.NewString()
			saga := &enginetest.CallbackSagaBehavior{
				SagaID: sagaID,
				HandleEventFn: func(_ context.Context, event Event, state State) (*SagaAction, error) {
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
				HandleResultFn: func(_ context.Context, _ string, _ State, _ State) (*SagaAction, error) {
					return &SagaAction{Complete: true}, nil
				},
			}
			ctx.Expect(engine.Saga(bg, saga, 0, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			_, _, err := engine.SendCommand(bg, entityAID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())

			// Entity B's HandleCommand must eventually run via the saga hop.
			ctx.Eventually(func() any { return entityB.invocationCount() }, specs.Equal(1),
				specs.WithTimeout(waitTimeout), specs.WithInterval(50*time.Millisecond))

			// TENANT-003 T4 (corrected): Engine.Entity/Engine.Saga never call
			// Resolve at spawn — entity A, entity B, and the saga each declare their
			// tenant via engine.WithTenant instead. Only Engine.SendCommand's own
			// resolve, at the command trust boundary, counts here. The saga's own
			// dispatch to entity B (sendCommand) never re-resolves either: it
			// reuses the already-bound/reconstructed TenantContext on its ctx,
			// which is what the observedTenant assertion below proves.
			ctx.Expect(resolver.callCount()).To(specs.Equal(int64(1)))

			tcA, err := tenancy.NewTenantID("acme")
			ctx.Expect(err).To(specs.BeNil())
			wantTenant, err := tenancy.NewTenantContext(tcA)
			ctx.Expect(err).To(specs.BeNil())

			// Entity B must observe a TenantContext, and the same one entity A's
			// command was resolved under.
			observed, ok := entityB.observedTenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(observed).ToEqual(wantTenant)

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}

// TestEngineSagaStatusTenantIsolation covers the PR#78 review round 2 P1
// finding that Engine.SagaStatus sent its query under the caller's raw ctx,
// never resolving or attaching a TenantContext, so SagaActor's own gate
// (checkStateReadTenant) always saw a tenant-less ctx: a tenant-aware
// resolver would reject every caller, tenant-matching or not, defeating the
// isolation checkStateReadTenant is supposed to enforce.
func TestEngineSagaStatusTenantIsolation(t *testing.T) {
	specs.Describe(t, "Engine.SagaStatus resolves and attaches the caller's tenant", func(s *specs.Spec) {
		bg := context.Background()

		var (
			engine   *Engine
			resolver *countingTenantResolver
			sagaID   string
		)

		s.BeforeEach(func(ctx *specs.Context) {
			store := connectedEventsStore(ctx)

			resolver = &countingTenantResolver{id: "acme"}
			engine = newSpecsEngine(ctx, "SagaStatusTenantIsolation", store, WithTenantResolver(resolver))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			entityAID := uuid.NewString()
			entityA := NewAccountEventSourcedBehavior(entityAID)
			ctx.Expect(engine.Entity(bg, entityA, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			sagaID = "saga-" + uuid.NewString()
			saga := &enginetest.CallbackSagaBehavior{
				SagaID: sagaID,
				HandleEventFn: func(_ context.Context, event Event, _ State) (*SagaAction, error) {
					created, ok := event.(*testpb.AccountCreated)
					if !ok || created.GetAccountId() != entityAID {
						return &SagaAction{}, nil
					}
					return &SagaAction{Complete: true}, nil
				},
			}
			ctx.Expect(engine.Saga(bg, saga, 0, WithTenant(tenancy.TenantID("acme")))).To(specs.BeNil())

			_, _, err := engine.SendCommand(bg, entityAID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())

			// The saga must bind to acme via the triggering event before SagaStatus can succeed.
			ctx.Eventually(func() any {
				_, statusErr := engine.SagaStatus(bg, sagaID, 5*time.Second)
				return statusErr
			}, specs.BeNil(), specs.WithTimeout(waitTimeout), specs.WithInterval(50*time.Millisecond))
		})

		s.It("the tenant the saga bound to can read its own status", func(ctx *specs.Context) {
			// SagaStatus must resolve and attach the caller's tenant, not send the query under a bare ctx.
			info, err := engine.SagaStatus(bg, sagaID, 5*time.Second)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(info).To(specs.Not(specs.BeNil()))

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})

		s.It("a different resolved tenant is rejected, not just any tenant-less caller", func(ctx *specs.Context) {
			resolver.id = "globex"
			ctx.Cleanup(func() { resolver.id = "acme" })

			// SagaStatus for a saga bound to a different tenant must be rejected.
			_, err := engine.SagaStatus(bg, sagaID, 5*time.Second)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))

			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}
