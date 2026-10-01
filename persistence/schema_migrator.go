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

import "context"

// SchemaMigrator is an optional interface a store implements when it owns the
// schema of its backend and can bring it up to date itself. It is a separate
// interface on purpose: adding Migrate to EventsStore, StateStore or
// SnapshotStore would break every store that implements them, including the
// in-memory stores of testkit and any store written by a user.
//
// The word "schema" is deliberate. The migration package already exists and
// means data migration (journal and snapshot rewrites, tenant adoption);
// SchemaMigrator only changes the shape of the backend, never the meaning of
// the data in it.
//
// An engine configured with engine.WithSchemaMigration calls Migrate on every
// configured store that implements this interface, during Engine.Start and
// before the engine accepts a command. Without that option nothing migrates
// and the schema stays the operator's responsibility.
//
// Both methods need a store that is already connected. conformance.
// RunSchemaMigratorConformance is the suite an implementation passes.
type SchemaMigrator interface {
	// Migrate brings the backend schema to the latest version this store knows.
	// It is idempotent: calling it on an up-to-date schema changes nothing. It is
	// also safe under concurrent callers, for example several cluster nodes
	// starting at once, in the same process or in different ones: every caller
	// returns nil and the schema ends up migrated exactly once.
	//
	// A backend created before schemas were versioned is recognised by its shape
	// and recorded at the matching version instead of being created again.
	Migrate(ctx context.Context) error

	// SchemaVersion returns the version of the schema the backend currently has.
	// A backend that was never migrated reports 0. It does not change the
	// backend.
	SchemaVersion(ctx context.Context) (uint, error)
}
