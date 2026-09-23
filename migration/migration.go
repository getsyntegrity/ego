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

// Package migration provides utilities for migrating event-sourced entities from
// the legacy format (where events carried resulting_state inline) to the new format
// (where snapshots are stored separately in a SnapshotStore).
//
// Usage:
//
//	migrator, err := migration.New(eventsStore, snapshotStore,
//	    migration.WithPageSize(100),
//	    migration.WithLogger(logger), // any kit-logger Logger
//	    migration.WithScope(scope),   // optional; persistence.Unscoped() by default
//	)
//	if err != nil {
//	    return err // e.g. an invalid scope
//	}
//	if err := migrator.Run(ctx); err != nil {
//	    logger.Error("migration failed", "error", err)
//	}
//
// When no logger is supplied the migrator logs through ego.DefaultLogger(),
// kit-logger's process-wide logger.
//
// The migrator reads every persistence ID in its scope (see WithScope), finds the latest
// event for each entity that carried a resulting_state (field 5 in the old proto),
// and writes a snapshot to the snapshot store seeded from that state. This is a
// one-time, idempotent operation — running it again will overwrite existing snapshots
// with the same data.
//
// # Adopting tenancy for existing data
//
// A separate tool, TenantAdopter, addresses a different migration: a
// deployment that already has data written under persistence.Unscoped()
// and now wants to adopt tenancy (TENANT-003). See TenantAdopter's own doc
// comment for the full usage; in short:
//
//	adopter, err := migration.NewTenantAdopter(assignTenant,
//	    migration.WithEventsStore(eventsStore),
//	    migration.WithSnapshotStore(snapshotStore),
//	    migration.WithStateStore(stateStore),
//	    migration.WithWriteEnabled(), // required opt-in; the default is dry-run
//	    migration.WithAdoptionFence(fence), // required whenever writes are enabled
//	)
//	report, err := adopter.Run(ctx)
//
// TenantAssignment (assignTenant above) is a required constructor
// argument, not an option: the framework has no way to know which tenant an
// existing aggregate belongs to, since persistence_id is an opaque,
// caller-assigned string. That is a business decision only the operator
// holds.
//
// TenantAdopter defaults to dry-run (plans and reports, writes nothing) and
// never deletes source data unless WithSourceDeletion is also set, and then
// only after a copy has been read back and matched, via proto.Equal, against
// the exact record this tool intended to write (the source record with
// tenant_metadata replaced by the target tenant's plus its adoption
// receipt; events matched by SequenceNumber rather than slice position or
// count) — not merely a
// count, sequence number, or version number, none of which can detect a
// corrupted payload, a dropped tenant_metadata, or a missing encryption
// envelope.
//
// A target tenant scope that already holds a record is never trusted merely
// because it exists or belongs to the assigned tenant. It is already_present
// only when it is proven to be this adoption: while the source exists, by
// exact comparison (an events target contains every source event and may
// append later ones; a snapshot or durable-state target is the identical
// record at the same position — a later one proves nothing, since a single
// latest record keeps no lineage); once WithSourceDeletion removed the
// source, by the adoption receipt stamped into every adopted record's
// tenant_metadata, which binds the record's exact content to the source
// scope it came from. Otherwise that record kind fails closed and nothing is
// written or deleted. A re-run right after a deleting run is therefore a
// no-op, while a re-run after the tenant-bound actor has rewritten a
// snapshot or durable state fails closed: that record no longer carries a
// receipt, and nothing else can prove it descends from the deleted source.
//
// Durable-state enumeration limitation: persistence.EventsStore has
// PersistenceIDs to enumerate a scope, but neither persistence.SnapshotStore
// nor persistence.StateStore does. When no events store is configured (a
// durable-state-only, or snapshot-only, deployment), TenantAdopter has no
// way to discover which persistence IDs exist on its own — the operator
// must supply them explicitly via WithPersistenceIDs. This is a real gap in
// today's persistence SPI, not an oversight in this tool.
package migration

import (
	"context"
	"fmt"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	ego "github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
)

// legacyResultingStateFieldNumber is the protobuf field number that was used
// for resulting_state in the old Event message. We decode it directly from
// the raw event bytes to avoid depending on a generated LegacyEvent type.
const legacyResultingStateFieldNumber = 5

// Migrator reads legacy events that embedded resulting_state and seeds
// the snapshot store with the latest state for each entity.
type Migrator struct {
	eventsStore   persistence.EventsStore
	snapshotStore persistence.SnapshotStore
	pageSize      uint64
	logger        kitlog.Logger
	scope         persistence.Scope
}

// New creates a Migrator.
func New(eventsStore persistence.EventsStore, snapshotStore persistence.SnapshotStore, opts ...Option) (*Migrator, error) {
	m := &Migrator{
		eventsStore:   eventsStore,
		snapshotStore: snapshotStore,
		pageSize:      500,
		scope:         persistence.Unscoped(),
	}
	for _, opt := range opts {
		opt.apply(m)
	}
	// Options may have set a nil or typed-nil logger, which would panic on the
	// first log call. Resolving after the loop covers every option path.
	m.logger = ego.ResolveLogger(m.logger)
	if !m.scope.Valid() {
		return nil, fmt.Errorf("migration: WithScope: %w", persistence.ErrInvalidScope)
	}
	return m, nil
}

// Run executes the migration. It iterates over every persistence ID the events
// store lists in the Migrator's scope and, for each entity, extracts the
// resulting_state from the latest event that carried one, writing it as a
// snapshot in that same scope.
//
// The operation is idempotent: running it multiple times produces the same result.
// Events in the store are not modified.
func (m *Migrator) Run(ctx context.Context) error {
	if err := m.eventsStore.Ping(ctx); err != nil {
		return fmt.Errorf("migration: events store not reachable: %w", err)
	}
	if err := m.snapshotStore.Ping(ctx); err != nil {
		return fmt.Errorf("migration: snapshot store not reachable: %w", err)
	}

	var (
		pageToken string
		total     int
	)

	for {
		// A Migrator walks exactly one scope, m.scope (WithScope; Unscoped()
		// by default): listing, replay, and the snapshot write below all use
		// it, so records under the same persistence ID in any other scope are
		// never read or written. It does not sweep every tenant — the SPI has
		// no way to enumerate scopes — so a deployment with tenant data runs
		// one Migrator per scope.
		ids, nextToken, err := m.eventsStore.PersistenceIDs(ctx, m.scope, m.pageSize, pageToken)
		if err != nil {
			return fmt.Errorf("migration: failed to list persistence IDs: %w", err)
		}

		for _, id := range ids {
			if err := m.migrateEntity(ctx, id); err != nil {
				return fmt.Errorf("migration: failed to migrate entity %s: %w", id, err)
			}
			total++
		}

		if nextToken == "" || len(ids) == 0 {
			break
		}
		pageToken = nextToken
	}

	m.logger.InfoContext(ctx, "migration: completed successfully", "entities", total)
	return nil
}

// migrateEntity processes a single entity: reads its events and finds
// the latest one with a resulting_state, then writes a snapshot.
func (m *Migrator) migrateEntity(ctx context.Context, persistenceID string) error {
	// Same scope Run listed persistenceID in; see Run's comment.
	// maxReplayLimit (tenant_adoption.go) fits in an int on every
	// architecture, so a store converting it cannot overflow.
	events, err := m.eventsStore.ReplayEvents(ctx, m.scope, persistenceID, 1, maxReplayLimit, maxReplayLimit)
	if err != nil {
		return err
	}

	var (
		bestSnapshot *egopb.Snapshot
		bestEvent    *egopb.Event
	)
	for _, evt := range events {
		state := extractLegacyResultingState(evt)
		if state != nil {
			if bestSnapshot == nil || evt.GetSequenceNumber() > bestSnapshot.GetSequenceNumber() {
				bestEvent = evt
				bestSnapshot = &egopb.Snapshot{
					PersistenceId:  persistenceID,
					SequenceNumber: evt.GetSequenceNumber(),
					State:          state,
					Timestamp:      evt.GetTimestamp(),
				}
			}
		}
	}

	if bestSnapshot == nil {
		m.logger.DebugContext(ctx, "migration: entity has no legacy resulting_state, skipping", "persistence_id", persistenceID)
		return nil
	}

	if err := m.stampSnapshotTenant(bestSnapshot, bestEvent); err != nil {
		return err
	}

	// Written in the same scope the events were read from; see Run's comment.
	if err := m.snapshotStore.WriteSnapshot(ctx, m.scope, bestSnapshot); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	m.logger.DebugContext(ctx, "migration: snapshot written",
		"persistence_id", persistenceID,
		"sequence_number", bestSnapshot.GetSequenceNumber())
	return nil
}

// extractLegacyResultingState attempts to read the resulting_state (field 5)
// from an event. Since the field was removed from the proto, it lands in the
// event's proto unknown fields when deserialized by the current generated code.
// We extract it by re-reading the unknown fields.
//
// The function uses the Event's proto unknown fields to find field 5 (the old
// resulting_state). If found, it unmarshals it as an anypb.Any.
func extractLegacyResultingState(evt *egopb.Event) *anypb.Any {
	if evt == nil {
		return nil
	}

	// When the current generated code deserializes an old event that has field 5,
	// it places the bytes into the message's unknown fields. We can scan them.
	raw := evt.ProtoReflect().GetUnknown()
	if len(raw) == 0 {
		return nil
	}

	// Parse the unknown fields looking for field number 5 (length-delimited, wire type 2)
	for len(raw) > 0 {
		fieldNum, wireType, n := consumeTag(raw)
		if n < 0 {
			break
		}
		raw = raw[n:]

		if wireType == 2 { // length-delimited
			length, n := consumeVarint(raw)
			if n < 0 {
				break
			}
			raw = raw[n:]

			if uint64(len(raw)) < length {
				break
			}

			if fieldNum == legacyResultingStateFieldNumber {
				state := new(anypb.Any)
				if err := proto.Unmarshal(raw[:length], state); err != nil {
					return nil
				}
				if state.GetTypeUrl() != "" {
					return state
				}
				return nil
			}

			raw = raw[length:]
		} else if wireType == 0 { // varint
			_, n := consumeVarint(raw)
			if n < 0 {
				break
			}
			raw = raw[n:]
		} else if wireType == 5 { // 32-bit
			if len(raw) < 4 {
				break
			}
			raw = raw[4:]
		} else if wireType == 1 { // 64-bit
			if len(raw) < 8 {
				break
			}
			raw = raw[8:]
		} else {
			break // unknown wire type
		}
	}
	return nil
}

// consumeTag parses a protobuf tag (field number + wire type) from raw bytes.
// Returns field number, wire type, and number of bytes consumed. Returns -1 for n on error.
func consumeTag(b []byte) (fieldNum uint32, wireType int, n int) {
	v, n := consumeVarint(b)
	if n < 0 {
		return 0, 0, -1
	}
	return uint32(v >> 3), int(v & 0x7), n
}

// consumeVarint parses a protobuf varint from raw bytes.
// Returns the value and number of bytes consumed. Returns -1 for n on error.
func consumeVarint(b []byte) (uint64, int) {
	var v uint64
	for i, c := range b {
		if i >= 10 {
			return 0, -1
		}
		v |= uint64(c&0x7f) << (uint(i) * 7)
		if c < 0x80 {
			return v, i + 1
		}
	}
	return 0, -1
}

// stampSnapshotTenant gives a snapshot written in a tenant scope the
// tenant_metadata a tenant-aware EventSourcedActor requires: it loads the
// snapshot first and refuses one without a tenant scope in its metadata.
// The metadata comes from source, the event whose resulting_state the
// snapshot was taken from, and must decode to exactly the Migrator's tenant
// (tenancy.ErrInvalid when missing or malformed, tenancy.ErrDenied for
// another tenant); the snapshot is then stamped canonically, the way the
// actors stamp it. Nothing is written when that cannot be proven. Unscoped
// runs are unchanged: their snapshots carry no tenant_metadata, as before.
func (m *Migrator) stampSnapshotTenant(snapshot *egopb.Snapshot, source *egopb.Event) error {
	if m.scope.IsUnscoped() {
		return nil
	}
	want, err := tenancy.NewTenantContext(m.scope.TenantID())
	if err != nil {
		return fmt.Errorf("snapshot tenant for scope %s: %w", m.scope, err)
	}
	got, err := tenancy.UnmarshalMetadata(tenancy.Metadata(source.GetTenantMetadata()))
	if err != nil {
		return fmt.Errorf("source event %d carries no tenant metadata for scope %s: %w", source.GetSequenceNumber(), m.scope, err)
	}
	if err := tenancy.VerifyUnchanged(want, got); err != nil {
		return fmt.Errorf("source event %d is stamped for another tenant than scope %s: %w", source.GetSequenceNumber(), m.scope, err)
	}
	snapshot.TenantMetadata = tenancy.MarshalMetadata(want)
	return nil
}
