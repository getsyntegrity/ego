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

// This file proves EGO-TENANT-003 T2's plumbing: every store method that now
// takes a persistence.Scope rejects the invalid zero-value Scope with
// persistence.ErrInvalidScope before touching any state, and Unscoped()
// continues to behave exactly as the pre-TENANT-003 stores did (regression
// guard). The deeper cross-tenant isolation matrix (two different valid
// scopes never seeing each other's records) is deliberately out of scope
// here — that is T3's dedicated conformance suite.
package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/urd/egopb"
	testpb "github.com/getsyntegrity/urd/internal/testpb"
	"github.com/getsyntegrity/urd/persistence"
)

// The payload builders below fail the spec through ctx when a payload cannot
// be built. They run inside each case, so every case owns its own records.

func scopeGuardEvents(ctx *specs.Context) []*egopb.Event {
	ctx.T.Helper()
	anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
	ctx.Expect(err).To(specs.BeNil())
	return []*egopb.Event{
		{PersistenceId: "scope-guard", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
}

func scopeGuardState(ctx *specs.Context) *egopb.DurableState {
	ctx.T.Helper()
	anyState, err := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 500})
	ctx.Expect(err).To(specs.BeNil())
	return &egopb.DurableState{PersistenceId: "scope-guard-ds", ResultingState: anyState, VersionNumber: 1}
}

func scopeGuardSnapshot(ctx *specs.Context) *egopb.Snapshot {
	ctx.T.Helper()
	anyState, err := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 500})
	ctx.Expect(err).To(specs.BeNil())
	return &egopb.Snapshot{PersistenceId: "scope-guard-snap", SequenceNumber: 1, State: anyState}
}

// ---------------------------------------------------------------------------
// EventStore: invalid scope rejection
// ---------------------------------------------------------------------------

func TestEventStore_InvalidScopeRejected(t *testing.T) {
	specs.Describe(t, "EventStore rejects the zero-value scope with ErrInvalidScope and leaves state untouched", func(s *specs.Spec) {
		bg := context.TODO()
		var invalid persistence.Scope

		s.It("WriteEvents", func(ctx *specs.Context) {
			events := scopeGuardEvents(ctx)
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			err := store.WriteEvents(bg, invalid, events, persistence.Unconditional())
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))

			ids, _, listErr := store.PersistenceIDs(bg, persistence.Unscoped(), 10, "")
			ctx.Expect(listErr).To(specs.BeNil())
			ctx.Expect(ids).To(specs.BeEmpty())
		})

		s.It("DeleteEvents", func(ctx *specs.Context) {
			events := scopeGuardEvents(ctx)
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			err := store.DeleteEvents(bg, invalid, "scope-guard", 1)
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))

			replayed, replayErr := store.ReplayEvents(bg, persistence.Unscoped(), "scope-guard", 1, 1, 10)
			ctx.Expect(replayErr).To(specs.BeNil())
			ctx.Expect(replayed).To(specs.HaveLen(1))
		})

		s.It("ReplayEvents", func(ctx *specs.Context) {
			events := scopeGuardEvents(ctx)
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			got, err := store.ReplayEvents(bg, invalid, "scope-guard", 1, 1, 10)
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))
			ctx.Expect(got).To(specs.BeNil())
		})

		s.It("GetLatestEvent", func(ctx *specs.Context) {
			events := scopeGuardEvents(ctx)
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			got, err := store.GetLatestEvent(bg, invalid, "scope-guard")
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))
			ctx.Expect(got).To(specs.BeNil())
		})

		s.It("PersistenceIDs", func(ctx *specs.Context) {
			events := scopeGuardEvents(ctx)
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), events, persistence.Unconditional())).To(specs.BeNil())

			ids, token, err := store.PersistenceIDs(bg, invalid, 10, "")
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))
			ctx.Expect(ids).To(specs.BeEmpty())
			ctx.Expect(token).To(specs.BeEmpty())
		})
	})
}

// ---------------------------------------------------------------------------
// DurableStore: invalid scope rejection
// ---------------------------------------------------------------------------

func TestDurableStore_InvalidScopeRejected(t *testing.T) {
	specs.Describe(t, "DurableStore rejects the zero-value scope with ErrInvalidScope and leaves state untouched", func(s *specs.Spec) {
		bg := context.TODO()
		var invalid persistence.Scope

		s.It("WriteState", func(ctx *specs.Context) {
			state := scopeGuardState(ctx)
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			err := store.WriteState(bg, invalid, state, persistence.Unconditional())
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))

			got, getErr := store.GetLatestState(bg, persistence.Unscoped(), "scope-guard-ds")
			ctx.Expect(getErr).To(specs.BeNil())
			ctx.Expect(got).To(specs.BeNil())
		})

		s.It("GetLatestState", func(ctx *specs.Context) {
			state := scopeGuardState(ctx)
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), state, persistence.Unconditional())).To(specs.BeNil())

			got, err := store.GetLatestState(bg, invalid, "scope-guard-ds")
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))
			ctx.Expect(got).To(specs.BeNil())
		})
	})
}

// ---------------------------------------------------------------------------
// SnapshotStore: invalid scope rejection
// ---------------------------------------------------------------------------

func TestSnapshotStore_InvalidScopeRejected(t *testing.T) {
	specs.Describe(t, "SnapshotStore rejects the zero-value scope with ErrInvalidScope and leaves state untouched", func(s *specs.Spec) {
		bg := context.TODO()
		var invalid persistence.Scope

		s.It("WriteSnapshot", func(ctx *specs.Context) {
			snapshot := scopeGuardSnapshot(ctx)
			store := NewSnapshotStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			err := store.WriteSnapshot(bg, invalid, snapshot)
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))

			got, getErr := store.GetLatestSnapshot(bg, persistence.Unscoped(), "scope-guard-snap")
			ctx.Expect(getErr).To(specs.BeNil())
			ctx.Expect(got).To(specs.BeNil())
		})

		s.It("GetLatestSnapshot", func(ctx *specs.Context) {
			snapshot := scopeGuardSnapshot(ctx)
			store := NewSnapshotStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.WriteSnapshot(bg, persistence.Unscoped(), snapshot)).To(specs.BeNil())

			got, err := store.GetLatestSnapshot(bg, invalid, "scope-guard-snap")
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))
			ctx.Expect(got).To(specs.BeNil())
		})

		s.It("DeleteSnapshots", func(ctx *specs.Context) {
			snapshot := scopeGuardSnapshot(ctx)
			store := NewSnapshotStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())
			ctx.Expect(store.WriteSnapshot(bg, persistence.Unscoped(), snapshot)).To(specs.BeNil())

			err := store.DeleteSnapshots(bg, invalid, "scope-guard-snap", 1)
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))

			got, getErr := store.GetLatestSnapshot(bg, persistence.Unscoped(), "scope-guard-snap")
			ctx.Expect(getErr).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
		})
	})
}

// ---------------------------------------------------------------------------
// Unscoped() regression guard: a single writer under Unscoped() behaves
// exactly as the pre-TENANT-003 stores did (the existing suites in
// stores_test.go, eventstore_test.go, durablestore_test.go and
// concurrency_test.go already exercise this for every method by always
// passing persistence.Unscoped(); this test is a focused, explicit
// same-persistenceID-different-scope check that a second, independent
// tenant scope does not collide with Unscoped()'s own records).
// ---------------------------------------------------------------------------

func TestEventStore_UnscopedDoesNotCollideWithTenantScope(t *testing.T) {
	specs.Describe(t, "EventStore keeps Unscoped and tenant scope records apart for the same persistence id", func(s *specs.Spec) {
		s.It("a tenant can genesis-write an id already written unscoped, and both remain readable", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			tenantA, err := persistence.NewTenantScope("tenant-a")
			ctx.Expect(err).To(specs.BeNil())

			anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
			ctx.Expect(err).To(specs.BeNil())

			unscopedEvents := []*egopb.Event{
				{PersistenceId: "shared-id", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
			}
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), unscopedEvents, persistence.Unconditional())).To(specs.BeNil())

			// tenantA writing the SAME persistenceID with ExpectGenesis() must
			// succeed, proving the precondition's persisted revision is scoped to
			// (scope, persistenceID), not persistenceID alone.
			tenantEvents := []*egopb.Event{
				{PersistenceId: "shared-id", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
			}
			ctx.Expect(store.WriteEvents(bg, tenantA, tenantEvents, persistence.ExpectGenesis())).To(specs.BeNil())

			unscopedLatest, err := store.GetLatestEvent(bg, persistence.Unscoped(), "shared-id")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(unscopedLatest).To(specs.Not(specs.BeNil()))

			tenantLatest, err := store.GetLatestEvent(bg, tenantA, "shared-id")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(tenantLatest).To(specs.Not(specs.BeNil()))
		})
	})
}
