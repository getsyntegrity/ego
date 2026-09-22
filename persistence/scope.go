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
	"errors"

	"github.com/pablogore/ego/v4/tenancy"
)

// ErrInvalidScope is returned when an empty/invalid tenancy.TenantID is
// passed to NewTenantScope, or when an otherwise-unconstructed Scope (the
// invalid zero value) is passed where a valid Scope is required.
var ErrInvalidScope = errors.New("persistence: scope is not valid")

// scopeKind enumerates the two valid Scope states. The zero value
// (scopeUnspecified) is deliberately not one of them: it exists only to
// make the zero value of Scope invalid, mirroring preconditionMode in
// precondition.go.
type scopeKind uint8

const (
	scopeUnspecified scopeKind = iota
	scopeUnscoped
	scopeTenant
)

// Scope declares the tenant boundary a persisted record belongs to. It is a
// comparable value type constructed only through its named constructors,
// Unscoped and NewTenantScope. The zero value is invalid and callers MUST
// NOT rely on it standing in for Unscoped(): Scope forces every caller to
// choose explicitly, exactly as WritePrecondition forces every conditional
// write to choose explicitly (see precondition.go).
//
// # Effective identity
//
// The effective identity of a persisted aggregate is the PAIR (Scope,
// persistence_id), never persistence_id alone. persistence_id stays
// opaque and caller-assigned (see events_store.go, state_store.go,
// snapshot_store.go, and openspec/changes/ego-store-001's
// persistence-store-contract spec, which documents that today's stores key
// records by a bare persistence_id string with no tenant awareness); this
// type introduces the missing half of that key. Nothing in this package
// prefixes, parses, concatenates, or otherwise reinterprets persistence_id
// to derive or embed a Scope — the two stay structurally separate values
// that a store keys on together.
//
// Two different Scope values presented with the same persistence_id
// identify DIFFERENT records. A read performed in one Scope MUST NOT
// return, and a write performed in one Scope MUST NOT modify, a record
// that belongs to another Scope.
//
// # Backward compatibility
//
// Unscoped() is the Scope used when tenancy is not activated on the
// engine. It MUST remain byte-compatible with pre-TENANT-003 behavior, so
// that an existing non-tenant deployment needs no data migration to adopt
// this type: a store that always receives Unscoped() behaves exactly as it
// did before Scope existed. Unscoped() MUST NOT be equal to, or collide
// with, any tenant Scope — including a tenant whose id happens to be the
// literal text "unscoped" (see String()'s forging-guard note).
//
// # tenant_metadata stays non-authoritative
//
// egopb.Event, egopb.Snapshot and egopb.DurableState already carry a
// tenant_metadata map (shipped by EGO-TENANT-002). That field remains
// descriptive only: it travels with the payload and back out on reads, but
// it is never consulted to decide whether a read or write is allowed.
// Isolation comes from Scope alone, passed as an explicit parameter
// alongside persistence_id — never inferred from payload content.
type Scope struct {
	kind   scopeKind
	tenant tenancy.TenantID
}

// Unscoped returns the Scope used when tenancy is not activated on the
// engine. See the Scope doc comment's Backward compatibility section.
func Unscoped() Scope {
	return Scope{kind: scopeUnscoped}
}

// NewTenantScope returns a Scope bound to id. It returns ErrInvalidScope
// when id is empty or otherwise fails tenancy.NewTenantID's validation
// rules (id is revalidated here rather than merely checked for emptiness,
// since tenancy.TenantID is a defined string type a caller can construct
// via a bare conversion, bypassing tenancy.NewTenantID).
func NewTenantScope(id tenancy.TenantID) (Scope, error) {
	validated, err := tenancy.NewTenantID(string(id))
	if err != nil {
		return Scope{}, errors.Join(ErrInvalidScope, err)
	}
	return Scope{kind: scopeTenant, tenant: validated}, nil
}

// Valid reports whether s was actually constructed through Unscoped or
// NewTenantScope. It is false only for the zero value of Scope.
func (s Scope) Valid() bool {
	switch s.kind {
	case scopeUnscoped, scopeTenant:
		return true
	default:
		return false
	}
}

// IsUnscoped reports whether s is the Unscoped() scope.
func (s Scope) IsUnscoped() bool {
	return s.kind == scopeUnscoped
}

// TenantID returns s's bound tenant identity. It is the zero TenantID
// ("") when s is Unscoped() or the invalid zero value of Scope; callers
// MUST check IsUnscoped()/Valid() rather than treat an empty TenantID as
// meaningful on its own.
func (s Scope) TenantID() tenancy.TenantID {
	if s.kind != scopeTenant {
		return ""
	}
	return s.tenant
}

// Equal reports whether s and other identify the same scope: both
// Unscoped(), or both tenant-scoped with the same tenancy.TenantID. Two
// scopes of different kinds are never equal, regardless of what their
// TenantID() or String() values happen to render as — see the forging-guard
// note on String().
func (s Scope) Equal(other Scope) bool {
	if s.kind != other.kind {
		return false
	}
	switch s.kind {
	case scopeUnscoped:
		return true
	case scopeTenant:
		return s.tenant == other.tenant
	default:
		// Two invalid zero values are not considered a meaningful match;
		// Valid() must be checked by the caller before comparing.
		return false
	}
}

// String renders s in a stable, human-readable form for errors and logs:
// "unscoped" for Unscoped(), "tenant:<id>" for a tenant scope, and
// "unspecified" for the invalid zero value.
//
// String() is a DIAGNOSTIC rendering ONLY. It MUST NOT be used by a store
// to build a storage key, a cache key, or any other value that stands in
// for the (Scope, persistence_id) pair: a tenant whose id contains the
// literal text "unscoped" (or the literal text of another tenant's
// rendering) renders as e.g. "tenant:unscoped", which is a different
// string from "unscoped" — but any code that reduces a Scope to its
// String() before comparing loses the structural guarantee Equal()
// provides. A store MUST key on the Scope value (or its constituent kind
// and TenantID()) structurally, never on this string.
func (s Scope) String() string {
	switch s.kind {
	case scopeUnscoped:
		return "unscoped"
	case scopeTenant:
		return "tenant:" + string(s.tenant)
	default:
		return "unspecified"
	}
}
