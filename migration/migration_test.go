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

package migration

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"github.com/pablogore/kit-logger/pkg/logger/kitlogtest"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/engine"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/tenancy"
	"github.com/getsyntegrity/ego/testkit"
)

// buildLegacyEventBytes constructs raw protobuf bytes for an Event that includes
// the old resulting_state at field 5. Since the current generated Event no longer
// has that field, we manually append the field 5 bytes to a normally-serialized Event.
func buildLegacyEventBytes(t testing.TB, persistenceID string, seqNr uint64, event *anypb.Any, state *anypb.Any, timestamp int64, shard uint64) []byte {
	t.Helper()

	// Serialize the base event (without resulting_state)
	baseEvent := &egopb.Event{
		PersistenceId:  persistenceID,
		SequenceNumber: seqNr,
		IsDeleted:      false,
		Event:          event,
		Timestamp:      timestamp,
		Shard:          shard,
	}
	baseBytes, err := proto.Marshal(baseEvent)
	if err != nil {
		t.Fatalf("marshal base event for %q: %v", persistenceID, err)
	}

	if state == nil {
		return baseBytes
	}

	// Serialize the state as field 5 (length-delimited, wire type 2)
	// Tag = (5 << 3) | 2 = 42
	stateBytes, err := proto.Marshal(state)
	if err != nil {
		t.Fatalf("marshal legacy state for %q: %v", persistenceID, err)
	}

	// Build the field 5 tag + length + value
	tag := encodeVarint(42) // field 5, wire type 2
	length := encodeVarint(uint64(len(stateBytes)))

	// Append field 5 to the base event bytes
	result := make([]byte, 0, len(baseBytes)+len(tag)+len(length)+len(stateBytes))
	result = append(result, baseBytes...)
	result = append(result, tag...)
	result = append(result, length...)
	result = append(result, stateBytes...)
	return result
}

// encodeVarint encodes a uint64 as a protobuf varint
func encodeVarint(v uint64) []byte {
	var buf [10]byte
	n := 0
	for v >= 0x80 {
		buf[n] = byte(v&0x7f) | 0x80
		v >>= 7
		n++
	}
	buf[n] = byte(v)
	return buf[:n+1]
}

// writeLegacyEvent writes an event with legacy resulting_state into the events store.
// It serializes the event with field 5 appended, then deserializes it back using the
// current Event proto (which puts field 5 into unknown fields), and writes it.
func writeLegacyEvent(t testing.TB, store *testkit.EventStore, persistenceID string, seqNr uint64, eventPayload *anypb.Any, state *anypb.Any, ts int64, shard uint64) {
	t.Helper()
	raw := buildLegacyEventBytes(t, persistenceID, seqNr, eventPayload, state, ts, shard)

	// Deserialize using the current Event proto — field 5 goes into unknown fields
	evt := new(egopb.Event)
	if err := proto.Unmarshal(raw, evt); err != nil {
		t.Fatalf("unmarshal legacy event %q/%d: %v", persistenceID, seqNr, err)
	}

	if err := store.WriteEvents(context.Background(), persistence.Unscoped(), []*egopb.Event{evt}, persistence.Unconditional()); err != nil {
		t.Fatalf("write legacy event %q/%d: %v", persistenceID, seqNr, err)
	}
}

// connectedStores returns a connected events store and snapshot store.
func connectedStores(t testing.TB, bg context.Context) (*testkit.EventStore, *testkit.SnapshotStore) {
	t.Helper()
	eventStore := testkit.NewEventsStore()
	if err := eventStore.Connect(bg); err != nil {
		t.Fatalf("connect events store: %v", err)
	}
	snapshotStore := testkit.NewSnapshotStore()
	if err := snapshotStore.Connect(bg); err != nil {
		t.Fatalf("connect snapshot store: %v", err)
	}
	return eventStore, snapshotStore
}

// mustAny wraps a message in an Any and fails the test if that is impossible.
func mustAny(t testing.TB, m proto.Message) *anypb.Any {
	t.Helper()
	a, err := anypb.New(m)
	if err != nil {
		t.Fatalf("wrap %T in Any: %v", m, err)
	}
	return a
}

// panicValue runs fn and returns what it panicked with, or nil when it did not panic.
func panicValue(fn func()) (recovered any) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}

func TestMigratorRun(t *testing.T) {
	specs.Describe(t, "Migrator.Run turns legacy resulting_state events into snapshots", func(s *specs.Spec) {
		s.It("migrates legacy events with resulting_state to snapshots", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx.T, bg)

			// Create a fake state to embed as resulting_state
			ts := timestamppb.Now()
			stateAny := mustAny(ctx.T, ts)
			eventAny := mustAny(ctx.T, timestamppb.Now())

			// Write 3 legacy events for entity "entity-1"
			for i := uint64(1); i <= 3; i++ {
				writeLegacyEvent(ctx.T, eventStore, "entity-1", i, eventAny, stateAny, int64(i*100), 0)
			}

			// Run migration
			migrator := mustNew(ctx.T, eventStore, snapshotStore,
				WithPageSize(10),
				WithLogger(engine.DiscardLogger),
			)
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())

			// Verify snapshot was written for entity-1 at sequence 3 (the latest)
			snapshot, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "entity-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snapshot).To(specs.Not(specs.BeNil()))
			ctx.Expect(snapshot.GetPersistenceId()).ToEqual("entity-1")
			ctx.Expect(snapshot.GetSequenceNumber()).ToEqual(uint64(3))
			ctx.Expect(snapshot.GetState()).To(specs.Not(specs.BeNil()))

			// Verify the state content
			var recovered timestamppb.Timestamp
			ctx.Expect(snapshot.GetState().UnmarshalTo(&recovered)).To(specs.BeNil())
			ctx.Expect(proto.Equal(ts, &recovered)).To(specs.BeTrue())

			ctx.Expect(eventStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(snapshotStore.Disconnect(bg)).To(specs.BeNil())
		})

		s.It("skips entities with no resulting_state", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx.T, bg)

			eventAny := mustAny(ctx.T, timestamppb.Now())

			// Write events without resulting_state (new-format events)
			for i := uint64(1); i <= 3; i++ {
				writeLegacyEvent(ctx.T, eventStore, "entity-new", i, eventAny, nil, int64(i*100), 0)
			}

			migrator := mustNew(ctx.T, eventStore, snapshotStore,
				WithPageSize(10),
				WithLogger(engine.DiscardLogger),
			)
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())

			// No snapshot should exist
			snapshot, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "entity-new")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snapshot).To(specs.BeNil())

			ctx.Expect(eventStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(snapshotStore.Disconnect(bg)).To(specs.BeNil())
		})

		s.It("handles multiple entities", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx.T, bg)

			state1, _ := anypb.New(&timestamppb.Timestamp{Seconds: 111})
			state2, _ := anypb.New(&timestamppb.Timestamp{Seconds: 222})
			eventAny, _ := anypb.New(timestamppb.Now())

			writeLegacyEvent(ctx.T, eventStore, "e1", 1, eventAny, state1, 100, 0)
			writeLegacyEvent(ctx.T, eventStore, "e1", 2, eventAny, state1, 200, 0)
			writeLegacyEvent(ctx.T, eventStore, "e2", 1, eventAny, state2, 100, 1)

			migrator := mustNew(ctx.T, eventStore, snapshotStore,
				WithPageSize(2),
				WithLogger(engine.DiscardLogger),
			)
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())

			snap1, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "e1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snap1).To(specs.Not(specs.BeNil()))
			ctx.Expect(snap1.GetSequenceNumber()).ToEqual(uint64(2))

			snap2, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "e2")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snap2).To(specs.Not(specs.BeNil()))
			ctx.Expect(snap2.GetSequenceNumber()).ToEqual(uint64(1))

			ctx.Expect(eventStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(snapshotStore.Disconnect(bg)).To(specs.BeNil())
		})

		s.It("is idempotent", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx.T, bg)

			stateAny, _ := anypb.New(&timestamppb.Timestamp{Seconds: 999})
			eventAny, _ := anypb.New(timestamppb.Now())

			writeLegacyEvent(ctx.T, eventStore, "idem-1", 1, eventAny, stateAny, 100, 0)

			migrator := mustNew(ctx.T, eventStore, snapshotStore,
				WithPageSize(10),
				WithLogger(engine.DiscardLogger),
			)

			// Run twice
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())

			snapshot, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "idem-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snapshot).To(specs.Not(specs.BeNil()))
			ctx.Expect(snapshot.GetSequenceNumber()).ToEqual(uint64(1))

			ctx.Expect(eventStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(snapshotStore.Disconnect(bg)).To(specs.BeNil())
		})

		s.It("returns error when events store is unreachable", func(ctx *specs.Context) {
			bg := context.Background()

			eventStore := testkit.NewEventsStore()
			// deliberately don't connect

			snapshotStore := testkit.NewSnapshotStore()
			if err := snapshotStore.Connect(bg); err != nil {
				ctx.T.Fatalf("connect snapshot store: %v", err)
			}

			migrator := mustNew(ctx.T, eventStore, snapshotStore,
				WithLogger(engine.DiscardLogger),
			)
			// Ping will auto-connect the testkit store, so this will actually succeed.
			// That's fine — testkit stores auto-connect on Ping.
			err := migrator.Run(bg)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(snapshotStore.Disconnect(bg)).To(specs.BeNil())
		})

		s.It("with empty events store", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx.T, bg)

			migrator := mustNew(ctx.T, eventStore, snapshotStore,
				WithLogger(engine.DiscardLogger),
			)
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())

			ctx.Expect(eventStore.Disconnect(bg)).To(specs.BeNil())
			ctx.Expect(snapshotStore.Disconnect(bg)).To(specs.BeNil())
		})
	})
}

func TestExtractLegacyResultingState(t *testing.T) {
	specs.Describe(t, "extractLegacyResultingState recovers the field 5 state of a legacy event", func(s *specs.Spec) {
		s.It("returns nil for nil event", func(ctx *specs.Context) {
			ctx.Expect(extractLegacyResultingState(nil)).To(specs.BeNil())
		})

		s.It("returns nil for event without unknown fields", func(ctx *specs.Context) {
			evt := &egopb.Event{
				PersistenceId:  "test",
				SequenceNumber: 1,
			}
			ctx.Expect(extractLegacyResultingState(evt)).To(specs.BeNil())
		})

		s.It("extracts state from legacy event", func(ctx *specs.Context) {
			state, _ := anypb.New(&timestamppb.Timestamp{Seconds: 42})
			eventAny, _ := anypb.New(timestamppb.Now())

			raw := buildLegacyEventBytes(ctx.T, "test", 1, eventAny, state, 100, 0)

			evt := new(egopb.Event)
			ctx.Expect(proto.Unmarshal(raw, evt)).To(specs.BeNil())

			extracted := extractLegacyResultingState(evt)
			ctx.Expect(extracted).To(specs.Not(specs.BeNil()))
			ctx.Expect(extracted.GetTypeUrl()).To(specs.Not(specs.Equal("")))

			var ts timestamppb.Timestamp
			ctx.Expect(extracted.UnmarshalTo(&ts)).To(specs.BeNil())
			ctx.Expect(ts.GetSeconds()).ToEqual(int64(42))
		})

		s.It("returns nil for event without field 5", func(ctx *specs.Context) {
			eventAny, _ := anypb.New(timestamppb.Now())
			raw := buildLegacyEventBytes(ctx.T, "test", 1, eventAny, nil, 100, 0)

			evt := new(egopb.Event)
			ctx.Expect(proto.Unmarshal(raw, evt)).To(specs.BeNil())

			ctx.Expect(extractLegacyResultingState(evt)).To(specs.BeNil())
		})
	})
}

func TestConsumeVarint(t *testing.T) {
	specs.Describe(t, "consumeVarint decodes a protobuf varint and reports how many bytes it used", func(s *specs.Spec) {
		s.It("single byte", func(ctx *specs.Context) {
			v, n := consumeVarint([]byte{0x05})
			ctx.Expect(v).ToEqual(uint64(5))
			ctx.Expect(n).ToEqual(1)
		})

		s.It("multi byte", func(ctx *specs.Context) {
			v, n := consumeVarint([]byte{0xAC, 0x02})
			ctx.Expect(v).ToEqual(uint64(300))
			ctx.Expect(n).ToEqual(2)
		})

		s.It("empty input", func(ctx *specs.Context) {
			_, n := consumeVarint([]byte{})
			ctx.Expect(n).ToEqual(-1)
		})
	})
}

func TestConsumeTag(t *testing.T) {
	specs.Describe(t, "consumeTag splits a protobuf tag into field number and wire type", func(s *specs.Spec) {
		s.It("field 5 wire type 2", func(ctx *specs.Context) {
			// Tag for field 5, wire type 2 = (5 << 3) | 2 = 42
			fieldNum, wireType, n := consumeTag([]byte{42})
			ctx.Expect(fieldNum).ToEqual(uint32(5))
			ctx.Expect(wireType).ToEqual(2)
			ctx.Expect(n).ToEqual(1)
		})

		s.It("empty input", func(ctx *specs.Context) {
			_, _, n := consumeTag([]byte{})
			ctx.Expect(n).ToEqual(-1)
		})
	})
}

func TestMigratorOptions(t *testing.T) {
	specs.Describe(t, "Migrator options configure the page size and the logger", func(s *specs.Spec) {
		s.It("WithPageSize", func(ctx *specs.Context) {
			m := mustNew(ctx.T, nil, nil, WithPageSize(100))
			ctx.Expect(m.pageSize).ToEqual(uint64(100))
		})

		s.It("WithLogger", func(ctx *specs.Context) {
			logger := engine.DiscardLogger
			m := mustNew(ctx.T, nil, nil, WithLogger(logger))
			ctx.Expect(m.logger).ToEqual(logger)
		})

		s.It("defaults", func(ctx *specs.Context) {
			m := mustNew(ctx.T, nil, nil)
			ctx.Expect(m.pageSize).ToEqual(uint64(500))
			ctx.Expect(m.logger).To(specs.Not(specs.BeNil()))
		})

		s.It("WithLogger(nil) falls back to the default logger", func(ctx *specs.Context) {
			m := mustNew(ctx.T, nil, nil, WithLogger(nil))
			ctx.Expect(m.logger == engine.DefaultLogger()).To(specs.BeTrue())
		})

		s.It("WithLogger with a typed-nil logger falls back to the default logger", func(ctx *specs.Context) {
			var typedNil *kitlogtest.MockLogger
			m := mustNew(ctx.T, nil, nil, WithLogger(typedNil))
			ctx.Expect(m.logger == engine.DefaultLogger()).To(specs.BeTrue())
		})
	})
}

func TestMigratorRunWithNilLogger(t *testing.T) {
	tests := []struct {
		name   string
		logger kitlog.Logger
	}{
		{"untyped nil", nil},
		{"typed nil", (*kitlogtest.MockLogger)(nil)},
	}
	specs.Describe(t, "Migrator.Run does not panic when the logger is nil", func(s *specs.Spec) {
		for _, tt := range tests {
			s.It(tt.name, func(ctx *specs.Context) {
				bg := context.Background()
				eventStore, snapshotStore := connectedStores(ctx.T, bg)

				stateAny := mustAny(ctx.T, &timestamppb.Timestamp{Seconds: 7})
				eventAny := mustAny(ctx.T, timestamppb.Now())
				writeLegacyEvent(ctx.T, eventStore, "nil-logger-1", 1, eventAny, stateAny, 1, 0)

				migrator := mustNew(ctx.T, eventStore, snapshotStore, WithLogger(tt.logger))
				var runErr error
				ctx.Expect(panicValue(func() { runErr = migrator.Run(bg) })).To(specs.BeNil())
				ctx.Expect(runErr).To(specs.BeNil())

				ctx.Expect(eventStore.Disconnect(bg)).To(specs.BeNil())
				ctx.Expect(snapshotStore.Disconnect(bg)).To(specs.BeNil())
			})
		}
	})
}

// messagesAt returns the messages a MockLogger captured at the given level,
// so a test can prove a caller-supplied logger really reaches the Migrator's
// logging call sites.
func messagesAt(logger *kitlogtest.MockLogger, level slog.Level) []string {
	var msgs []string
	for _, entry := range logger.Entries {
		if entry.Level == level {
			msgs = append(msgs, entry.Message)
		}
	}
	return msgs
}

func TestMigratorUsesKitLogger(t *testing.T) {
	specs.Describe(t, "Migrator logs through the kit-logger Logger it was given", func(s *specs.Spec) {
		s.It("defaults to engine.DefaultLogger()", func(ctx *specs.Context) {
			m := mustNew(ctx.T, nil, nil)
			ctx.Expect(m.logger == engine.DefaultLogger()).To(specs.BeTrue())
		})

		s.It("WithLogger injects a custom kit-logger Logger", func(ctx *specs.Context) {
			logger := kitlogtest.NewMockLogger()
			m := mustNew(ctx.T, nil, nil, WithLogger(logger))
			ctx.Expect(m.logger == kitlog.Logger(logger)).To(specs.BeTrue())
		})

		s.It("the injected logger receives the migration messages", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx.T, bg)

			stateAny := mustAny(ctx.T, timestamppb.Now())
			eventAny := mustAny(ctx.T, timestamppb.Now())

			writeLegacyEvent(ctx.T, eventStore, "entity-1", 1, eventAny, stateAny, 100, 0)

			logger := kitlogtest.NewMockLogger()
			migrator := mustNew(ctx.T, eventStore, snapshotStore, WithLogger(logger))
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())

			ctx.Expect(messagesAt(logger, slog.LevelInfo)).To(specs.Contain("migration: completed successfully"))
			ctx.Expect(messagesAt(logger, slog.LevelDebug)).To(specs.Contain("migration: snapshot written"))
		})
	})
}

// mustNew builds a Migrator and fails the test if New rejects the
// configuration.
func mustNew(t testing.TB, eventsStore persistence.EventsStore, snapshotStore persistence.SnapshotStore, opts ...Option) *Migrator {
	t.Helper()
	m, err := New(eventsStore, snapshotStore, opts...)
	if err != nil {
		t.Fatalf("New must accept the configuration: %v", err)
	}
	return m
}

// scopeSpy records the scope of every record-addressing store call, so a
// test can prove no call still hard-codes a scope.
type scopeSpy struct {
	mu    sync.Mutex
	calls []string
}

func (s *scopeSpy) record(method string, scope persistence.Scope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, method+"@"+scope.String())
}

func (s *scopeSpy) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

type spyEventsStore struct {
	persistence.EventsStore
	spy *scopeSpy
}

func (e *spyEventsStore) PersistenceIDs(ctx context.Context, scope persistence.Scope, pageSize uint64, pageToken string) ([]string, string, error) {
	e.spy.record("PersistenceIDs", scope)
	return e.EventsStore.PersistenceIDs(ctx, scope, pageSize, pageToken)
}

func (e *spyEventsStore) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string, from, to, maxNumber uint64) ([]*egopb.Event, error) {
	e.spy.record("ReplayEvents", scope)
	return e.EventsStore.ReplayEvents(ctx, scope, persistenceID, from, to, maxNumber)
}

func (e *spyEventsStore) GetLatestEvent(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Event, error) {
	e.spy.record("GetLatestEvent", scope)
	return e.EventsStore.GetLatestEvent(ctx, scope, persistenceID)
}

func (e *spyEventsStore) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	e.spy.record("WriteEvents", scope)
	return e.EventsStore.WriteEvents(ctx, scope, events, precondition)
}

func (e *spyEventsStore) DeleteEvents(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	e.spy.record("DeleteEvents", scope)
	return e.EventsStore.DeleteEvents(ctx, scope, persistenceID, toSequenceNumber)
}

type spySnapshotStore struct {
	persistence.SnapshotStore
	spy *scopeSpy
}

func (s *spySnapshotStore) WriteSnapshot(ctx context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	s.spy.record("WriteSnapshot", scope)
	return s.SnapshotStore.WriteSnapshot(ctx, scope, snapshot)
}

func (s *spySnapshotStore) GetLatestSnapshot(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Snapshot, error) {
	s.spy.record("GetLatestSnapshot", scope)
	return s.SnapshotStore.GetLatestSnapshot(ctx, scope, persistenceID)
}

func (s *spySnapshotStore) DeleteSnapshots(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	s.spy.record("DeleteSnapshots", scope)
	return s.SnapshotStore.DeleteSnapshots(ctx, scope, persistenceID, toSequenceNumber)
}

// writeScopedLegacyEvent is writeLegacyEvent for an explicit scope; in a
// tenant scope the event carries that tenant's metadata.
func writeScopedLegacyEvent(t testing.TB, store persistence.EventsStore, scope persistence.Scope, persistenceID string, seqNr uint64, state *anypb.Any) {
	t.Helper()
	eventAny := mustAny(t, timestamppb.Now())
	evt := new(egopb.Event)
	if err := proto.Unmarshal(buildLegacyEventBytes(t, persistenceID, seqNr, eventAny, state, int64(seqNr*100), 0), evt); err != nil {
		t.Fatalf("unmarshal legacy event %q/%d: %v", persistenceID, seqNr, err)
	}
	if !scope.IsUnscoped() {
		// Events in a tenant scope carry that tenant's metadata, as the
		// tenant-bound actor (or TenantAdopter) stamps them.
		tenant, err := tenancy.NewTenantContext(scope.TenantID())
		if err != nil {
			t.Fatalf("tenant context for %s: %v", scope, err)
		}
		evt.TenantMetadata = tenancy.MarshalMetadata(tenant)
	}
	if err := store.WriteEvents(context.Background(), scope, []*egopb.Event{evt}, persistence.Unconditional()); err != nil {
		t.Fatalf("write scoped legacy event %q/%d in %s: %v", persistenceID, seqNr, scope, err)
	}
}

func TestNewRejectsAnInvalidScope(t *testing.T) {
	specs.Describe(t, "New rejects a zero-value Scope when the Migrator is built", func(s *specs.Spec) {
		s.It("returns ErrInvalidScope and no Migrator", func(ctx *specs.Context) {
			eventStore := testkit.NewEventsStore()
			snapshotStore := testkit.NewSnapshotStore()

			m, err := New(eventStore, snapshotStore, WithScope(persistence.Scope{}))
			// the zero-value Scope must be rejected when the Migrator is built
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))
			// no Migrator may be returned for an invalid configuration
			ctx.Expect(m).To(specs.BeNil())
		})
	})
}

func TestMigratorScope(t *testing.T) {
	bg := context.Background()
	acme, err := persistence.NewTenantScope("acme")
	if err != nil {
		t.Fatalf("scope for acme: %v", err)
	}
	globex, err := persistence.NewTenantScope("globex")
	if err != nil {
		t.Fatalf("scope for globex: %v", err)
	}

	stateFor := func(t testing.TB, seconds int64) *anypb.Any {
		t.Helper()
		return mustAny(t, &timestamppb.Timestamp{Seconds: seconds})
	}

	// The same persistence id holds different legacy data in three scopes.
	seed := func(t testing.TB) (*testkit.EventStore, *testkit.SnapshotStore) {
		t.Helper()
		eventStore, snapshotStore := connectedStores(t, bg)
		writeScopedLegacyEvent(t, eventStore, persistence.Unscoped(), "order-1", 1, stateFor(t, 1))
		writeScopedLegacyEvent(t, eventStore, acme, "order-1", 1, stateFor(t, 2))
		writeScopedLegacyEvent(t, eventStore, globex, "order-1", 1, stateFor(t, 3))
		return eventStore, snapshotStore
	}
	snapshotSeconds := func(t testing.TB, store *testkit.SnapshotStore, scope persistence.Scope) (int64, bool) {
		t.Helper()
		snapshot, err := store.GetLatestSnapshot(bg, scope, "order-1")
		if err != nil {
			t.Fatalf("latest snapshot in %s: %v", scope, err)
		}
		if snapshot == nil {
			return 0, false
		}
		var state timestamppb.Timestamp
		if err := snapshot.GetState().UnmarshalTo(&state); err != nil {
			t.Fatalf("unmarshal snapshot state in %s: %v", scope, err)
		}
		return state.GetSeconds(), true
	}
	expectEveryCallIn := func(ctx *specs.Context, spy *scopeSpy, scope persistence.Scope) {
		calls := spy.all()
		ctx.Expect(len(calls) > 0).To(specs.BeTrue())
		for _, call := range []string{"PersistenceIDs@" + scope.String(), "ReplayEvents@" + scope.String(), "WriteSnapshot@" + scope.String()} {
			ctx.Expect(calls).To(specs.Contain(call))
		}
		// store calls that do not use the Migrator's scope
		var strays []string
		for _, call := range calls {
			if !strings.HasSuffix(call, "@"+scope.String()) {
				strays = append(strays, call)
			}
		}
		ctx.Expect(strays).To(specs.BeNil())
	}

	specs.Describe(t, "Migrator addresses every store call to its configured scope", func(s *specs.Spec) {
		s.It("without WithScope it migrates only the unscoped records, exactly as before", func(ctx *specs.Context) {
			eventStore, snapshotStore := seed(ctx.T)
			spy := &scopeSpy{}
			ctx.Expect(mustNew(ctx.T, &spyEventsStore{EventsStore: eventStore, spy: spy}, &spySnapshotStore{SnapshotStore: snapshotStore, spy: spy}).Run(bg)).To(specs.BeNil())

			expectEveryCallIn(ctx, spy, persistence.Unscoped())
			seconds, ok := snapshotSeconds(ctx.T, snapshotStore, persistence.Unscoped())
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(seconds).ToEqual(int64(1))
			_, ok = snapshotSeconds(ctx.T, snapshotStore, acme)
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = snapshotSeconds(ctx.T, snapshotStore, globex)
			ctx.Expect(ok).To(specs.BeFalse())
		})

		s.It("WithScope migrates that tenant's records and nothing in another scope", func(ctx *specs.Context) {
			eventStore, snapshotStore := seed(ctx.T)
			spy := &scopeSpy{}
			ctx.Expect(mustNew(ctx.T, &spyEventsStore{EventsStore: eventStore, spy: spy}, &spySnapshotStore{SnapshotStore: snapshotStore, spy: spy}, WithScope(acme)).Run(bg)).To(specs.BeNil())

			expectEveryCallIn(ctx, spy, acme)
			// the tenant's legacy state must become its snapshot, from its own events and not a homonym
			seconds, ok := snapshotSeconds(ctx.T, snapshotStore, acme)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(seconds).ToEqual(int64(2))
			// the unscoped homonym must not be touched
			_, ok = snapshotSeconds(ctx.T, snapshotStore, persistence.Unscoped())
			ctx.Expect(ok).To(specs.BeFalse())
			// another tenant's homonym must not be touched
			_, ok = snapshotSeconds(ctx.T, snapshotStore, globex)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

// TestMigratorReplaysSequencesBeyondTheLimitValue pins that the Migrator
// builds its snapshot from the LATEST legacy state even when that event's
// sequence number is above math.MaxInt, instead of a stale earlier one.
func TestMigratorReplaysSequencesBeyondTheLimitValue(t *testing.T) {
	specs.Describe(t, "Migrator builds the snapshot from the latest legacy state, even above math.MaxInt", func(s *specs.Spec) {
		s.It("uses the event above math.MaxInt instead of a stale earlier one", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx.T, bg)

			stale := mustAny(ctx.T, &timestamppb.Timestamp{Seconds: 1})
			latest := mustAny(ctx.T, &timestamppb.Timestamp{Seconds: 2})
			high := uint64(math.MaxInt) + 1
			writeScopedLegacyEvent(ctx.T, eventStore, persistence.Unscoped(), "order-1", 1, stale)
			writeScopedLegacyEvent(ctx.T, eventStore, persistence.Unscoped(), "order-1", high, latest)

			ctx.Expect(mustNew(ctx.T, eventStore, snapshotStore).Run(bg)).To(specs.BeNil())

			snapshot, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "order-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snapshot).To(specs.Not(specs.BeNil()))
			// the snapshot must come from the event above math.MaxInt
			ctx.Expect(snapshot.GetSequenceNumber()).ToEqual(high)
		})
	})
}
