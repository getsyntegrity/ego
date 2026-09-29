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

// This file is a white-box (package tenancy) test: it uses the unexported
// tenantContextKey directly to bind a TenantContext into a context.Context
// WITHOUT going through Attach. That is otherwise unreachable from outside
// this package (Attach is the only exported way to bind a TenantContext,
// and — as of design.md Decision D8 — it now refuses an invalid one on the
// way in). This file proves Require's OWN independent rejection: defense
// in depth for the hypothetical case where a value is bound some other
// way, not a substitute for Attach's check.
package tenancy

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestRequire_RejectsInvalidTenantContextEvenIfSomehowBound(t *testing.T) {
	specs.Describe(t, "Require rejects an invalid TenantContext on its own", func(s *specs.Spec) {
		s.It("fails with ErrInvalid for a zero value bound without Attach", func(ctx *specs.Context) {
			// Bypasses Attach entirely: simulates a bound-but-invalid TenantContext
			// reaching Require despite Attach's own rejection, e.g. a future
			// refactor that binds via context.WithValue directly.
			var zero TenantContext
			bound := context.WithValue(context.Background(), tenantContextKey, zero)

			_, err := Require(bound)
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.MatchError(ErrInvalid))
		})
	})
}

func TestRequire_AcceptsValidTenantContextBoundDirectly(t *testing.T) {
	specs.Describe(t, "Require accepts a valid TenantContext bound without Attach", func(s *specs.Spec) {
		s.It("returns the bound context", func(ctx *specs.Context) {
			tc, err := NewTenantContext("acme-corp")
			ctx.Expect(err).To(specs.BeNil())
			bound := context.WithValue(context.Background(), tenantContextKey, tc)

			got, err := Require(bound)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).ToEqual(tc)
		})
	})
}
