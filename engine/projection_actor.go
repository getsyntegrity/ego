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
	"runtime"

	gerrors "github.com/tochemey/goakt/v4/errors"
	"github.com/tochemey/goakt/v4/supervisor"
)

// projectionRunnerError reports an event that cannot be processed: a failed
// decryption, a failed event adaptation, or a handler error under the Fail
// and RetryAndFail recovery policies. Retrying would only replay the same
// event, so the processing loop stops permanently and the host actor
// escalates the error through supervision to make the failure visible.
//
// The type stays in this package on purpose: goakt keys supervisor
// directives by the error's type name ("engine.projectionRunnerError") and
// ships those names to peer nodes with singleton spawns, so renaming or moving
// the type would break supervision in a cluster running mixed versions.
type projectionRunnerError struct {
	err error
}

// Error returns the underlying runner error message.
func (e *projectionRunnerError) Error() string {
	return e.err.Error()
}

// Unwrap exposes the underlying runner error.
func (e *projectionRunnerError) Unwrap() error {
	return e.err
}

// newProjectionSupervisor returns the supervisor applied to projection actors:
// an escalated runner error stops the projection visibly. Only unprocessable
// events escalate — the runner retries failed store round trips in place with
// exponential backoff — and a restart would only replay the failing event.
// Panics and internal errors keep the stop-on-failure semantics of goakt's
// default singleton supervisor, which this supervisor replaces in cluster mode.
func newProjectionSupervisor() *supervisor.Supervisor {
	return supervisor.NewSupervisor(
		supervisor.WithDirective(&projectionRunnerError{}, supervisor.StopDirective),
		supervisor.WithDirective(&gerrors.PanicError{}, supervisor.StopDirective),
		supervisor.WithDirective(&gerrors.InternalError{}, supervisor.StopDirective),
		supervisor.WithDirective(&runtime.PanicNilError{}, supervisor.StopDirective),
	)
}

// NewProjectionActor creates an instance of ProjectionActor
func NewProjectionActor() *ProjectionActor {
	return &ProjectionActor{}
}
