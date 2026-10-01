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
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// This file holds the go-specs helpers of the expected-revision, integration
// and batch-precondition engine specs. They do what helper_test.go does for
// plain tests, but report through the case and register their teardown with
// ctx.Cleanup. The G2 suffix keeps them from clashing with the helpers other
// spec files add next to them.

// connectedEventsStoreG2 returns an in-memory events store that is connected
// and is disconnected when the case ends.
func connectedEventsStoreG2(ctx *specs.Context) *testkit.EventStore {
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })
	return store
}

// connectedDurableStoreG2 returns an in-memory durable state store that is
// connected and is disconnected when the case ends.
func connectedDurableStoreG2(ctx *specs.Context) *testkit.DurableStore {
	store := testkit.NewDurableStore()
	ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })
	return store
}

// startEngineG2 starts a single-node goakt actor system and an Engine on it,
// the way newTestEngine does, and stops both when the case ends. The engine
// is already started.
func startEngineG2(ctx *specs.Context, name string, eventsStore persistence.EventsStore, opts ...Option) *Engine {
	cfg := NewConfig(eventsStore, opts...)
	sys, err := goakt.NewActorSystem(name, cfg.GoaktOptions()...)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(sys.Start(context.Background())).To(specs.BeNil())

	engine, err := NewEngine(sys, cfg)
	ctx.Expect(err).To(specs.BeNil())

	ctx.Cleanup(func() {
		_ = engine.Stop(context.Background())
		_ = sys.Stop(context.Background())
	})

	ctx.Expect(engine.Start(context.Background())).To(specs.BeNil())
	return engine
}

// buildEnvelopeG2 wraps payload in a command.Envelope that carries opts.
func buildEnvelopeG2(ctx *specs.Context, payload proto.Message, opts ...command.MetadataOption) command.Envelope {
	md, err := command.NewMetadata(command.OperationID(uuid.NewString()), opts...)
	ctx.Expect(err).To(specs.BeNil())
	env, err := command.NewEnvelope(payload, md)
	ctx.Expect(err).To(specs.BeNil())
	return env
}

// dispatchG2 sends payload through engine.Dispatch, the canonical entry point
// that declares an ExpectedRevision, and returns the command.Result.
func dispatchG2(ctx *specs.Context, engine *Engine, entityID string, payload proto.Message, opts ...command.MetadataOption) command.Result {
	env := buildEnvelopeG2(ctx, payload, opts...)
	result, err := engine.Dispatch(context.Background(), entityID, env, time.Minute)
	ctx.Expect(err).To(specs.BeNil())
	return result
}

// expectSuccessG2 asserts that result succeeded.
func expectSuccessG2(ctx *specs.Context, result command.Result) {
	ctx.Expect(result.Outcome()).ToEqual(command.OutcomeSuccess)
}

// expectConcurrencyConflictG2 asserts that result was rejected with the
// concurrency_conflict failure code.
func expectConcurrencyConflictG2(ctx *specs.Context, result command.Result) {
	ctx.Expect(result.Outcome()).ToEqual(command.OutcomeRejected)
	failure, ok := result.Failure()
	ctx.Expect(ok).To(specs.BeTrue())
	code, hasCode := failure.Code()
	ctx.Expect(hasCode).To(specs.BeTrue())
	ctx.Expect(code).ToEqual(command.CodeConcurrencyConflict)
}

// conflictErrorG2 asserts that result carries a *persistence.ConflictError and
// returns it.
func conflictErrorG2(ctx *specs.Context, result command.Result) *persistence.ConflictError {
	var conflict *persistence.ConflictError
	ctx.Expect(result.Err()).To(specs.MatchErrorAs(&conflict))
	return conflict
}

// accountOfG2 returns the *testpb.Account that result carries as its state.
func accountOfG2(ctx *specs.Context, result command.Result) *testpb.Account {
	state, ok := result.State()
	ctx.Expect(ok).To(specs.BeTrue())
	account, ok := state.(*testpb.Account)
	ctx.Expect(ok).To(specs.BeTrue())
	return account
}
