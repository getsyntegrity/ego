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

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/port/behavior"
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
	specs.Describe(t, "Domain-only behaviors run through the neutral contracts", func(s *specs.Spec) {
		s.It("runs an event-sourced, a durable-state and a saga behavior through their interfaces", func(ctx *specs.Context) {
			bg := context.Background()

			var es behavior.EventSourced = eventSourcedEnvelope{eventSourced{id: "es-1"}}
			ctx.Expect(es.ID()).ToEqual("es-1")
			events, err := es.HandleCommand(bg, new(emptypb.Empty), es.InitialState())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(events)).ToEqual(0)
			_, isEnvelope := es.(behavior.EventSourcedEnvelope)
			ctx.Expect(isEnvelope).To(specs.BeTrue())

			var ds behavior.DurableState = durableState{id: "ds-1"}
			_, version, err := ds.HandleCommand(bg, new(emptypb.Empty), 0, ds.InitialState())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(version).ToEqual(uint64(1))
			_, isEnvelope = ds.(behavior.DurableStateEnvelope)
			ctx.Expect(isEnvelope).To(specs.BeFalse())

			var sg behavior.Saga = saga{id: "saga-1"}
			action, err := sg.HandleEvent(bg, new(emptypb.Empty), sg.InitialState())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(action.Complete).To(specs.BeTrue())
			compensation, err := sg.Compensate(bg, sg.InitialState())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(len(compensation)).ToEqual(1)
			ctx.Expect(compensation[0].EntityID).ToEqual("account-1")
		})
	})
}
