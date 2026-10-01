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
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/tenancy"
)

func TestMarshalMetadata_TenantScope_UsesEgoTenantKeys(t *testing.T) {
	specs.Describe(t, "MarshalMetadata encodes a tenant-scoped context under the ego.tenant keys", func(s *specs.Spec) {
		s.It("writes the tenant scope and id and no administrative keys", func(ctx *specs.Context) {
			id := mustTenantID(ctx, "acme-corp")
			tc, err := tenancy.NewTenantContext(id)
			ctx.Expect(err).To(specs.BeNil())

			md := tenancy.MarshalMetadata(tc)

			ctx.Expect(md).To(specs.HavePair("ego.tenant.scope", "tenant"))
			ctx.Expect(md).To(specs.HavePair("ego.tenant.id", "acme-corp"))
			// a tenant-scoped context must not carry administrative keys
			ctx.Expect(md).To(specs.Not(specs.HaveKey("ego.tenant.admin_actor")))
		})
	})
}

func TestMarshalMetadata_AdministrativeScope_UsesEgoTenantKeys(t *testing.T) {
	specs.Describe(t, "MarshalMetadata encodes an administrative context under the ego.tenant keys", func(s *specs.Spec) {
		s.It("writes the administrative scope, actor, reason and correlation id and no tenant id", func(ctx *specs.Context) {
			admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
			ctx.Expect(err).To(specs.BeNil())
			admin = admin.WithCorrelationID("corr-123")
			tc, err := tenancy.NewAdministrativeContext(admin)
			ctx.Expect(err).To(specs.BeNil())

			md := tenancy.MarshalMetadata(tc)

			ctx.Expect(md).To(specs.HavePair("ego.tenant.scope", "administrative"))
			ctx.Expect(md).To(specs.HavePair("ego.tenant.admin_actor", "ops-oncall"))
			ctx.Expect(md).To(specs.HavePair("ego.tenant.admin_reason", "crypto-shred deleted tenant data"))
			ctx.Expect(md).To(specs.HavePair("ego.tenant.admin_correlation_id", "corr-123"))
			// an administrative context must not carry a tenant id key
			ctx.Expect(md).To(specs.Not(specs.HaveKey("ego.tenant.id")))
		})
	})
}

func TestMarshalMetadata_AdministrativeScope_OmitsCorrelationIDWhenAbsent(t *testing.T) {
	specs.Describe(t, "MarshalMetadata omits an absent correlation id", func(s *specs.Spec) {
		s.It("leaves the correlation id key out of an administrative context without one", func(ctx *specs.Context) {
			admin, err := tenancy.NewAdministrative("ops-oncall", "manual replay")
			ctx.Expect(err).To(specs.BeNil())
			tc, err := tenancy.NewAdministrativeContext(admin)
			ctx.Expect(err).To(specs.BeNil())

			md := tenancy.MarshalMetadata(tc)

			ctx.Expect(md).To(specs.Not(specs.HaveKey("ego.tenant.admin_correlation_id")))
		})
	})
}

func TestMetadata_RoundTrip_TenantScope(t *testing.T) {
	specs.Describe(t, "a tenant-scoped context survives a metadata round trip", func(s *specs.Spec) {
		s.It("unmarshals to the context that was marshaled", func(ctx *specs.Context) {
			id := mustTenantID(ctx, "globex-corp")
			want, err := tenancy.NewTenantContext(id)
			ctx.Expect(err).To(specs.BeNil())

			md := tenancy.MarshalMetadata(want)
			got, err := tenancy.UnmarshalMetadata(md)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(got).ToEqual(want)
			gotID, ok := got.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotID).ToEqual(id)
		})
	})
}

func TestMetadata_RoundTrip_AdministrativeScope(t *testing.T) {
	specs.Describe(t, "an administrative context survives a metadata round trip", func(s *specs.Spec) {
		s.It("unmarshals to the context that was marshaled", func(ctx *specs.Context) {
			admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
			ctx.Expect(err).To(specs.BeNil())
			admin = admin.WithCorrelationID("corr-456")
			want, err := tenancy.NewAdministrativeContext(admin)
			ctx.Expect(err).To(specs.BeNil())

			md := tenancy.MarshalMetadata(want)
			got, err := tenancy.UnmarshalMetadata(md)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(got).ToEqual(want)
			gotAdmin, ok := got.Administrative()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotAdmin.Actor()).ToEqual("ops-oncall")
			ctx.Expect(gotAdmin.Reason()).ToEqual("crypto-shred deleted tenant data")
			corrID, ok := gotAdmin.CorrelationID()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(corrID).ToEqual("corr-456")
		})
	})
}

func TestUnmarshalMetadata_RejectsMissingScope(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects metadata without a scope", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{})
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestUnmarshalMetadata_RejectsUnrecognizedScope(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects an unrecognized scope", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{"ego.tenant.scope": "bogus"})
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestUnmarshalMetadata_RejectsTenantScopeWithInvalidID(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects a tenant scope with an invalid id", func(s *specs.Spec) {
		s.It("fails with ErrInvalid for an empty id", func(ctx *specs.Context) {
			_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{"ego.tenant.scope": "tenant", "ego.tenant.id": ""})
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}

func TestUnmarshalMetadata_RejectsAdministrativeScopeMissingAttribution(t *testing.T) {
	specs.Describe(t, "UnmarshalMetadata rejects an administrative scope without attribution", func(s *specs.Spec) {
		s.It("fails with ErrInvalid", func(ctx *specs.Context) {
			_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{"ego.tenant.scope": "administrative"})
			ctx.Expect(err).To(specs.MatchError(tenancy.ErrInvalid))
		})
	})
}
