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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/discovery"
	"google.golang.org/protobuf/proto"

	samplepb "github.com/getsyntegrity/ego/example/examplepb"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/persistence"
)

// newTestEngine bootstraps a goakt.ActorSystem and a plugged-in eGo Engine
// the way callers are expected to do it post-refactor, and registers a
// t.Cleanup hook that stops both at the end of the test.
//
// Single-node, non-clustered. Cluster-mode tests build their actor system
// manually with goakt.NewClusterConfig().
func newTestEngine(t *testing.T, name string, eventsStore persistence.EventsStore, opts ...Option) *Engine {
	t.Helper()
	ctx := context.Background()

	cfg := NewConfig(eventsStore, opts...)
	sys, err := goakt.NewActorSystem(name, cfg.GoaktOptions()...)
	require.NoError(t, err)
	require.NoError(t, sys.Start(ctx))

	engine, err := NewEngine(sys, cfg)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = engine.Stop(context.Background())
		_ = sys.Stop(context.Background())
	})

	return engine
}

// mockClusterProvider is a discovery.Provider used by cluster-mode tests.
// It returns a static peers list, suitable for spinning up single-node
// "clusters" in tests.
type mockClusterProvider struct {
	id    string
	peers []string
}

var _ discovery.Provider = (*mockClusterProvider)(nil)

func (m *mockClusterProvider) ID() string                       { return m.id }
func (m *mockClusterProvider) Initialize() error                { return nil }
func (m *mockClusterProvider) Register() error                  { return nil }
func (m *mockClusterProvider) Deregister() error                { return nil }
func (m *mockClusterProvider) DiscoverPeers() ([]string, error) { return m.peers, nil }
func (m *mockClusterProvider) Close() error                     { return nil }

type testSagaBehavior struct {
	sagaID   string
	entityID string
}

var _ SagaBehavior = (*testSagaBehavior)(nil)

func (s *testSagaBehavior) ID() string {
	return s.sagaID
}

func (s *testSagaBehavior) InitialState() State {
	return new(samplepb.Account)
}

func (s *testSagaBehavior) HandleEvent(_ context.Context, _ Event, _ State) (*SagaAction, error) {
	return &SagaAction{}, nil
}

func (s *testSagaBehavior) HandleResult(_ context.Context, _ string, _ State, _ State) (*SagaAction, error) {
	return &SagaAction{Complete: true}, nil
}

func (s *testSagaBehavior) HandleError(_ context.Context, _ string, _ error, _ State) (*SagaAction, error) {
	return &SagaAction{Compensate: true}, nil
}

func (s *testSagaBehavior) ApplyEvent(_ context.Context, _ Event, state State) (State, error) {
	return state, nil
}

func (s *testSagaBehavior) Compensate(_ context.Context, _ State) ([]SagaCommand, error) {
	return nil, nil
}

func (s *testSagaBehavior) MarshalBinary() ([]byte, error) {
	data := struct {
		SagaID   string `json:"saga_id"`
		EntityID string `json:"entity_id"`
	}{
		SagaID:   s.sagaID,
		EntityID: s.entityID,
	}
	return json.Marshal(data)
}

func (s *testSagaBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		SagaID   string `json:"saga_id"`
		EntityID string `json:"entity_id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	s.sagaID = aux.SagaID
	s.entityID = aux.EntityID
	return nil
}

// callbackSagaBehavior is a configurable SagaBehavior for testing
// that delegates each method to a user-supplied function field.
type callbackSagaBehavior struct {
	id           string
	initialState func() State
	handleEvent  func(ctx context.Context, event Event, state State) (*SagaAction, error)
	handleResult func(ctx context.Context, entityID string, result State, sagaState State) (*SagaAction, error)
	handleError  func(ctx context.Context, entityID string, err error, sagaState State) (*SagaAction, error)
	applyEvent   func(ctx context.Context, event Event, state State) (State, error)
	compensate   func(ctx context.Context, state State) ([]SagaCommand, error)
}

var _ SagaBehavior = (*callbackSagaBehavior)(nil)

func (c *callbackSagaBehavior) ID() string { return c.id }

func (c *callbackSagaBehavior) InitialState() State {
	if c.initialState != nil {
		return c.initialState()
	}
	return new(samplepb.Account)
}

func (c *callbackSagaBehavior) HandleEvent(ctx context.Context, event Event, state State) (*SagaAction, error) {
	if c.handleEvent != nil {
		return c.handleEvent(ctx, event, state)
	}
	return &SagaAction{}, nil
}

func (c *callbackSagaBehavior) HandleResult(ctx context.Context, entityID string, result State, sagaState State) (*SagaAction, error) {
	if c.handleResult != nil {
		return c.handleResult(ctx, entityID, result, sagaState)
	}
	return &SagaAction{Complete: true}, nil
}

func (c *callbackSagaBehavior) HandleError(ctx context.Context, entityID string, err error, sagaState State) (*SagaAction, error) {
	if c.handleError != nil {
		return c.handleError(ctx, entityID, err, sagaState)
	}
	return &SagaAction{Compensate: true}, nil
}

func (c *callbackSagaBehavior) ApplyEvent(ctx context.Context, event Event, state State) (State, error) {
	if c.applyEvent != nil {
		return c.applyEvent(ctx, event, state)
	}
	return state, nil
}

func (c *callbackSagaBehavior) Compensate(ctx context.Context, state State) ([]SagaCommand, error) {
	if c.compensate != nil {
		return c.compensate(ctx, state)
	}
	return nil, nil
}

func (c *callbackSagaBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(c.id)
}

func (c *callbackSagaBehavior) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, &c.id)
}

// simpleReplyActor is a test actor that responds to any message with a fixed reply.
type simpleReplyActor struct {
	reply proto.Message
}

var _ goakt.Actor = (*simpleReplyActor)(nil)

func (a *simpleReplyActor) PreStart(_ *goakt.Context) error   { return nil }
func (a *simpleReplyActor) PostStop(_ *goakt.Context) error   { return nil }
func (a *simpleReplyActor) Receive(ctx *goakt.ReceiveContext) { ctx.Response(a.reply) }

// ensure time is used
var _ = time.Second

// The event sourced fixtures live in enginetest so that the actor packages
// share one definition with these tests.
type (
	AccountEventSourcedBehavior      = enginetest.AccountEventSourcedBehavior
	tenancyProbeEventSourcedBehavior = enginetest.TenancyProbeEventSourcedBehavior
)

var (
	NewAccountEventSourcedBehavior      = enginetest.NewAccountEventSourcedBehavior
	newTenancyProbeEventSourcedBehavior = enginetest.NewTenancyProbeEventSourcedBehavior
)

type FailingHandleEventBehavior = enginetest.FailingHandleEventBehavior

var (
	newEnvelopeCapturingEventSourcedBehavior = enginetest.NewEnvelopeCapturingEventSourcedBehavior
)

type envelopeCapturingEventSourcedBehavior = enginetest.EnvelopeCapturingEventSourcedBehavior

type (
	AccountDurableStateBehavior      = enginetest.AccountDurableStateBehavior
	tenancyProbeDurableStateBehavior = enginetest.TenancyProbeDurableStateBehavior
)

var (
	NewAccountDurableStateBehavior      = enginetest.NewAccountDurableStateBehavior
	newTenancyProbeDurableStateBehavior = enginetest.NewTenancyProbeDurableStateBehavior
)

type envelopeCapturingDurableStateBehavior = enginetest.EnvelopeCapturingDurableStateBehavior

var newEnvelopeCapturingDurableStateBehavior = enginetest.NewEnvelopeCapturingDurableStateBehavior
