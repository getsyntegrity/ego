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

// contextKey is an unexported type for this package's context.Context key,
// so it can never collide with a key used by another package (the standard
// Go idiom for context values).
type contextKey struct{}

// tenantContextKey is the single key under which a TenantContext is stored
// in a context.Context by Attach.
var tenantContextKey = contextKey{}

// Attach binds tc into ctx and returns the resulting context.
//
// This is the executable form of the normative invariant (design.md):
// once an execution has a tenant identity, it MUST NOT change implicitly
// while crossing a boundary. Attach is idempotent when tc equals the
// TenantContext already bound to ctx (safe to call again with the same
// value, e.g. a re-entrant call at the same trust boundary), but returns
// ErrDenied — leaving ctx unchanged — when a DIFFERENT TenantContext is
// already bound. It never silently overwrites an existing binding.
//
// Attach also refuses to ever bind an invalid TenantContext in the first
// place: tc must have been produced by NewTenantContext or
// NewAdministrativeContext (design.md Decision D8, EGO-TENANT-006
// reconciliation). A zero-value TenantContext{} — the only state an
// external TenantResolver.Resolve implementation can return as
// `tenancy.TenantContext{}, nil` without an error, since every field is
// unexported — is rejected here with ErrInvalid and ctx is left unchanged,
// so it can never reach a trust boundary as if it were a real identity.
//
// Attach only covers the same-node, context.Context-preserving path
// (proposal.md S3). A boundary that resets context.Context (e.g. a saga
// step) MUST reconstruct a TenantContext from carried Metadata and Attach
// it into the fresh context instead — see MarshalMetadata/UnmarshalMetadata.
func Attach(ctx context.Context, tc TenantContext) (context.Context, error) {
	if !tc.valid() {
		return ctx, newError(ReasonInvalid, "tenancy: cannot attach an invalid (zero-value) tenant context", nil)
	}
	if bound, ok := From(ctx); ok {
		if bound == tc {
			return ctx, nil
		}
		return ctx, newError(ReasonDenied, "tenancy: cannot change the tenant identity already attached to this context", nil)
	}
	return context.WithValue(ctx, tenantContextKey, tc), nil
}

// From returns the TenantContext bound to ctx by Attach, if any. The
// second return value is false when no TenantContext is attached. From
// never errors: it is the non-failing read used where "no tenant
// attached" is itself a meaningful, handled case.
func From(ctx context.Context) (TenantContext, bool) {
	tc, ok := ctx.Value(tenantContextKey).(TenantContext)
	return tc, ok
}

// Require returns the TenantContext bound to ctx, or ErrMissing when none
// is attached. Behavior/Saga implementations call Require (design.md
// Decision R2: "domain code reads") — they never call a TenantResolver
// themselves, and an absent TenantContext fails closed rather than running
// under an implicit default tenant.
//
// Require also re-validates whatever it finds: a bound-but-invalid
// TenantContext returns ErrInvalid rather than being handed back as if it
// were a real identity (design.md Decision D8, EGO-TENANT-006
// reconciliation). This is defense in depth alongside Attach's own
// rejection, not a substitute for it — Attach already refuses to bind an
// invalid TenantContext in the first place; this guards the case where one
// somehow got bound anyway (e.g. a future caller of context.WithValue that
// bypasses Attach entirely).
func Require(ctx context.Context) (TenantContext, error) {
	tc, ok := From(ctx)
	if !ok {
		return TenantContext{}, newError(ReasonMissing, "tenancy: no tenant identity attached to context", nil)
	}
	if !tc.valid() {
		return TenantContext{}, newError(ReasonInvalid, "tenancy: tenant identity attached to context is invalid", nil)
	}
	return tc, nil
}

// VerifyUnchanged compares bound and incoming explicitly and returns
// ErrDenied when they differ. Unlike Attach, it operates directly on two
// TenantContext values rather than a context.Context: it is the primitive
// a boundary that reconstructs identity out-of-band (e.g. after a saga's
// context.Background() reset, or when validating a command against an
// aggregate's already-established tenant per spec.md "Tenant Plus
// Aggregate Effective Identity") uses to prove the reconstructed identity
// still matches the one that was bound before the boundary.
func VerifyUnchanged(bound, incoming TenantContext) error {
	if bound != incoming {
		return newError(ReasonDenied, "tenancy: tenant identity changed across a boundary", nil)
	}
	return nil
}
