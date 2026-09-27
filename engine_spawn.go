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
	"time"

	behaviorport "github.com/pablogore/ego/v4/port/behavior"
)

// SpawnEventSourced spawns an event-sourced entity for b, a behavior written
// against the runtime-neutral contract in port/behavior. It is Entity for
// that contract and shares its spawn path and options: the entity handles
// commands, persists the resulting events and rebuilds its state from them.
// Send it commands with SendCommand.
//
// b does not need MarshalBinary/UnmarshalBinary on a single node. In cluster
// mode GoAkt serializes every spawn so it can place the entity on any node,
// so b must then be a non-nil pointer that implements
// encoding.BinaryMarshaler and encoding.BinaryUnmarshaler, and its type must
// be registered with WithEntityKinds on every node that may host it.
// Otherwise SpawnEventSourced returns a *BehaviorPlacementError wrapping
// ErrBehaviorNotSerializable or ErrBehaviorNotPointer, before anything is
// spawned. A nil behavior is rejected the same way in every mode.
func (engine *Engine) SpawnEventSourced(ctx context.Context, b behaviorport.EventSourced, opts ...SpawnOption) error {
	return engine.spawnEventSourced(ctx, b, opts...)
}

// SpawnDurableState spawns a durable-state entity for b, a behavior written
// against the runtime-neutral contract in port/behavior. It is
// DurableStateEntity for that contract and shares its spawn path and
// options: the entity handles commands and persists only its latest state.
// It requires a durable state store (WithStateStore) and returns
// ErrDurableStateStoreRequired without one.
//
// Placement in cluster mode follows the same rules as SpawnEventSourced: a
// behavior GoAkt cannot serialize is rejected with a *BehaviorPlacementError
// before anything is spawned.
func (engine *Engine) SpawnDurableState(ctx context.Context, b behaviorport.DurableState, opts ...SpawnOption) error {
	return engine.spawnDurableState(ctx, b, opts...)
}

// SpawnSaga spawns a saga for b, a behavior written against the
// runtime-neutral contract in port/behavior. It is Saga for that contract and
// shares its spawn path and options: the saga reacts to events, sends
// commands to entities and compensates on failure. timeout bounds the whole
// saga; zero means no timeout.
//
// Placement in cluster mode follows the same rules as SpawnEventSourced: a
// behavior GoAkt cannot serialize is rejected with a *BehaviorPlacementError
// before anything is spawned.
func (engine *Engine) SpawnSaga(ctx context.Context, b behaviorport.Saga, timeout time.Duration, opts ...SpawnOption) error {
	return engine.spawnSaga(ctx, b, timeout, opts...)
}
