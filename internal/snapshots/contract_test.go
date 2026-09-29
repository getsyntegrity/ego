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

package snapshots

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"github.com/tochemey/goakt/v4/log"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/tenancy"
	"github.com/getsyntegrity/ego/testkit"
)

// call is one store call observed by the contract stores.
type call struct {
	op    string
	scope persistence.Scope
	seqNr uint64
}

// callLog records, in arrival order, the calls of both stores.
type callLog struct {
	mu    sync.Mutex
	calls []call
}

func (l *callLog) add(op string, scope persistence.Scope, seqNr uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call{op: op, scope: scope, seqNr: seqNr})
}

func (l *callLog) snapshot() []call {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]call(nil), l.calls...)
}

func (l *callLog) count(op string) int {
	n := 0
	for _, c := range l.snapshot() {
		if c.op == op {
			n++
		}
	}
	return n
}

func (l *callLog) index(op string, seqNr uint64) int {
	for i, c := range l.snapshot() {
		if c.op == op && c.seqNr == seqNr {
			return i
		}
	}
	return -1
}

type contractEventsStore struct {
	persistence.EventsStore
	log *callLog
}

func (x *contractEventsStore) DeleteEvents(_ context.Context, scope persistence.Scope, _ string, toSequenceNumber uint64) error {
	x.log.add("DeleteEvents", scope, toSequenceNumber)
	return nil
}

type contractSnapshotStore struct {
	persistence.SnapshotStore
	log      *callLog
	writeErr error
}

func (x *contractSnapshotStore) WriteSnapshot(_ context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	x.log.add("WriteSnapshot", scope, snapshot.GetSequenceNumber())
	return x.writeErr
}

func (x *contractSnapshotStore) DeleteSnapshots(_ context.Context, scope persistence.Scope, _ string, toSequenceNumber uint64) error {
	x.log.add("DeleteSnapshots", scope, toSequenceNumber)
	return nil
}

// failingEncryptor fails every encryption.
type failingEncryptor struct{}

func (failingEncryptor) Encrypt(context.Context, string, []byte) ([]byte, string, error) {
	return nil, "", assert.AnError
}

func (failingEncryptor) Decrypt(context.Context, string, []byte, string) ([]byte, error) {
	return nil, assert.AnError
}

// tellCommand asks the parent actor to call Tell from its Receive, which is
// where the entity calls it in production.
type tellCommand struct {
	writer    *goakt.PID
	snapshot  *egopb.Snapshot
	scope     persistence.Scope
	retention *Retention
}

// tellParent stands in for the EventSourcedActor: Tell needs a ReceiveContext.
type tellParent struct{}

var _ goakt.Actor = (*tellParent)(nil)

func (*tellParent) PreStart(*goakt.Context) error { return nil }
func (*tellParent) PostStop(*goakt.Context) error { return nil }

func (*tellParent) Receive(ctx *goakt.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goakt.PostStart:
	case *tellCommand:
		Tell(ctx, msg.writer, msg.snapshot, msg.scope, msg.retention)
	default:
		ctx.Unhandled()
	}
}

// contractRig wires the writer, the janitor and the parent on one actor system.
type contractRig struct {
	log     *callLog
	writer  *goakt.PID
	janitor *goakt.PID
	parent  *goakt.PID
}

func newContractRig(t *testing.T, snapshotErr error, encryptor *extensions.EncryptorExtension) *contractRig {
	t.Helper()
	ctx := context.Background()

	calls := new(callLog)
	baseEvents := testkit.NewEventsStore()
	require.NoError(t, baseEvents.Connect(ctx))
	t.Cleanup(func() { _ = baseEvents.Disconnect(ctx) })

	exts := []extension.Extension{
		extensions.NewEventsStore(&contractEventsStore{EventsStore: baseEvents, log: calls}),
		extensions.NewSnapshotStore(&contractSnapshotStore{SnapshotStore: testkit.NewSnapshotStore(), log: calls, writeErr: snapshotErr}),
	}
	if encryptor != nil {
		exts = append(exts, encryptor)
	}

	actorSystem, err := goakt.NewActorSystem("SnapshotsContractSystem",
		goakt.WithLogger(log.DiscardLogger),
		goakt.WithExtensions(exts...),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	t.Cleanup(func() { _ = actorSystem.Stop(ctx) })

	rig := &contractRig{log: calls}
	rig.writer, err = actorSystem.Spawn(ctx, "snapshots-writer", NewWriter())
	require.NoError(t, err)
	rig.janitor, err = actorSystem.Spawn(ctx, "events-janitor", NewJanitor())
	require.NoError(t, err)
	rig.parent, err = actorSystem.Spawn(ctx, "parent", new(tellParent))
	require.NoError(t, err)
	return rig
}

func (r *contractRig) tell(t *testing.T, scope persistence.Scope, retention *Retention) {
	t.Helper()
	state, err := anypb.New(wrapperspb.String("state"))
	require.NoError(t, err)
	snapshot := &egopb.Snapshot{PersistenceId: "entity-1", SequenceNumber: 4, State: state}
	require.NoError(t, goakt.Tell(context.Background(), r.parent, &tellCommand{
		writer: r.writer, snapshot: snapshot, scope: scope, retention: retention,
	}))
}

func (r *contractRig) retention() *Retention {
	return &Retention{
		Janitor:                   r.janitor,
		PersistenceID:             "entity-1",
		EventsCounter:             4,
		SnapshotInterval:          2,
		DeleteEventsOnSnapshot:    true,
		DeleteSnapshotsOnSnapshot: true,
	}
}

func TestSnapshotsContract(t *testing.T) {
	t.Run("the scope reaches the snapshot write and the retention deletes unchanged", func(t *testing.T) {
		scope, err := persistence.NewTenantScope(tenancy.TenantID("tenant-a"))
		require.NoError(t, err)
		rig := newContractRig(t, nil, nil)

		rig.tell(t, scope, rig.retention())

		require.Eventually(t, func() bool {
			return rig.log.count("WriteSnapshot") == 1 && rig.log.count("DeleteEvents") == 1 && rig.log.count("DeleteSnapshots") == 1
		}, 5*time.Second, 10*time.Millisecond)
		for _, c := range rig.log.snapshot() {
			assert.Equal(t, scope, c.scope, "call %s", c.op)
		}
	})

	t.Run("retention runs after the snapshot write", func(t *testing.T) {
		rig := newContractRig(t, nil, nil)

		rig.tell(t, persistence.Unscoped(), rig.retention())

		require.Eventually(t, func() bool {
			return rig.log.count("DeleteEvents") == 1 && rig.log.count("DeleteSnapshots") == 1
		}, 5*time.Second, 10*time.Millisecond)
		snapshot := rig.log.index("WriteSnapshot", 4)
		require.GreaterOrEqual(t, snapshot, 0)
		assert.Greater(t, rig.log.index("DeleteEvents", 4), snapshot)
		assert.Greater(t, rig.log.index("DeleteSnapshots", 2), snapshot)
	})

	t.Run("a failed snapshot write forwards no retention", func(t *testing.T) {
		rig := newContractRig(t, assert.AnError, nil)

		rig.tell(t, persistence.Unscoped(), rig.retention())

		// the writer retries the write before giving up
		require.Eventually(t, func() bool { return rig.log.count("WriteSnapshot") == defaultMaxRetries+1 },
			10*time.Second, 10*time.Millisecond)
		require.Never(t, func() bool {
			return rig.log.count("DeleteEvents") > 0 || rig.log.count("DeleteSnapshots") > 0
		}, 500*time.Millisecond, 10*time.Millisecond)
	})

	t.Run("an encryption failure writes nothing and forwards no retention", func(t *testing.T) {
		rig := newContractRig(t, nil, extensions.NewEncryptor(failingEncryptor{}))

		rig.tell(t, persistence.Unscoped(), rig.retention())

		require.Never(t, func() bool { return len(rig.log.snapshot()) > 0 }, time.Second, 10*time.Millisecond)
	})

	t.Run("a nil retention writes the snapshot and sends no janitor work", func(t *testing.T) {
		rig := newContractRig(t, nil, nil)

		rig.tell(t, persistence.Unscoped(), nil)

		require.Eventually(t, func() bool { return rig.log.count("WriteSnapshot") == 1 }, 5*time.Second, 10*time.Millisecond)
		require.Never(t, func() bool {
			return rig.log.count("DeleteEvents") > 0 || rig.log.count("DeleteSnapshots") > 0
		}, 500*time.Millisecond, 10*time.Millisecond)
	})
}
