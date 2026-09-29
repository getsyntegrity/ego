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
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/internal/engine/durablestate"
	"github.com/getsyntegrity/ego/internal/engine/eventsource"
)

// The four actor types below are the cluster kinds of eGo. Each one stays
// declared in package engine and delegates to an implementation in an internal
// package, and that is deliberate: GoAkt names an actor kind
// lower(reflect.TypeOf(actor).Elem().String()), so EventSourcedActor travels
// as "engine.eventsourcedactor" in the spawn, relocation and singleton records
// it ships between nodes. Moving the type to another package would rename the
// kind and break a cluster that runs two versions during a rolling upgrade.
// TestClusterKindsExposesEgoActors pins the names.
//
// Each type is a struct with one unexported value field, so new(T) and
// reflect.New(T) yield a ready zero value, exactly like the types they wrap.

// EventSourcedActor persists state changes as a sequence of immutable events.
//
// Command processing generates events that are persisted through a child
// events writer actor before the in-memory state is updated. This guarantees
// that the actor state always matches what is stored.
//
// The persistence write is dispatched asynchronously via goakt's PipeTo, so
// the actor's dispatcher worker is never blocked waiting on the writer child.
// The originating command is stashed and redelivered once the write completes,
// preserving command ordering and preventing concurrent state mutations
// without holding a worker idle.
//
// Snapshots and retention cleanup are handled asynchronously by dedicated child
// actors and never add latency to command processing.
//
// The implementation lives in internal/engine/eventsource.
type EventSourcedActor struct {
	impl eventsource.Actor
}

var _ goakt.Actor = (*EventSourcedActor)(nil)

// PreStart loads extensions and dependencies, validates configuration, and
// recovers the actor state from the events and snapshot stores.
func (a *EventSourcedActor) PreStart(ctx *goakt.Context) error {
	return a.impl.PreStart(ctx)
}

// Receive handles the messages sent to the actor: commands, state queries and
// the replies of its persistence children.
func (a *EventSourcedActor) Receive(ctx *goakt.ReceiveContext) {
	a.impl.Receive(ctx)
}

// PostStop releases the actor's resources when it stops.
func (a *EventSourcedActor) PostStop(ctx *goakt.Context) error {
	return a.impl.PostStop(ctx)
}

// DurableStateActor is a durable state based actor.
//
// The implementation lives in internal/engine/durablestate.
type DurableStateActor struct {
	impl durablestate.Actor
}

var _ goakt.Actor = (*DurableStateActor)(nil)

// PreStart loads extensions and dependencies, validates configuration, and
// recovers the actor state from the durable state store.
func (a *DurableStateActor) PreStart(ctx *goakt.Context) error {
	return a.impl.PreStart(ctx)
}

// Receive handles the commands and state queries sent to the actor.
func (a *DurableStateActor) Receive(ctx *goakt.ReceiveContext) {
	a.impl.Receive(ctx)
}

// PostStop releases the actor's resources when it stops.
func (a *DurableStateActor) PostStop(ctx *goakt.Context) error {
	return a.impl.PostStop(ctx)
}

// ClusterKinds returns the actor kinds eGo needs registered in the cluster
// configuration so that entity, durable-state, saga, and projection actors
// can be relocated across nodes.
//
// Plug them into goakt.NewClusterConfig().WithKinds(...) alongside any
// caller-defined kinds. Has no effect in single-node deployments.
//
// ClusterKinds covers the actor types only. The behaviors those actors are
// spawned with travel as dependencies and need their own registration on
// every node — see WithBehaviorKinds.
func ClusterKinds() []goakt.Actor {
	return []goakt.Actor{
		new(EventSourcedActor),
		new(DurableStateActor),
		new(SagaActor),
		new(ProjectionActor),
	}
}
