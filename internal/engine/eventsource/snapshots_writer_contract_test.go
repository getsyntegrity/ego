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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	mockencryption "github.com/getsyntegrity/ego/mocks/encryption"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/testkit"
)

// snapshotWriterRig wires a snapshots writer and an events janitor on one
// actor system whose stores record into one shared, ordered log.
type snapshotWriterRig struct {
	calls   *storeCalls
	writer  *goakt.PID
	janitor *goakt.PID
}

func newSnapshotWriterRig(t *testing.T, snapshotErr error, encryptor *extensions.EncryptorExtension, writeDelay time.Duration) *snapshotWriterRig {
	t.Helper()
	ctx := context.Background()

	calls := new(storeCalls)
	baseEvents := testkit.NewEventsStore()
	baseSnapshots := testkit.NewSnapshotStore()
	require.NoError(t, baseEvents.Connect(ctx))
	require.NoError(t, baseSnapshots.Connect(ctx))
	t.Cleanup(func() {
		_ = baseEvents.Disconnect(ctx)
		_ = baseSnapshots.Disconnect(ctx)
	})

	exts := []extension.Extension{
		extensions.NewEventsStore(&loggingEventsStore{EventsStore: baseEvents, calls: calls}),
		extensions.NewSnapshotStore(&loggingSnapshotStore{SnapshotStore: baseSnapshots, calls: calls, writeErr: snapshotErr, writeDelay: writeDelay}),
	}
	if encryptor != nil {
		exts = append(exts, encryptor)
	}

	actorSystem, err := goakt.NewActorSystem("SnapshotWriterContractSystem",
		goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
		goakt.WithExtensions(exts...),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	t.Cleanup(func() { _ = actorSystem.Stop(ctx) })

	rig := &snapshotWriterRig{calls: calls}
	rig.writer, err = actorSystem.Spawn(ctx, "snapshots-writer", newSnapshotsWriterActor())
	require.NoError(t, err)
	rig.janitor, err = actorSystem.Spawn(ctx, "events-janitor", newEventsJanitorActor())
	require.NoError(t, err)
	return rig
}

// persist sends one snapshot at sequence 4 to the writer. With withRetention
// the request carries a retention policy for interval 2, so main computes the
// previous snapshot as sequence 2.
func (r *snapshotWriterRig) persist(t *testing.T, scope persistence.Scope, withRetention bool) {
	t.Helper()
	state, err := anypb.New(wrapperspb.String("state"))
	require.NoError(t, err)
	req := &persistSnapshotRequest{
		snapshot: &egopb.Snapshot{PersistenceId: "entity-1", SequenceNumber: 4, State: state},
		scope:    scope,
	}
	if withRetention {
		req.janitor = r.janitor
		req.retentionReq = &applyRetentionRequest{
			persistenceID:             "entity-1",
			eventsCounter:             4,
			snapshotInterval:          2,
			deleteEventsOnSnapshot:    true,
			deleteSnapshotsOnSnapshot: true,
			scope:                     scope,
		}
	}
	require.NoError(t, goakt.Tell(context.Background(), r.writer, req))
}

func (r *snapshotWriterRig) awaitRetention(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		return r.calls.count(opDeleteEvents) == 1 && r.calls.count(opDeleteSnapshots) == 1
	}, 10*time.Second, 10*time.Millisecond)
}

func TestSnapshotsWriterContract(t *testing.T) {
	t.Run("the scope reaches the snapshot write and the retention deletes unchanged", func(t *testing.T) {
		scope, err := persistence.NewTenantScope("tenant-a")
		require.NoError(t, err)
		rig := newSnapshotWriterRig(t, nil, nil, 0)

		rig.persist(t, scope, true)

		rig.awaitRetention(t)
		require.Equal(t, 1, rig.calls.count(opWriteSnapshot))
		for _, call := range rig.calls.snapshot() {
			assert.Equal(t, scope, call.scope, "call %s", call.op)
		}
	})

	t.Run("retention runs after the snapshot write", func(t *testing.T) {
		rig := newSnapshotWriterRig(t, nil, nil, 150*time.Millisecond)

		rig.persist(t, persistence.Unscoped(), true)

		rig.awaitRetention(t)
		snapshot := rig.calls.indexOf(opWriteSnapshot, 4)
		require.GreaterOrEqual(t, snapshot, 0)
		assert.Greater(t, rig.calls.indexOf(opDeleteEvents, 4), snapshot)
		// the previous snapshot is the current sequence minus the interval
		assert.Greater(t, rig.calls.indexOf(opDeleteSnapshots, 2), snapshot)
	})

	t.Run("a failed snapshot write is retried and then forwards no retention", func(t *testing.T) {
		rig := newSnapshotWriterRig(t, assert.AnError, nil, 0)

		rig.persist(t, persistence.Unscoped(), true)

		require.Eventually(t, func() bool { return rig.calls.count(opWriteSnapshot) == defaultMaxRetries+1 },
			10*time.Second, 10*time.Millisecond)
		require.Never(t, func() bool {
			return rig.calls.anyRetention() || rig.calls.count(opWriteSnapshot) > defaultMaxRetries+1
		}, time.Second, 10*time.Millisecond)
	})

	t.Run("an encryption failure writes nothing and forwards no retention", func(t *testing.T) {
		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Encrypt(mock.Anything, "entity-1", mock.Anything).Return(nil, "", assert.AnError)
		rig := newSnapshotWriterRig(t, nil, extensions.NewEncryptor(encryptor), 0)

		rig.persist(t, persistence.Unscoped(), true)

		require.Never(t, func() bool { return len(rig.calls.snapshot()) > 0 }, time.Second, 10*time.Millisecond)
		encryptor.AssertExpectations(t)
	})

	t.Run("a nil retention request writes the snapshot and triggers no deletes", func(t *testing.T) {
		rig := newSnapshotWriterRig(t, nil, nil, 0)

		rig.persist(t, persistence.Unscoped(), false)

		require.Eventually(t, func() bool { return rig.calls.count(opWriteSnapshot) == 1 }, 10*time.Second, 10*time.Millisecond)
		require.Never(t, func() bool { return rig.calls.anyRetention() }, time.Second, 10*time.Millisecond)
	})
}
