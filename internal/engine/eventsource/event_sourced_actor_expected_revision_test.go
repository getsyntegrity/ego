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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/getsyntegrity/ego/persistence"
)

// -----------------------------------------------------------------------
// Unit tests for the pure helpers introduced for design.md D4/D9/D10
// (EGO-WRITE-004 PR3). These exercise the mapping/decision logic in
// isolation from the actor runtime; the end-to-end scenarios further down
// prove the same logic wired correctly into the real command.Result path.
// -----------------------------------------------------------------------

func TestShouldStayAliveAfterConflict(t *testing.T) {
	t.Run("non-conflict error never stays alive", func(t *testing.T) {
		entity := &Actor{eventsCounter: 3}
		assert.False(t, entity.shouldStayAliveAfterConflict(errors.New("boom")))
	})

	t.Run("actual revision matches in-memory counter: provably in sync, stays alive", func(t *testing.T) {
		entity := &Actor{eventsCounter: 3}
		conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(3))
		assert.True(t, entity.shouldStayAliveAfterConflict(conflictErr))
	})

	t.Run("actual revision diverges from in-memory counter: not provably in sync, shuts down", func(t *testing.T) {
		entity := &Actor{eventsCounter: 3}
		conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(7))
		assert.False(t, entity.shouldStayAliveAfterConflict(conflictErr))
	})

	t.Run("conflict without an actual revision cannot be proven in sync", func(t *testing.T) {
		entity := &Actor{eventsCounter: 3}
		conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5))
		assert.False(t, entity.shouldStayAliveAfterConflict(conflictErr))
	})
}

func TestResolveBatchPrecondition(t *testing.T) {
	t.Run("no admitted command declared a revision: unconditional", func(t *testing.T) {
		entity := &Actor{batchHasPrecondition: false, batchBase: 7}
		assert.Equal(t, persistence.Unconditional(), entity.resolveBatchPrecondition())
	})

	t.Run("declared, base is empty store: genesis", func(t *testing.T) {
		entity := &Actor{batchHasPrecondition: true, batchBase: 0}
		assert.Equal(t, persistence.ExpectGenesis(), entity.resolveBatchPrecondition())
	})

	t.Run("declared, non-empty base: exact revision", func(t *testing.T) {
		entity := &Actor{batchHasPrecondition: true, batchBase: 4}
		assert.Equal(t, persistence.ExpectRevision(4), entity.resolveBatchPrecondition())
	})
}
