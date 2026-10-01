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
	"errors"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// This file holds every go-specs helper shared by the engine specs. They report
// through the case (ctx.Expect) and register their teardown with ctx.Cleanup,
// so a spec never has to close what a helper opened.

// waitTimeout bounds every ctx.Eventually poll on a real actor system. It is a
// ceiling, not a delay: a poll returns as soon as its condition holds.
const waitTimeout = 10 * time.Second

// errAnyFailure is the error the publisher tests make a mock return when the
// value does not matter, only that it is a failure.
var errAnyFailure = errors.New("an error for testing")

// Argument matchers that stand for "any non-nil message of this type".
var (
	anEvent = mock.MatchT("an event", func(e *egopb.Event) bool { return e != nil })
	aState  = mock.MatchT("a durable state", func(s *egopb.DurableState) bool { return s != nil })
)

// panicValue runs fn and returns what it panicked with, or nil when it did not
// panic. Expect(panicValue(fn)).To(specs.BeNil()) is the go-specs counterpart
// of testify's NotPanics.
func panicValue(fn func()) (recovered any) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}

// projectionRunning returns a poll function reporting whether the named
// projection is running. A lookup error counts as not running yet.
func projectionRunning(ctx context.Context, engine *Engine, name string) func() any {
	return func() any {
		running, err := engine.IsProjectionRunning(ctx, name)
		return err == nil && running
	}
}

// connectedEventsStore returns an in-memory events store that is connected and
// is disconnected when the case ends.
func connectedEventsStore(ctx *specs.Context) *testkit.EventStore {
	bg := context.Background()
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(bg) })
	return store
}

// connectedDurableStore returns an in-memory durable state store that is
// connected and is disconnected when the case ends.
func connectedDurableStore(ctx *specs.Context) *testkit.DurableStore {
	bg := context.Background()
	store := testkit.NewDurableStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(bg) })
	return store
}

// connectedOffsetStore returns an in-memory offset store that is connected and
// is disconnected when the case ends.
func connectedOffsetStore(ctx *specs.Context) *testkit.OffsetStore {
	bg := context.Background()
	store := testkit.NewOffsetStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(bg) })
	return store
}

// newSpecsEngine builds a goakt actor system and an Engine on top of it, and
// stops both when the case ends. The engine is NOT started: specs that assert
// on the pre-Start state use this one.
func newSpecsEngine(ctx *specs.Context, name string, eventsStore persistence.EventsStore, opts ...Option) *Engine {
	bg := context.Background()

	cfg := NewConfig(eventsStore, opts...)
	sys, err := goakt.NewActorSystem(name, cfg.GoaktOptions()...)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(sys.Start(bg)).To(specs.BeNil())
	ctx.Cleanup(func() { _ = sys.Stop(bg) })

	engine, err := NewEngine(sys, cfg)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Cleanup(func() { _ = engine.Stop(bg) })

	return engine
}

// startEngine is newSpecsEngine followed by Engine.Start.
func startEngine(ctx *specs.Context, name string, eventsStore persistence.EventsStore, opts ...Option) *Engine {
	engine := newSpecsEngine(ctx, name, eventsStore, opts...)
	ctx.Expect(engine.Start(context.Background())).To(specs.BeNil())
	return engine
}

// buildEnvelope wraps payload in a command envelope with a fresh operation id.
func buildEnvelope(ctx *specs.Context, payload proto.Message, opts ...command.MetadataOption) command.Envelope {
	md, err := command.NewMetadata(command.OperationID(uuid.NewString()), opts...)
	ctx.Expect(err).To(specs.BeNil())
	env, err := command.NewEnvelope(payload, md)
	ctx.Expect(err).To(specs.BeNil())
	return env
}

// dispatch sends payload to entityID and returns the command result. It
// expects the dispatch itself to succeed; the result carries the outcome.
func dispatch(ctx *specs.Context, engine *Engine, entityID string, payload proto.Message, opts ...command.MetadataOption) command.Result {
	env := buildEnvelope(ctx, payload, opts...)
	result, err := engine.Dispatch(context.Background(), entityID, env, time.Minute)
	ctx.Expect(err).To(specs.BeNil())
	return result
}

// expectSuccess asserts the command succeeded.
func expectSuccess(ctx *specs.Context, result command.Result) {
	ctx.Expect(result.Outcome()).ToEqual(command.OutcomeSuccess)
}

// expectConcurrencyConflict asserts the command was rejected with the
// concurrency-conflict code.
func expectConcurrencyConflict(ctx *specs.Context, result command.Result) {
	ctx.Expect(result.Outcome()).ToEqual(command.OutcomeRejected)
	failure, ok := result.Failure()
	ctx.Expect(ok).To(specs.BeTrue())
	code, hasCode := failure.Code()
	ctx.Expect(hasCode).To(specs.BeTrue())
	ctx.Expect(code).ToEqual(command.CodeConcurrencyConflict)
}

// conflictError returns the *persistence.ConflictError behind a failed result.
func conflictError(ctx *specs.Context, result command.Result) *persistence.ConflictError {
	var conflict *persistence.ConflictError
	ctx.Expect(result.Err()).To(specs.MatchErrorAs(&conflict))
	return conflict
}

// accountOf returns the account state carried by a successful result.
func accountOf(ctx *specs.Context, result command.Result) *testpb.Account {
	state, ok := result.State()
	ctx.Expect(ok).To(specs.BeTrue())
	account, ok := state.(*testpb.Account)
	ctx.Expect(ok).To(specs.BeTrue())
	return account
}
