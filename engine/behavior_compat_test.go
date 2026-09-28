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
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tochemey/goakt/v4/extension"

	"github.com/getsyntegrity/ego/v4/command"
	behaviorport "github.com/getsyntegrity/ego/v4/port/behavior"
)

// The old behavior names in package engine are re-expressed on top of the
// runtime-neutral contracts in port/behavior (#123, slice S3-1 of
// openspec/changes/ego-arch-002-s3/design.md). Their method sets must not
// change inside v4, because callers use them as extension.Dependency values
// (design.md §3 and §5.2).
//
// Two interface types are mutually assignable exactly when their method sets
// are identical, so each pair of assertions below pins a method set in both
// directions.

// The *At77beda6 interfaces spell out each old contract as it was declared at
// the design baseline (main at 77beda6), without referring to port/behavior.
// They pin the old method sets independently of how the old names are
// declared now.
type (
	eventSourcedBehaviorAt77beda6 interface {
		extension.Dependency
		InitialState() State
		HandleCommand(ctx context.Context, command Command, priorState State) (events []Event, err error)
		HandleEvent(ctx context.Context, event Event, priorState State) (state State, err error)
	}
	eventSourcedEnvelopeBehaviorAt77beda6 interface {
		eventSourcedBehaviorAt77beda6
		HandleEnvelope(ctx context.Context, env command.Envelope, priorState State) (events []Event, err error)
	}
	durableStateBehaviorAt77beda6 interface {
		extension.Dependency
		InitialState() State
		HandleCommand(ctx context.Context, command Command, priorVersion uint64, priorState State) (newState State, newVersion uint64, err error)
	}
	durableStateEnvelopeBehaviorAt77beda6 interface {
		durableStateBehaviorAt77beda6
		HandleEnvelope(ctx context.Context, env command.Envelope, priorVersion uint64, priorState State) (newState State, newVersion uint64, err error)
	}
	sagaBehaviorAt77beda6 interface {
		extension.Dependency
		ID() string
		InitialState() State
		HandleEvent(ctx context.Context, event Event, state State) (*SagaAction, error)
		HandleResult(ctx context.Context, entityID string, result State, sagaState State) (*SagaAction, error)
		HandleError(ctx context.Context, entityID string, err error, sagaState State) (*SagaAction, error)
		ApplyEvent(ctx context.Context, event Event, state State) (State, error)
		Compensate(ctx context.Context, state State) ([]SagaCommand, error)
	}
)

// Each old name has the method set of its neutral contract plus
// extension.Dependency, and nothing else.
var (
	_ EventSourcedBehavior = interface {
		behaviorport.EventSourced
		extension.Dependency
	}(nil)
	_ interface {
		behaviorport.EventSourced
		extension.Dependency
	} = EventSourcedBehavior(nil)
	_ EventSourcedEnvelopeBehavior = interface {
		behaviorport.EventSourcedEnvelope
		extension.Dependency
	}(nil)
	_ interface {
		behaviorport.EventSourcedEnvelope
		extension.Dependency
	} = EventSourcedEnvelopeBehavior(nil)
	_ DurableStateBehavior = interface {
		behaviorport.DurableState
		extension.Dependency
	}(nil)
	_ interface {
		behaviorport.DurableState
		extension.Dependency
	} = DurableStateBehavior(nil)
	_ DurableStateEnvelopeBehavior = interface {
		behaviorport.DurableStateEnvelope
		extension.Dependency
	}(nil)
	_ interface {
		behaviorport.DurableStateEnvelope
		extension.Dependency
	} = DurableStateEnvelopeBehavior(nil)
	_ SagaBehavior = interface {
		behaviorport.Saga
		extension.Dependency
	}(nil)
	_ interface {
		behaviorport.Saga
		extension.Dependency
	} = SagaBehavior(nil)
)

// Each old name keeps the method set it had at the design baseline.
var (
	_ EventSourcedBehavior                  = eventSourcedBehaviorAt77beda6(nil)
	_ eventSourcedBehaviorAt77beda6         = EventSourcedBehavior(nil)
	_ EventSourcedEnvelopeBehavior          = eventSourcedEnvelopeBehaviorAt77beda6(nil)
	_ eventSourcedEnvelopeBehaviorAt77beda6 = EventSourcedEnvelopeBehavior(nil)
	_ DurableStateBehavior                  = durableStateBehaviorAt77beda6(nil)
	_ durableStateBehaviorAt77beda6         = DurableStateBehavior(nil)
	_ DurableStateEnvelopeBehavior          = durableStateEnvelopeBehaviorAt77beda6(nil)
	_ durableStateEnvelopeBehaviorAt77beda6 = DurableStateEnvelopeBehavior(nil)
	_ SagaBehavior                          = sagaBehaviorAt77beda6(nil)
	_ sagaBehaviorAt77beda6                 = SagaBehavior(nil)
)

// SagaAction and SagaCommand are aliases, not copies: assignability between
// pointer, slice and function types requires identical types.
var (
	_ *behaviorport.SagaAction                                  = (*SagaAction)(nil)
	_ *SagaAction                                               = (*behaviorport.SagaAction)(nil)
	_ []behaviorport.SagaCommand                                = []SagaCommand(nil)
	_ []SagaCommand                                             = []behaviorport.SagaCommand(nil)
	_ func(*SagaAction) []SagaCommand                           = func(*behaviorport.SagaAction) []behaviorport.SagaCommand { return nil }
	_ func(*behaviorport.SagaAction) []behaviorport.SagaCommand = func(*SagaAction) []SagaCommand { return nil }
)

func TestSagaActionAndSagaCommandAreAliases(t *testing.T) {
	require.Equal(t, reflect.TypeFor[behaviorport.SagaAction](), reflect.TypeFor[SagaAction]())
	require.Equal(t, reflect.TypeFor[behaviorport.SagaCommand](), reflect.TypeFor[SagaCommand]())

	// A literal written against the old names is a value of the new type.
	var action any = &SagaAction{
		Commands: []SagaCommand{{EntityID: "account-1", Timeout: time.Second}},
		Complete: true,
	}
	moved, ok := action.(*behaviorport.SagaAction)
	require.True(t, ok)
	require.Equal(t, "account-1", moved.Commands[0].EntityID)
}

// TestSagaActionIsNoop covers the helper that replaced the unexported method
// (*SagaAction).isNoop, which cannot stay a method once SagaAction is declared
// in port/behavior (design.md §5.2).
func TestSagaActionIsNoop(t *testing.T) {
	testCases := []struct {
		name   string
		action *SagaAction
		noop   bool
	}{
		{name: "nil action", action: nil, noop: true},
		{name: "empty action", action: &SagaAction{}, noop: true},
		{name: "empty slices", action: &SagaAction{Commands: []SagaCommand{}, Events: []Event{}}, noop: true},
		{name: "command", action: &SagaAction{Commands: []SagaCommand{{EntityID: "a"}}}, noop: false},
		{name: "event", action: &SagaAction{Events: []Event{nil}}, noop: false},
		{name: "complete", action: &SagaAction{Complete: true}, noop: false},
		{name: "compensate", action: &SagaAction{Compensate: true}, noop: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.noop, sagaActionIsNoop(tc.action))
		})
	}
}
