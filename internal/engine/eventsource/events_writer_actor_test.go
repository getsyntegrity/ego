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

package eventsource

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/urd/egopb"
	"github.com/getsyntegrity/urd/eventstream"
	"github.com/getsyntegrity/urd/internal/engine/enginetest"
	"github.com/getsyntegrity/urd/internal/extensions"
	"github.com/getsyntegrity/urd/internal/goaktlog"
	"github.com/getsyntegrity/urd/persistence"
	"github.com/getsyntegrity/urd/testkit"
)

// errEventsStoreDown is the failure the events store mocks of this package return.
var errEventsStoreDown = errors.New("eventsource test: events store down")

// startEventsSystem starts an in-process goakt actor system carrying exts and
// stops it when the case ends. Create any mock.Controller before calling it:
// cleanups run last in, first out, so the system is stopped before the
// controller verifies its expectations and no late call is reported after the
// case.
func startEventsSystem(ctx *specs.Context, name string, initRetries int, exts ...extension.Extension) goakt.ActorSystem {
	system, err := goakt.NewActorSystem(name,
		goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
		goakt.WithExtensions(exts...),
		goakt.WithActorInitMaxRetries(initRetries))
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(system.Start(context.Background())).To(specs.BeNil())
	ctx.Cleanup(func() { ctx.Expect(system.Stop(context.Background())).To(specs.BeNil()) })
	return system
}

// newConnectedTestkitStore returns an in-memory events store that disconnects
// when the case ends.
func newConnectedTestkitStore(ctx *specs.Context) *testkit.EventStore {
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })
	return store
}

// newClosingEventStream returns an events stream that closes when the case ends.
func newClosingEventStream(ctx *specs.Context) eventstream.Stream {
	stream := eventstream.New()
	ctx.Cleanup(stream.Close)
	return stream
}

// beOfType matches a value whose dynamic type is T.
func beOfType[T any]() specs.Matcher {
	return specs.Satisfy(fmt.Sprintf("be a %T", *new(T)), func(v any) bool {
		_, ok := v.(T)
		return ok
	})
}

// pingAnyTimes lets the store be pinged any number of times, as the actor
// system does when it starts a store extension.
func pingAnyTimes(ctrl *mock.Controller) {
	ctrl.Method("Ping").Expect(mock.Any()).AnyTimes().Return(nil)
}

// noReplyEnvelopes returns count envelopes of one entity, numbered from 1.
func noReplyEnvelopes(ctx *specs.Context, persistenceID string, count int) []*egopb.Event {
	event, err := anypb.New(&egopb.NoReply{})
	ctx.Expect(err).To(specs.BeNil())
	envelopes := make([]*egopb.Event, 0, count)
	for i := 1; i <= count; i++ {
		envelopes = append(envelopes, &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: uint64(i),
			Event:          event,
			Timestamp:      time.Now().Unix(),
		})
	}
	return envelopes
}

// askPersist sends one persist request to the events writer and returns its response.
func askPersist(ctx *specs.Context, pid *goakt.PID, req *persistEventsRequest) *persistEventsResponse {
	reply, err := goakt.Ask(context.Background(), pid, req, 5*time.Second)
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(reply).To(beOfType[*persistEventsResponse]())
	return reply.(*persistEventsResponse)
}

func TestEventsWriterActor(t *testing.T) {
	specs.Describe(t, "eventsWriterActor persists a batch, publishes it only after the store accepted it, and fails closed on a bad setup", func(s *specs.Spec) {
		s.It("persists events and publishes to stream on success", func(ctx *specs.Context) {
			eventStore := newConnectedTestkitStore(ctx)
			eventStream := newClosingEventStream(ctx)
			sub := eventStream.AddSubscriber()
			eventStream.Subscribe(sub, "topic.events.0")
			system := startEventsSystem(ctx, "TestWriterSystem", 1,
				extensions.NewEventsStore(eventStore), extensions.NewEventsStream(eventStream))

			pid, err := system.Spawn(context.Background(), "event-writer-test", newEventsWriterActor())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(pid).To(specs.Not(specs.BeNil()))

			resp := askPersist(ctx, pid, &persistEventsRequest{
				scope:        persistence.Unscoped(),
				envelopes:    noReplyEnvelopes(ctx, "entity-1", 2),
				topic:        "topic.events.0",
				precondition: persistence.Unconditional(),
			})
			ctx.Expect(resp.Err).To(specs.BeNil())

			latest, err := eventStore.GetLatestEvent(context.Background(), persistence.Unscoped(), "entity-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetSequenceNumber()).To(specs.Equal(uint64(2)))

			var published int
			ctx.Eventually(func() any {
				for range sub.Iterator() {
					published++
				}
				return published
			}, specs.Equal(2), specs.WithTimeout(2*time.Second), specs.WithInterval(10*time.Millisecond))
		})

		s.It("returns error in response when store write fails", func(ctx *specs.Context) {
			ctrl := mock.NewController(ctx)
			eventStore := enginetest.NewEventsStoreMock(ctrl)
			pingAnyTimes(ctrl)
			ctrl.Method("WriteEvents").
				Expect(mock.Any(), persistence.Unscoped(), mock.Any(), mock.Any()).
				Times(1).Return(errEventsStoreDown)
			eventStream := newClosingEventStream(ctx)
			sub := eventStream.AddSubscriber()
			eventStream.Subscribe(sub, "topic.events.0")
			system := startEventsSystem(ctx, "TestWriterSystem", 1,
				extensions.NewEventsStore(eventStore), extensions.NewEventsStream(eventStream))

			pid, err := system.Spawn(context.Background(), "event-writer-test", newEventsWriterActor())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(pid).To(specs.Not(specs.BeNil()))

			resp := askPersist(ctx, pid, &persistEventsRequest{
				scope:     persistence.Unscoped(),
				envelopes: noReplyEnvelopes(ctx, "entity-1", 1),
				topic:     "topic.events.0",
			})
			ctx.Expect(resp.Err).To(specs.MatchError(errEventsStoreDown))

			// Nothing may reach the stream after a failed write: watch it for a
			// short window instead of sleeping.
			published := 0
			ctx.Consistently(func() any {
				for range sub.Iterator() {
					published++
				}
				return published
			}, specs.Equal(0), specs.WithTimeout(300*time.Millisecond), specs.WithInterval(10*time.Millisecond))
		})

		s.It("handles empty envelopes list", func(ctx *specs.Context) {
			eventStore := newConnectedTestkitStore(ctx)
			eventStream := newClosingEventStream(ctx)
			system := startEventsSystem(ctx, "TestWriterSystem", 1,
				extensions.NewEventsStore(eventStore), extensions.NewEventsStream(eventStream))

			pid, err := system.Spawn(context.Background(), "event-writer-test", newEventsWriterActor())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(pid).To(specs.Not(specs.BeNil()))

			resp := askPersist(ctx, pid, &persistEventsRequest{
				scope:        persistence.Unscoped(),
				envelopes:    nil,
				topic:        "topic.events.0",
				precondition: persistence.Unconditional(),
			})
			ctx.Expect(resp.Err).To(specs.BeNil())
		})

		s.It("returns an error instead of panicking when the events store extension is missing", func(ctx *specs.Context) {
			eventStream := newClosingEventStream(ctx)

			// Deliberately omit extensions.NewEventsStore: this reproduces the
			// events writer being spawned against an actor system that never
			// registered (or already reset, e.g. via shutdown) the events store
			// extension. Before the fix for issue #99, the unchecked type
			// assertion in PreStart panicked with an unrecoverable "interface
			// conversion" error that crashed the whole process instead of
			// failing this one Spawn call.
			system := startEventsSystem(ctx, "TestWriterMissingExtSystem", 1, extensions.NewEventsStream(eventStream))

			pid, err := system.Spawn(context.Background(), "event-writer-missing-ext", newEventsWriterActor())
			ctx.Expect(err).To(specs.MatchError(extensions.ErrMissingRequiredExtensions))
			ctx.Expect(pid).To(specs.BeNil())
		})

		s.It("marks unhandled messages as unhandled", func(ctx *specs.Context) {
			eventStore := newConnectedTestkitStore(ctx)
			eventStream := newClosingEventStream(ctx)
			system := startEventsSystem(ctx, "TestWriterSystem", 1,
				extensions.NewEventsStore(eventStore), extensions.NewEventsStream(eventStream))

			pid, err := system.Spawn(context.Background(), "event-writer-test", newEventsWriterActor())
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(pid).To(specs.Not(specs.BeNil()))

			// send an unexpected message type
			reply, err := goakt.Ask(context.Background(), pid, &egopb.NoReply{}, 500*time.Millisecond)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(reply).To(specs.BeNil())
		})
	})
}
