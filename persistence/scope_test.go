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

package persistence_test

import (
	"testing"

	"github.com/getsyntegrity/urd/persistence"
	"github.com/getsyntegrity/urd/tenancy"
	"github.com/getsyntegrity/go-specs/specs"
)

// mustTenantScope builds a tenant scope for the tenant id, failing the case on error.
func mustTenantScope(ctx *specs.Context, id string) (tenancy.TenantID, persistence.Scope) {
	tenantID, err := tenancy.NewTenantID(id)
	ctx.Expect(err).To(specs.BeNil())

	scope, err := persistence.NewTenantScope(tenantID)
	ctx.Expect(err).To(specs.BeNil())

	return tenantID, scope
}

func TestScopeZeroValueIsInvalid(t *testing.T) {
	specs.Describe(t, "the zero Scope", func(s *specs.Spec) {
		s.It("is invalid", func(ctx *specs.Context) {
			var zero persistence.Scope

			ctx.Expect(zero.Valid()).To(specs.BeFalse())
		})
	})
}

func TestScopeUnscopedIsValid(t *testing.T) {
	specs.Describe(t, "Unscoped scope", func(s *specs.Spec) {
		s.It("is valid, unscoped and carries no tenant id", func(ctx *specs.Context) {
			sc := persistence.Unscoped()

			ctx.Expect(sc.Valid()).To(specs.BeTrue())
			ctx.Expect(sc.IsUnscoped()).To(specs.BeTrue())
			ctx.Expect(sc.TenantID()).ToEqual(tenancy.TenantID(""))
		})
	})
}

func TestScopeNewTenantScopeIsValid(t *testing.T) {
	specs.Describe(t, "NewTenantScope with a valid tenant id", func(s *specs.Spec) {
		s.It("is valid, not unscoped and keeps the tenant id", func(ctx *specs.Context) {
			id, sc := mustTenantScope(ctx, "acme")

			ctx.Expect(sc.Valid()).To(specs.BeTrue())
			ctx.Expect(sc.IsUnscoped()).To(specs.BeFalse())
			ctx.Expect(sc.TenantID()).ToEqual(id)
		})
	})
}

func TestNewTenantScopeRejectsEmptyTenantID(t *testing.T) {
	specs.Describe(t, "NewTenantScope with an empty tenant id", func(s *specs.Spec) {
		s.It("fails with ErrInvalidScope", func(ctx *specs.Context) {
			var empty tenancy.TenantID

			_, err := persistence.NewTenantScope(empty)

			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(persistence.ErrInvalidScope))
		})
	})
}

func TestScopeUnscopedNotEqualToTenantScope(t *testing.T) {
	specs.Describe(t, "Scope.Equal between unscoped and tenant scopes", func(s *specs.Spec) {
		s.It("is false in both directions", func(ctx *specs.Context) {
			_, tenantScope := mustTenantScope(ctx, "acme")

			unscoped := persistence.Unscoped()

			ctx.Expect(unscoped.Equal(tenantScope)).To(specs.BeFalse())
			ctx.Expect(tenantScope.Equal(unscoped)).To(specs.BeFalse())
		})
	})
}

func TestScopeTwoTenantScopesWithDifferentIDsAreNotEqual(t *testing.T) {
	specs.Describe(t, "Scope.Equal between tenant scopes", func(s *specs.Spec) {
		s.It("is false for different tenant ids", func(ctx *specs.Context) {
			_, acmeScope := mustTenantScope(ctx, "acme")
			_, otherScope := mustTenantScope(ctx, "other")

			ctx.Expect(acmeScope.Equal(otherScope)).To(specs.BeFalse())
		})
	})
}

func TestScopeTwoTenantScopesWithSameIDAreEqual(t *testing.T) {
	specs.Describe(t, "Scope.Equal between tenant scopes of one tenant", func(s *specs.Spec) {
		s.It("is true for the same tenant id", func(ctx *specs.Context) {
			_, first := mustTenantScope(ctx, "acme")
			_, second := mustTenantScope(ctx, "acme")

			ctx.Expect(first.Equal(second)).To(specs.BeTrue())
			ctx.Expect(persistence.Unscoped().Equal(persistence.Unscoped())).To(specs.BeTrue())
		})
	})
}

func TestScopeIsUnscoped(t *testing.T) {
	specs.Describe(t, "Scope.IsUnscoped", func(s *specs.Spec) {
		s.It("is true for Unscoped and false for a tenant scope", func(ctx *specs.Context) {
			_, tenantScope := mustTenantScope(ctx, "acme")

			ctx.Expect(persistence.Unscoped().IsUnscoped()).To(specs.BeTrue())
			ctx.Expect(tenantScope.IsUnscoped()).To(specs.BeFalse())
		})
	})
}

func TestScopeTenantIDRoundTrips(t *testing.T) {
	specs.Describe(t, "Scope.TenantID", func(s *specs.Spec) {
		s.It("returns the tenant id of a tenant scope and empty for Unscoped", func(ctx *specs.Context) {
			id, tenantScope := mustTenantScope(ctx, "acme")

			ctx.Expect(tenantScope.TenantID()).ToEqual(id)
			ctx.Expect(persistence.Unscoped().TenantID()).ToEqual(tenancy.TenantID(""))
		})
	})
}

func TestScopeStringDistinguishesKinds(t *testing.T) {
	specs.Describe(t, "Scope.String", func(s *specs.Spec) {
		s.It("renders unscoped and tenant scopes differently", func(ctx *specs.Context) {
			_, tenantScope := mustTenantScope(ctx, "acme")

			ctx.Expect(persistence.Unscoped().String()).ToEqual("unscoped")
			ctx.Expect(tenantScope.String()).ToEqual("tenant:acme")
		})
	})
}

// A tenant whose id is literally the string "unscoped" MUST NOT equal
// Unscoped(): String() is a diagnostic rendering only, never a key, and a
// tenant id that happens to collide with another scope's rendering must
// never be able to forge that scope.
func TestScopeTenantNamedUnscopedDoesNotEqualUnscoped(t *testing.T) {
	specs.Describe(t, "a tenant scope whose id is \"unscoped\"", func(s *specs.Spec) {
		s.It("does not equal Unscoped", func(ctx *specs.Context) {
			_, tenantScope := mustTenantScope(ctx, "unscoped")

			unscoped := persistence.Unscoped()

			ctx.Expect(unscoped.String()).To(specs.NotEqual(""))
			ctx.Expect(unscoped.Equal(tenantScope)).To(specs.BeFalse())
			ctx.Expect(tenantScope.Equal(unscoped)).To(specs.BeFalse())
			ctx.Expect(unscoped).To(specs.NotEqual(tenantScope))
		})
	})
}
