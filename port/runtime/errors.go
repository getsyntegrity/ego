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

package runtime

import (
	"errors"
	"fmt"
	"strconv"
)

// The sentinels below keep the names and exact messages they had in package
// engine, where the same values are still reachable under the old names
// (engine.ErrEngineNotStarted is ErrEngineNotStarted). Some messages name engine
// options; they stay as they are because tests and log-based alerts compare
// them.
var (
	// ErrEngineNotStarted is returned when the runtime has not started, or
	// has stopped.
	ErrEngineNotStarted = errors.New("urd engine has not started")
	// ErrUndefinedEntityID is returned when sending a command to an undefined entity
	ErrUndefinedEntityID = errors.New("urd entity id is not defined")
	// ErrDurableStateStoreRequired is returned when a durable state entity is
	// spawned and the runtime has no durable state store.
	ErrDurableStateStoreRequired = errors.New("durable state store is required")
	// ErrEventsStoreRequired is returned when an event-sourced entity or a
	// saga is spawned and the runtime has no events store (valid for a
	// durable-state-only deployment). Nothing is spawned.
	ErrEventsStoreRequired = errors.New("events store is required")
	// ErrProjectionNotRegistered is returned by StartProjection when the given
	// name was never registered with the runtime (engine.WithProjection for the
	// GoAkt adapter).
	ErrProjectionNotRegistered = errors.New("projection is not registered; register it with engine.WithProjection")
	// ErrSpawnTenantUndetermined is returned by a spawn when tenancy is
	// active but the runtime cannot determine which tenant to bind the
	// spawned entity to: the caller did not pass WithTenant, and the
	// registered tenancy.TenantResolver does not expose a fixed tenant via
	// tenancy.FixedTenantResolver (TENANT-003 T4). The runtime never falls
	// back to persistence.Unscoped() in this case.
	ErrSpawnTenantUndetermined = errors.New("urd: tenant-aware spawn requires engine.WithTenant (the registered resolver exposes no fixed tenant); see tenancy.FixedTenantResolver")
	// ErrSpawnTenantMismatch is returned by a spawn in tenant-aware mode when
	// the entity that holds the requested id is bound to a different tenant
	// than the one this spawn declared (TENANT-003 T4). Re-spawning a live id
	// under the same tenant stays an idempotent success.
	ErrSpawnTenantMismatch = errors.New("urd: entity id is already bound to a different tenant")
	// ErrSpawnTenantUnverified is returned by a spawn in tenant-aware mode
	// when the tenant binding of the spawned entity could not be verified.
	// The spawn fails closed, but unlike ErrSpawnTenantMismatch it asserts no
	// cross-tenant conflict; retrying the spawn is safe.
	ErrSpawnTenantUnverified = errors.New("urd: the spawned actor's tenant binding could not be verified")
	// ErrNotACommand is returned by Dispatch and SendCommand when the payload
	// is a runtime-internal control message rather than a command.
	ErrNotACommand = errors.New("urd: payload is an engine-internal control message, not a command")
	// ErrEntityFamilyNotDeclared is returned by a spawn when the runtime
	// declares its entity families (engine.WithEntityFamilies for the GoAkt
	// adapter) and the spawned behavior's family is not among them. The error
	// names the family. Nothing is spawned.
	ErrEntityFamilyNotDeclared = errors.New("urd: entity family is not declared; declare it with engine.WithEntityFamilies")

	// ErrUnsupported reports an operation this runtime does not provide. It
	// wraps errors.ErrUnsupported, so both errors.Is checks hold.
	ErrUnsupported = fmt.Errorf("urd: operation not supported by this runtime: %w", errors.ErrUnsupported)
)

// UnsupportedError names the runtime and the operation it does not provide.
// It matches ErrUnsupported and errors.ErrUnsupported with errors.Is.
type UnsupportedError struct {
	// Runtime names the runtime, for example "inmem".
	Runtime string
	// Operation names the operation, for example "StartProjection".
	Operation string
}

// Error implements error.
func (e *UnsupportedError) Error() string {
	return "urd: runtime " + strconv.Quote(e.Runtime) + " does not support " + e.Operation
}

// Unwrap returns ErrUnsupported.
func (e *UnsupportedError) Unwrap() error {
	return ErrUnsupported
}
