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
