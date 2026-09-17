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

package testkit

import (
	"context"
	"sort"
	"sync"

	"go.uber.org/atomic"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
)

// EventKey identified an individual event record in the old, per-event
// keying scheme.
//
// Deprecated: EventStore now keys its internal map by persistenceID alone,
// storing an immutable *eventLog per persistenceID so that conditional
// writes can be evaluated and committed with a single sync.Map
// CompareAndSwap/LoadOrStore. EventKey is unused and kept only for source
// compatibility.
type EventKey struct {
	PersistenceID  string
	SequenceNumber uint64
}

// eventLog is the immutable value published for a given persistenceID.
// revision is the StorageRevision (the highest committed SequenceNumber);
// it is deliberately tracked independently of len(events) so that
// DeleteEvents can truncate events for retention without resetting the
// revision a conditional write compares against.
type eventLog struct {
	revision uint64
	events   []*egopb.Event
}

type EventStore struct {
	// db maps persistenceID (string) -> *eventLog.
	db        *sync.Map
	connected *atomic.Bool
}

var _ persistence.EventsStore = (*EventStore)(nil)

func NewEventsStore() *EventStore {
	return &EventStore{
		db:        &sync.Map{},
		connected: atomic.NewBool(false),
	}
}

func (x *EventStore) Connect(_ context.Context) error {
	if x.connected.Load() {
		return nil
	}
	x.connected.Store(true)
	return nil
}

func (x *EventStore) Disconnect(_ context.Context) error {
	if !x.connected.Load() {
		return nil
	}
	x.db.Range(func(key interface{}, _ interface{}) bool {
		x.db.Delete(key)
		return true
	})
	x.connected.Store(false)
	return nil
}

// WriteEvents implements persistence.EventsStore. See that interface's doc
// comment for the full contract; the conditional path is evaluated and
// committed via sync.Map's own CompareAndSwap/LoadOrStore so that two
// unserialized goroutines racing against the same persistenceID compete
// directly against the store, never against caller-side ordering.
func (x *EventStore) WriteEvents(_ context.Context, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	if !precondition.Valid() {
		return persistence.ErrInvalidPrecondition
	}

	if precondition.IsUnconditional() {
		return x.writeUnconditional(events)
	}

	if len(events) == 0 {
		return persistence.ErrPreconditionScope
	}
	persistenceID := events[0].GetPersistenceId()
	for _, event := range events[1:] {
		if event.GetPersistenceId() != persistenceID {
			return persistence.ErrPreconditionScope
		}
	}
	return x.writeConditional(persistenceID, events, precondition)
}

// writeConditional evaluates a genesis or exact-revision precondition for
// persistenceID and commits events as one atomic operation. A single
// LoadOrStore (genesis) or CompareAndSwap (exact-revision) attempt decides
// the outcome — a failed attempt is a terminal conflict, never retried,
// since retrying would silently convert a declared, no-longer-valid
// expectation into success.
func (x *EventStore) writeConditional(persistenceID string, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	raw, exists := x.db.Load(persistenceID)
	var old *eventLog
	if exists {
		old = raw.(*eventLog)
	}

	if precondition.IsGenesis() {
		if exists {
			return persistence.NewConflictError(persistenceID, precondition, persistence.WithActualRevision(old.revision))
		}
		newLog := newEventLog(nil, events)
		if actual, loaded := x.db.LoadOrStore(persistenceID, newLog); loaded {
			return persistence.NewConflictError(persistenceID, precondition, persistence.WithActualRevision(actual.(*eventLog).revision))
		}
		return nil
	}

	expectedRevision, _ := precondition.Revision()
	if !exists || old.revision != expectedRevision {
		if exists {
			return persistence.NewConflictError(persistenceID, precondition, persistence.WithActualRevision(old.revision))
		}
		return persistence.NewConflictError(persistenceID, precondition)
	}

	newLog := newEventLog(old, events)
	if !x.db.CompareAndSwap(persistenceID, old, newLog) {
		if actual, ok := x.db.Load(persistenceID); ok {
			return persistence.NewConflictError(persistenceID, precondition, persistence.WithActualRevision(actual.(*eventLog).revision))
		}
		return persistence.NewConflictError(persistenceID, precondition)
	}
	return nil
}

// writeUnconditional preserves legacy, precondition-free write semantics:
// every event commits, grouped by its own PersistenceId. Concurrent
// unconditional writers targeting the same persistenceID still compete
// against the store's own CompareAndSwap/LoadOrStore rather than losing
// events to a race, but a losing attempt here simply retries against the
// latest state instead of returning a conflict, since Unconditional() never
// declares an expectation that can go stale.
func (x *EventStore) writeUnconditional(events []*egopb.Event) error {
	if len(events) == 0 {
		return nil
	}

	grouped := make(map[string][]*egopb.Event, len(events))
	order := make([]string, 0, len(events))
	for _, event := range events {
		id := event.GetPersistenceId()
		if _, seen := grouped[id]; !seen {
			order = append(order, id)
		}
		grouped[id] = append(grouped[id], event)
	}

	for _, persistenceID := range order {
		group := grouped[persistenceID]
		for {
			raw, exists := x.db.Load(persistenceID)
			var old *eventLog
			if exists {
				old = raw.(*eventLog)
			}
			newLog := newEventLog(old, group)

			if !exists {
				if _, loaded := x.db.LoadOrStore(persistenceID, newLog); !loaded {
					break
				}
				continue
			}
			if x.db.CompareAndSwap(persistenceID, old, newLog) {
				break
			}
		}
	}
	return nil
}

// newEventLog builds the successor log for old (nil for a nonexistent
// persistenceID) by appending newEvents, advancing revision to the highest
// SequenceNumber seen. old is never mutated; a wholly new *eventLog is
// published so that CompareAndSwap's pointer-identity comparison stays
// meaningful.
func newEventLog(old *eventLog, newEvents []*egopb.Event) *eventLog {
	var revision uint64
	var existing []*egopb.Event
	if old != nil {
		revision = old.revision
		existing = old.events
	}

	merged := make([]*egopb.Event, 0, len(existing)+len(newEvents))
	merged = append(merged, existing...)
	merged = append(merged, newEvents...)

	for _, event := range newEvents {
		if sn := event.GetSequenceNumber(); sn > revision {
			revision = sn
		}
	}

	return &eventLog{revision: revision, events: merged}
}

func (x *EventStore) Ping(ctx context.Context) error {
	if !x.connected.Load() {
		return x.Connect(ctx)
	}
	return nil
}

// DeleteEvents truncates persistenceID's retained events up to and
// including toSequenceNumber, while preserving the log's revision: retention
// must never reset StorageRevision, or a stale writer whose exact-revision
// precondition predates the deleted events would incorrectly win a
// conditional write.
func (x *EventStore) DeleteEvents(_ context.Context, persistenceID string, toSequenceNumber uint64) error {
	for {
		raw, exists := x.db.Load(persistenceID)
		if !exists {
			return nil
		}
		old := raw.(*eventLog)

		retained := make([]*egopb.Event, 0, len(old.events))
		for _, event := range old.events {
			if event.GetSequenceNumber() > toSequenceNumber {
				retained = append(retained, event)
			}
		}
		if len(retained) == len(old.events) {
			return nil
		}

		newLog := &eventLog{revision: old.revision, events: retained}
		if x.db.CompareAndSwap(persistenceID, old, newLog) {
			return nil
		}
	}
}

func (x *EventStore) ReplayEvents(_ context.Context, persistenceID string, fromSequenceNumber, toSequenceNumber uint64, limit uint64) ([]*egopb.Event, error) {
	raw, exists := x.db.Load(persistenceID)
	if !exists {
		return nil, nil
	}
	log := raw.(*eventLog)

	var events []*egopb.Event
	for _, event := range log.events {
		sn := event.GetSequenceNumber()
		if sn >= fromSequenceNumber && sn <= toSequenceNumber {
			events = append(events, event)
		}
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].GetSequenceNumber() < events[j].GetSequenceNumber()
	})

	if len(events) > int(limit) {
		events = events[:int(limit)]
	}

	return events, nil
}

func (x *EventStore) GetLatestEvent(_ context.Context, persistenceID string) (*egopb.Event, error) {
	raw, exists := x.db.Load(persistenceID)
	if !exists {
		return nil, nil
	}
	log := raw.(*eventLog)
	if len(log.events) == 0 {
		return nil, nil
	}

	latest := log.events[0]
	for _, event := range log.events[1:] {
		if event.GetSequenceNumber() > latest.GetSequenceNumber() {
			latest = event
		}
	}
	return latest, nil
}

func (x *EventStore) PersistenceIDs(_ context.Context, pageSize uint64, pageToken string) (persistenceIDs []string, nextPageToken string, err error) {
	// step 1: collect the persistence ids
	keys := make([]string, 0)
	x.db.Range(func(key, _ any) bool {
		keys = append(keys, key.(string))
		return true
	})

	// step 2: sort them
	sort.Strings(keys)

	// step 3: paginate the sorted keys
	startIndex := 0
	if pageToken != "" {
		// Find the index of the pageToken in the sorted keys
		for i, key := range keys {
			if key > pageToken {
				startIndex = i
				break
			}
		}
	}

	// Collect up to pageSize items starting from startIndex
	endIndex := startIndex + int(pageSize)
	if endIndex > len(keys) {
		endIndex = len(keys)
	}
	persistenceIDs = keys[startIndex:endIndex]

	// step 4: determine the nextPageToken
	switch {
	case endIndex < len(keys):
		nextPageToken = keys[endIndex]
	default:
		nextPageToken = ""
	}

	return persistenceIDs, nextPageToken, nil
}

func (x *EventStore) GetShardEvents(_ context.Context, shardNumber uint64, offset int64, limit uint64) ([]*egopb.Event, int64, error) {
	var shardEvents []*egopb.Event
	x.db.Range(func(_ any, value any) bool {
		log := value.(*eventLog)
		for _, event := range log.events {
			if event.GetShard() == shardNumber {
				shardEvents = append(shardEvents, event)
			}
		}
		return true
	})

	if len(shardEvents) == 0 {
		return nil, 0, nil
	}

	var events []*egopb.Event
	for _, event := range shardEvents {
		if event.GetTimestamp() > offset {
			if len(events) <= int(limit) {
				events = append(events, event)
			}
		}
	}

	if len(events) == 0 {
		return nil, 0, nil
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].GetTimestamp() <= events[j].GetTimestamp()
	})

	nextOffset := events[len(events)-1].GetTimestamp()
	return events, nextOffset, nil
}

func (x *EventStore) ShardOffsets(context.Context) (map[uint64]int64, error) {
	offsets := make(map[uint64]int64)
	x.db.Range(func(_ any, value any) bool {
		log := value.(*eventLog)
		for _, event := range log.events {
			if event.GetTimestamp() > offsets[event.GetShard()] {
				offsets[event.GetShard()] = event.GetTimestamp()
			}
		}
		return true
	})
	return offsets, nil
}
