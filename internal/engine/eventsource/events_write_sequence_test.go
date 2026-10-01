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
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/eventstream"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/engine/protocol"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// writeSequence records, in order, the observable steps of an event write:
// the store round trip, each publication on the events stream, and the reply
// the caller receives.
type writeSequence struct {
	mu    sync.Mutex
	steps []string
}

func (s *writeSequence) add(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

func (s *writeSequence) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// sequenceEventsStore records every WriteEvents call and optionally fails it.
// It wraps a real in-memory store, because what the case checks is the order
// of observable steps around the write, not the arguments of the call.
type sequenceEventsStore struct {
	persistence.EventsStore
	seq    *writeSequence
	failed error
	mu     sync.Mutex
	scopes []persistence.Scope
}

func (x *sequenceEventsStore) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	x.mu.Lock()
	x.scopes = append(x.scopes, scope)
	x.mu.Unlock()
	x.seq.add("write")
	if x.failed != nil {
		return x.failed
	}
	return x.EventsStore.WriteEvents(ctx, scope, events, precondition)
}

func (x *sequenceEventsStore) writtenScopes() []persistence.Scope {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]persistence.Scope(nil), x.scopes...)
}

// sequenceEventsStream records every publication on the events stream.
type sequenceEventsStream struct {
	eventstream.Stream
	seq *writeSequence
}

func (x *sequenceEventsStream) Publish(topic string, msg any) {
	if topic == protocol.EventsTopic {
		x.seq.add("publish")
	}
	x.Stream.Publish(topic, msg)
}

// TestEventWriteObservableSequence pins what a caller and a stream subscriber
// can observe of one command's event write: the store is written first, the
// events are published only after the write succeeds, and the reply comes
// last. A failed write publishes nothing and replies with the store error.
func TestEventWriteObservableSequence(t *testing.T) {
	spawn := func(ctx *specs.Context, storeErr error) (*writeSequence, *sequenceEventsStore, *goakt.PID) {
		seq := new(writeSequence)
		base := newConnectedTestkitStore(ctx)
		store := &sequenceEventsStore{EventsStore: base, seq: seq, failed: storeErr}
		stream := &sequenceEventsStream{Stream: newClosingEventStream(ctx), seq: seq}
		system := startEventsSystem(ctx, "SequenceSystem", 3,
			extensions.NewEventsStore(store), extensions.NewEventsStream(stream))

		behavior := enginetest.NewAccountEventSourcedBehavior(uuid.NewString())
		pid, err := system.Spawn(context.Background(), behavior.ID(), New(),
			goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		ctx.Expect(err).To(specs.BeNil())
		return seq, store, pid
	}

	ask := func(ctx *specs.Context, seq *writeSequence, pid *goakt.PID) *egopb.CommandReply {
		reply, err := goakt.Ask(context.Background(), pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		ctx.Expect(err).To(specs.BeNil())
		seq.add("reply")
		ctx.Expect(reply).To(beOfType[*egopb.CommandReply]())
		return reply.(*egopb.CommandReply)
	}

	specs.Describe(t, "one command's event write is observed as store write, then stream publication, then reply", func(s *specs.Spec) {
		s.It("writes, then publishes, then replies", func(ctx *specs.Context) {
			seq, store, pid := spawn(ctx, nil)

			reply := ask(ctx, seq, pid)

			ctx.Expect(reply.GetReply()).To(beOfType[*egopb.CommandReply_StateReply]())
			ctx.Expect(seq.snapshot()).To(specs.HaveElementsInOrder(specs.Equal("write"), specs.Equal("publish"), specs.Equal("reply")))
			ctx.Expect(store.writtenScopes()).ToEqual([]persistence.Scope{persistence.Unscoped()})
		})

		s.It("a failed write publishes nothing, replies the error and stops the entity", func(ctx *specs.Context) {
			seq, _, pid := spawn(ctx, errEventsStoreDown)

			reply := ask(ctx, seq, pid)

			ctx.Expect(reply.GetReply()).To(beOfType[*egopb.CommandReply_ErrorReply]())
			ctx.Expect(reply.GetErrorReply().GetMessage()).To(specs.Contain(errEventsStoreDown.Error()))
			ctx.Expect(seq.snapshot()).ToEqual([]string{"write", "reply"})
			ctx.Eventually(func() any { return pid.IsRunning() }, specs.BeFalse(),
				specs.WithTimeout(5*time.Second), specs.WithInterval(10*time.Millisecond))
		})

		s.It("a revision conflict publishes nothing, replies the conflict and keeps the entity", func(ctx *specs.Context) {
			conflict := persistence.NewConflictError(persistence.Unscoped(), "account", persistence.ExpectGenesis(),
				persistence.WithActualRevision(0))
			seq, _, pid := spawn(ctx, conflict)

			reply := ask(ctx, seq, pid)

			ctx.Expect(reply.GetReply()).To(beOfType[*egopb.CommandReply_ErrorReply]())
			ctx.Expect(reply.GetErrorReply().GetMessage()).To(specs.Equal(conflict.Error()))
			ctx.Expect(seq.snapshot()).ToEqual([]string{"write", "reply"})
			ctx.Expect(pid.IsRunning()).To(specs.BeTrue())
		})
	})
}
