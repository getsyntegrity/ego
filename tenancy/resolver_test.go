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

package tenancy_test

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/tenancy"
)

// fixedResolver is a stand-in for a "real" multi-tenant TenantResolver: it
// resolves to whatever TenantContext it was built with. It exists only to
// prove WithSingleTenant's output is indistinguishable in kind from any
// other TenantResolver's (spec.md: "Unified Single/Multi-Tenant Resolver
// Machinery").
type fixedResolver struct {
	tc tenancy.TenantContext
}

func (r fixedResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	return r.tc, nil
}

func mustTenantID(t testing.TB, s string) tenancy.TenantID {
	t.Helper()
	id, err := tenancy.NewTenantID(s)
	if err != nil {
		t.Fatalf("NewTenantID(%q): %v", s, err)
	}
	return id
}

func TestWithSingleTenant_RejectsInvalidTenantID(t *testing.T) {
	specs.Describe(t, "WithSingleTenant rejects an invalid tenant id", func(s *specs.Spec) {
		s.It("fails with ErrInvalid for an empty id", func(ctx *specs.Context) {
			_, err := tenancy.WithSingleTenant(tenancy.TenantID(""))
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestWithSingleTenant_ProducesTenantScopedContext(t *testing.T) {
	specs.Describe(t, "WithSingleTenant resolves to a tenant-scoped context", func(s *specs.Spec) {
		s.It("resolves to the configured tenant", func(ctx *specs.Context) {
			id := mustTenantID(ctx.T, "acme-corp")

			resolver, err := tenancy.WithSingleTenant(id)
			ctx.Expect(err).To(specs.BeNil())

			tc, err := resolver.Resolve(context.Background())
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(tc.Scope()).ToEqual(tenancy.ScopeTenant)
			gotID, ok := tc.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotID).ToEqual(id)
		})
	})
}

func TestWithSingleTenant_IgnoresIncomingContext(t *testing.T) {
	specs.Describe(t, "a single-tenant resolver ignores the incoming context", func(s *specs.Spec) {
		s.It("resolves the same identity regardless of the incoming context", func(ctx *specs.Context) {
			id := mustTenantID(ctx.T, "acme-corp")
			resolver, err := tenancy.WithSingleTenant(id)
			ctx.Expect(err).To(specs.BeNil())

			type otherKey struct{}
			ctxA := context.Background()
			ctxB := context.WithValue(context.Background(), otherKey{}, "unrelated")

			tcA, err := resolver.Resolve(ctxA)
			ctx.Expect(err).To(specs.BeNil())
			tcB, err := resolver.Resolve(ctxB)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(tcA).ToEqual(tcB)
		})
	})
}

// TestWithSingleTenant_IndistinguishableFromAnyResolver is the acceptance
// scenario named in proposal.md/spec.md: a TenantContext produced by
// WithSingleTenant must have the same shape and guarantees as one produced
// by any other TenantResolver implementation — not merely "look similar",
// but compare equal and expose identical Scope()/Tenant() behavior.
func TestWithSingleTenant_IndistinguishableFromAnyResolver(t *testing.T) {
	specs.Describe(t, "WithSingleTenant is indistinguishable from any other resolver", func(s *specs.Spec) {
		s.It("yields a context equal in kind, scope and tenant to a custom resolver's", func(ctx *specs.Context) {
			id := mustTenantID(ctx.T, "globex-corp")

			singleTenant, err := tenancy.WithSingleTenant(id)
			ctx.Expect(err).To(specs.BeNil())

			wantTC, err := tenancy.NewTenantContext(id)
			ctx.Expect(err).To(specs.BeNil())
			custom := fixedResolver{tc: wantTC}

			var resolvers = []tenancy.TenantResolver{singleTenant, custom}

			var results []tenancy.TenantContext
			for _, r := range resolvers {
				tc, err := r.Resolve(context.Background())
				ctx.Expect(err).To(specs.BeNil())
				results = append(results, tc)
			}

			// WithSingleTenant's TenantContext must be indistinguishable in kind from any other resolver's
			ctx.Expect(results[0]).ToEqual(results[1])
			ctx.Expect(results[0].Scope()).ToEqual(results[1].Scope())

			gotID0, ok0 := results[0].Tenant()
			gotID1, ok1 := results[1].Tenant()
			ctx.Expect(ok0).To(specs.BeTrue())
			ctx.Expect(ok1).To(specs.BeTrue())
			ctx.Expect(gotID0).ToEqual(gotID1)
		})
	})
}

// advertisingResolver implements FixedTenantResolver and reports whatever
// it was built with. With has=false it is the multi-tenant resolver the
// FixedTenantResolver contract allows: it can be asked for a fixed tenant
// but has none.
type advertisingResolver struct {
	fixedResolver
	id  tenancy.TenantID
	has bool
}

func (r advertisingResolver) FixedTenant() (tenancy.TenantID, bool) { return r.id, r.has }

var _ tenancy.FixedTenantResolver = advertisingResolver{}

// resolveCountingResolver implements FixedTenantResolver and counts Resolve
// calls, which asking for a fixed tenant must never make.
type resolveCountingResolver struct{ calls int }

func (r *resolveCountingResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	r.calls++
	return tenancy.TenantContext{}, nil
}

func (r *resolveCountingResolver) FixedTenant() (tenancy.TenantID, bool) { return "", false }

func TestCapFixedTenant_IsAnUntypedConstant(t *testing.T) {
	specs.Describe(t, "CapFixedTenant is an untyped constant", func(s *specs.Spec) {
		s.It("converts to a plain string and to a named string type", func(ctx *specs.Context) {
			// Untyped: it converts to adapter.Capability without tenancy importing
			// port/adapter (ego-arch-004 design §D3). Assigning it to a plain
			// string and to a named string type both compile only if it is untyped.
			type capability string
			asString := tenancy.CapFixedTenant
			var asNamed capability = tenancy.CapFixedTenant
			ctx.Expect(asString).ToEqual("tenancy.fixed-tenant")
			ctx.Expect(asNamed).ToEqual(capability("tenancy.fixed-tenant"))
		})
	})
}

func TestAsFixedTenantResolver(t *testing.T) {
	specs.Describe(t, "AsFixedTenantResolver reports whether a resolver can be asked for a fixed tenant", func(s *specs.Spec) {
		single, err := tenancy.WithSingleTenant(mustTenantID(t, "acme"))
		if err != nil {
			t.Fatalf("WithSingleTenant(%q): %v", "acme", err)
		}

		tests := []struct {
			name     string
			resolver tenancy.TenantResolver
			want     bool
		}{
			{"nil resolver", nil, false},
			{"plain resolver", fixedResolver{}, false},
			{"single-tenant resolver", single, true},
			{"resolver with a fixed tenant", advertisingResolver{id: "acme", has: true}, true},
			// The capability is the interface ("can be asked"), not the
			// answer: a multi-tenant resolver that implements it and reports
			// no fixed tenant still has it.
			{"multi-tenant resolver implementing the interface", advertisingResolver{}, true},
		}
		for _, tt := range tests {
			s.It(tt.name, func(ctx *specs.Context) {
				fixed, ok := tenancy.AsFixedTenantResolver(tt.resolver)
				ctx.Expect(ok).ToEqual(tt.want)
				if !tt.want {
					ctx.Expect(fixed).To(specs.BeNil())
					return
				}
				ctx.Expect(fixed).To(specs.Not(specs.BeNil()))
				var asResolver tenancy.TenantResolver = fixed
				// the accessor returns the resolver itself
				ctx.Expect(asResolver).ToEqual(tt.resolver)
			})
		}
	})
}

func TestFixedTenantOf(t *testing.T) {
	specs.Describe(t, "FixedTenantOf reports the fixed tenant of a resolver", func(s *specs.Spec) {
		single, err := tenancy.WithSingleTenant(mustTenantID(t, "acme"))
		if err != nil {
			t.Fatalf("WithSingleTenant(%q): %v", "acme", err)
		}

		tests := []struct {
			name     string
			resolver tenancy.TenantResolver
			wantID   tenancy.TenantID
			wantOK   bool
		}{
			{"nil resolver", nil, "", false},
			{"plain resolver", fixedResolver{}, "", false},
			{"single-tenant resolver", single, "acme", true},
			{"resolver with a fixed tenant", advertisingResolver{id: "globex", has: true}, "globex", true},
			// ego-arch-004 spec 3 scenario "a multi-tenant resolver that
			// implements the interface": it reports no fixed tenant.
			{"multi-tenant resolver implementing the interface", advertisingResolver{}, "", false},
			// An ID reported next to false is not a fixed tenant.
			{"resolver reporting an ID with false", advertisingResolver{id: "stale", has: false}, "", false},
		}
		for _, tt := range tests {
			s.It(tt.name, func(ctx *specs.Context) {
				id, ok := tenancy.FixedTenantOf(tt.resolver)
				ctx.Expect(ok).ToEqual(tt.wantOK)
				ctx.Expect(id).ToEqual(tt.wantID)
			})
		}
	})
}

// Asking for a fixed tenant is not an execution-time resolution: neither
// accessor calls Resolve (TENANT-003 T4).
func TestFixedTenantAccessors_NeverResolve(t *testing.T) {
	specs.Describe(t, "the fixed-tenant accessors never resolve", func(s *specs.Spec) {
		s.It("calls Resolve zero times through FixedTenantOf and AsFixedTenantResolver", func(ctx *specs.Context) {
			r := &resolveCountingResolver{}
			_, _ = tenancy.FixedTenantOf(r)
			_, _ = tenancy.AsFixedTenantResolver(r)
			ctx.Expect(r.calls).ToEqual(0)
		})
	})
}
