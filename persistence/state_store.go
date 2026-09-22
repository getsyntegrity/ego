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

package persistence

import (
	"context"

	"github.com/pablogore/ego/v4/egopb"
)

// StateStore defines the API to interact with the durable state store
//
// # Breaking change: WriteState gained a required precondition parameter
//
// WRITE-004 changed WriteState's signature from
// WriteState(ctx, state *egopb.DurableState) error to
// WriteState(ctx, state *egopb.DurableState, precondition WritePrecondition) error
// (design.md M-1/M-4). Any external implementation of StateStore written
// against the pre-WRITE-004 signature fails to compile against this
// interface. Manual verification: temporarily assign a value of such an
// implementation to a var of type StateStore (e.g.
// var _ StateStore = (*oldStyleStore)(nil) where oldStyleStore's WriteState
// takes only (ctx, state)) and observe the compiler reject it with "missing
// method WriteState" / a wrong-signature error. M-3 documents the upgrade
// recipe.
//
// # Breaking change: every record-addressing method gained a required Scope parameter
//
// TENANT-003 (T2) changed both record-addressing methods' signatures to
// take a persistence.Scope as the parameter immediately after ctx:
//
//	WriteState(ctx, state *egopb.DurableState, precondition WritePrecondition) error ->
//	WriteState(ctx, scope Scope, state *egopb.DurableState, precondition WritePrecondition) error
//	GetLatestState(ctx, persistenceID string) (*egopb.DurableState, error) ->
//	GetLatestState(ctx, scope Scope, persistenceID string) (*egopb.DurableState, error)
//
// Any external implementation of StateStore fails to compile against this
// interface. Manual verification: temporarily assign a value of such an
// implementation to a var of type StateStore (e.g.
// var _ StateStore = (*oldStyleStore)(nil) where oldStyleStore's methods
// still take the pre-TENANT-003 argument lists) and observe the compiler
// reject it with "missing method WriteState" / a wrong-signature error for
// each changed method.
//
// Upgrade recipe: accept the new scope Scope parameter and include it
// structurally in the record key (e.g. a Go map or table key built from
// the pair (scope, persistenceID), never from Scope.String() or a
// persistenceID transformation — see persistence.Scope's doc comment). An
// adapter that wants pre-TENANT-003 behavior for existing rows maps
// Unscoped() to its current key layout unchanged, so no data migration is
// needed for non-tenant deployments: a store that always receives
// Unscoped() behaves exactly as it did before Scope existed.
type StateStore interface {
	// Connect connects to the journal store
	Connect(ctx context.Context) error
	// Disconnect disconnect the journal store
	Disconnect(ctx context.Context) error
	// Ping verifies a connection to the database is still alive, establishing a connection if
	// necessary. Deliberately unscoped: connection lifecycle is not record-addressing, so it
	// carries no tenant boundary.
	Ping(ctx context.Context) error
	// WriteState persist durable state for a given (scope, persistenceID), subject to
	// precondition.
	//
	// The precondition is evaluated against the persisted VersionNumber for (scope,
	// state.persistenceID) (see WritePrecondition) and the write is committed as one atomic
	// operation: there is no observable window in which another writer's commit can interleave
	// between the precondition check and the commit. Implementations MUST NOT implement this as a
	// separate read-then-compare followed by an unconditional write.
	//
	// scope and persistenceID together form the record's effective identity (see
	// persistence.Scope's doc comment). An invalid (zero-value) scope returns ErrInvalidScope and
	// nothing is read or written. A write performed in one scope MUST NOT modify a record that
	// belongs to another scope, even when both share the same persistenceID.
	//
	// An invalid precondition (the zero value of WritePrecondition) returns ErrInvalidPrecondition.
	// When the precondition does not hold against the persisted VersionNumber, the prior state is
	// left intact and a *ConflictError is returned, identifiable via errors.As or
	// errors.Is(err, ErrConcurrencyConflict).
	WriteState(ctx context.Context, scope Scope, state *egopb.DurableState, precondition WritePrecondition) error
	// GetLatestState fetches the latest durable state for the given (scope, persistenceID). scope
	// and persistenceID together form the record's effective identity: an invalid (zero-value)
	// scope returns ErrInvalidScope and nothing is read, and a read performed in one scope MUST
	// NOT return a record that belongs to another scope.
	GetLatestState(ctx context.Context, scope Scope, persistenceID string) (*egopb.DurableState, error)
}
