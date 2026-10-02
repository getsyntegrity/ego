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

package durablestate

import (
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/persistence"
)

// -----------------------------------------------------------------------
// Unit test for the Actor-specific D10 helper (EGO-WRITE-004
// PR4, task 4.7). Mirrors TestShouldStayAliveAfterConflict in
// event_sourced_actor_expected_revision_test.go: same decision logic
// (provably in sync iff err is a *persistence.ConflictError whose
// ActualRevision is known and equals the actor's current in-memory
// version), inverted in effect (recover in place instead of shutting down).
// -----------------------------------------------------------------------

func TestProvablyInSyncAfterConflict(t *testing.T) {
	specs.Describe(t, "provablyInSyncAfterConflict is true only when a conflict proves the actor's version matches the store", func(s *specs.Spec) {
		s.It("non-conflict error is never provably in sync", func(ctx *specs.Context) {
			entity := &Actor{currentVersion: 3}
			ctx.Expect(entity.provablyInSyncAfterConflict(errors.New("boom"))).To(specs.BeFalse())
		})

		s.It("actual revision matches in-memory version: provably in sync", func(ctx *specs.Context) {
			entity := &Actor{currentVersion: 3}
			conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(3))
			ctx.Expect(entity.provablyInSyncAfterConflict(conflictErr)).To(specs.BeTrue())
		})

		s.It("actual revision diverges from in-memory version: not provably in sync", func(ctx *specs.Context) {
			entity := &Actor{currentVersion: 3}
			conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(7))
			ctx.Expect(entity.provablyInSyncAfterConflict(conflictErr)).To(specs.BeFalse())
		})

		s.It("conflict without an actual revision cannot be proven in sync", func(ctx *specs.Context) {
			entity := &Actor{currentVersion: 3}
			conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5))
			ctx.Expect(entity.provablyInSyncAfterConflict(conflictErr)).To(specs.BeFalse())
		})
	})
}
