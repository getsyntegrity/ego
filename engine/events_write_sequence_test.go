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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/eventstream"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
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

// sequenceEventsStream records every publication on the events stream.
type sequenceEventsStream struct {
	eventstream.Stream
	seq *writeSequence
}

func (x *sequenceEventsStream) Publish(topic string, msg any) {
	if topic == eventsTopic {
		x.seq.add("publish")
	}
	x.Stream.Publish(topic, msg)
}

// TestEventWriteObservableSequence pins what a caller and a stream subscriber
// can observe of one command's event write: the store is written first, the
// events are published only after the write succeeds, and the reply comes
// last. A failed write publishes nothing and replies with the store error.
func TestEventWriteObservableSequence(t *testing.T) {
	spawn := func(t *testing.T, storeErr error) (*writeSequence, *sequenceEventsStore, *goakt.PID, goakt.ActorSystem) {
		t.Helper()
		ctx := context.Background()

		seq := new(writeSequence)
		base := testkit.NewEventsStore()
		require.NoError(t, base.Connect(ctx))
		store := &sequenceEventsStore{EventsStore: base, seq: seq, failed: storeErr}
		stream := &sequenceEventsStream{Stream: eventstream.New(), seq: seq}

		actorSystem, err := goakt.NewActorSystem("SequenceSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(store),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		t.Cleanup(func() {
			_ = actorSystem.Stop(ctx)
			_ = base.Disconnect(ctx)
		})

		behavior := NewAccountEventSourcedBehavior(uuid.NewString())
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), newEventSourcedActor(),
			goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		return seq, store, pid, actorSystem
	}

	ask := func(t *testing.T, seq *writeSequence, pid *goakt.PID) *egopb.CommandReply {
		t.Helper()
		reply, err := goakt.Ask(context.Background(), pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		seq.add("reply")
		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		return commandReply
	}

	t.Run("writes, then publishes, then replies", func(t *testing.T) {
		seq, store, pid, _ := spawn(t, nil)

		reply := ask(t, seq, pid)

		require.IsType(t, new(egopb.CommandReply_StateReply), reply.GetReply())
		assert.Equal(t, []string{"write", "publish", "reply"}, seq.snapshot())
		assert.Equal(t, []persistence.Scope{persistence.Unscoped()}, store.scopes)
	})

	t.Run("a failed write publishes nothing, replies the error and stops the entity", func(t *testing.T) {
		seq, _, pid, _ := spawn(t, assert.AnError)

		reply := ask(t, seq, pid)

		require.IsType(t, new(egopb.CommandReply_ErrorReply), reply.GetReply())
		assert.Contains(t, reply.GetErrorReply().GetMessage(), assert.AnError.Error())
		assert.Equal(t, []string{"write", "reply"}, seq.snapshot())
		require.Eventually(t, func() bool { return !pid.IsRunning() }, 5*time.Second, 50*time.Millisecond)
	})

	t.Run("a revision conflict publishes nothing, replies the conflict and keeps the entity", func(t *testing.T) {
		conflict := persistence.NewConflictError(persistence.Unscoped(), "account", persistence.ExpectGenesis(),
			persistence.WithActualRevision(0))
		seq, _, pid, _ := spawn(t, conflict)

		reply := ask(t, seq, pid)

		require.IsType(t, new(egopb.CommandReply_ErrorReply), reply.GetReply())
		assert.Equal(t, conflict.Error(), reply.GetErrorReply().GetMessage())
		assert.Equal(t, []string{"write", "reply"}, seq.snapshot())
		assert.True(t, pid.IsRunning())
	})
}
