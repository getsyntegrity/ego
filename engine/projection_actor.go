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
	"fmt"
	"runtime"

	goakt "github.com/tochemey/goakt/v4/actor"
	gerrors "github.com/tochemey/goakt/v4/errors"
	"github.com/tochemey/goakt/v4/supervisor"

	"github.com/getsyntegrity/ego/internal/engine/protocol"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	"github.com/getsyntegrity/ego/internal/instrumentation"
	"github.com/getsyntegrity/ego/internal/projectionrunner"
)

// runnerFailed is the internal message the projection actor sends itself when
// the runner's processing loop stops permanently on an unprocessable event.
type runnerFailed struct {
	err error
}

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

// ProjectionActor defines the projection actor
// Only a single instance of this will run throughout the cluster
type ProjectionActor struct {
	runner  *projectionrunner.Runner
	metrics *instrumentation.Instruments
}

// implements the Actor contract
var _ goakt.Actor = (*ProjectionActor)(nil)

// NewProjectionActor creates an instance of ProjectionActor
func NewProjectionActor() *ProjectionActor {
	return &ProjectionActor{}
}

// PreStart prepares the projection
func (x *ProjectionActor) PreStart(ctx *goakt.Context) error {
	offsetStoreExt, err := extensions.Require[*extensions.OffsetStore](ctx, extensions.OffsetStoreExtensionID)
	if err != nil {
		return err
	}
	eventsStoreExt, err := extensions.Require[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
	if err != nil {
		return err
	}
	registry, err := extensions.Require[*extensions.ProjectionExtension](ctx, extensions.ProjectionExtensionID)
	if err != nil {
		return err
	}
	offsetStore := offsetStoreExt.Underlying()
	eventsStore := eventsStoreExt.Underlying()

	// The actor name is the projection name: resolve this projection's own
	// handler and options from the registry built by WithProjection.
	options := registry.Get(ctx.ActorName())
	if options == nil {
		return fmt.Errorf("projection %q is not registered: register it with engine.WithProjection on every node", ctx.ActorName())
	}

	opts := []projectionrunner.Option{
		projectionrunner.WithLogger(goaktlog.Backend(ctx.ActorSystem().Logger())),
		projectionrunner.WithRecoveryStrategy(options.Recovery),
		projectionrunner.WithStartOffset(options.StartOffset),
		projectionrunner.WithResetOffset(options.ResetOffset),
		projectionrunner.WithMaxBufferSize(options.BufferSize),
		projectionrunner.WithPullInterval(options.PullInterval),
	}

	if options.DeadLetterHandler != nil {
		opts = append(opts, projectionrunner.WithDeadLetterHandler(options.DeadLetterHandler))
	}

	eventAdaptersExt, err := extensions.Optional[*extensions.EventAdapters](ctx, extensions.EventAdaptersExtensionID)
	if err != nil {
		return err
	}
	if eventAdaptersExt != nil {
		opts = append(opts, projectionrunner.WithEventAdapters(eventAdaptersExt.Adapters()))
	}

	// Events persisted on this node trigger an immediate pull instead of
	// waiting for the next pull interval.
	eventsStreamExt, err := extensions.Optional[*extensions.EventsStream](ctx, extensions.EventsStreamExtensionID)
	if err != nil {
		return err
	}
	if eventsStreamExt != nil {
		opts = append(opts, projectionrunner.WithEventsStream(eventsStreamExt.Underlying(), protocol.EventsTopic))
	}

	encryptorExt, err := extensions.Optional[*extensions.EncryptorExtension](ctx, extensions.EncryptorExtensionID)
	if err != nil {
		return err
	}
	if encryptorExt != nil {
		opts = append(opts, projectionrunner.WithEncryptor(encryptorExt.Encryptor()))
	}

	telemetryExt, err := extensions.Optional[*extensions.TelemetryExtension](ctx, extensions.TelemetryExtensionID)
	if err != nil {
		return err
	}
	if telemetryExt != nil {
		x.metrics = instrumentation.New(telemetryExt.Meter())
		if x.metrics != nil {
			opts = append(opts, projectionrunner.WithMetrics(x.metrics))
		}
	}

	x.runner = projectionrunner.New(ctx.ActorName(), options.Handler, eventsStore, offsetStore, opts...)

	// Use context.Background() instead of ctx.Context() because PreStart's
	// context is ephemeral — goakt wraps it in context.WithTimeout and cancels
	// it immediately after PreStart returns. The runner's Start performs store
	// pings and offset resets that must not be tied to that short-lived context.
	if err := x.runner.Start(context.Background()); err != nil {
		return err
	}

	x.metrics.ProjectionStarted(context.Background())

	return nil
}

// Receive handle the message sent to the projection actor
func (x *ProjectionActor) Receive(ctx *goakt.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goakt.PostStart:
		// Hand the runner a way back to this actor before the processing loop
		// starts: a loop that dies on an unprocessable event sends
		// runnerFailed back so the actor fails through the normal supervision
		// path instead of staying healthy-looking with a dead runner.
		pid := ctx.Self()
		x.runner.Run(ctx.Context(), func(cause error) {
			// A failed delivery means the host actor is already stopping, in
			// which case the projection is going down anyway.
			_ = goakt.Tell(context.Background(), pid, &runnerFailed{err: &projectionRunnerError{err: cause}})
		})
	case *runnerFailed:
		ctx.Err(msg.err)
	default:
		ctx.Unhandled()
	}
}

// PostStop prepares the actor to gracefully shutdown
func (x *ProjectionActor) PostStop(ctx *goakt.Context) error {
	x.metrics.ProjectionStopped(ctx.Context())
	return x.runner.Stop()
}
