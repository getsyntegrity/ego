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
	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"

	behaviorport "github.com/pablogore/ego/v4/port/behavior"
)

// SagaBehavior defines a long-running business process that coordinates
// multiple entities. Sagas react to events, send commands to entities,
// and manage compensation logic for rollback on failures.
//
// A saga is itself event-sourced: it persists its own events to track
// which steps have completed, enabling recovery after restarts.
//
// It is the runtime-neutral contract [behaviorport.Saga] from package
// port/behavior plus GoAkt's extension.Dependency, which adds MarshalBinary
// and UnmarshalBinary so GoAkt can copy the behavior to another cluster node.
// Its method set is the same as before port/behavior existed; the domain
// methods and their documentation live on [behaviorport.Saga].
type SagaBehavior interface {
	behaviorport.Saga
	extension.Dependency
}

// SagaAction describes what the saga should do next after processing an event
// or result. It is an alias of [behaviorport.SagaAction], so ego.SagaAction and
// behavior.SagaAction are the same type.
type SagaAction = behaviorport.SagaAction

// SagaCommand represents a command to send to another entity. It is an alias
// of [behaviorport.SagaCommand], so ego.SagaCommand and behavior.SagaCommand
// are the same type. When its Metadata is left as the zero value, SagaActor
// derives one from the saga's own root Metadata.
type SagaCommand = behaviorport.SagaCommand

// sagaActionIsNoop reports whether the action has no observable effect: nothing
// to persist, no command to dispatch, no completion, no compensation. A saga
// behavior returns such an action for stream events it recognizes as
// irrelevant (SG4: this lets the caller skip tenant binding for events the
// saga was never going to act on). It is a function rather than a method
// because SagaAction is declared in port/behavior.
func sagaActionIsNoop(a *SagaAction) bool {
	return a == nil || (len(a.Commands) == 0 && len(a.Events) == 0 && !a.Complete && !a.Compensate)
}

// SagaStatus represents the current status of a saga.
type SagaStatus int

const (
	// SagaRunning indicates the saga is actively processing.
	SagaRunning SagaStatus = iota
	// SagaCompleted indicates the saga finished successfully.
	SagaCompleted
	// SagaCompensating indicates the saga is rolling back.
	SagaCompensating
	// SagaFailed indicates the saga failed and compensation also failed.
	SagaFailed
)

// String returns the string representation of the saga status.
func (s SagaStatus) String() string {
	switch s {
	case SagaRunning:
		return "running"
	case SagaCompleted:
		return "completed"
	case SagaCompensating:
		return "compensating"
	case SagaFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// SagaInfo holds runtime information about a saga.
type SagaInfo struct {
	// ID is the saga's unique identifier.
	ID string
	// Status is the saga's current status.
	Status SagaStatus
	// State is the saga's current state (may be nil if not started).
	State proto.Message
}
