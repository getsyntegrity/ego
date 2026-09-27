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

package behavior_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/port/behavior"
)

// The types below implement only ID() and the domain methods: no
// MarshalBinary, no UnmarshalBinary, nothing from GoAkt. They must satisfy the
// neutral contracts, which is the point of port/behavior (#123 criterion 1).
// Value receivers are used on purpose: a contract does not require a behavior
// to be a pointer.

type eventSourced struct{ id string }

func (e eventSourced) ID() string                 { return e.id }
func (eventSourced) InitialState() behavior.State { return new(emptypb.Empty) }
func (eventSourced) HandleCommand(context.Context, behavior.Command, behavior.State) ([]behavior.Event, error) {
	return nil, nil
}
func (eventSourced) HandleEvent(_ context.Context, _ behavior.Event, prior behavior.State) (behavior.State, error) {
	return prior, nil
}

type eventSourcedEnvelope struct{ eventSourced }

func (eventSourcedEnvelope) HandleEnvelope(context.Context, command.Envelope, behavior.State) ([]behavior.Event, error) {
	return nil, nil
}

type durableState struct{ id string }

func (d durableState) ID() string                 { return d.id }
func (durableState) InitialState() behavior.State { return new(emptypb.Empty) }
func (durableState) HandleCommand(_ context.Context, _ behavior.Command, v uint64, s behavior.State) (behavior.State, uint64, error) {
	return s, v + 1, nil
}

type durableStateEnvelope struct{ durableState }

func (durableStateEnvelope) HandleEnvelope(_ context.Context, _ command.Envelope, v uint64, s behavior.State) (behavior.State, uint64, error) {
	return s, v + 1, nil
}

type saga struct{ id string }

func (s saga) ID() string                 { return s.id }
func (saga) InitialState() behavior.State { return new(emptypb.Empty) }
func (saga) HandleEvent(context.Context, behavior.Event, behavior.State) (*behavior.SagaAction, error) {
	return &behavior.SagaAction{Complete: true}, nil
}
func (saga) HandleResult(context.Context, string, behavior.State, behavior.State) (*behavior.SagaAction, error) {
	return nil, nil
}
func (saga) HandleError(context.Context, string, error, behavior.State) (*behavior.SagaAction, error) {
	return &behavior.SagaAction{Compensate: true}, nil
}
func (saga) ApplyEvent(_ context.Context, _ behavior.Event, s behavior.State) (behavior.State, error) {
	return s, nil
}
func (saga) Compensate(context.Context, behavior.State) ([]behavior.SagaCommand, error) {
	return []behavior.SagaCommand{{EntityID: "account-1", Command: new(emptypb.Empty), Timeout: time.Second}}, nil
}

var (
	_ behavior.EventSourced         = eventSourced{}
	_ behavior.EventSourcedEnvelope = eventSourcedEnvelope{}
	_ behavior.DurableState         = durableState{}
	_ behavior.DurableStateEnvelope = durableStateEnvelope{}
	_ behavior.Saga                 = saga{}

	// An envelope-capable behavior is still a plain behavior.
	_ behavior.EventSourced = behavior.EventSourcedEnvelope(nil)
	_ behavior.DurableState = behavior.DurableStateEnvelope(nil)
)

// TestDomainOnlyBehaviorsRunThroughTheContracts calls the contract methods
// through the interfaces, so the assertions above are exercised at run time
// and not only at compile time.
func TestDomainOnlyBehaviorsRunThroughTheContracts(t *testing.T) {
	ctx := context.Background()

	var es behavior.EventSourced = eventSourcedEnvelope{eventSourced{id: "es-1"}}
	require.Equal(t, "es-1", es.ID())
	events, err := es.HandleCommand(ctx, new(emptypb.Empty), es.InitialState())
	require.NoError(t, err)
	require.Empty(t, events)
	_, isEnvelope := es.(behavior.EventSourcedEnvelope)
	require.True(t, isEnvelope, "the envelope extension must be discoverable by type assertion")

	var ds behavior.DurableState = durableState{id: "ds-1"}
	_, version, err := ds.HandleCommand(ctx, new(emptypb.Empty), 0, ds.InitialState())
	require.NoError(t, err)
	require.Equal(t, uint64(1), version)
	_, isEnvelope = ds.(behavior.DurableStateEnvelope)
	require.False(t, isEnvelope, "a behavior without HandleEnvelope must not satisfy the envelope extension")

	var sg behavior.Saga = saga{id: "saga-1"}
	action, err := sg.HandleEvent(ctx, new(emptypb.Empty), sg.InitialState())
	require.NoError(t, err)
	require.True(t, action.Complete)
	compensation, err := sg.Compensate(ctx, sg.InitialState())
	require.NoError(t, err)
	require.Len(t, compensation, 1)
	require.Equal(t, "account-1", compensation[0].EntityID)
}
