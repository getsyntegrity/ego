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

import "context"

// TenantResolver resolves the tenant identity for the current execution.
// A resolver MUST be invoked exactly once, at the trust boundary
// (design.md Decision R2: "entrypoint invokes, domain code reads").
// Configuring a resolver (e.g. via WithSingleTenant, or wiring one into an
// engine) never invokes it: the transport/runtime entrypoint at each trust
// boundary is responsible for calling Resolve and then Attach before
// domain code (Behavior/Saga) runs. Domain code only ever calls From or
// Require.
//
// Implementations MUST NOT depend on GoAkt or any actor-runtime type
// (spec.md: "TenantResolver Core Independence"); this package's import
// graph is enforced by an automated conformance check.
type TenantResolver interface {
	// Resolve returns the TenantContext for ctx. It is safe to call ctx
	// with no tenant-related values at all — resolution is the mechanism
	// that produces the first TenantContext of an execution, not a
	// lookup of one already attached.
	Resolve(ctx context.Context) (TenantContext, error)
}

// singleTenantResolver is the built-in TenantResolver returned by
// WithSingleTenant. It always resolves to the same TenantContext,
// regardless of ctx.
type singleTenantResolver struct {
	tc TenantContext
}

// Resolve implements TenantResolver. It ignores ctx: a single-tenant
// deployment has exactly one tenant identity, independent of the
// execution that asks for it.
func (r singleTenantResolver) Resolve(context.Context) (TenantContext, error) {
	return r.tc, nil
}

// WithSingleTenant returns a TenantResolver that always resolves to id.
//
// This is a built-in resolver/policy, not a second execution path
// (proposal.md S5, spec.md "Unified Single/Multi-Tenant Resolver
// Machinery"): the TenantContext it produces has the same shape and
// guarantees as one produced by any other TenantResolver, because it is
// built through the same NewTenantContext constructor. WithSingleTenant
// fails if id fails NewTenantContext's validation, so an invalid
// single-tenant configuration is rejected exactly as any other resolver's
// invalid output would be.
//
// The returned TenantResolver also implements FixedTenantResolver, which is
// what lets a single-tenant deployment spawn entities, durable-state
// entities, and sagas without ever passing engine.WithTenant (EGO-TENANT-003
// acceptance criterion 6: single-tenant mode needs no tenant plumbing
// invented by the application).
func WithSingleTenant(id TenantID) (TenantResolver, error) {
	tc, err := NewTenantContext(id)
	if err != nil {
		return nil, err
	}
	return singleTenantResolver{tc: tc}, nil
}

// FixedTenantResolver is an optional capability a TenantResolver may
// implement to expose a single, statically-known tenant identity without
// ever having Resolve called.
//
// It exists for the engine's spawn-time tenant declaration (TENANT-003 T4,
// corrected after CI caught a Resolve-Once, Propagate-After violation in an
// earlier design that called Resolve at spawn): the engine MUST NOT call
// Resolve outside the single command trust boundary, yet a single-tenant
// deployment still needs entity/durable-state/saga spawns to bind a tenant
// scope without the application repeating that one fixed tenant on every
// spawn via engine.WithTenant. A resolver that has exactly one fixed tenant —
// built-in WithSingleTenant, or a custom resolver that chooses to advertise
// one — implements FixedTenantResolver so the engine can read that identity
// directly, with no Resolve call and no execution-time side effect.
//
// An ordinary multi-tenant resolver has no such fixed identity: it either
// does not implement this interface at all, or implements it and returns
// (zero TenantID, false). Either way, the engine falls back to requiring an
// explicit engine.WithTenant at spawn.
type FixedTenantResolver interface {
	TenantResolver

	// FixedTenant returns the resolver's single, statically-known tenant
	// identity and true when the resolver always resolves to exactly one
	// tenant regardless of ctx. It returns the zero TenantID and false when
	// the resolver has no such fixed identity.
	FixedTenant() (TenantID, bool)
}

// ensures singleTenantResolver satisfies the optional FixedTenantResolver
// capability described above.
var _ FixedTenantResolver = singleTenantResolver{}

// CapFixedTenant is the adapter capability "implements FixedTenantResolver"
// (ego-arch-004 design §D3). A tenant resolver that declares an
// adapter.Descriptor lists it exactly when it implements
// FixedTenantResolver; compose.Spec.Validate (rule V8) and the adapter
// conformance suite check both directions.
//
// It says the resolver can be asked for a fixed tenant, not that it has
// one: FixedTenant may still report (zero TenantID, false) at run time, as
// a multi-tenant resolver that implements the interface does. Ask for the
// answer with FixedTenantOf.
//
// It is an untyped string constant on purpose: it converts to
// adapter.Capability where it is used, so this package never imports
// port/adapter.
const CapFixedTenant = "tenancy.fixed-tenant"

// AsFixedTenantResolver returns r as a FixedTenantResolver and true when r
// implements it, or nil and false otherwise, including for a nil r. It is
// the only type assertion on FixedTenantResolver: every other caller —
// the engine, composition-time validation — goes through it or through
// FixedTenantOf, so the rule for what counts as "implements" lives in one
// place. It never calls Resolve or FixedTenant.
func AsFixedTenantResolver(r TenantResolver) (FixedTenantResolver, bool) {
	fixed, ok := r.(FixedTenantResolver)
	return fixed, ok
}

// FixedTenantOf returns r's fixed tenant and true when r implements
// FixedTenantResolver and reports one. It returns the zero TenantID and
// false when r is nil, does not implement FixedTenantResolver, or
// implements it and reports no fixed tenant. It calls
// AsFixedTenantResolver and asserts nothing itself, and it never calls
// Resolve.
func FixedTenantOf(r TenantResolver) (TenantID, bool) {
	fixed, ok := AsFixedTenantResolver(r)
	if !ok {
		return "", false
	}
	id, has := fixed.FixedTenant()
	if !has {
		return "", false
	}
	return id, true
}

// FixedTenant implements FixedTenantResolver. A singleTenantResolver is
// always built from a valid, tenant-scoped TenantContext (WithSingleTenant
// fails construction otherwise), so this always reports true.
func (r singleTenantResolver) FixedTenant() (TenantID, bool) {
	return r.tc.Tenant()
}
