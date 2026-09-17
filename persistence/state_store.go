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
type StateStore interface {
	// Connect connects to the journal store
	Connect(ctx context.Context) error
	// Disconnect disconnect the journal store
	Disconnect(ctx context.Context) error
	// Ping verifies a connection to the database is still alive, establishing a connection if necessary.
	Ping(ctx context.Context) error
	// WriteState persist durable state for a given persistenceID, subject to precondition.
	//
	// The precondition is evaluated against the persisted VersionNumber for state's persistenceID
	// (see WritePrecondition) and the write is committed as one atomic operation: there is no
	// observable window in which another writer's commit can interleave between the precondition
	// check and the commit. Implementations MUST NOT implement this as a separate read-then-compare
	// followed by an unconditional write.
	//
	// An invalid precondition (the zero value of WritePrecondition) returns ErrInvalidPrecondition.
	// When the precondition does not hold against the persisted VersionNumber, the prior state is
	// left intact and a *ConflictError is returned, identifiable via errors.As or
	// errors.Is(err, ErrConcurrencyConflict).
	WriteState(ctx context.Context, state *egopb.DurableState, precondition WritePrecondition) error
	// GetLatestState fetches the latest durable state
	GetLatestState(ctx context.Context, persistenceID string) (*egopb.DurableState, error)
}
