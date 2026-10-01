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

package protocol

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
)

// TestAnswerTenantBinding pins the actors' shared query handler: it answers
// from the bound scope only, never discloses the bound tenant, and reports
// no binding in legacy mode or for an administrative-looking query.
func TestAnswerTenantBinding(t *testing.T) {
	specs.Describe(t, "AnswerTenantBinding answers from the bound scope without disclosing the tenant", func(s *specs.Spec) {
		s.It("reports a match for the bound tenant, no match for another, an invalid one, or legacy mode", func(ctx *specs.Context) {
			acme, err := persistence.NewTenantScope("acme")
			ctx.Expect(err).To(specs.BeNil())

			match := AnswerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: "acme"})
			ctx.Expect(match.GetTenantAware()).To(specs.BeTrue())
			ctx.Expect(match.GetMatches()).To(specs.BeTrue())

			other := AnswerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: "globex"})
			ctx.Expect(other.GetTenantAware()).To(specs.BeTrue())
			ctx.Expect(other.GetMatches()).To(specs.BeFalse())

			// an invalid queried tenant never matches
			invalid := AnswerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: ""})
			ctx.Expect(invalid.GetMatches()).To(specs.BeFalse())

			legacy := AnswerTenantBinding(false, persistence.Unscoped(), &egopb.TenantBindingQuery{TenantId: "acme"})
			ctx.Expect(legacy.GetTenantAware()).To(specs.BeFalse())
			ctx.Expect(legacy.GetMatches()).To(specs.BeFalse())
		})
	})
}
