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
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/tenancy"
)

func TestNewTenantContext_RejectsEmptyTenantID(t *testing.T) {
	specs.Describe(t, "NewTenantContext rejects an empty tenant id", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.NewTenantContext("")
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantContext_RejectsZeroValueTenantID(t *testing.T) {
	specs.Describe(t, "NewTenantContext rejects a zero-value tenant id", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			var zero tenancy.TenantID
			_, err := tenancy.NewTenantContext(zero)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestNewTenantContext_RevalidatesTenantIDBypassingConstructor(t *testing.T) {
	specs.Describe(t, "NewTenantContext revalidates a tenant id built without NewTenantID", func(s *specs.Spec) {
		// TenantID is a defined string type, so a caller can bypass
		// NewTenantID with a bare conversion. NewTenantContext must not trust
		// an already-typed TenantID — it must re-run R1 validation.
		invalidUTF8 := string([]byte{0xff, 0xfe, 0xfd})
		tests := []struct {
			name string
			id   tenancy.TenantID
		}{
			{"leading whitespace", tenancy.TenantID(" acme")},
			{"trailing whitespace", tenancy.TenantID("acme ")},
			{"control rune", tenancy.TenantID("acme\x00corp")},
			{"too long", tenancy.TenantID(strings.Repeat("a", 129))},
			{"invalid UTF-8", tenancy.TenantID(invalidUTF8)},
		}

		for _, tt := range tests {
			s.It(tt.name, func(ctx *specs.Context) {
				_, err := tenancy.NewTenantContext(tt.id)
				ctx.Expect(err).To(specs.Not(specs.BeNil()))
				ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
			})
		}
	})
}

func TestNewTenantContext_ProducesTenantScopedContext(t *testing.T) {
	specs.Describe(t, "NewTenantContext produces a tenant-scoped context", func(s *specs.Spec) {
		s.It("reports the tenant scope and id and no administrative identity", func(ctx *specs.Context) {
			id, err := tenancy.NewTenantID("acme-corp")
			ctx.Expect(err).To(specs.BeNil())

			tc, err := tenancy.NewTenantContext(id)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(tc.Scope()).ToEqual(tenancy.ScopeTenant)

			gotID, ok := tc.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotID).ToEqual(id)

			_, ok = tc.Administrative()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestNewTenantContext_DifferentTenantsProduceDifferentContexts(t *testing.T) {
	specs.Describe(t, "NewTenantContext keeps different tenants apart", func(s *specs.Spec) {
		s.It("reports different tenant ids for different tenants", func(ctx *specs.Context) {
			acme, err := tenancy.NewTenantID("acme-corp")
			ctx.Expect(err).To(specs.BeNil())
			globex, err := tenancy.NewTenantID("globex-corp")
			ctx.Expect(err).To(specs.BeNil())

			tcAcme, err := tenancy.NewTenantContext(acme)
			ctx.Expect(err).To(specs.BeNil())
			tcGlobex, err := tenancy.NewTenantContext(globex)
			ctx.Expect(err).To(specs.BeNil())

			gotAcme, _ := tcAcme.Tenant()
			gotGlobex, _ := tcGlobex.Tenant()
			ctx.Expect(gotAcme).To(specs.NotEqual(gotGlobex))
		})
	})
}

func TestNewAdministrative_RequiresActorAndReason(t *testing.T) {
	specs.Describe(t, "NewAdministrative requires an actor and a reason", func(s *specs.Spec) {
		tests := []struct {
			name   string
			actor  string
			reason string
		}{
			{"empty actor", "", "incident response"},
			{"empty reason", "ops-oncall", ""},
			{"both empty", "", ""},
			{"whitespace only actor", "   ", "incident response"},
			{"whitespace only reason", "ops-oncall", "   "},
		}

		for _, tt := range tests {
			s.It(tt.name, func(ctx *specs.Context) {
				_, err := tenancy.NewAdministrative(tt.actor, tt.reason)
				ctx.Expect(err).To(specs.Not(specs.BeNil()))
				ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
			})
		}
	})
}

func TestNewAdministrative_AcceptsActorAndReason(t *testing.T) {
	specs.Describe(t, "NewAdministrative accepts a valid actor and reason", func(s *specs.Spec) {
		s.It("keeps both on the administrative identity", func(ctx *specs.Context) {
			admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(admin.Actor()).ToEqual("ops-oncall")
			ctx.Expect(admin.Reason()).ToEqual("crypto-shred deleted tenant data")
		})
	})
}

func TestNewAdministrativeContext_IsTypeDistinctAndAttributed(t *testing.T) {
	specs.Describe(t, "NewAdministrativeContext produces an attributed administrative context", func(s *specs.Spec) {
		s.It("reports the administrative scope, its actor and reason, and never a tenant", func(ctx *specs.Context) {
			admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
			ctx.Expect(err).To(specs.BeNil())

			tc, err := tenancy.NewAdministrativeContext(admin)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(tc.Scope()).ToEqual(tenancy.ScopeAdministrative)
			ctx.Expect(tc.Scope()).To(specs.NotEqual(tenancy.ScopeTenant))

			gotAdmin, ok := tc.Administrative()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotAdmin.Actor()).ToEqual("ops-oncall")
			ctx.Expect(gotAdmin.Reason()).ToEqual("crypto-shred deleted tenant data")

			// an administrative context must never report a tenant identity
			_, ok = tc.Tenant()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestNewAdministrativeContext_RejectsZeroValueAdministrative(t *testing.T) {
	specs.Describe(t, "NewAdministrativeContext rejects a zero-value administrative identity", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			var zero tenancy.Administrative
			_, err := tenancy.NewAdministrativeContext(zero)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestAdministrative_WithCorrelationIDIsOptional(t *testing.T) {
	specs.Describe(t, "Administrative.WithCorrelationID is optional", func(s *specs.Spec) {
		s.It("leaves the correlation id unset by default, sets it on a copy and never mutates the receiver", func(ctx *specs.Context) {
			admin, err := tenancy.NewAdministrative("ops-oncall", "manual replay")
			ctx.Expect(err).To(specs.BeNil())

			// correlation id is optional and unset by default
			_, ok := admin.CorrelationID()
			ctx.Expect(ok).To(specs.BeFalse())

			withCorrelation := admin.WithCorrelationID("corr-123")
			gotID, ok := withCorrelation.CorrelationID()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotID).ToEqual("corr-123")

			// WithCorrelationID must not mutate the receiver (value semantics).
			_, ok = admin.CorrelationID()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestTenantContext_ZeroValueIsNeitherScope(t *testing.T) {
	specs.Describe(t, "the zero TenantContext", func(s *specs.Spec) {
		s.It("is neither tenant-scoped nor administrative", func(ctx *specs.Context) {
			var zero tenancy.TenantContext
			ctx.Expect(zero.Scope()).To(specs.NotEqual(tenancy.ScopeTenant))
			ctx.Expect(zero.Scope()).To(specs.NotEqual(tenancy.ScopeAdministrative))

			_, ok := zero.Tenant()
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = zero.Administrative()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}
