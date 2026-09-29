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

package runtime

import (
	"github.com/getsyntegrity/ego/port/behavior"
)

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
	State behavior.State
}
