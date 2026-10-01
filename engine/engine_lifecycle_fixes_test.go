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
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/internal/engine/protocol"
	"github.com/getsyntegrity/ego/testkit"
)

// closingEventPublisherG4 returns an event publisher mock with the given ID
// whose Close is expected exactly closes times and answers closeErr.
func closingEventPublisherG4(ctx *specs.Context, id string, closes int, closeErr error) eventPublisherMock {
	ctrl := mock.NewController(ctx)
	ctrl.Method("ID").Expect().Return(id).AnyTimes()
	if closes > 0 {
		ctrl.Method("Close").Expect(mock.Any()).Return(closeErr).Times(closes)
	}
	return eventPublisherMock{ctrl}
}

// closingStatePublisherG4 is closingEventPublisherG4 for a state publisher.
func closingStatePublisherG4(ctx *specs.Context, id string, closes int, closeErr error) statePublisherMock {
	ctrl := mock.NewController(ctx)
	ctrl.Method("ID").Expect().Return(id).AnyTimes()
	if closes > 0 {
		ctrl.Method("Close").Expect(mock.Any()).Return(closeErr).Times(closes)
	}
	return statePublisherMock{ctrl}
}

// TestEngineStopAttemptsEveryStep is the #126 defect 1 regression: a
// publisher whose Close fails must not stop Engine.Stop from closing the
// other publishers and the event stream, and the errors are joined.
func TestEngineStopAttemptsEveryStep(t *testing.T) {
	specs.Describe(t, "Engine.Stop attempts every step even when a publisher fails to close", func(s *specs.Spec) {
		s.It("closes every publisher and the streams, and joins the errors", func(ctx *specs.Context) {
			bg := context.Background()
			engine := newTestEngine(ctx.T, "stop-every-step-"+uuid.NewString(), testkit.NewEventsStore())
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			// Both event publishers fail on Close, so whichever one the map yields
			// first, the other is closed only if Stop carries on after the error.
			// Each Close is expected once, which also proves a second Stop closes
			// nothing twice.
			errFirst := errors.New("close failed: events-1")
			errSecond := errors.New("close failed: events-2")
			first := closingEventPublisherG4(ctx, "events-1", 1, errFirst)
			second := closingEventPublisherG4(ctx, "events-2", 1, errSecond)
			states := closingStatePublisherG4(ctx, "states-1", 1, nil)
			ctx.Expect(engine.AddEventPublishers(first, second)).To(specs.BeNil())
			ctx.Expect(engine.AddStatePublishers(states)).To(specs.BeNil())
			ctx.Expect(engine.eventStream.SubscribersCount(protocol.EventsTopic)).To(specs.BeGreaterThan(0))

			err := engine.Stop(bg)

			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err.Error()).To(specs.Contain(`close events publisher "events-1": close failed: events-1`))
			ctx.Expect(err.Error()).To(specs.Contain(`close events publisher "events-2": close failed: events-2`))
			// the first and the second publisher's errors are reachable through the join
			ctx.Expect(err).To(specs.MatchError(errFirst))
			ctx.Expect(err).To(specs.MatchError(errSecond))

			// event stream closed
			ctx.Expect(engine.eventStream.SubscribersCount(protocol.EventsTopic)).To(specs.BeZero())
			ctx.Expect(engine.eventStream.SubscribersCount(protocol.StatesTopic)).To(specs.BeZero())
			ctx.Expect(engine.eventsStreams.Len()).To(specs.BeZero())
			ctx.Expect(engine.statesStreams.Len()).To(specs.BeZero())
			ctx.Expect(engine.Started()).To(specs.BeFalse())
			// actor system reference detached
			ctx.Expect(engine.ActorSystem() == nil).To(specs.BeTrue())

			// A second Stop has nothing left to do and closes nothing twice.
			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}

// newStateOnlyEngineG4 builds a started engine with a durable-state store and
// no events store, which NewConfig(nil, ...) allows.
func newStateOnlyEngineG4(ctx *specs.Context) *Engine {
	bg := context.Background()
	cfg := NewConfig(nil, WithStateStore(connectedStateStoreG4(ctx)))
	sys, err := goakt.NewActorSystem("state-only-"+uuid.NewString(), cfg.GoaktOptions()...)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(sys.Start(bg)).To(specs.BeNil())
	engine, err := NewEngine(sys, cfg)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Cleanup(func() {
		_ = engine.Stop(context.Background())
		_ = sys.Stop(context.Background())
	})
	ctx.Expect(engine.Start(bg)).To(specs.BeNil())
	return engine
}

// stateOnlySpawnCaseG4 is one spawn on an engine without an events store.
type stateOnlySpawnCaseG4 struct {
	name     string
	id       string
	spawn    func(engine *Engine, id string) error
	rejected bool
}

// TestSpawnWithoutEventsStore is the #126 defect 2 regression: spawning an
// event-sourced entity or a saga on an engine without an events store
// returns ErrEventsStoreRequired and spawns nothing.
func TestSpawnWithoutEventsStore(t *testing.T) {
	specs.Describe(t, "an engine without an events store refuses the families that need one", func(s *specs.Spec) {
		bg := context.Background()
		specs.Table(s, []stateOnlySpawnCaseG4{
			{"event-sourced entity", "account-" + uuid.NewString(), func(e *Engine, id string) error {
				return e.Entity(bg, NewAccountEventSourcedBehavior(id))
			}, true},
			{"saga", "saga-" + uuid.NewString(), func(e *Engine, id string) error {
				return e.Saga(bg, &testSagaBehavior{sagaID: id}, time.Second)
			}, true},
			{"durable-state entity still spawns", "durable-" + uuid.NewString(), func(e *Engine, id string) error {
				return e.DurableStateEntity(bg, NewAccountDurableStateBehavior(id))
			}, false},
		}, func(c stateOnlySpawnCaseG4) string { return c.name }, func(ctx *specs.Context, c stateOnlySpawnCaseG4) {
			engine := newStateOnlyEngineG4(ctx)

			err := c.spawn(engine, c.id)

			if !c.rejected {
				ctx.Expect(err).To(specs.BeNil())
				return
			}
			ctx.Expect(err).To(specs.MatchError(ErrEventsStoreRequired))
			exists, lookupErr := engine.ActorSystem().ActorExists(bg, c.id)
			ctx.Expect(lookupErr).To(specs.BeNil())
			// no actor spawned
			ctx.Expect(exists).To(specs.BeFalse())
		})
	})
}

// publisherKindG4 is one publisher kind of AddPublishers: its topic, how many
// publishers the engine has registered, a mock publisher of the kind and the
// Add method that takes it.
type publisherKindG4 struct {
	name       string
	topic      string
	registered func(*Engine) int
	// newPub builds a publisher mock with the given ID that must be closed
	// closes times (zero: any Close call fails the case).
	newPub func(ctx *specs.Context, id string, closes int) any
	add    func(*Engine, ...any) error
}

func publisherKindsG4() []publisherKindG4 {
	return []publisherKindG4{
		{
			name:       "events",
			topic:      protocol.EventsTopic,
			registered: func(e *Engine) int { return e.eventsStreams.Len() },
			newPub: func(ctx *specs.Context, id string, closes int) any {
				return closingEventPublisherG4(ctx, id, closes, nil)
			},
			add: func(e *Engine, ps ...any) error {
				pubs := make([]EventPublisher, 0, len(ps))
				for _, p := range ps {
					pubs = append(pubs, p.(EventPublisher))
				}
				return e.AddEventPublishers(pubs...)
			},
		},
		{
			name:       "states",
			topic:      protocol.StatesTopic,
			registered: func(e *Engine) int { return e.statesStreams.Len() },
			newPub: func(ctx *specs.Context, id string, closes int) any {
				return closingStatePublisherG4(ctx, id, closes, nil)
			},
			add: func(e *Engine, ps ...any) error {
				pubs := make([]StatePublisher, 0, len(ps))
				for _, p := range ps {
					pubs = append(pubs, p.(StatePublisher))
				}
				return e.AddStatePublishers(pubs...)
			},
		},
	}
}

// TestAddPublishersRejectsDuplicateIDs is the #126 defect 3 regression: a
// publisher ID that is already registered for its kind, or repeated within
// one batch, fails the whole call with ErrDuplicatePublisherID and leaves
// the engine exactly as it was: no publisher of the batch is registered,
// subscribed or started, and Stop closes only the publishers registered
// before the call.
func TestAddPublishersRejectsDuplicateIDs(t *testing.T) {
	specs.Describe(t, "AddPublishers rejects a duplicate publisher ID and leaves the engine as it was", func(s *specs.Spec) {
		bg := context.Background()
		kinds := publisherKindsG4()

		specs.Table(s, kinds, func(k publisherKindG4) string { return k.name + "/duplicate within the batch" },
			func(ctx *specs.Context, k publisherKindG4) {
				engine := newTestEngine(ctx.T, "dup-batch-"+uuid.NewString(), testkit.NewEventsStore())
				ctx.Expect(engine.Start(bg)).To(specs.BeNil())

				// A rejected publisher is never started, so never closed: none of
				// these declares a Close expectation.
				fresh := k.newPub(ctx, "fresh", 0)
				first := k.newPub(ctx, "dup", 0)
				second := k.newPub(ctx, "dup", 0)

				err := k.add(engine, fresh, first, second)

				ctx.Expect(err).To(specs.MatchError(ErrDuplicatePublisherID))
				ctx.Expect(err.Error()).To(specs.Contain(`"dup"`))
				// nothing registered, nothing subscribed
				ctx.Expect(k.registered(engine)).To(specs.BeZero())
				ctx.Expect(engine.eventStream.SubscribersCount(k.topic)).To(specs.BeZero())
				ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
			})

		specs.Table(s, kinds, func(k publisherKindG4) string { return k.name + "/duplicate of a registered publisher" },
			func(ctx *specs.Context, k publisherKindG4) {
				engine := newTestEngine(ctx.T, "dup-registered-"+uuid.NewString(), testkit.NewEventsStore())
				ctx.Expect(engine.Start(bg)).To(specs.BeNil())

				// the registered publisher is still closed by Stop; the two
				// rejected ones are not
				existing := k.newPub(ctx, "dup", 1)
				ctx.Expect(k.add(engine, existing)).To(specs.BeNil())
				subscribers := engine.eventStream.SubscribersCount(k.topic)

				fresh := k.newPub(ctx, "fresh", 0)
				again := k.newPub(ctx, "dup", 0)
				err := k.add(engine, fresh, again)

				ctx.Expect(err).To(specs.MatchError(ErrDuplicatePublisherID))
				ctx.Expect(err.Error()).To(specs.Contain(`"dup"`))
				// only the publisher registered before, and no new subscriber
				ctx.Expect(k.registered(engine)).To(specs.Equal(1))
				ctx.Expect(engine.eventStream.SubscribersCount(k.topic)).To(specs.Equal(subscribers))
				ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
			})

		s.It("the same ID may be used once per kind", func(ctx *specs.Context) {
			engine := newTestEngine(ctx.T, "dup-kinds-"+uuid.NewString(), testkit.NewEventsStore())
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())
			ctx.Expect(engine.AddEventPublishers(closingEventPublisherG4(ctx, "shared", 1, nil))).To(specs.BeNil())
			ctx.Expect(engine.AddStatePublishers(closingStatePublisherG4(ctx, "shared", 1, nil))).To(specs.BeNil())
			// both publishers are registered, so Stop closes each of them once
			ctx.Expect(engine.Stop(bg)).To(specs.BeNil())
		})
	})
}
