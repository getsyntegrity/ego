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
	"github.com/tochemey/goakt/v4/extension"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/eventstream"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/tenancy"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// storeCall is one call observed on the events store or the snapshot store:
// which operation, the scope it received and the sequence number it carried
// (the first event of a write, the snapshot's sequence number, or the upper
// bound of a delete).
type storeCall struct {
	op    string
	scope persistence.Scope
	seqNr uint64
}

// storeCalls records, in arrival order, the calls of both stores.
type storeCalls struct {
	mu    sync.Mutex
	calls []storeCall
}

func (s *storeCalls) add(op string, scope persistence.Scope, seqNr uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, storeCall{op: op, scope: scope, seqNr: seqNr})
}

func (s *storeCalls) snapshot() []storeCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]storeCall(nil), s.calls...)
}

// indexOf returns the position of the first call matching op and seqNr, or -1.
func indexOf(calls []storeCall, op string, seqNr uint64) int {
	for i, call := range calls {
		if call.op == op && call.seqNr == seqNr {
			return i
		}
	}
	return -1
}

func (s *storeCalls) has(op string, seqNr uint64) bool {
	return indexOf(s.snapshot(), op, seqNr) >= 0
}

func (s *storeCalls) count(op string) int {
	n := 0
	for _, call := range s.snapshot() {
		if call.op == op {
			n++
		}
	}
	return n
}

// recordingEventsStore records WriteEvents and DeleteEvents and optionally
// fails the write.
type recordingEventsStore struct {
	persistence.EventsStore
	calls    *storeCalls
	writeErr error
}

func (x *recordingEventsStore) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	var first uint64
	if len(events) > 0 {
		first = events[0].GetSequenceNumber()
	}
	x.calls.add("WriteEvents", scope, first)
	if x.writeErr != nil {
		return x.writeErr
	}
	return x.EventsStore.WriteEvents(ctx, scope, events, precondition)
}

func (x *recordingEventsStore) DeleteEvents(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	x.calls.add("DeleteEvents", scope, toSequenceNumber)
	return x.EventsStore.DeleteEvents(ctx, scope, persistenceID, toSequenceNumber)
}

// recordingSnapshotStore records WriteSnapshot and DeleteSnapshots.
type recordingSnapshotStore struct {
	persistence.SnapshotStore
	calls *storeCalls
}

func (x *recordingSnapshotStore) WriteSnapshot(ctx context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	x.calls.add("WriteSnapshot", scope, snapshot.GetSequenceNumber())
	return x.SnapshotStore.WriteSnapshot(ctx, scope, snapshot)
}

func (x *recordingSnapshotStore) DeleteSnapshots(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	x.calls.add("DeleteSnapshots", scope, toSequenceNumber)
	return x.SnapshotStore.DeleteSnapshots(ctx, scope, persistenceID, toSequenceNumber)
}

// TestSnapshotAndRetentionObservableSequence pins what the stores can observe
// of the snapshot path after a command's event write: the events store is
// written first, the snapshot only after it, and the retention deletes only
// after the snapshot, all with the entity's scope. A failed events write
// never produces a snapshot.
//
// The snapshot interval is 1, so every command crosses a boundary; the second
// command is the one whose retention deletes both the events up to sequence 2
// and the previous snapshot (sequence 1).
func TestSnapshotAndRetentionObservableSequence(t *testing.T) {
	spawn := func(t *testing.T, tenant string, writeErr error) (*storeCalls, *goakt.PID, string) {
		t.Helper()
		ctx := context.Background()

		calls := new(storeCalls)
		baseEvents := testkit.NewEventsStore()
		baseSnapshots := testkit.NewSnapshotStore()
		require.NoError(t, baseEvents.Connect(ctx))
		require.NoError(t, baseSnapshots.Connect(ctx))
		stream := eventstream.New()

		exts := []extension.Extension{
			extensions.NewEventsStore(&recordingEventsStore{EventsStore: baseEvents, calls: calls, writeErr: writeErr}),
			extensions.NewEventsStream(stream),
			extensions.NewSnapshotStore(&recordingSnapshotStore{SnapshotStore: baseSnapshots, calls: calls}),
		}

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		entityCfg := &extensions.EntityConfig{
			SnapshotInterval:          1,
			HasRetentionPolicy:        true,
			DeleteEventsOnSnapshot:    true,
			DeleteSnapshotsOnSnapshot: true,
		}
		deps := []extension.Dependency{behavior, entityCfg}
		if tenant != "" {
			exts = append(exts, extensions.NewTenancyMarker())
			deps = append(deps, extensions.NewEntityTenantScope(tenant))
		}

		actorSystem, err := goakt.NewActorSystem("SnapshotSequenceSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(exts...),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		t.Cleanup(func() {
			_ = actorSystem.Stop(ctx)
			stream.Close()
			_ = baseEvents.Disconnect(ctx)
			_ = baseSnapshots.Disconnect(ctx)
		})

		pid, err := actorSystem.Spawn(ctx, behavior.ID(), newEventSourcedActor(),
			goakt.WithDependencies(deps...), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		return calls, pid, persistenceID
	}

	ask := func(t *testing.T, ctx context.Context, pid *goakt.PID, msg any) *egopb.CommandReply {
		t.Helper()
		reply, err := goakt.Ask(ctx, pid, msg, 5*time.Second)
		require.NoError(t, err)
		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		return commandReply
	}

	// runTwoCommands drives the entity across two snapshot boundaries and waits
	// for the asynchronous retention of the second one.
	runTwoCommands := func(t *testing.T, ctx context.Context, calls *storeCalls, pid *goakt.PID, persistenceID string) []storeCall {
		t.Helper()
		first := ask(t, ctx, pid, &testpb.CreateAccount{AccountBalance: 500})
		require.IsType(t, new(egopb.CommandReply_StateReply), first.GetReply())
		second := ask(t, ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100})
		require.IsType(t, new(egopb.CommandReply_StateReply), second.GetReply())

		require.Eventually(t, func() bool {
			return calls.has("DeleteEvents", 2) && calls.has("DeleteSnapshots", 1)
		}, 10*time.Second, 25*time.Millisecond)
		return calls.snapshot()
	}

	// requireOrdered asserts, for the second boundary, that the events write
	// precedes the snapshot write and that both retention deletes follow it.
	requireOrdered := func(t *testing.T, got []storeCall) {
		t.Helper()
		write := indexOf(got, "WriteEvents", 2)
		snapshot := indexOf(got, "WriteSnapshot", 2)
		require.GreaterOrEqual(t, write, 0, "events write for sequence 2: %v", got)
		require.Greater(t, snapshot, write, "snapshot must follow the events write: %v", got)
		require.Greater(t, indexOf(got, "DeleteEvents", 2), snapshot, "event delete must follow the snapshot: %v", got)
		require.Greater(t, indexOf(got, "DeleteSnapshots", 1), snapshot, "snapshot delete must follow the snapshot: %v", got)
	}

	t.Run("events write, then snapshot write, then the retention deletes", func(t *testing.T) {
		ctx := context.Background()
		calls, pid, persistenceID := spawn(t, "", nil)

		got := runTwoCommands(t, ctx, calls, pid, persistenceID)

		requireOrdered(t, got)
		for _, call := range got {
			assert.Equal(t, persistence.Unscoped(), call.scope, "call %s", call.op)
		}
	})

	t.Run("a failed events write never writes a snapshot", func(t *testing.T) {
		ctx := context.Background()
		calls, pid, _ := spawn(t, "", assert.AnError)

		reply := ask(t, ctx, pid, &testpb.CreateAccount{AccountBalance: 500})
		require.IsType(t, new(egopb.CommandReply_ErrorReply), reply.GetReply())

		require.True(t, calls.has("WriteEvents", 1))
		require.Never(t, func() bool {
			return calls.count("WriteSnapshot") > 0 || calls.count("DeleteEvents") > 0 || calls.count("DeleteSnapshots") > 0
		}, time.Second, 25*time.Millisecond)
	})

	t.Run("a tenant scope reaches the snapshot write and the retention deletes", func(t *testing.T) {
		ctx := context.Background()
		calls, pid, persistenceID := spawn(t, "acme", nil)

		want, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)
		tenantContext, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		tenantCtx, err := tenancy.Attach(ctx, tenantContext)
		require.NoError(t, err)

		got := runTwoCommands(t, tenantCtx, calls, pid, persistenceID)

		requireOrdered(t, got)
		for _, op := range []string{"WriteEvents", "WriteSnapshot", "DeleteEvents", "DeleteSnapshots"} {
			seen := false
			for _, call := range got {
				if call.op == op {
					seen = true
					assert.Equal(t, want, call.scope, "%s must receive the tenant scope", op)
				}
			}
			assert.True(t, seen, "%s was never called", op)
		}
	})
}
