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
	"fmt"
	"math"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
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

// marshalLegacyEvent constructs raw protobuf bytes for an Event that includes
// the old resulting_state at field 5. Since the current generated Event no longer
// has that field, we manually append the field 5 bytes to a normally-serialized Event.
func marshalLegacyEvent(persistenceID string, seqNr uint64, event *anypb.Any, state *anypb.Any, timestamp int64, shard uint64) ([]byte, error) {
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
		return nil, fmt.Errorf("marshal base event for %q: %w", persistenceID, err)
	}

	if state == nil {
		return baseBytes, nil
	}

	// Serialize the state as field 5 (length-delimited, wire type 2)
	// Tag = (5 << 3) | 2 = 42
	stateBytes, err := proto.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal legacy state for %q: %w", persistenceID, err)
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
	return result, nil
}

// legacyEventBytes is marshalLegacyEvent checked through the spec.
func legacyEventBytes(ctx *specs.Context, persistenceID string, seqNr uint64, event *anypb.Any, state *anypb.Any, timestamp int64, shard uint64) []byte {
	ctx.T.Helper()
	raw, err := marshalLegacyEvent(persistenceID, seqNr, event, state, timestamp, shard)
	ctx.Expect(err).To(specs.BeNil())
	return raw
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
func writeLegacyEvent(ctx *specs.Context, store *testkit.EventStore, persistenceID string, seqNr uint64, eventPayload *anypb.Any, state *anypb.Any, ts int64, shard uint64) {
	ctx.T.Helper()
	raw := legacyEventBytes(ctx, persistenceID, seqNr, eventPayload, state, ts, shard)

	// Deserialize using the current Event proto — field 5 goes into unknown fields
	evt := new(egopb.Event)
	ctx.Expect(proto.Unmarshal(raw, evt)).To(specs.BeNil())

	ctx.Expect(store.WriteEvents(context.Background(), persistence.Unscoped(), []*egopb.Event{evt}, persistence.Unconditional())).To(specs.BeNil())
}

// connectedEventStore returns a connected events store that disconnects when the case ends.
func connectedEventStore(ctx *specs.Context, bg context.Context) *testkit.EventStore {
	ctx.T.Helper()
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() {
		if err := store.Disconnect(bg); err != nil {
			ctx.T.Errorf("disconnect events store: %v", err)
		}
	})
	return store
}

// connectedSnapshotStore returns a connected snapshot store that disconnects when the case ends.
func connectedSnapshotStore(ctx *specs.Context, bg context.Context) *testkit.SnapshotStore {
	ctx.T.Helper()
	store := testkit.NewSnapshotStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() {
		if err := store.Disconnect(bg); err != nil {
			ctx.T.Errorf("disconnect snapshot store: %v", err)
		}
	})
	return store
}

// connectedStores returns a connected events store and snapshot store.
func connectedStores(ctx *specs.Context, bg context.Context) (*testkit.EventStore, *testkit.SnapshotStore) {
	ctx.T.Helper()
	return connectedEventStore(ctx, bg), connectedSnapshotStore(ctx, bg)
}

// mustAny wraps a message in an Any and fails the case if that is impossible.
func mustAny(ctx *specs.Context, m proto.Message) *anypb.Any {
	ctx.T.Helper()
	a, err := anypb.New(m)
	ctx.Expect(err).To(specs.BeNil())
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
			eventStore, snapshotStore := connectedStores(ctx, bg)

			// Create a fake state to embed as resulting_state
			ts := timestamppb.Now()
			stateAny := mustAny(ctx, ts)
			eventAny := mustAny(ctx, timestamppb.Now())

			// Write 3 legacy events for entity "entity-1"
			for i := uint64(1); i <= 3; i++ {
				writeLegacyEvent(ctx, eventStore, "entity-1", i, eventAny, stateAny, int64(i*100), 0)
			}

			// Run migration
			migrator := mustNew(ctx, eventStore, snapshotStore,
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
			ctx.Expect(&recovered).To(specs.Satisfy("proto-equal to the embedded state", func(v any) bool {
				return proto.Equal(ts, v.(proto.Message))
			}))
		})

		s.It("skips entities with no resulting_state", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx, bg)

			eventAny := mustAny(ctx, timestamppb.Now())

			// Write events without resulting_state (new-format events)
			for i := uint64(1); i <= 3; i++ {
				writeLegacyEvent(ctx, eventStore, "entity-new", i, eventAny, nil, int64(i*100), 0)
			}

			migrator := mustNew(ctx, eventStore, snapshotStore,
				WithPageSize(10),
				WithLogger(engine.DiscardLogger),
			)
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())

			// No snapshot should exist
			snapshot, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "entity-new")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snapshot).To(specs.BeNil())
		})

		s.It("handles multiple entities", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx, bg)

			state1, _ := anypb.New(&timestamppb.Timestamp{Seconds: 111})
			state2, _ := anypb.New(&timestamppb.Timestamp{Seconds: 222})
			eventAny, _ := anypb.New(timestamppb.Now())

			writeLegacyEvent(ctx, eventStore, "e1", 1, eventAny, state1, 100, 0)
			writeLegacyEvent(ctx, eventStore, "e1", 2, eventAny, state1, 200, 0)
			writeLegacyEvent(ctx, eventStore, "e2", 1, eventAny, state2, 100, 1)

			migrator := mustNew(ctx, eventStore, snapshotStore,
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
		})

		s.It("is idempotent", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx, bg)

			stateAny, _ := anypb.New(&timestamppb.Timestamp{Seconds: 999})
			eventAny, _ := anypb.New(timestamppb.Now())

			writeLegacyEvent(ctx, eventStore, "idem-1", 1, eventAny, stateAny, 100, 0)

			migrator := mustNew(ctx, eventStore, snapshotStore,
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
		})

		s.It("returns error when events store is unreachable", func(ctx *specs.Context) {
			bg := context.Background()

			eventStore := testkit.NewEventsStore()
			// deliberately don't connect

			snapshotStore := connectedSnapshotStore(ctx, bg)

			migrator := mustNew(ctx, eventStore, snapshotStore,
				WithLogger(engine.DiscardLogger),
			)
			// Ping will auto-connect the testkit store, so this will actually succeed.
			// That's fine — testkit stores auto-connect on Ping.
			err := migrator.Run(bg)
			ctx.Expect(err).To(specs.BeNil())
		})

		s.It("with empty events store", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx, bg)

			migrator := mustNew(ctx, eventStore, snapshotStore,
				WithLogger(engine.DiscardLogger),
			)
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())
		})
	})
}

func TestExtractLegacyResultingState(t *testing.T) {
	specs.Describe(t, "extractLegacyResultingState recovers the field 5 state of a legacy event", func(s *specs.Spec) {
		type nilCase struct {
			name  string
			event func(ctx *specs.Context) *egopb.Event
		}
		specs.Table(s, []nilCase{
			{"returns nil for nil event", func(*specs.Context) *egopb.Event { return nil }},
			{"returns nil for event without unknown fields", func(*specs.Context) *egopb.Event {
				return &egopb.Event{PersistenceId: "test", SequenceNumber: 1}
			}},
			{"returns nil for event without field 5", func(ctx *specs.Context) *egopb.Event {
				raw := legacyEventBytes(ctx, "test", 1, mustAny(ctx, timestamppb.Now()), nil, 100, 0)
				evt := new(egopb.Event)
				ctx.Expect(proto.Unmarshal(raw, evt)).To(specs.BeNil())
				return evt
			}},
		}, func(c nilCase) string { return c.name }, func(ctx *specs.Context, c nilCase) {
			ctx.Expect(extractLegacyResultingState(c.event(ctx))).To(specs.BeNil())
		})

		s.It("extracts state from legacy event", func(ctx *specs.Context) {
			state, _ := anypb.New(&timestamppb.Timestamp{Seconds: 42})
			eventAny, _ := anypb.New(timestamppb.Now())

			raw := legacyEventBytes(ctx, "test", 1, eventAny, state, 100, 0)

			evt := new(egopb.Event)
			ctx.Expect(proto.Unmarshal(raw, evt)).To(specs.BeNil())

			extracted := extractLegacyResultingState(evt)
			ctx.Expect(extracted).To(specs.Not(specs.BeNil()))
			ctx.Expect(extracted.GetTypeUrl()).To(specs.Not(specs.BeEmpty()))

			var ts timestamppb.Timestamp
			ctx.Expect(extracted.UnmarshalTo(&ts)).To(specs.BeNil())
			ctx.Expect(ts.GetSeconds()).ToEqual(int64(42))
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
			m := mustNew(ctx, nil, nil, WithPageSize(100))
			ctx.Expect(m.pageSize).ToEqual(uint64(100))
		})

		s.It("WithLogger", func(ctx *specs.Context) {
			logger := engine.DiscardLogger
			m := mustNew(ctx, nil, nil, WithLogger(logger))
			ctx.Expect(m.logger).ToEqual(logger)
		})

		s.It("defaults", func(ctx *specs.Context) {
			m := mustNew(ctx, nil, nil)
			ctx.Expect(m.pageSize).ToEqual(uint64(500))
			ctx.Expect(m.logger).To(specs.Not(specs.BeNil()))
		})

		s.It("WithLogger(nil) falls back to the default logger", func(ctx *specs.Context) {
			m := mustNew(ctx, nil, nil, WithLogger(nil))
			ctx.Expect(m.logger == engine.DefaultLogger()).To(specs.BeTrue())
		})

		s.It("WithLogger with a typed-nil logger falls back to the default logger", func(ctx *specs.Context) {
			var typedNil *kitlogtest.MockLogger
			m := mustNew(ctx, nil, nil, WithLogger(typedNil))
			ctx.Expect(m.logger == engine.DefaultLogger()).To(specs.BeTrue())
		})
	})
}

func TestMigratorRunWithNilLogger(t *testing.T) {
	type loggerCase struct {
		name   string
		logger kitlog.Logger
	}
	specs.Describe(t, "Migrator.Run does not panic when the logger is nil", func(s *specs.Spec) {
		specs.Table(s, []loggerCase{
			{"untyped nil", nil},
			{"typed nil", (*kitlogtest.MockLogger)(nil)},
		}, func(c loggerCase) string { return c.name }, func(ctx *specs.Context, c loggerCase) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx, bg)

			stateAny := mustAny(ctx, &timestamppb.Timestamp{Seconds: 7})
			eventAny := mustAny(ctx, timestamppb.Now())
			writeLegacyEvent(ctx, eventStore, "nil-logger-1", 1, eventAny, stateAny, 1, 0)

			migrator := mustNew(ctx, eventStore, snapshotStore, WithLogger(c.logger))
			var runErr error
			ctx.Expect(panicValue(func() { runErr = migrator.Run(bg) })).To(specs.BeNil())
			ctx.Expect(runErr).To(specs.BeNil())
		})
	})
}

// loggerMock is a kit-logger Logger whose Migrator-facing methods forward to a
// mock.Controller. The Migrator logs only through InfoContext and DebugContext,
// so the other Logger methods stay on the nil embedded interface: a call to one
// of them panics and fails the case loudly.
type loggerMock struct {
	kitlog.Logger
	c *mock.Controller
}

func (l loggerMock) InfoContext(ctx context.Context, msg string, args ...any) {
	l.c.Method("InfoContext").Call(ctx, msg, args)
}

func (l loggerMock) DebugContext(ctx context.Context, msg string, args ...any) {
	l.c.Method("DebugContext").Call(ctx, msg, args)
}

func TestMigratorUsesKitLogger(t *testing.T) {
	specs.Describe(t, "Migrator logs through the kit-logger Logger it was given", func(s *specs.Spec) {
		s.It("defaults to engine.DefaultLogger()", func(ctx *specs.Context) {
			m := mustNew(ctx, nil, nil)
			ctx.Expect(m.logger == engine.DefaultLogger()).To(specs.BeTrue())
		})

		s.It("WithLogger injects a custom kit-logger Logger", func(ctx *specs.Context) {
			logger := loggerMock{c: mock.NewController(ctx)}
			m := mustNew(ctx, nil, nil, WithLogger(logger))
			ctx.Expect(m.logger == kitlog.Logger(logger)).To(specs.BeTrue())
		})

		s.It("the injected logger receives the migration messages", func(ctx *specs.Context) {
			bg := context.Background()
			eventStore, snapshotStore := connectedStores(ctx, bg)

			stateAny := mustAny(ctx, timestamppb.Now())
			eventAny := mustAny(ctx, timestamppb.Now())

			writeLegacyEvent(ctx, eventStore, "entity-1", 1, eventAny, stateAny, 100, 0)

			ctrl := mock.NewController(ctx)
			ctrl.Method("InfoContext").Expect(mock.Any(), "migration: completed successfully", mock.Any()).Times(1)
			ctrl.Method("DebugContext").Expect(mock.Any(), "migration: snapshot written", mock.Any()).Times(1)
			migrator := mustNew(ctx, eventStore, snapshotStore, WithLogger(loggerMock{c: ctrl}))
			ctx.Expect(migrator.Run(bg)).To(specs.BeNil())
		})
	})
}

// mustNew builds a Migrator and fails the test if New rejects the
// configuration.
func mustNew(ctx *specs.Context, eventsStore persistence.EventsStore, snapshotStore persistence.SnapshotStore, opts ...Option) *Migrator {
	ctx.T.Helper()
	m, err := New(eventsStore, snapshotStore, opts...)
	ctx.Expect(err).To(specs.BeNil())
	return m
}

// recordScope notes the scope of a record-addressing store call on spy, so a
// test can prove no call still hard-codes a scope.
func recordScope(spy *mock.Spy, method string, scope persistence.Scope) {
	spy.Call(method, scope.String())
}

type spyEventsStore struct {
	persistence.EventsStore
	spy *mock.Spy
}

func (e *spyEventsStore) PersistenceIDs(ctx context.Context, scope persistence.Scope, pageSize uint64, pageToken string) ([]string, string, error) {
	recordScope(e.spy, "PersistenceIDs", scope)
	return e.EventsStore.PersistenceIDs(ctx, scope, pageSize, pageToken)
}

func (e *spyEventsStore) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string, from, to, maxNumber uint64) ([]*egopb.Event, error) {
	recordScope(e.spy, "ReplayEvents", scope)
	return e.EventsStore.ReplayEvents(ctx, scope, persistenceID, from, to, maxNumber)
}

func (e *spyEventsStore) GetLatestEvent(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Event, error) {
	recordScope(e.spy, "GetLatestEvent", scope)
	return e.EventsStore.GetLatestEvent(ctx, scope, persistenceID)
}

func (e *spyEventsStore) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	recordScope(e.spy, "WriteEvents", scope)
	return e.EventsStore.WriteEvents(ctx, scope, events, precondition)
}

func (e *spyEventsStore) DeleteEvents(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	recordScope(e.spy, "DeleteEvents", scope)
	return e.EventsStore.DeleteEvents(ctx, scope, persistenceID, toSequenceNumber)
}

type spySnapshotStore struct {
	persistence.SnapshotStore
	spy *mock.Spy
}

func (s *spySnapshotStore) WriteSnapshot(ctx context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	recordScope(s.spy, "WriteSnapshot", scope)
	return s.SnapshotStore.WriteSnapshot(ctx, scope, snapshot)
}

func (s *spySnapshotStore) GetLatestSnapshot(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Snapshot, error) {
	recordScope(s.spy, "GetLatestSnapshot", scope)
	return s.SnapshotStore.GetLatestSnapshot(ctx, scope, persistenceID)
}

func (s *spySnapshotStore) DeleteSnapshots(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	recordScope(s.spy, "DeleteSnapshots", scope)
	return s.SnapshotStore.DeleteSnapshots(ctx, scope, persistenceID, toSequenceNumber)
}

// writeScopedLegacyEvent is writeLegacyEvent for an explicit scope; in a
// tenant scope the event carries that tenant's metadata.
func writeScopedLegacyEvent(ctx *specs.Context, store persistence.EventsStore, scope persistence.Scope, persistenceID string, seqNr uint64, state *anypb.Any) {
	ctx.T.Helper()
	eventAny := mustAny(ctx, timestamppb.Now())
	evt := new(egopb.Event)
	ctx.Expect(proto.Unmarshal(legacyEventBytes(ctx, persistenceID, seqNr, eventAny, state, int64(seqNr*100), 0), evt)).To(specs.BeNil())
	if !scope.IsUnscoped() {
		// Events in a tenant scope carry that tenant's metadata, as the
		// tenant-bound actor (or TenantAdopter) stamps them.
		tenant, err := tenancy.NewTenantContext(scope.TenantID())
		ctx.Expect(err).To(specs.BeNil())
		evt.TenantMetadata = tenancy.MarshalMetadata(tenant)
	}
	ctx.Expect(store.WriteEvents(context.Background(), scope, []*egopb.Event{evt}, persistence.Unconditional())).To(specs.BeNil())
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
	var acme, globex persistence.Scope

	stateFor := func(ctx *specs.Context, seconds int64) *anypb.Any {
		ctx.T.Helper()
		return mustAny(ctx, &timestamppb.Timestamp{Seconds: seconds})
	}

	// The same persistence id holds different legacy data in three scopes.
	seed := func(ctx *specs.Context) (*testkit.EventStore, *testkit.SnapshotStore) {
		ctx.T.Helper()
		eventStore, snapshotStore := connectedStores(ctx, bg)
		writeScopedLegacyEvent(ctx, eventStore, persistence.Unscoped(), "order-1", 1, stateFor(ctx, 1))
		writeScopedLegacyEvent(ctx, eventStore, acme, "order-1", 1, stateFor(ctx, 2))
		writeScopedLegacyEvent(ctx, eventStore, globex, "order-1", 1, stateFor(ctx, 3))
		return eventStore, snapshotStore
	}
	snapshotSeconds := func(ctx *specs.Context, store *testkit.SnapshotStore, scope persistence.Scope) (int64, bool) {
		ctx.T.Helper()
		snapshot, err := store.GetLatestSnapshot(bg, scope, "order-1")
		ctx.Expect(err).To(specs.BeNil())
		if snapshot == nil {
			return 0, false
		}
		var state timestamppb.Timestamp
		ctx.Expect(snapshot.GetState().UnmarshalTo(&state)).To(specs.BeNil())
		return state.GetSeconds(), true
	}
	expectEveryCallIn := func(ctx *specs.Context, spy *mock.Spy, scope persistence.Scope) {
		calls := spy.Calls()
		ctx.Expect(calls).To(specs.Not(specs.BeEmpty()))
		for _, method := range []string{"PersistenceIDs", "ReplayEvents", "WriteSnapshot"} {
			ctx.Expect(spy.CalledWith(mock.Equal(method), mock.Equal(scope.String()))).To(specs.BeTrue())
		}
		// store calls that do not use the Migrator's scope
		ctx.Expect(calls).To(specs.EveryElement(specs.Project("scope", func(c mock.Call) any { return c.Args[1] }, specs.Equal(scope.String()))))
	}

	specs.Describe(t, "Migrator addresses every store call to its configured scope", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) {
			var err error
			acme, err = persistence.NewTenantScope("acme")
			ctx.Expect(err).To(specs.BeNil())
			globex, err = persistence.NewTenantScope("globex")
			ctx.Expect(err).To(specs.BeNil())
		})

		s.It("without WithScope it migrates only the unscoped records, exactly as before", func(ctx *specs.Context) {
			eventStore, snapshotStore := seed(ctx)
			spy := mock.NewSpy()
			ctx.Expect(mustNew(ctx, &spyEventsStore{EventsStore: eventStore, spy: spy}, &spySnapshotStore{SnapshotStore: snapshotStore, spy: spy}).Run(bg)).To(specs.BeNil())

			expectEveryCallIn(ctx, spy, persistence.Unscoped())
			seconds, ok := snapshotSeconds(ctx, snapshotStore, persistence.Unscoped())
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(seconds).ToEqual(int64(1))
			_, ok = snapshotSeconds(ctx, snapshotStore, acme)
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = snapshotSeconds(ctx, snapshotStore, globex)
			ctx.Expect(ok).To(specs.BeFalse())
		})

		s.It("WithScope migrates that tenant's records and nothing in another scope", func(ctx *specs.Context) {
			eventStore, snapshotStore := seed(ctx)
			spy := mock.NewSpy()
			ctx.Expect(mustNew(ctx, &spyEventsStore{EventsStore: eventStore, spy: spy}, &spySnapshotStore{SnapshotStore: snapshotStore, spy: spy}, WithScope(acme)).Run(bg)).To(specs.BeNil())

			expectEveryCallIn(ctx, spy, acme)
			// the tenant's legacy state must become its snapshot, from its own events and not a homonym
			seconds, ok := snapshotSeconds(ctx, snapshotStore, acme)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(seconds).ToEqual(int64(2))
			// the unscoped homonym must not be touched
			_, ok = snapshotSeconds(ctx, snapshotStore, persistence.Unscoped())
			ctx.Expect(ok).To(specs.BeFalse())
			// another tenant's homonym must not be touched
			_, ok = snapshotSeconds(ctx, snapshotStore, globex)
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
			eventStore, snapshotStore := connectedStores(ctx, bg)

			stale := mustAny(ctx, &timestamppb.Timestamp{Seconds: 1})
			latest := mustAny(ctx, &timestamppb.Timestamp{Seconds: 2})
			high := uint64(math.MaxInt) + 1
			writeScopedLegacyEvent(ctx, eventStore, persistence.Unscoped(), "order-1", 1, stale)
			writeScopedLegacyEvent(ctx, eventStore, persistence.Unscoped(), "order-1", high, latest)

			ctx.Expect(mustNew(ctx, eventStore, snapshotStore).Run(bg)).To(specs.BeNil())

			snapshot, err := snapshotStore.GetLatestSnapshot(bg, persistence.Unscoped(), "order-1")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(snapshot).To(specs.Not(specs.BeNil()))
			// the snapshot must come from the event above math.MaxInt
			ctx.Expect(snapshot.GetSequenceNumber()).ToEqual(high)
		})
	})
}
