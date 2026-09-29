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

package eventswriter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/log"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/v4/egopb"
	"github.com/getsyntegrity/ego/v4/eventstream"
	"github.com/getsyntegrity/ego/v4/internal/extensions"
	"github.com/getsyntegrity/ego/v4/persistence"
	"github.com/getsyntegrity/ego/v4/tenancy"
)

const contractTopic = "topic.events"

// recordingStore captures the arguments of every WriteEvents call and
// returns the configured error.
type recordingStore struct {
	persistence.EventsStore
	err error

	mu            sync.Mutex
	scopes        []persistence.Scope
	preconditions []persistence.WritePrecondition
	batches       [][]*egopb.Event
}

func (x *recordingStore) WriteEvents(_ context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.scopes = append(x.scopes, scope)
	x.preconditions = append(x.preconditions, precondition)
	x.batches = append(x.batches, events)
	return x.err
}

// recordingStream captures every publication.
type recordingStream struct {
	eventstream.Stream

	mu        sync.Mutex
	published []any
	topics    []string
}

func (x *recordingStream) Publish(topic string, msg any) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.topics = append(x.topics, topic)
	x.published = append(x.published, msg)
}

func (x *recordingStream) count() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return len(x.published)
}

func startWriter(t *testing.T, store persistence.EventsStore, stream eventstream.Stream) *goakt.PID {
	t.Helper()
	ctx := context.Background()

	actorSystem, err := goakt.NewActorSystem("WriterContractSystem",
		goakt.WithLogger(log.DiscardLogger),
		goakt.WithExtensions(
			extensions.NewEventsStore(store),
			extensions.NewEventsStream(stream),
		),
		goakt.WithActorInitMaxRetries(1))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	t.Cleanup(func() { _ = actorSystem.Stop(ctx) })

	pid, err := actorSystem.Spawn(ctx, "events-writer", New())
	require.NoError(t, err)
	return pid
}

func contractEnvelopes(t *testing.T) []*egopb.Event {
	t.Helper()
	event, err := anypb.New(&egopb.NoReply{})
	require.NoError(t, err)
	return []*egopb.Event{
		{PersistenceId: "entity-1", SequenceNumber: 1, Event: event},
		{PersistenceId: "entity-1", SequenceNumber: 2, Event: event},
	}
}

func TestWriterContract(t *testing.T) {
	t.Run("a successful write reaches the store once and publishes every envelope in order on the request topic", func(t *testing.T) {
		store := &recordingStore{}
		stream := &recordingStream{Stream: eventstream.New()}
		pid := startWriter(t, store, stream)
		envelopes := contractEnvelopes(t)

		resp, err := Ask(pid, envelopes, contractTopic, 5*time.Second, persistence.ExpectRevision(0), persistence.Unscoped())

		require.NoError(t, err)
		require.NoError(t, resp.Err)
		require.Len(t, store.batches, 1)
		assert.Equal(t, envelopes, store.batches[0])
		assert.Equal(t, []persistence.WritePrecondition{persistence.ExpectRevision(0)}, store.preconditions)
		assert.Equal(t, []any{envelopes[0], envelopes[1]}, stream.published)
		assert.Equal(t, []string{contractTopic, contractTopic}, stream.topics)
	})

	t.Run("a revision conflict is replied unchanged and publishes nothing", func(t *testing.T) {
		conflict := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(0),
			persistence.WithActualRevision(3))
		store := &recordingStore{err: conflict}
		stream := &recordingStream{Stream: eventstream.New()}
		pid := startWriter(t, store, stream)

		resp, err := Ask(pid, contractEnvelopes(t), contractTopic, 5*time.Second, persistence.ExpectRevision(0), persistence.Unscoped())

		require.NoError(t, err)
		var got *persistence.ConflictError
		require.ErrorAs(t, resp.Err, &got)
		assert.Same(t, conflict, got)
		actual, ok := got.ActualRevision()
		assert.True(t, ok)
		assert.EqualValues(t, 3, actual)
		assert.Zero(t, stream.count())
	})

	t.Run("a store failure is replied unchanged and publishes nothing", func(t *testing.T) {
		store := &recordingStore{err: assert.AnError}
		stream := &recordingStream{Stream: eventstream.New()}
		pid := startWriter(t, store, stream)

		resp, err := Ask(pid, contractEnvelopes(t), contractTopic, 5*time.Second, persistence.Unconditional(), persistence.Unscoped())

		require.NoError(t, err)
		assert.ErrorIs(t, resp.Err, assert.AnError)
		assert.Zero(t, stream.count())
	})

	t.Run("the tenant scope of the request reaches the store unchanged", func(t *testing.T) {
		scope, err := persistence.NewTenantScope(tenancy.TenantID("tenant-a"))
		require.NoError(t, err)
		store := &recordingStore{}
		stream := &recordingStream{Stream: eventstream.New()}
		pid := startWriter(t, store, stream)

		resp, err := Ask(pid, contractEnvelopes(t), contractTopic, 5*time.Second, persistence.Unconditional(), scope)

		require.NoError(t, err)
		require.NoError(t, resp.Err)
		assert.Equal(t, []persistence.Scope{scope}, store.scopes)
	})

	t.Run("a transport failure is embedded in the response, never returned", func(t *testing.T) {
		store := &recordingStore{}
		stream := &recordingStream{Stream: eventstream.New()}
		pid := startWriter(t, store, stream)
		require.NoError(t, pid.Shutdown(context.Background()))

		resp, err := Ask(pid, contractEnvelopes(t), contractTopic, time.Second, persistence.Unconditional(), persistence.Unscoped())

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Error(t, resp.Err)
		assert.Empty(t, store.batches)
		assert.Zero(t, stream.count())
	})
}
