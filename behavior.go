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

	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"

	"github.com/pablogore/ego/v4/command"
	behaviorport "github.com/pablogore/ego/v4/port/behavior"
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

// EventSourcedBehavior defines an event-sourced behavior when modeling a CQRS EventSourcedBehavior.
//
// It is the runtime-neutral contract [behaviorport.EventSourced] from package
// port/behavior plus GoAkt's extension.Dependency, which adds MarshalBinary
// and UnmarshalBinary so GoAkt can copy the behavior to another cluster node.
// Its method set is the same as before port/behavior existed; the domain
// methods and their documentation live on [behaviorport.EventSourced].
//
// Deprecated: implement [behaviorport.EventSourced] from port/behavior and
// spawn with [Engine.SpawnEventSourced]. Removed in the next major release
// (#124).
type EventSourcedBehavior interface {
	behaviorport.EventSourced
	extension.Dependency
}

// EventSourcedEnvelopeBehavior is an additive, optional extension to
// EventSourcedBehavior (#60, EGO-WRITE-00x runtime integration). A behavior
// that also implements this interface receives the full command.Envelope —
// the command together with its Metadata (operation/correlation/causation
// IDs, timestamp) — instead of the bare Command. EventSourcedBehavior's
// HandleCommand remains mandatory and is called unchanged whenever no
// Metadata is available for the incoming command (e.g. the entity was
// reached directly rather than through Engine.Dispatch/SendCommand), so
// existing behaviors that do not implement this interface are unaffected.
//
// Deprecated: implement [behaviorport.EventSourcedEnvelope] from port/behavior
// and spawn with [Engine.SpawnEventSourced]. Removed in the next major release
// (#124).
type EventSourcedEnvelopeBehavior interface {
	EventSourcedBehavior
	// HandleEnvelope is like HandleCommand but receives env, the full
	// command.Envelope rematerialized on the actor side of a local dispatch
	// (see command_context.go). It is preferred over HandleCommand whenever
	// Metadata is available.
	HandleEnvelope(ctx context.Context, env command.Envelope, priorState State) (events []Event, err error)
}

// DurableStateBehavior represents a type of Actor that persists its full state after processing each command instead of using event sourcing.
// This type of Actor keeps its current state in memory during command handling and based upon the command response
// persists its full state into a durable store. The store can be a SQL or NoSQL database.
// The whole concept is given the current state of the actor and a command produce a new state with a higher version as shown in this diagram: (State, Command) => State
// DurableStateBehavior reacts to commands which result in a new version of the actor state. Only the latest version of the actor state is
// persisted to the durable store. There is no concept of history regarding the actor state since this is not an event sourced actor.
// However, one can rely on the version number of the actor state and exactly know how the actor state has evolved overtime.
// State actor version number are numerically incremented by the command handler which means it is imperative that the newer version of the state is greater than the current version by one.
//
// DurableStateBehavior will attempt to recover its state whenever available from the durable state.
// During a normal shutdown process, it will persist its current state to the durable store prior to shutting down.
// This behavior help maintain some consistency across the actor state evolution.
//
// It is the runtime-neutral contract [behaviorport.DurableState] from package
// port/behavior plus GoAkt's extension.Dependency, which adds MarshalBinary
// and UnmarshalBinary so GoAkt can copy the behavior to another cluster node.
// Its method set is the same as before port/behavior existed; the domain
// methods and their documentation live on [behaviorport.DurableState].
//
// Deprecated: implement [behaviorport.DurableState] from port/behavior and
// spawn with [Engine.SpawnDurableState]. Removed in the next major release
// (#124).
type DurableStateBehavior interface {
	behaviorport.DurableState
	extension.Dependency
}

// DurableStateEnvelopeBehavior is an additive, optional extension to
// DurableStateBehavior (#60, EGO-WRITE-00x runtime integration), mirroring
// EventSourcedEnvelopeBehavior: a behavior that also implements this
// interface receives the full command.Envelope instead of the bare Command.
// DurableStateBehavior's HandleCommand remains mandatory and is called
// unchanged whenever no Metadata is available for the incoming command.
//
// Deprecated: implement [behaviorport.DurableStateEnvelope] from port/behavior
// and spawn with [Engine.SpawnDurableState]. Removed in the next major release
// (#124).
type DurableStateEnvelopeBehavior interface {
	DurableStateBehavior
	// HandleEnvelope is like HandleCommand but receives env, the full
	// command.Envelope rematerialized on the actor side of a local dispatch
	// (see command_context.go). It is preferred over HandleCommand whenever
	// Metadata is available.
	HandleEnvelope(ctx context.Context, env command.Envelope, priorVersion uint64, priorState State) (newState State, newVersion uint64, err error)
}
