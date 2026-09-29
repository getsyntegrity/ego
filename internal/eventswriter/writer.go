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

// Package eventswriter holds the GoAkt actor that writes an event-sourced
// entity's events: it persists a batch of envelopes to the events store and,
// only after the store confirms the write, publishes them on the in-process
// events stream. The entity actor spawns it as a child with New, talks to it
// only through Ask, and receives its outcome as a *Response.
package eventswriter

import (
	"context"
	"fmt"
	"time"

	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/v4/egopb"
	"github.com/getsyntegrity/ego/v4/eventstream"
	"github.com/getsyntegrity/ego/v4/internal/extensions"
	"github.com/getsyntegrity/ego/v4/persistence"
)

// request is sent from the entity actor to the events writer, through Ask, to
// persist a batch of event envelopes and publish them to the event stream.
//
// scope carries the owning entity actor's bound persistence.Scope
// (TENANT-003 T4). The writer is a separate child actor with no PreStart
// access to the parent's dependencies, so the scope must travel on this
// request rather than be re-derived here.
type request struct {
	envelopes    []*egopb.Event
	topic        string
	precondition persistence.WritePrecondition
	scope        persistence.Scope
}

// Response is sent from the events writer back to the entity actor after an
// attempt to persist events. A nil Err indicates success; a non-nil Err
// carries the store write failure, or the transport failure Ask observed, so
// the parent can decide whether to stop itself.
//
// Response is exported because it is the one message that crosses the
// package boundary: Ask runs inside the parent's PipeTo, which delivers the
// Response to the parent's mailbox.
type Response struct {
	Err error
}

// actor persists events to the events store and publishes them to the event
// stream. Events are published only after the store write succeeds, ensuring
// that downstream consumers never observe events that failed to persist.
//
// This actor is spawned as a child of the entity actor. It receives request
// messages via Ask and replies with a Response indicating success or failure.
type actor struct {
	eventsStore  persistence.EventsStore
	eventsStream eventstream.Stream
}

var _ goakt.Actor = (*actor)(nil)

// New creates an events writer actor, to be spawned as a child of the entity
// actor whose events it writes.
func New() goakt.Actor {
	return &actor{}
}

// PreStart loads the events store and event stream from the actor system extensions.
func (a *actor) PreStart(ctx *goakt.Context) error {
	eventsStoreExt, err := extensions.Require[*extensions.EventsStore](ctx, extensions.EventsStoreExtensionID)
	if err != nil {
		return err
	}
	eventsStreamExt, err := extensions.Require[*extensions.EventsStream](ctx, extensions.EventsStreamExtensionID)
	if err != nil {
		return err
	}

	a.eventsStore = eventsStoreExt.Underlying()
	a.eventsStream = eventsStreamExt.Underlying()
	return nil
}

// Receive handles incoming messages. Only request is expected.
func (a *actor) Receive(ctx *goakt.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goakt.PostStart:
		// no-op
	case *request:
		a.handlePersistEvents(ctx, msg)
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup when the actor is stopped.
func (a *actor) PostStop(_ *goakt.Context) error {
	return nil
}

// handlePersistEvents writes events to the store and publishes them to the stream
// only after the write succeeds. The result including any error is returned via
// Response so the parent receives the reply through its Ask call.
func (a *actor) handlePersistEvents(ctx *goakt.ReceiveContext, req *request) {
	if err := a.eventsStore.WriteEvents(ctx.Context(), req.scope, req.envelopes, req.precondition); err != nil {
		ctx.Response(&Response{Err: err})
		return
	}

	for _, envelope := range req.envelopes {
		a.eventsStream.Publish(req.topic, envelope)
	}

	ctx.Response(&Response{})
}

// Ask sends envelopes to the events writer over a plain goakt.Ask call — safe
// to run inside a plain goroutine via ctx.PipeTo, unlike ctx.Ask, which blocks
// the calling dispatcher worker (see the entity actor's persistAsync and
// flushBatch). Any transport-level failure is embedded in the returned
// *Response's Err field rather than returned as a Go error, so PipeTo always
// delivers a Response message that the entity actor already knows how to
// route.
func Ask(writer *goakt.PID, envelopes []*egopb.Event, topic string, timeout time.Duration, precondition persistence.WritePrecondition, scope persistence.Scope) (*Response, error) {
	reply, err := goakt.Ask(context.Background(), writer, &request{
		envelopes:    envelopes,
		topic:        topic,
		precondition: precondition,
		scope:        scope,
	}, timeout)

	if err != nil {
		return &Response{Err: err}, nil
	}

	if reply == nil {
		return &Response{Err: fmt.Errorf("event writer returned no response")}, nil
	}

	resp, ok := reply.(*Response)
	if !ok {
		return &Response{Err: fmt.Errorf("unexpected response type %T from event writer", reply)}, nil
	}
	return resp, nil
}
