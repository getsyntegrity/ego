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

// Package behavior defines the runtime-neutral contracts for the domain code
// a user writes for Ego: the command and event handlers of an event-sourced
// entity (EventSourced), of a durable state entity (DurableState), and of a
// saga (Saga).
//
// It is a contract package: it depends only on the standard library, the
// protobuf runtime and the command contract, never on the GoAkt runtime. A
// behavior written against these interfaces needs only ID() and its domain
// methods; it does not have to implement MarshalBinary or UnmarshalBinary.
//
// Package ego keeps its older names on top of these contracts:
// ego.EventSourcedBehavior is behavior.EventSourced plus GoAkt's
// extension.Dependency, and the same holds for ego.DurableStateBehavior and
// ego.SagaBehavior. ego.SagaAction and ego.SagaCommand are aliases of the types
// declared here.
package behavior

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/v4/command"
)

// Command is a command sent to an entity. It is an alias for [proto.Message],
// so a behavior may be written in terms of either name.
type Command = proto.Message

// Event is an event persisted by an entity. It is an alias for [proto.Message],
// so a behavior may be written in terms of either name.
type Event = proto.Message

// State is the state held by an entity. It is an alias for [proto.Message],
// so a behavior may be written in terms of either name.
type State = proto.Message

// EventSourced is the domain contract of an event-sourced entity when modeling
// CQRS: it decides which events a command produces and how each event changes
// the state.
type EventSourced interface {
	// ID returns the entity's persistence ID.
	ID() string
	// InitialState returns the event sourced actor initial state.
	// This is set as the initial state when there are no snapshots found the entity
	InitialState() State
	// HandleCommand helps handle commands received by the event sourced actor. The command handlers define how to handle each incoming command,
	// which validations must be applied, and finally, which events will be persisted if any. When there is no event to be persisted a nil can
	// be returned as a no-op. Command handlers are the meat of the event sourced actor.
	// They encode the business rules of your event sourced actor and act as a guardian of the event sourced actor consistency.
	// The command must first validate that the incoming command can be applied to the current model state.
	//  Any decision should be solely based on the data passed in the commands and the state of the Behavior.
	// In case of successful validation, one or more events expressing the mutations are persisted.
	// Once the events are persisted, they are applied to the state producing a new valid state.
	// Every event emitted are processed one after the other in the same order they were emitted to guarantee consistency.
	// It is at the discretion of the application developer to know in which order a given command should return the list of events
	// This is really powerful when a command needs to return two events. For instance, an OpenAccount command can result in two events: one is AccountOpened and the second is AccountCredited
	HandleCommand(ctx context.Context, command Command, priorState State) (events []Event, err error)
	// HandleEvent handle events emitted by the command handlers. The event handlers are used to mutate the state of the event sourced actor by applying the events to it.
	// Event handlers must be pure functions as they will be used when instantiating the event sourced actor and replaying the event journal.
	HandleEvent(ctx context.Context, event Event, priorState State) (state State, err error)
}

// EventSourcedEnvelope is an additive, optional extension to EventSourced
// (#60, EGO-WRITE-00x runtime integration). A behavior that also implements
// this interface receives the full command.Envelope — the command together
// with its Metadata (operation/correlation/causation IDs, timestamp) — instead
// of the bare Command. EventSourced's HandleCommand remains mandatory and is
// called unchanged whenever no Metadata is available for the incoming command
// (e.g. the entity was reached directly rather than through Engine.Dispatch or
// Engine.SendCommand), so behaviors that do not implement this interface are
// unaffected.
type EventSourcedEnvelope interface {
	EventSourced
	// HandleEnvelope is like HandleCommand but receives env, the full
	// command.Envelope rematerialized on the actor side of a local dispatch.
	// It is preferred over HandleCommand whenever Metadata is available.
	HandleEnvelope(ctx context.Context, env command.Envelope, priorState State) (events []Event, err error)
}

// DurableState is the domain contract of an entity that persists its full
// state after processing each command instead of using event sourcing.
// Given the current state and a command it produces a new state with a higher
// version: (State, Command) => State. Only the latest version of the state is
// persisted; there is no history, but the version number shows how the state
// has evolved over time. The command handler must increment the version by
// exactly one.
type DurableState interface {
	// ID returns the entity's persistence ID.
	ID() string
	// InitialState returns the durable state actor initial state.
	// This is set as the initial state when there are no snapshots found the entity
	InitialState() State
	// HandleCommand processes every command sent to the DurableState. One needs to use the command, the priorVersion and the priorState sent to produce a newState and newVersion.
	// This defines how to handle each incoming command, which validations must be applied, and finally, whether a resulting state will be persisted depending upon the response.
	// They encode the business rules of your durable state actor and act as a guardian of the actor consistency.
	// The command handler must first validate that the incoming command can be applied to the current model state.
	// Any decision should be solely based on the data passed in the command, the priorVersion and the priorState.
	// In case of successful validation and processing , the new state will be stored in the durable store depending upon response.
	// The actor state will be updated with the newState only if the newVersion is 1 more than the already existing state.
	HandleCommand(ctx context.Context, command Command, priorVersion uint64, priorState State) (newState State, newVersion uint64, err error)
}

// DurableStateEnvelope is an additive, optional extension to DurableState
// (#60, EGO-WRITE-00x runtime integration), mirroring EventSourcedEnvelope: a
// behavior that also implements this interface receives the full
// command.Envelope instead of the bare Command. DurableState's HandleCommand
// remains mandatory and is called unchanged whenever no Metadata is available
// for the incoming command.
type DurableStateEnvelope interface {
	DurableState
	// HandleEnvelope is like HandleCommand but receives env, the full
	// command.Envelope rematerialized on the actor side of a local dispatch.
	// It is preferred over HandleCommand whenever Metadata is available.
	HandleEnvelope(ctx context.Context, env command.Envelope, priorVersion uint64, priorState State) (newState State, newVersion uint64, err error)
}
