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
func WithSingleTenant(id TenantID) (TenantResolver, error) {
	tc, err := NewTenantContext(id)
	if err != nil {
		return nil, err
	}
	return singleTenantResolver{tc: tc}, nil
}
