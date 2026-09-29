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

// Package snapshots holds the two GoAkt actors that persist an event-sourced
// entity's snapshots and then apply its retention policy: the writer stores
// the snapshot and, only after the store confirms it, forwards the retention
// request to the janitor, which deletes old events and snapshots.
//
// The entity actor spawns both as children with NewWriter and NewJanitor and
// talks to the writer only through Tell. The messages the two actors exchange
// stay unexported: they are a protocol between the writer and the janitor,
// not part of the entity's contract.
package snapshots

import (
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
)

// Retention describes the cleanup to run once a snapshot is persisted. It
// carries the janitor to notify and the values the entity already computed.
type Retention struct {
	// Janitor is the child actor created with NewJanitor that applies the cleanup.
	Janitor *goakt.PID
	// PersistenceID identifies the entity whose events and snapshots are cleaned.
	PersistenceID string
	// EventsCounter is the entity's latest event sequence number.
	EventsCounter uint64
	// SnapshotInterval is how many events separate two snapshots.
	SnapshotInterval uint64
	// DeleteEventsOnSnapshot deletes the events covered by the snapshot.
	DeleteEventsOnSnapshot bool
	// DeleteSnapshotsOnSnapshot deletes the previous snapshot.
	DeleteSnapshotsOnSnapshot bool
	// EventsRetentionCount is how many of the latest events survive the cleanup.
	EventsRetentionCount uint64
}

// NewWriter creates the actor that persists snapshots. It is meant to be
// spawned as a child of the entity actor.
func NewWriter() goakt.Actor {
	return newWriterActor()
}

// NewJanitor creates the actor that deletes old events and snapshots after a
// snapshot is persisted. It is meant to be spawned as a child of the entity
// actor.
func NewJanitor() goakt.Actor {
	return newJanitorActor()
}

// Tell asks writer to persist snapshot under scope, without waiting for the
// outcome. When retention is not nil, the writer forwards the cleanup to
// retention.Janitor, with the same scope, only after the snapshot is
// confirmed persisted. The snapshot carries unencrypted state; the writer
// encrypts it.
func Tell(ctx *goakt.ReceiveContext, writer *goakt.PID, snapshot *egopb.Snapshot, scope persistence.Scope, retention *Retention) {
	req := &persistSnapshotRequest{
		snapshot: snapshot,
		scope:    scope,
	}

	if retention != nil {
		req.retentionReq = &applyRetentionRequest{
			persistenceID:             retention.PersistenceID,
			eventsCounter:             retention.EventsCounter,
			snapshotInterval:          retention.SnapshotInterval,
			deleteEventsOnSnapshot:    retention.DeleteEventsOnSnapshot,
			deleteSnapshotsOnSnapshot: retention.DeleteSnapshotsOnSnapshot,
			eventsRetentionCount:      retention.EventsRetentionCount,
			scope:                     scope,
		}
		req.janitor = retention.Janitor
	}

	ctx.Tell(writer, req)
}
