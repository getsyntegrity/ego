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

// This file is a white-box (package tenancy) test: it exercises the
// unexported newError/newErrorWithTenant constructors directly, since
// *Error can only be produced by the package itself (design.md: exact
// signatures, unexported-field constructor-only pattern).
package tenancy

import (
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestError_ReasonAccessor(t *testing.T) {
	specs.Describe(t, "Error.Reason returns the reason the error was built with", func(s *specs.Spec) {
		s.It("returns ReasonMissing for a missing error", func(ctx *specs.Context) {
			err := newError(ReasonMissing, "tenancy: tenant context missing", nil)
			ctx.Expect(err.Reason()).ToEqual(ReasonMissing)
		})
	})
}

func TestError_ErrorMessage(t *testing.T) {
	specs.Describe(t, "Error.Error returns the message the error was built with", func(s *specs.Spec) {
		s.It("returns the message verbatim", func(ctx *specs.Context) {
			err := newError(ReasonInvalid, "tenancy: tenant id must not be empty", nil)
			ctx.Expect(err.Error()).ToEqual("tenancy: tenant id must not be empty")
		})
	})
}

func TestError_UnwrapReturnsCause(t *testing.T) {
	specs.Describe(t, "Error.Unwrap returns the underlying cause", func(s *specs.Spec) {
		s.It("exposes the cause through errors.Is and errors.Unwrap", func(ctx *specs.Context) {
			cause := errors.New("store: tenant not found")
			err := newError(ReasonInvalid, "tenancy: tenant identity invalid", cause)

			ctx.Expect(err).To(specs.MatchError(cause))
			ctx.Expect(errors.Unwrap(err)).ToEqual(cause)
		})
	})
}

func TestError_UnwrapReturnsNilWithoutCause(t *testing.T) {
	specs.Describe(t, "Error.Unwrap without a cause", func(s *specs.Spec) {
		s.It("returns nil", func(ctx *specs.Context) {
			err := newError(ReasonDenied, "tenancy: tenant identity denied", nil)
			ctx.Expect(errors.Unwrap(err)).To(specs.BeNil())
		})
	})
}

func TestError_TenantAccessorWithoutAttribution(t *testing.T) {
	specs.Describe(t, "Error.Tenant on an error without tenant attribution", func(s *specs.Spec) {
		s.It("reports no tenant and the zero id", func(ctx *specs.Context) {
			err := newError(ReasonInvalid, "tenancy: tenant id must not be empty", nil)

			id, ok := err.Tenant()
			ctx.Expect(ok).To(specs.BeFalse())
			ctx.Expect(id).ToEqual(TenantID(""))
		})
	})
}

func TestError_TenantAccessorWithAttribution(t *testing.T) {
	specs.Describe(t, "Error.Tenant on an error with tenant attribution", func(s *specs.Spec) {
		s.It("reports the attributed tenant", func(ctx *specs.Context) {
			err := newErrorWithTenant(ReasonDenied, TenantID("acme-corp"), "tenancy: tenant identity denied", nil)

			id, ok := err.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(id).ToEqual(TenantID("acme-corp"))
		})
	})
}

func TestError_IsMatchesSentinelByReason(t *testing.T) {
	specs.Describe(t, "errors.Is matches an Error to the sentinel of its reason", func(s *specs.Spec) {
		type sentinelCase struct {
			name     string
			reason   Reason
			sentinel error
		}

		specs.Table(s, []sentinelCase{
			{"missing", ReasonMissing, ErrMissing},
			{"invalid", ReasonInvalid, ErrInvalid},
			{"denied", ReasonDenied, ErrDenied},
		}, func(c sentinelCase) string { return c.name }, func(ctx *specs.Context, c sentinelCase) {
			err := newError(c.reason, "boom", nil)
			ctx.Expect(err).To(specs.MatchError(c.sentinel))
		})
	})
}

func TestError_IsDoesNotMatchOtherSentinels(t *testing.T) {
	specs.Describe(t, "errors.Is does not match an Error to the sentinels of other reasons", func(s *specs.Spec) {
		s.It("a missing error matches neither ErrInvalid nor ErrDenied", func(ctx *specs.Context) {
			err := newError(ReasonMissing, "tenancy: tenant context missing", nil)

			ctx.Expect(err).To(specs.Not(specs.MatchError(ErrInvalid)))
			ctx.Expect(err).To(specs.Not(specs.MatchError(ErrDenied)))
		})
	})
}

func TestSentinels_AreDistinctFromEachOther(t *testing.T) {
	specs.Describe(t, "the tenancy sentinels are distinct", func(s *specs.Spec) {
		s.It("no sentinel matches another", func(ctx *specs.Context) {
			ctx.Expect(ErrMissing).To(specs.Not(specs.MatchError(ErrInvalid)))
			ctx.Expect(ErrMissing).To(specs.Not(specs.MatchError(ErrDenied)))
			ctx.Expect(ErrInvalid).To(specs.Not(specs.MatchError(ErrDenied)))
		})
	})
}
