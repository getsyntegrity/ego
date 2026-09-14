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

package tenancy

import "strings"

// Scope discriminates a TenantContext between ordinary tenant-scoped
// execution and administrative (non-tenant) execution. The zero value is
// intentionally invalid: a TenantContext can only be built through
// NewTenantContext or NewAdministrativeContext, never left unscoped.
type Scope uint8

const (
	_ Scope = iota
	// ScopeTenant marks a TenantContext bound to a specific TenantID.
	ScopeTenant
	// ScopeAdministrative marks a TenantContext for non-tenant execution
	// (e.g. crypto-shredding, cross-tenant migration tooling). It is a
	// distinct type-level scope, never a sentinel tenant or an empty
	// TenantID standing in for "no tenant".
	ScopeAdministrative
)

// String returns a lower-case, human-readable name for the scope.
func (s Scope) String() string {
	switch s {
	case ScopeTenant:
		return "tenant"
	case ScopeAdministrative:
		return "administrative"
	default:
		return "unknown"
	}
}

// Administrative carries required attribution for non-tenant execution:
// who is acting (actor) and why (reason). Both are required at
// construction; correlationID is optional. Administrative is only
// reachable from a TenantContext via Scope/Administrative — it can never
// be mistaken for a tenant identity.
type Administrative struct {
	actor            string
	reason           string
	correlationID    string
	hasCorrelationID bool
}

// NewAdministrative validates actor and reason and returns an
// Administrative attribution. Both must be non-empty and not
// whitespace-only; construction fails otherwise.
func NewAdministrative(actor, reason string) (Administrative, error) {
	if strings.TrimSpace(actor) == "" {
		return Administrative{}, newError(ReasonInvalid, "tenancy: administrative actor must not be empty", nil)
	}
	if strings.TrimSpace(reason) == "" {
		return Administrative{}, newError(ReasonInvalid, "tenancy: administrative reason must not be empty", nil)
	}
	return Administrative{actor: actor, reason: reason}, nil
}

// WithCorrelationID returns a copy of a with an optional correlation ID
// attached, for tying administrative action back to a request/trace.
// It does not mutate the receiver.
func (a Administrative) WithCorrelationID(id string) Administrative {
	a.correlationID = id
	a.hasCorrelationID = id != ""
	return a
}

// Actor returns who is acting administratively.
func (a Administrative) Actor() string {
	return a.actor
}

// Reason returns why the administrative action is being taken.
func (a Administrative) Reason() string {
	return a.reason
}

// CorrelationID returns the optional correlation ID, if one was attached
// via WithCorrelationID.
func (a Administrative) CorrelationID() (string, bool) {
	return a.correlationID, a.hasCorrelationID
}

// TenantContext is the canonical, transport-neutral tenant identity
// carried through an execution. It is either tenant-scoped (a TenantID)
// or administrative (an Administrative attribution) — never both, never
// neither. TenantContext cannot be constructed invalid: unexported
// fields force every instance through NewTenantContext or
// NewAdministrativeContext.
type TenantContext struct {
	scope  Scope
	tenant TenantID
	admin  Administrative
}

// NewTenantContext builds a tenant-scoped TenantContext for id.
//
// id is revalidated through NewTenantID's rules, not merely checked for
// emptiness: TenantID is a defined string type, so a caller can bypass
// NewTenantID with a bare conversion (tenancy.TenantID("acme corp ")).
// Revalidating here keeps this trust-boundary constructor the single
// place an invalid tenant identity is refused, regardless of how the
// caller obtained the TenantID value.
func NewTenantContext(id TenantID) (TenantContext, error) {
	validated, err := NewTenantID(string(id))
	if err != nil {
		return TenantContext{}, err
	}
	return TenantContext{scope: ScopeTenant, tenant: validated}, nil
}

// NewAdministrativeContext builds an administrative TenantContext from a.
// It fails if a is the zero-value Administrative (i.e. was never built
// through NewAdministrative), preserving the "always attributed" guarantee.
func NewAdministrativeContext(a Administrative) (TenantContext, error) {
	if a.actor == "" || a.reason == "" {
		return TenantContext{}, newError(ReasonInvalid, "tenancy: administrative context requires actor and reason", nil)
	}
	return TenantContext{scope: ScopeAdministrative, admin: a}, nil
}

// Scope reports whether c is tenant-scoped or administrative.
func (c TenantContext) Scope() Scope {
	return c.scope
}

// Tenant returns c's TenantID when c is tenant-scoped. The second return
// value is false for an administrative (or zero-value) TenantContext.
func (c TenantContext) Tenant() (TenantID, bool) {
	if c.scope != ScopeTenant {
		return "", false
	}
	return c.tenant, true
}

// Administrative returns c's attribution when c is administrative. The
// second return value is false for a tenant-scoped (or zero-value)
// TenantContext.
func (c TenantContext) Administrative() (Administrative, bool) {
	if c.scope != ScopeAdministrative {
		return Administrative{}, false
	}
	return c.admin, true
}

// valid reports whether c was actually constructed through NewTenantContext
// or NewAdministrativeContext, i.e. Scope() is ScopeTenant or
// ScopeAdministrative. The zero value TenantContext{} — the only state
// reachable from outside this package via a bare struct literal, since
// every field here is unexported — is the sole invalid case and is neither.
//
// context.go's Attach and Require both call valid() so that a malformed
// TenantContext (e.g. one a caller's own TenantResolver.Resolve returns as
// `tenancy.TenantContext{}, nil` — no error, but no real identity either)
// can never be treated as an attached tenant identity, on the way in
// (Attach) or on the way out (Require). See design.md Decision D8
// (EGO-TENANT-006 reconciliation).
func (c TenantContext) valid() bool {
	switch c.scope {
	case ScopeTenant, ScopeAdministrative:
		return true
	default:
		return false
	}
}
