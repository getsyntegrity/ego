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
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// newAccountState builds a *egopb.DurableState for persistenceID at
// versionNumber, for use as the payload of a conditional WriteState call.
func newAccountState(t testing.TB, persistenceID string, versionNumber uint64) *egopb.DurableState {
	t.Helper()
	anyState, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
	if err != nil {
		t.Fatalf("build state payload for %q: %v", persistenceID, err)
	}
	return &egopb.DurableState{PersistenceId: persistenceID, ResultingState: anyState, VersionNumber: versionNumber}
}

// ---------------------------------------------------------------------------
// WriteState: conditional-write contract (2.2x)
// ---------------------------------------------------------------------------

func TestDurableStore_WriteState_InvalidPreconditionIsRejected(t *testing.T) {
	specs.Describe(t, "DurableStore.WriteState with an invalid precondition", func(s *specs.Spec) {
		s.It("is rejected and persists nothing", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			var zero persistence.WritePrecondition
			err := store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "invalid-precondition", 1), zero)

			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidPrecondition))

			got, getErr := store.GetLatestState(bg, persistence.Unscoped(), "invalid-precondition")
			ctx.Expect(getErr).To(specs.BeNil())
			// a rejected precondition must not persist anything
			ctx.Expect(got).To(specs.BeNil())
		})
	})
}

func TestDurableStore_WriteState_UnconditionalIsLegacyBehavior(t *testing.T) {
	specs.Describe(t, "DurableStore.WriteState with an unconditional precondition", func(s *specs.Spec) {
		s.It("keeps the legacy behavior of the last write winning", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "legacy", 1), persistence.Unconditional())).To(specs.BeNil())
			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "legacy", 2), persistence.Unconditional())).To(specs.BeNil())

			got, err := store.GetLatestState(bg, persistence.Unscoped(), "legacy")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(2))
		})
	})
}

func TestDurableStore_WriteState_ExactRevisionSucceedsWhenCurrent(t *testing.T) {
	specs.Describe(t, "DurableStore.WriteState with an exact revision precondition", func(s *specs.Spec) {
		s.It("succeeds when the revision is the current one", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "exact-ok", 1), persistence.ExpectGenesis())).To(specs.BeNil())
			err := store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "exact-ok", 2), persistence.ExpectRevision(1))
			ctx.Expect(err).To(specs.BeNil())

			got, err := store.GetLatestState(bg, persistence.Unscoped(), "exact-ok")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(2))
		})
	})
}

func TestDurableStore_WriteState_StaleRevisionIsConflict(t *testing.T) {
	specs.Describe(t, "DurableStore.WriteState with a stale revision precondition", func(s *specs.Spec) {
		s.It("reports a conflict and leaves the persisted state untouched", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "stale", 1), persistence.ExpectGenesis())).To(specs.BeNil())

			err := store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "stale", 2), persistence.ExpectRevision(99))

			var conflict *persistence.ConflictError
			ctx.Expect(err).To(specs.MatchErrorAs(&conflict))
			ctx.Expect(err).To(specs.MatchError(persistence.ErrConcurrencyConflict))
			ctx.Expect(conflict.PersistenceID()).ToEqual("stale")
			ctx.Expect(conflict.Expected()).ToEqual(persistence.ExpectRevision(99))

			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(actual).ToEqual(uint64(1))

			got, err := store.GetLatestState(bg, persistence.Unscoped(), "stale")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			// a rejected conditional write must not modify the persisted state
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(1))
		})
	})
}

func TestDurableStore_WriteState_GenesisSucceedsOnEmpty(t *testing.T) {
	specs.Describe(t, "DurableStore.WriteState with a genesis precondition", func(s *specs.Spec) {
		s.It("succeeds when the entity has no state yet", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			err := store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "genesis-ok", 1), persistence.ExpectGenesis())
			ctx.Expect(err).To(specs.BeNil())

			got, err := store.GetLatestState(bg, persistence.Unscoped(), "genesis-ok")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(1))
		})
	})
}

func TestDurableStore_WriteState_GenesisConflictsOnExisting(t *testing.T) {
	specs.Describe(t, "DurableStore.WriteState with a genesis precondition on an existing entity", func(s *specs.Spec) {
		s.It("reports a conflict and keeps the existing state", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "genesis-taken", 1), persistence.ExpectGenesis())).To(specs.BeNil())

			err := store.WriteState(bg, persistence.Unscoped(), newAccountState(ctx.T, "genesis-taken", 2), persistence.ExpectGenesis())

			var conflict *persistence.ConflictError
			ctx.Expect(err).To(specs.MatchErrorAs(&conflict))
			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(actual).ToEqual(uint64(1))

			got, err := store.GetLatestState(bg, persistence.Unscoped(), "genesis-taken")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(1))
		})
	})
}
